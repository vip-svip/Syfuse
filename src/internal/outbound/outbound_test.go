package outbound

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 测试替身
// ---------------------------------------------------------------------------

// fakeServer 起一个本地 TCP 服务，accept 后按 script 逐帧处理。
type fakeServer struct {
	ln   net.Listener
	addr string

	// onConn 对每个连接执行的处理逻辑；返回 nil 表示正常结束。
	onConn func(c net.Conn) error

	mu    sync.Mutex
	conns int

	// active 统计**尚未处理完**的连接数。
	//
	// 必须有它：DialContext 返回只代表"客户端这边握手完了"，
	// 服务端 goroutine 可能还在读后续帧（trojan 的请求帧就是这种情况）。
	// 没有这个等待，断言会在服务端读完之前跑，表现为偶发空值。
	active int32
	done   chan struct{}
}

func newFakeServer(t *testing.T, onConn func(net.Conn) error) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeServer{ln: ln, addr: ln.Addr().String(), onConn: onConn, done: make(chan struct{})}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			f.mu.Lock()
			f.conns++
			f.active++
			f.mu.Unlock()
			go func() {
				_ = f.onConn(c)
				c.Close()
				f.mu.Lock()
				f.active--
				if f.active <= 0 {
					select {
					case <-f.done:
					default:
						close(f.done)
					}
				}
				f.mu.Unlock()
			}()
		}
	}()
	return f
}

// waitIdle 等到所有已 accept 的连接都处理完（或超时）。
//
// 只有确认服务端读完了客户端发的帧，断言才站得住 —— 否则测的是竞态。
func (f *fakeServer) waitIdle(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	if f.active <= 0 {
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("等待假服务端处理完连接超时")
	}
}

func (f *fakeServer) connectionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns
}

func (f *fakeServer) hostPort(t *testing.T) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(f.addr)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("atoi: %v", err)
	}
	return h, port
}

// readN 精确读 n 字节。
func readN(c net.Conn, n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ---------------------------------------------------------------------------
// 直连
// ---------------------------------------------------------------------------

func TestDirectDialerSucceeds(t *testing.T) {
	srv := newFakeServer(t, func(c net.Conn) error {
		c.Write([]byte("PONG"))
		// 写完必须把客户端的输入排空再返回。
		//
		// Windows 的 TCP 语义：socket 关闭时若接收缓冲区里还有数据，
		// 内核发 RST 而不是 FIN。RST 一到，本地已发出但还在途的
		// "PONG" 直接被丢弃，客户端读到的是
		// "An existing connection was forcibly closed"。
		// 直连的断言当然是读回显 —— 不 drain 就会偶发失败，
		// 而且失败只出现在 Windows 上，Linux/macOS 永远绿。
		io.Copy(io.Discard, c)
		return nil
	})
	host, port := srv.hostPort(t)

	d := &DirectDialer{}
	c, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("PING")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "PONG" {
		t.Fatalf("got %q", buf)
	}
	if d.Name() != "direct" {
		t.Fatalf("Name = %q", d.Name())
	}
}

func TestDirectDialerRejectsUnreachable(t *testing.T) {
	// 拨一个没人监听的端口必须报错，不能返回一个"看起来成功"的连接。
	d := &DirectDialer{Timeout: 2 * time.Second}
	// 127.0.0.1:1 在测试环境几乎必然拒接。
	_, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:1")
	if err == nil {
		t.Fatalf("期望拨号失败，却成功了")
	}
}

// ---------------------------------------------------------------------------
// SOCKS5 上游
// ---------------------------------------------------------------------------

// socks5RequestFields 记录上游（客户端视角）实际收到的请求字段。
type socks5RequestFields struct {
	ver, nMethods byte
	methods       []byte
	cmd, atyp     byte
	host          string
	port          int
}

// serveSocks5Upstream 是一个**按 RFC 1928 逐字节读**的假上游。
//
// 为什么单独写一个而不是在每个测试里内联：第一版内联的假上游把
// 问候语读成 4 字节，而真实客户端只发 3 字节（VER NMETHODS METHODS），
// 于是假上游永远等不到第 4 个字节 —— 测试全部 i/o timeout。
// 这是**测试替身**的错，不是产品的错；但若没有逐字节断言，
// 这种错会一直伪装成"产品有 bug"。
//
// replyCode 是回给客户端的应答码；bound 固定用 127.0.0.1:0。
func serveSocks5Upstream(c net.Conn, capture *socks5RequestFields, replyCode byte) error {
	greet := make([]byte, 2)
	if _, err := io.ReadFull(c, greet); err != nil {
		return err
	}
	capture.ver, capture.nMethods = greet[0], greet[1]
	if capture.ver != 0x05 {
		return fmt.Errorf("问候语版本 = 0x%02x, want 0x05", capture.ver)
	}
	methods := make([]byte, capture.nMethods)
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}
	capture.methods = methods

	// 方法选取：客户端提供 user-pass(0x02) 时才选它，否则选 no-auth。
	var chosen byte = 0x00
	for _, m := range methods {
		if m == 0x02 {
			chosen = 0x02
			break
		}
	}
	if _, err := c.Write([]byte{0x05, chosen}); err != nil {
		return err
	}

	if chosen == 0x02 {
		// RFC 1929: VER(1) ULEN(1) UNAME PS UNAME... PLEN(1) PASSWD
		head := make([]byte, 2)
		if _, err := io.ReadFull(c, head); err != nil {
			return err
		}
		user := make([]byte, head[1])
		if _, err := io.ReadFull(c, user); err != nil {
			return err
		}
		pl := make([]byte, 1)
		if _, err := io.ReadFull(c, pl); err != nil {
			return err
		}
		pass := make([]byte, pl[0])
		if _, err := io.ReadFull(c, pass); err != nil {
			return err
		}
		if _, err := c.Write([]byte{0x01, 0x00}); err != nil {
			return err
		}
	}

	head, err := readN(c, 4) // VER CMD RSV ATYP
	if err != nil {
		return err
	}
	capture.cmd, capture.atyp = head[1], head[3]
	switch head[3] {
	case 0x01:
		b, err := readN(c, 4)
		if err != nil {
			return err
		}
		capture.host = net.IP(b).String()
	case 0x04:
		b, err := readN(c, 16)
		if err != nil {
			return err
		}
		capture.host = net.IP(b).String()
	case 0x03:
		n, err := readN(c, 1)
		if err != nil {
			return err
		}
		d, err := readN(c, int(n[0]))
		if err != nil {
			return err
		}
		capture.host = string(d)
	default:
		return fmt.Errorf("未知 ATYP 0x%02x", head[3])
	}
	p, err := readN(c, 2)
	if err != nil {
		return err
	}
	capture.port = int(p[0])<<8 | int(p[1])

	// 成功应答：REP RSV ATYP BND.ADDR BND.PORT（bound 用 IPv4 0.0.0.0:0）
	if _, err := c.Write([]byte{0x05, replyCode, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return err
	}
	io.Copy(io.Discard, c)
	return nil
}

func TestSocks5DialerSendsCorrectHandshake(t *testing.T) {
	// 关键断言：客户端发出去的帧必须逐字节符合 SOCKS5。
	// 少一个 CRLF、地址类型写错，真实上游会静默断开 —— 那种 bug
	// 从表现上看就是"节点连不上"，很难反推到协议帧写错。
	var cap5 socks5RequestFields
	srv := newFakeServer(t, func(c net.Conn) error {
		return serveSocks5Upstream(c, &cap5, 0x00)
	})
	host, port := srv.hostPort(t)

	d := &Socks5Dialer{Server: host, Port: port}
	c, err := d.DialContext(context.Background(), "tcp", "93.184.216.34:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if cap5.ver != 0x05 {
		t.Errorf("问候语版本 = 0x%02x, want 0x05", cap5.ver)
	}
	if cap5.nMethods != 1 {
		t.Errorf("方法数 = %d, want 1", cap5.nMethods)
	}
	if cap5.cmd != 0x01 {
		t.Errorf("命令 = 0x%02x, want CONNECT", cap5.cmd)
	}
	if cap5.atyp != 0x01 {
		t.Errorf("地址类型 = 0x%02x, want IPv4", cap5.atyp)
	}
	if cap5.host != "93.184.216.34" {
		t.Errorf("主机 = %q", cap5.host)
	}
	if cap5.port != 443 {
		t.Errorf("端口 = %d, want 443", cap5.port)
	}
}

func TestSocks5DialerDomainTarget(t *testing.T) {
	var cap5 socks5RequestFields
	srv := newFakeServer(t, func(c net.Conn) error {
		return serveSocks5Upstream(c, &cap5, 0x00)
	})
	host, port := srv.hostPort(t)

	d := &Socks5Dialer{Server: host, Port: port}
	c, err := d.DialContext(context.Background(), "tcp", "a.example.com:80")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if cap5.host != "a.example.com" {
		t.Fatalf("主机 = %q, want a.example.com", cap5.host)
	}
	if cap5.atyp != 0x03 {
		t.Fatalf("ATYP = 0x%02x, want 0x03 (domain)", cap5.atyp)
	}
}

func TestSocks5DialerSurfacesUpstreamRejection(t *testing.T) {
	// 上游回一个非 0 的应答码时，拨号必须失败并带上那个码 ——
	// 上层要靠它区分"节点拒了"与"我的网络断了"。
	srv := newFakeServer(t, func(c net.Conn) error {
		var cap5 socks5RequestFields
		return serveSocks5Upstream(c, &cap5, 0x05) // connection refused
	})
	host, port := srv.hostPort(t)

	d := &Socks5Dialer{Server: host, Port: port}
	_, err := d.DialContext(context.Background(), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatalf("上游拒接时应失败")
	}
	if !strings.Contains(err.Error(), "0x05") {
		t.Fatalf("错误应带上应答码: %v", err)
	}
}

func TestSocks5DialerRejectsUDP(t *testing.T) {
	d := &Socks5Dialer{Server: "127.0.0.1", Port: 1}
	if _, err := d.DialContext(context.Background(), "udp", "10.0.0.1:53"); err == nil {
		t.Fatalf("udp 应被拒绝")
	}
}

// ---------------------------------------------------------------------------
// HTTP CONNECT 上游
// ---------------------------------------------------------------------------

func TestHttpConnectDialerSendsCorrectRequest(t *testing.T) {
	var request string
	srv := newFakeServer(t, func(c net.Conn) error {
		buf := make([]byte, 256)
		n, _ := c.Read(buf)
		request = string(buf[:n])
		c.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		io.Copy(io.Discard, c)
		return nil
	})
	host, port := srv.hostPort(t)

	d := &HttpConnectDialer{Server: host, Port: port}
	c, err := d.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if !strings.HasPrefix(request, "CONNECT example.com:443 HTTP/1.1\r\n") {
		t.Fatalf("请求行不对: %q", request)
	}
	if !strings.Contains(request, "Host: example.com:443\r\n") {
		t.Fatalf("缺 Host 头: %q", request)
	}
	if !strings.HasSuffix(request, "\r\n\r\n") {
		t.Fatalf("请求未正确结束: %q", request)
	}
}

func TestHttpConnectDialerRejectsNon2xx(t *testing.T) {
	// 403 必须失败。把 4xx 当成功会让"代理不给用"伪装成"节点连上了但没流量"。
	srv := newFakeServer(t, func(c net.Conn) error {
		c.Read(make([]byte, 128))
		c.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
		return nil
	})
	host, port := srv.hostPort(t)

	d := &HttpConnectDialer{Server: host, Port: port}
	if _, err := d.DialContext(context.Background(), "tcp", "example.com:443"); err == nil {
		t.Fatalf("403 应失败")
	}
}

func TestHttpConnectDialerWithBasicAuth(t *testing.T) {
	var request string
	srv := newFakeServer(t, func(c net.Conn) error {
		buf := make([]byte, 256)
		n, _ := c.Read(buf)
		request = string(buf[:n])
		c.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
		io.Copy(io.Discard, c)
		return nil
	})
	host, port := srv.hostPort(t)

	d := &HttpConnectDialer{Server: host, Port: port, Username: "u", Password: "p"}
	if _, err := d.DialContext(context.Background(), "tcp", "example.com:443"); err != nil {
		t.Fatalf("dial: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("u:p"))
	if !strings.Contains(request, "Proxy-Authorization: "+want+"\r\n") {
		t.Fatalf("认证头不对: %q", request)
	}
}

// ---------------------------------------------------------------------------
// Trojan 出站
// ---------------------------------------------------------------------------

// selfSignedCert 生成一张自签名证书给假 TLS 服务端用。
//
// 为什么需要它：Go 的 TLS **服务端**没有证书时会在握手阶段回
// `unrecognized_name` 警报，客户端拿到的错误与"SNI 不被识别"一模一样。
// 第一版测试替身因此误判成"产品把 IP 字面量当 SNI 发出去了"，
// 实测（见下）确认 IP 字面量 SNI 在有证书的服务端上握手完全正常 ——
// 问题在替身没证书。这个辅助函数就是那次实测的结论落点。
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成私钥: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发证书: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestTrojanDialerSendsProtocolFrame(t *testing.T) {
	// 起一个真的 TLS 服务端，逐字节校验客户端发来的 trojan 帧。
	var gotHex, gotHost string
	var gotAtyp byte

	cert := selfSignedCert(t)
	srv := newFakeServer(t, func(c net.Conn) error {
		tlsConn := tls.Server(c, &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
			Certificates:       []tls.Certificate{cert},
		})
		if err := tlsConn.Handshake(); err != nil {
			return err
		}
		defer tlsConn.Close()

		head, err := readN(tlsConn, 56+2) // hex(SHA224) = 56 字符 + CRLF
		if err != nil {
			return err
		}
		gotHex = string(head[:56])
		if string(head[56:]) != "\r\n" {
			return fmt.Errorf("口令后不是 CRLF: %q", head[56:])
		}

		line, err := readN(tlsConn, 2) // CMD + ATYP
		if err != nil {
			return err
		}
		if line[0] != 0x01 {
			return fmt.Errorf("CMD = 0x%02x, want CONNECT", line[0])
		}
		gotAtyp = line[1]
		switch line[1] {
		case 0x01:
			b, _ := readN(tlsConn, 4)
			gotHost = net.IP(b).String()
		case 0x03:
			n, _ := readN(tlsConn, 1)
			d, _ := readN(tlsConn, int(n[0]))
			gotHost = string(d)
		}
		readN(tlsConn, 2) // port
		tail, err := readN(tlsConn, 2)
		if err != nil {
			return err
		}
		if string(tail) != "\r\n" {
			return fmt.Errorf("请求行结尾不是 CRLF: %q", tail)
		}
		// 帧读全即返回：trojan 没有应答帧，继续 io.Copy 只会把连接
		// 挂到客户端关闭为止，waitIdle 就变成了等一个永不发生的结束。
		_ = gotHex
		return nil
	})

	host, port := srv.hostPort(t)
	d := &TrojanDialer{
		Server: host, Port: port, Password: "trojan-password", // secretscan:allow 测试夹具
		Insecure: true,
	}
	c, err := d.DialContext(context.Background(), "tcp", "93.184.216.34:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// 等假服务端把请求帧读完再断言，否则测的是竞态而不是协议。
	srv.waitIdle(t)

	if len(gotHex) != 56 {
		t.Fatalf("口令段长度 = %d, want 56 (hex SHA224)", len(gotHex))
	}
	// 口令必须是 hex(SHA224(password)) 本身，而不只是"长度对"。
	// 长度对但值错（比如用了 SHA1 截断）同样连不上真节点。
	if want := hexSha224("trojan-password"); gotHex != want {
		t.Fatalf("口令段 = %q, want hex(SHA224) = %q", gotHex, want)
	}
	if gotAtyp != 0x01 {
		t.Fatalf("地址类型 = 0x%02x, want IPv4", gotAtyp)
	}
	if gotHost != "93.184.216.34" {
		t.Fatalf("主机 = %q", gotHost)
	}
}

func TestHexSha224MatchesKnownVector(t *testing.T) {
	// SHA224 是小众算法，用固定向量锁住，防止哪天被"顺手"换成 SHA256 ——
	// 两者输出长度不同（56 vs 64 字符），但类型上都只是 string，
	// 编译期完全看不出来，只有真连上游才会失败。
	//
	// 向量来源：`printf 'password' | sha224sum`（coreutils 独立实现，
	// 与 Go 标准库无共享代码，因此这是一次真正的交叉验证）。
	got := hexSha224("password")
	want := "d63dc919e201d7bc4c825630d2cf25fdc93d4b2f0d46706d29038d01"
	if got != want {
		t.Fatalf("hexSha224(%q) = %q, want %q（固定向量不匹配，可能被换成了别的哈希）", "password", got, want)
	}
	if len(got) != 56 {
		t.Fatalf("hex SHA224 长度 = %d, want 56", len(got))
	}
	// 用标准库独立算一遍交叉验证。
	h := sha256.New224()
	h.Write([]byte("password"))
	expect := hex.EncodeToString(h.Sum(nil))
	if got != expect {
		t.Fatalf("hexSha224 = %q, want %q", got, expect)
	}
}

func TestNewDialerRoutesByProtocol(t *testing.T) {
	cases := []struct {
		protocol string
		wantName string
		wantErr  bool
	}{
		{"direct", "direct", false},
		{"", "direct", false},
		{"socks5", "socks5", false},
		{"http", "http", false},
		{"trojan", "trojan", false},
		{"ss", "ss", false},
		{"shadowsocks", "ss", false}, // 同一协议的完整拼写
		{"vmess", "", true},          // 尚未支持：必须明确报错，不能回落成直连
		{"vless", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			d, err := NewDialer(Config{Protocol: tc.protocol, Server: "s", Port: 1})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("协议 %q 应报错", tc.protocol)
				}
				if !errors.Is(err, ErrUnsupportedProtocol) {
					t.Fatalf("错误类型不对: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("协议 %q 不应报错: %v", tc.protocol, err)
			}
			if d.Name() != tc.wantName {
				t.Fatalf("Name = %q, want %q", d.Name(), tc.wantName)
			}
		})
	}
}
