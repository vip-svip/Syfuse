package syfuse

// 本文件是包 syfuse 测试用的隧道替身与最小 SOCKS5 客户端。
//
// 为什么不用 internal/dataplane 里那套：跨包复用测试替身要把它们导出，
// 而导出的唯一下游是测试 —— 那是用公共 API 污染换测试便利。
// 两边各自维护的代价是多一份代码，收益是"客户端与服务端互为交叉验证"
// （共用一份代码时，客户端写错、服务端照错照样全绿）。
//
// 与 dataplane 测试替身的差异只有一处：这里多一个 hostPort() 访问器，
// 因为节点 URI 需要 host:port 字面量，而 echoUpstream.addr() 返回的
// 地址带方括号（IPv6 场景），不能直接拼进 URI。

import (
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// errProtocol / errAuth 是替身在协议异常时返回的错误，仅用于区分失败原因。
var (
	errProtocol = errors.New("tunnel 替身: 协议帧不符合预期")
	errAuth     = errors.New("tunnel 替身: 认证被拒")
)

// tunnelEchoUpstream 起一个本地 TCP 服务：写一句 banner，然后原样回显。
type tunnelEchoUpstream struct {
	ln     net.Listener
	banner string

	mu    sync.Mutex
	conns int
}

func newTunnelEchoUpstream(t *testing.T, banner string) *tunnelEchoUpstream {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	e := &tunnelEchoUpstream{ln: ln, banner: banner}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			e.mu.Lock()
			e.conns++
			e.mu.Unlock()
			go func() {
				defer c.Close()
				if e.banner != "" {
					// 先写再读：与 socks5 替身踩过的坑同源 ——
					// 先读再写在 Windows 上容易撞上 RST 时序，
					// 写在前的字节会被对端Close时的RST一起丢掉。
					_, _ = c.Write([]byte(e.banner))
				}
				buf := make([]byte, 32*1024)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return e
}

func (e *tunnelEchoUpstream) addr() string { return e.ln.Addr().String() }

func (e *tunnelEchoUpstream) connections() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.conns
}

// tunnelChainSocks5 是一个假的 SOCKS5 **上游**：把连接原样转发到固定目标。
//
// 隧道里 Proxy 出站是 outbound.Socks5Dialer，它会对"节点"说 SOCKS5。
// 所以这个替身必须真的会讲 SOCKS5（协商 + CONNECT 应答），
// 之后再当纯 TCP 中继。targets 记录每次 CONNECT 的目标，用于断言
// "流量确实经过了这一跳"。
type tunnelChainSocks5 struct {
	ln     net.Listener
	target string
	user   string
	pass   string

	mu         sync.Mutex
	gotTargets []string
}

func newTunnelChainSocks5(t *testing.T, target string) *tunnelChainSocks5 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := &tunnelChainSocks5{ln: ln, target: target, user: "chain", pass: "pw"}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = c.serve(conn) }()
		}
	}()
	return c
}

func (c *tunnelChainSocks5) serve(conn net.Conn) error {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 方法协商
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 0x05 {
		return errProtocol
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	needAuth := false
	for _, m := range methods {
		if m == 0x02 {
			needAuth = true
		}
	}
	if needAuth {
		if _, err := conn.Write([]byte{0x05, 0x02}); err != nil {
			return err
		}
		ver := make([]byte, 2)
		if _, err := io.ReadFull(conn, ver); err != nil {
			return err
		}
		ulen := int(ver[1])
		user := make([]byte, ulen)
		if _, err := io.ReadFull(conn, user); err != nil {
			return err
		}
		plen := make([]byte, 1)
		if _, err := io.ReadFull(conn, plen); err != nil {
			return err
		}
		pass := make([]byte, plen[0])
		if _, err := io.ReadFull(conn, pass); err != nil {
			return err
		}
		if string(user) != c.user || string(pass) != c.pass {
			_, _ = conn.Write([]byte{0x01, 0x01})
			return errAuth
		}
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			return err
		}
	} else {
		if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
			return err
		}
	}

	// CONNECT 请求
	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil {
		return err
	}
	var host string
	switch req[3] {
	case 0x01:
		b := make([]byte, 4)
		if _, err := io.ReadFull(conn, b); err != nil {
			return err
		}
		host = net.IP(b).String()
	case 0x03:
		nb := make([]byte, 1)
		if _, err := io.ReadFull(conn, nb); err != nil {
			return err
		}
		h := make([]byte, nb[0])
		if _, err := io.ReadFull(conn, h); err != nil {
			return err
		}
		host = string(h)
	case 0x04:
		b := make([]byte, 16)
		if _, err := io.ReadFull(conn, b); err != nil {
			return err
		}
		host = net.IP(b).String()
	default:
		return errProtocol
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(conn, pb); err != nil {
		return err
	}
	port := int(pb[0])<<8 | int(pb[1])
	dst := net.JoinHostPort(host, strconv.Itoa(port))

	c.mu.Lock()
	c.gotTargets = append(c.gotTargets, dst)
	c.mu.Unlock()

	up, err := net.Dial("tcp", c.target)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return err
	}
	defer up.Close()
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	// 应答后撤掉截止时间：长连接不能被掐断。
	_ = conn.SetDeadline(time.Time{})

	// 双向中继。关键：任一方向读到头（EOF）就 CloseWrite 另一侧，
	// 否则两个 io.Copy 会互相等对方先结束 —— 一半关闭永远传不过去。
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(up, conn)
		if cw, ok := up.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, up)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
	return nil
}

func (c *tunnelChainSocks5) addr() string { return c.ln.Addr().String() }

// hostPort 返回 host:port 字面量，可直接拼进节点 URI。
func (c *tunnelChainSocks5) hostPort() string { return c.ln.Addr().String() }

func (c *tunnelChainSocks5) password() string { return c.pass }

func (c *tunnelChainSocks5) targets() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.gotTargets))
	copy(out, c.gotTargets)
	return out
}

// ---------------------------------------------------------------------------
// 最小 SOCKS5 客户端
// ---------------------------------------------------------------------------

// socks5Dial 通过 proxyAddr 向 target 发起 SOCKS5 CONNECT，返回连接与应答码。
//
// 独立于 internal/socks5 的服务端实现重写一遍：两边互为交叉验证。
func socks5Dial(t *testing.T, proxyAddr, target string) (net.Conn, byte) {
	t.Helper()
	c, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	// 问候：VER=5, NMETHODS=1, no-auth
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("greeting: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil {
		t.Fatalf("read method: %v", err)
	}
	if resp[1] != 0x00 {
		t.Fatalf("方法协商被拒: 0x%02x", resp[1])
	}

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatalf("split %q: %v", target, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi %q: %v", portStr, err)
	}

	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, 0x01)
			req = append(req, v4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		t.Fatalf("request: %v", err)
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatalf("read reply head: %v", err)
	}
	// bound 地址必须读走，否则残留字节会被当成业务数据
	switch head[3] {
	case 0x01:
		_, _ = io.ReadFull(c, make([]byte, 6))
	case 0x04:
		_, _ = io.ReadFull(c, make([]byte, 18))
	case 0x03:
		nb := make([]byte, 1)
		_, _ = io.ReadFull(c, nb)
		_, _ = io.ReadFull(c, make([]byte, int(nb[0])+2))
	}
	return c, head[1]
}

// readTunnelN 读 n 字节，超时报错而不是挂死。
func readTunnelN(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read %d bytes: %v", n, err)
	}
	return buf
}
