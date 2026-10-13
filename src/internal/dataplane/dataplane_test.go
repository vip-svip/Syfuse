package dataplane

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/vip-svip/syfuse/src/internal/outbound"
)

// ---------------------------------------------------------------------------
// 测试替身
// ---------------------------------------------------------------------------

// echoUpstream 起一个本地 TCP 服务：写一句 banner，然后把收到的字节原样回显。
//
// 为什么用真 socket 而不是假连接：本包的全部意义就是"字节真的流过去了"，
// 用假连接等于把要验证的东西提前假定成对的。真 socket 的唯一代价是要等
// accept，用 waitConn 处理。
type echoUpstream struct {
	ln     net.Listener
	banner string

	mu       sync.Mutex
	conns    int
	gotEOF   bool // 至少一个连接读到了 EOF（用于半关闭传播断言）
	lastPeer string
}

func newEchoUpstream(t *testing.T, banner string) *echoUpstream {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	e := &echoUpstream{ln: ln, banner: banner}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			e.mu.Lock()
			e.conns++
			e.lastPeer = c.RemoteAddr().String()
			e.mu.Unlock()
			go func() {
				defer c.Close()
				if e.banner != "" {
					// 先写再读：与 socks5 测试替身踩过的坑同源 ——
					// 先读再写在 Windows 上容易撞上 RST 时序。
					_, _ = c.Write([]byte(e.banner))
				}
				buf := make([]byte, 32*1024)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if err != nil {
						if err == io.EOF {
							e.mu.Lock()
							e.gotEOF = true
							e.mu.Unlock()
						}
						return
					}
				}
			}()
		}
	}()
	return e
}

func (e *echoUpstream) addr() string { return e.ln.Addr().String() }

func (e *echoUpstream) connectionCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.conns
}

// hostPort 拆出 host 与数字端口。
func hostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split %q: %v", addr, err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("atoi %q: %v", p, err)
	}
	return h, n
}

// chainSocks5 是一个假的上游 SOCKS5 服务器：完成 CONNECT 协商后，
// 把连接转到 inner（真实目标），双向转发。
//
// # 为什么需要它
//
// TestDataPlaneEndToEnd 想让"出站"是一个真的 Socks5Dialer，
// 那就得有一个说 SOCKS5 的上游 —— 回显服务不会说 SOCKS5。
// 少了这个替身，测试只能退化成"直连拨号器"，那就验证不到
// "入站+出站两个 SOCKS5 实现串起来"这条真正的链路。
//
// expectTarget 非空时校验客户端请求的目标；不符就直接断开（不回应答），
// 让上层看到失败而不是静默连到别处 —— 目标是抓"连错服务器"这类错。
type chainSocks5 struct {
	ln           net.Listener
	inner        string
	expectTarget string

	mu     sync.Mutex
	conns  int
	target []string
}

func newChainSocks5(t *testing.T, inner, expectTarget string) *chainSocks5 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := &chainSocks5{ln: ln, inner: inner, expectTarget: expectTarget}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if err := c.serve(conn); err != nil {
					_ = err // 替身的失败由上层断连体现，不在此打日志
				}
			}()
		}
	}()
	return c
}

// serve 完成 SOCKS5 协商并转发到 inner。
func (c *chainSocks5) serve(conn net.Conn) error {
	c.mu.Lock()
	c.conns++
	c.mu.Unlock()

	greet := make([]byte, 2)
	if _, err := io.ReadFull(conn, greet); err != nil {
		return err
	}
	if greet[0] != 0x05 {
		return fmt.Errorf("版本 %d", greet[0])
	}
	if _, err := io.ReadFull(conn, make([]byte, int(greet[1]))); err != nil {
		return err
	}
	// 只提供 no-auth：客户端带口令时会退化成不发认证帧，
	// 这条路径由 outbound 的用户口令测试单独覆盖。
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return err
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	var host string
	switch head[3] {
	case 0x01:
		b := make([]byte, 4)
		if _, err := io.ReadFull(conn, b); err != nil {
			return err
		}
		host = net.IP(b).String()
	case 0x04:
		b := make([]byte, 16)
		if _, err := io.ReadFull(conn, b); err != nil {
			return err
		}
		host = net.IP(b).String()
	case 0x03:
		nb := make([]byte, 1)
		if _, err := io.ReadFull(conn, nb); err != nil {
			return err
		}
		d := make([]byte, int(nb[0]))
		if _, err := io.ReadFull(conn, d); err != nil {
			return err
		}
		host = string(d)
	default:
		return fmt.Errorf("ATYP 0x%02x", head[3])
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(conn, pb); err != nil {
		return err
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(pb[0])<<8|int(pb[1])))

	c.mu.Lock()
	c.target = append(c.target, target)
	c.mu.Unlock()

	if c.expectTarget != "" && target != c.expectTarget {
		// 不回应答直接断开：让上层看到一个失败的连接。
		return fmt.Errorf("目标 %q 与预期 %q 不符", target, c.expectTarget)
	}
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}

	up, err := net.Dial("tcp", c.inner)
	if err != nil {
		return err
	}
	defer up.Close()
	relayFake(conn, up)
	return nil
}

func (c *chainSocks5) addr() string { return c.ln.Addr().String() }

func (c *chainSocks5) targets() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.target))
	copy(out, c.target)
	return out
}

// relayFake 替身侧的双向转发（与 internal/socks5 的 relay 同构，
// 这里是"peer 的行为模拟"，不是产品代码）。
func relayFake(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(b, a)
		if c, ok := b.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(a, b)
		if c, ok := a.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// recordingDialer 记录被拨的地址，并给每条连接起个名字。
//
// 用它把"走了哪条出站"变成可断言的字符串，而不是靠网络拓扑间接推断 ——
// 后者会为了表达一个测试意图而搭三层假服务。
type recordingDialer struct {
	name  string
	inner outbound.Dialer

	mu    sync.Mutex
	addrs []string
}

func (r *recordingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	r.mu.Lock()
	r.addrs = append(r.addrs, r.name+" "+address)
	r.mu.Unlock()
	return r.inner.DialContext(ctx, network, address)
}

func (r *recordingDialer) Name() string { return r.name }

func (r *recordingDialer) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.addrs))
	copy(out, r.addrs)
	return out
}

// socks5Client 是测试用的最小 SOCKS5 客户端。
//
// 独立于 internal/socks5 的服务端实现重写一遍：两边互为交叉验证，
// 共用一份代码的话"客户端写错但服务端也照错"照样全绿。
func socks5Client(t *testing.T, proxyAddr, target string) (net.Conn, byte) {
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

	host, port := hostPort(t, target)
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
	var pb [2]byte
	pb[0] = byte(port >> 8)
	pb[1] = byte(port)
	req = append(req, pb[:]...)
	if _, err := c.Write(req); err != nil {
		t.Fatalf("request: %v", err)
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatalf("read reply head: %v", err)
	}
	// bound 地址必须读走，否则 conn 上残留字节会被当成业务数据
	switch head[3] {
	case 0x01:
		_, _ = io.ReadFull(c, make([]byte, 4+2))
	case 0x04:
		_, _ = io.ReadFull(c, make([]byte, 16+2))
	case 0x03:
		nb := make([]byte, 1)
		_, _ = io.ReadFull(c, nb)
		_, _ = io.ReadFull(c, make([]byte, int(nb[0])+2))
	}
	return c, head[1]
}

// readWithTimeout 读 n 字节，超时报错而不是挂死。
func readWithTimeout(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read %d bytes: %v", n, err)
	}
	return buf
}

// waitFor 轮询等待条件成立（最多 2s）。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

// ---------------------------------------------------------------------------
// 端到端
// ---------------------------------------------------------------------------

func TestNewRejectsMissingListenAddr(t *testing.T) {
	_, err := New(Options{Proxy: &outbound.DirectDialer{}})
	if err == nil {
		t.Fatalf("缺监听地址必须报错")
	}
}

func TestNewRejectsMissingProxyDialer(t *testing.T) {
	_, err := New(Options{Listen: "127.0.0.1:0"})
	if err == nil {
		t.Fatalf("缺 Proxy 拨号器必须报错")
	}
}

// TestDataPlaneEndToEnd 是本包的**存在理由**测试：
// 真实 SOCKS5 客户端 → 数据面 → SOCKS5 上游（outbound 客户端）→ 假上游 → 回显服务。
// 两个 SOCKS5 实现（服务端与客户端）被串在一条连接上，字节逐字往返。
func TestDataPlaneEndToEnd(t *testing.T) {
	echo := newEchoUpstream(t, "BANNER")
	chain := newChainSocks5(t, echo.addr(), echo.addr())

	upstreamHost, upstreamPort := hostPort(t, chain.addr())

	// Proxy 拨号器用一个真的 Socks5Dialer：这样一条连接同时经过
	// 两个 SOCKS5 实现 —— 入站（服务端）与出站（客户端）。
	proxy := &outbound.Socks5Dialer{
		Server:   upstreamHost,
		Port:     upstreamPort,
		Username: "u",
		Password: "p",
	}

	dp, err := New(Options{
		Listen: "127.0.0.1:0",
		Proxy:  proxy,
		Direct: &outbound.DirectDialer{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dp.Close()

	c, rep := socks5Client(t, addr, echo.addr())
	defer c.Close()
	if rep != 0x00 {
		t.Fatalf("应答码 = 0x%02x, want 0x00", rep)
	}

	// 1) banner（经上游写出）
	if got := string(readWithTimeout(t, c, 6)); got != "BANNER" {
		t.Fatalf("banner = %q", got)
	}

	// 2) 业务数据往返（跨 16KB，确保不止一个 TCP 段）
	payload := bytes.Repeat([]byte("ABCDEFGH"), 2048) // 16384 字节
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readWithTimeout(t, c, len(payload))
	if !bytes.Equal(got, payload) {
		t.Fatalf("回显不一致：前 16 字节 %q", got[:16])
	}

	// 3) 上游收到的目标必须就是客户端请求的那个（连错服务器是最难查的错）
	if targets := chain.targets(); len(targets) != 1 || targets[0] != echo.addr() {
		t.Fatalf("上游收到的目标 = %v, want [%s]", targets, echo.addr())
	}

	// 4) 统计必须真的记上。
	//
	// 方向别搞反：banner 是**上游推到客户端**的，只进 Rx；
	// 载荷是客户端发出去经上游回显，Tx/Rx 各记一份。
	// 所以：Rx ≥ banner + 载荷，Tx ≥ 载荷。
	const bannerLen = 6
	waitFor(t, "统计记上字节", func() bool {
		s := dp.Stats()
		return s.Connections >= 1 &&
			s.TxBytes >= int64(len(payload)) &&
			s.RxBytes >= int64(bannerLen+len(payload))
	})
	s := dp.Stats()
	if s.Blocked != 0 {
		t.Errorf("不应有被拦连接，实际 %d", s.Blocked)
	}
}

// TestDataPlaneHalfClosePropagatesToUpstream 盯住 countingConn 的 CloseWrite 转发。
//
// 这条断言的是"客户端写完就半关"时，上游能读到 EOF。若包装层把
// CloseWrite 吞掉（只嵌入 net.Conn 接口时会发生），上游永远等不到 EOF，
// 症状是"下载停在 99%"——那种 bug 在真实设备上极难复现。
func TestDataPlaneHalfClosePropagatesToUpstream(t *testing.T) {
	echo := newEchoUpstream(t, "")
	dp, err := New(Options{
		Listen: "127.0.0.1:0",
		Proxy:  &outbound.DirectDialer{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dp.Close()

	c, rep := socks5Client(t, addr, echo.addr())
	if rep != 0x00 {
		t.Fatalf("应答码 = 0x%02x", rep)
	}
	// 写一点数据，然后半关
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := readWithTimeout(t, c, 5)
	if string(got) != "hello" {
		t.Fatalf("回显 = %q", got)
	}
	if tc, ok := c.(*net.TCPConn); ok {
		if err := tc.CloseWrite(); err != nil {
			t.Fatalf("CloseWrite: %v", err)
		}
	}
	// 读到 EOF 说明 FIN 真的穿过了数据面
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1)
	if _, err := c.Read(buf); err != io.EOF {
		t.Fatalf("半关后应读到 EOF，实际 %v", err)
	}

	echo.mu.Lock()
	gotEOF := echo.gotEOF
	echo.mu.Unlock()
	if !gotEOF {
		t.Fatalf("上游没读到 EOF：CloseWrite 没有被 countingConn 转发")
	}
}

// TestDataPlaneRejectRepliesNotAllowedByRuleset 验证"被拦"与"连不上"可区分。
//
// 目标用域名而不是 IP：规则是按主机名配的，用 IP 当目标就绕过了规则本身 ——
// 那测的是"拒绝码映射"，不是"分流命中"。
func TestDataPlaneRejectRepliesNotAllowedByRuleset(t *testing.T) {
	echo := newEchoUpstream(t, "")
	echoHost, echoPort := hostPort(t, echo.addr())

	router, err := NewRuleRouter(DecisionProxy, []RouteRule{
		{Domain: "localhost", Action: DecisionReject},
	})
	if err != nil {
		t.Fatalf("NewRuleRouter: %v", err)
	}
	dp, err := New(Options{
		Listen: "127.0.0.1:0",
		Proxy:  &outbound.DirectDialer{},
		Router: router,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dp.Close()

	before := echo.connectionCount()

	c, rep := socks5Client(t, addr, net.JoinHostPort("localhost", strconv.Itoa(echoPort)))
	defer c.Close()
	if rep != 0x02 {
		t.Fatalf("应答码 = 0x%02x, want 0x02 (not allowed by ruleset)", rep)
	}
	if after := echo.connectionCount(); after != before {
		t.Errorf("被拦的连接不应到达上游：before=%d after=%d", before, after)
	}
	waitFor(t, "blocked 计数", func() bool { return dp.Stats().Blocked == 1 })
	_ = echoHost
}

// TestDataPlaneRuleRoutesToDirectOrProxy 验证分流真的把连接送到不同拨号器。
//
// 断言落到 recordingDialer 上而不是"哪一个上游收到了连接"：
// 后者要为了表达意图去搭假拓扑，前者直接说明"决策结果是什么"。
func TestDataPlaneRuleRoutesToDirectOrProxy(t *testing.T) {
	// banner 长度必须与下面的 readWithTimeout 一致 —— 这里两种 banner
	// 分别是 6 字节（DIRECT）和 5 字节（PROXY），长度不同恰恰是用来
	// 区分"连到了哪一个上游"的。用 len(...) 而不是手写常量，
	// 否则改一个字符串就会让 readWithTimeout 的 io.ReadFull 永远
	// 等不到第 N 个字节，症状是 5 秒读超时而不是断言失败。
	const (
		directBanner = "DIRECT"
		proxyBanner  = "PROXY"
	)
	directEcho := newEchoUpstream(t, directBanner)
	proxyEcho := newEchoUpstream(t, proxyBanner)
	chain := newChainSocks5(t, proxyEcho.addr(), "")

	chainHost, chainPort := hostPort(t, chain.addr())
	// 直连的目标是主机名 localhost，拨号器收到的主机段也应是 localhost，
	// 所以 directEchoHost 在断言里用不上 —— 留 _ 表明"已知但不关心"。
	_, directEchoPort := hostPort(t, directEcho.addr())
	proxyEchoHost, proxyEchoPort := hostPort(t, proxyEcho.addr())

	router, err := NewRuleRouter(DecisionProxy, []RouteRule{
		{Domain: "localhost", Action: DecisionDirect},
	})
	if err != nil {
		t.Fatalf("NewRuleRouter: %v", err)
	}
	recDirect := &recordingDialer{name: "direct", inner: &outbound.DirectDialer{}}
	recProxy := &recordingDialer{name: "proxy", inner: &outbound.Socks5Dialer{
		Server: chainHost, Port: chainPort,
	}}

	dp, err := New(Options{
		Listen: "127.0.0.1:0",
		Proxy:  recProxy,
		Direct: recDirect,
		Router: router,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dp.Close()

	// 直连的那条：目标主机名命中 localhost → Direct
	cDirect, rep := socks5Client(t, addr,
		net.JoinHostPort("localhost", strconv.Itoa(directEchoPort)))
	defer cDirect.Close()
	if rep != 0x00 {
		t.Fatalf("应答码 = 0x%02x", rep)
	}
	if got := string(readWithTimeout(t, cDirect, len(directBanner))); got != directBanner {
		t.Fatalf("直连 banner = %q, want %q", got, directBanner)
	}

	// 走代理的那条：目标用 IP（不命中 localhost 规则）→ Proxy
	cProxy, rep := socks5Client(t, addr,
		net.JoinHostPort(proxyEchoHost, strconv.Itoa(proxyEchoPort)))
	defer cProxy.Close()
	if rep != 0x00 {
		t.Fatalf("应答码 = 0x%02x", rep)
	}
	if got := string(readWithTimeout(t, cProxy, len(proxyBanner))); got != proxyBanner {
		t.Fatalf("代理 banner = %q, want %q", got, proxyBanner)
	}

	waitFor(t, "两个拨号器各调一次", func() bool {
		return len(recDirect.calls()) == 1 && len(recProxy.calls()) == 1
	})
	dCalls, pCalls := recDirect.calls(), recProxy.calls()
	wantDirect := fmt.Sprintf("direct %s", net.JoinHostPort("localhost", strconv.Itoa(directEchoPort)))
	if dCalls[0] != wantDirect {
		t.Errorf("direct 拨号 = %q, want %q", dCalls[0], wantDirect)
	}
	wantProxy := fmt.Sprintf("proxy %s", net.JoinHostPort(proxyEchoHost, strconv.Itoa(proxyEchoPort)))
	if pCalls[0] != wantProxy {
		t.Errorf("proxy 拨号 = %q, want %q", pCalls[0], wantProxy)
	}
}

// TestDataPlaneCloseStopsAccepting 关闭后新连接必须被拒，不能挂着。
func TestDataPlaneCloseStopsAccepting(t *testing.T) {
	echo := newEchoUpstream(t, "")
	dp, err := New(Options{Listen: "127.0.0.1:0", Proxy: &outbound.DirectDialer{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := dp.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		t.Fatalf("Close 后仍能连上：监听没关干净")
	}
	// Close 必须幂等：调用方（含 defer）重复调是常见形态。
	if err := dp.Close(); err != nil {
		t.Fatalf("第二次 Close: %v", err)
	}
	_ = echo
}

// TestDataPlaneCloseRightAfterStartIsNotAnError 盯住 Close 的双重关闭竞态。
//
// 竞态本体：DataPlane.Close 关两次同一个监听器 —— 自己持有的 ln 一次，
// socks5.Server.Close 内部对它登记的同一个 ln 再一次。而 Serve 是
// Start 里 `go` 出去的，s.listener 何时被赋值与 Close 竞争，所以
// "哪一次真正关上了、哪一次拿到 net.ErrClosed"不确定。
//
// 这条断言之所以必须存在：症状是**间歇性** Close 报
// "use of closed network connection"，实测约 1/6 概率，只在
// "Start 之后立刻 Close"（未给 Serve goroutine 留时间）时出现。
// 单跑一次测试几乎必然绿，所以这里循环 hammer：race 类缺陷
// 不能用"跑一遍通过"来证明不存在，只能靠放大执行次数。
func TestDataPlaneCloseRightAfterStartIsNotAnError(t *testing.T) {
	// Start 之后不留任何等待，直接 Close —— 这是把 s.listener
	// 赋值和 ln.Close 的竞争窗口拉到最大的唯一办法。
	const rounds = 200
	for i := 0; i < rounds; i++ {
		dp, err := New(Options{Listen: "127.0.0.1:0", Proxy: &outbound.DirectDialer{}})
		if err != nil {
			t.Fatalf("第 %d 轮 New: %v", i, err)
		}
		if _, err := dp.Start(); err != nil {
			t.Fatalf("第 %d 轮 Start: %v", i, err)
		}
		if err := dp.Close(); err != nil {
			t.Fatalf("第 %d 轮 Close: %v（监听器被关了两次）", i, err)
		}
	}
}

// TestDataPlaneConcurrentConnections 并发多连接，统计不能少记。
func TestDataPlaneConcurrentConnections(t *testing.T) {
	echo := newEchoUpstream(t, "")
	dp, err := New(Options{Listen: "127.0.0.1:0", Proxy: &outbound.DirectDialer{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dp.Close()

	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, rep := socks5Client(t, addr, echo.addr())
			defer c.Close()
			if rep != 0x00 {
				errs <- fmt.Errorf("conn %d 应答码 0x%02x", i, rep)
				return
			}
			msg := []byte("payload-" + strconv.Itoa(i))
			if _, err := c.Write(msg); err != nil {
				errs <- err
				return
			}
			got := readWithTimeout(t, c, len(msg))
			if string(got) != string(msg) {
				errs <- fmt.Errorf("conn %d 回显 %q", i, got)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("并发连接失败: %v", err)
	}
	waitFor(t, "连接数计数", func() bool { return dp.Stats().Connections >= n })
}

// contextDialer 是包装一个 Dialer 并在拨号时检查 ctx 的替身，
// 用来验证 dataplane 传的 ctx 没有被静默丢弃。
type contextDialer struct {
	inner outbound.Dialer
	mu    sync.Mutex
	calls int
}

func (c *contextDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if ctx == nil {
		return nil, fmt.Errorf("ctx 为 nil：调用方丢了上下文")
	}
	return c.inner.DialContext(ctx, network, address)
}

func (c *contextDialer) Name() string { return "ctx-check" }

func TestDataPlanePassesContextToDialer(t *testing.T) {
	echo := newEchoUpstream(t, "")
	d := &contextDialer{inner: &outbound.DirectDialer{}}
	dp, err := New(Options{Listen: "127.0.0.1:0", Proxy: d})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dp.Close()

	c, rep := socks5Client(t, addr, echo.addr())
	defer c.Close()
	if rep != 0x00 {
		t.Fatalf("应答码 = 0x%02x", rep)
	}
	d.mu.Lock()
	calls := d.calls
	d.mu.Unlock()
	if calls != 1 {
		t.Fatalf("Proxy 拨号器被调用 %d 次, want 1", calls)
	}
}
