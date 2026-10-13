package socks5

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// 测试用的假上游：记录被拨的地址，并按脚本回写。
type fakeUpstream struct {
	ln       net.Listener
	dialedTo []string
	mu       sync.Mutex
	// reply 拨号成功后回给客户端的内容。
	reply []byte
	// expect 期望从客户端收到的内容（非空则校验后回 reply）。
	expect string
}

func newFakeUpstream(t *testing.T, reply string) *fakeUpstream {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeUpstream{ln: ln, reply: []byte(reply)}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeUpstream) serve(c net.Conn) {
	defer c.Close()
	// 真实上游的语义：**先发 greeting，然后保持连接**直到对端关。
	// HTTP/SMTP/Redis 都是服务端先开口，所以必须先 Write 再 drain。
	//
	// 早期两个版本都错过的原因值得记下来：
	//   ① "写完就 Close" —— 制造非真实竞态（fake 的 Close 会让服务端
	//      的 io.Copy 写收到 RST），测出来的是替身有多粗暴，与产品无关；
	//   ② "只 drain 不先写" —— 回复要等客户端关才发，而客户端不关，
	//      于是稳定超时。看起来像产品挂了，其实是替身把顺序搞反。
	// 替身必须逐字模仿真实对端的**时序**，顺序错了测的就是别的东西。
	c.Write(f.reply)
	io.Copy(io.Discard, c)
}

func (f *fakeUpstream) addr() string { return f.ln.Addr().String() }

func (f *fakeUpstream) dialed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.dialedTo))
	copy(out, f.dialedTo)
	return out
}

// dialLog 记录测试期间服务端拨过的地址（含并发保护）。
var dialLog struct {
	sync.Mutex
	to []string
}

func recordDial(addr string) {
	dialLog.Lock()
	dialLog.to = append(dialLog.to, addr)
	dialLog.Unlock()
}

func lastDialed(t *testing.T, _ *Server) []string {
	t.Helper()
	dialLog.Lock()
	defer dialLog.Unlock()
	out := make([]string, len(dialLog.to))
	copy(out, dialLog.to)
	return out
}

// startServer 起一个只允许 no-auth 的服务，并把 Dial 指向假上游。
// startServer 起服务，返回服务与监听地址。
// 直接返回测试自己的 listener 地址而不读 s.Addr() —— 后者有竞态
// （Serve 在另一个 goroutine 里才赋值 s.listener）。
func startServer(t *testing.T, target string) (*Server, string) {
	t.Helper()
	upstream := newFakeUpstream(t, "HELLO-FROM-UPSTREAM")
	dialLog.Lock()
	dialLog.to = nil
	dialLog.Unlock()
	s := New()
	s.Dial = func(network, address string) (net.Conn, error) {
		if address != target {
			t.Errorf("Dial 地址 = %q, want %q", address, target)
		}
		recordDial(address)
		// 真的连到假上游，保证后续字节流转发被覆盖到。
		return net.Dial(network, upstream.addr())
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)
	return s, addr
}

// doHandshake 走完认证协商 + CONNECT 请求，返回连接。
func doHandshake(t *testing.T, addr string, atyp byte, host string, port int) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	// 方法协商：只提 no-auth。
	if _, err := c.Write([]byte{Version5, 0x01, MethodNoAuth}); err != nil {
		t.Fatalf("write methods: %v", err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil {
		t.Fatalf("read method resp: %v", err)
	}
	if resp[0] != Version5 {
		t.Fatalf("方法应答版本 = %d, want 5", resp[0])
	}
	if resp[1] != MethodNoAuth {
		t.Fatalf("服务端选择的方法 = 0x%02x, want no-auth", resp[1])
	}

	// CONNECT 请求。
	req := []byte{Version5, CmdConnect, 0x00, atyp}
	switch atyp {
	case ATypIPv4:
		req = append(req, net.ParseIP(host).To4()...)
	case ATypIPv6:
		req = append(req, net.ParseIP(host).To16()...)
	case ATypDomain:
		req = append(req, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	req = append(req, portBytes[:]...)
	if _, err := c.Write(req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	return c
}

// readReply 读应答帧，返回 (rep, bound 地址)。
func readReply(t *testing.T, c net.Conn) (byte, string) {
	t.Helper()
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatalf("read reply head: %v", err)
	}
	addr, err := readAddr(c, head[3])
	if err != nil {
		t.Fatalf("read bound addr: %v", err)
	}
	return head[1], addr
}

// ---------------------------------------------------------------------------
// 正常路径
// ---------------------------------------------------------------------------

func TestConnectIPv4SucceedsAndRelays(t *testing.T) {
	target := "93.184.216.34:80"
	_, addr := startServer(t, target)
	c := doHandshake(t, addr, ATypIPv4, "93.184.216.34", 80)

	rep, _ := readReply(t, c)
	if rep != RepSucceeded {
		t.Fatalf("rep = 0x%02x, want 0x%02x", rep, RepSucceeded)
	}

	// 双向数据必须真的流过去：客户端写、上游收、上游回、客户端收。
	if _, err := c.Write([]byte("PING")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 64)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read relayed: %v", err)
	}
	if string(buf[:n]) != "HELLO-FROM-UPSTREAM" {
		t.Fatalf("relayed = %q, want HELLO-FROM-UPSTREAM", buf[:n])
	}

	if got := len(lastDialed(t, nil)); got != 1 {
		t.Errorf("Dial 次数 = %d, want 1", got)
	}
}

func TestConnectDomainSucceeds(t *testing.T) {
	target := "example.com:443"
	_, addr := startServer(t, target)
	c := doHandshake(t, addr, ATypDomain, "example.com", 443)

	rep, _ := readReply(t, c)
	if rep != RepSucceeded {
		t.Fatalf("rep = 0x%02x, want succeeded", rep)
	}
	if _, err := c.Write([]byte("X")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 32)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
}

func TestConnectIPv6Succeeds(t *testing.T) {
	target := "[2001:db8::1]:8080"
	_, addr := startServer(t, target)
	c := doHandshake(t, addr, ATypIPv6, "2001:db8::1", 8080)

	rep, _ := readReply(t, c)
	if rep != RepSucceeded {
		t.Fatalf("rep = 0x%02x, want succeeded", rep)
	}
}

// ---------------------------------------------------------------------------
// 认证
// ---------------------------------------------------------------------------

func TestUserPassAuthAccepted(t *testing.T) {
	upstream := newFakeUpstream(t, "OK")
	s := New()
	s.Auth = func(user, password string) bool { return user == "u" && password == "p" }
	s.Dial = func(network, address string) (net.Conn, error) {
		return net.Dial(network, upstream.addr())
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	c.Write([]byte{Version5, 0x01, MethodUserPass})
	resp := make([]byte, 2)
	io.ReadFull(c, resp)
	if resp[1] != MethodUserPass {
		t.Fatalf("选择的方法 = 0x%02x, want user/pass", resp[1])
	}

	// RFC 1929 认证帧。
	auth := []byte{0x01, 0x01, 'u', 0x01, 'p'}
	c.Write(auth)
	authResp := make([]byte, 2)
	if _, err := io.ReadFull(c, authResp); err != nil {
		t.Fatalf("read auth resp: %v", err)
	}
	if authResp[1] != 0x00 {
		t.Fatalf("认证应答 = 0x%02x, want 0x00 (成功)", authResp[1])
	}
}

func TestUserPassAuthRejectedRepliesFailureNotReset(t *testing.T) {
	// 关键断言：密码错时必须回一帧"失败"，**不能直接断开**。
	// 直接断开会让客户端看到"连接被重置"，无法区分"密码错"与"网络断"。
	s := New()
	s.Auth = func(user, password string) bool { return false }
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.Write([]byte{Version5, 0x01, MethodUserPass})
	io.ReadFull(c, make([]byte, 2))
	c.Write([]byte{0x01, 0x01, 'u', 0x01, 'x'})

	resp := make([]byte, 2)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(c, resp); err != nil {
		t.Fatalf("读认证应答失败（可能被直接断开）: %v", err)
	}
	if resp[0] != 0x01 || resp[1] != 0x01 {
		t.Fatalf("认证应答 = %v, want [01 01] (失败)", resp)
	}
}

func TestNoAuthNotOfferedWhenAuthConfigured(t *testing.T) {
	// 配了 Auth 就必须只通告 user/pass：客户端若提 no-auth 应收到 0xFF。
	s := New()
	s.Auth = func(string, string) bool { return true }
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	c, _ := net.Dial("tcp", ln.Addr().String())
	defer c.Close()
	c.Write([]byte{Version5, 0x01, MethodNoAuth})
	resp := make([]byte, 2)
	io.ReadFull(c, resp)
	if resp[1] != MethodNone {
		t.Fatalf("选择的方法 = 0x%02x, want 0xFF (无可接受方法)", resp[1])
	}
}

// ---------------------------------------------------------------------------
// 错误路径（应答码必须如实）
// ---------------------------------------------------------------------------

func TestUnsupportedCommandRepliesCommandNotSupported(t *testing.T) {
	// BIND 不支持时必须回 0x07，而不是笼统的 general failure ——
	// 客户端要靠它区分"服务端不支持这命令"与"网络出错"。
	s := New()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	c := doHandshakeCmd(t, ln.Addr().String(), CmdBind)
	defer c.Close()
	rep, _ := readReply(t, c)
	if rep != RepCommandNotSupported {
		t.Fatalf("rep = 0x%02x, want 0x%02x (command not supported)", rep, RepCommandNotSupported)
	}
}

func TestUDPAssociateRepliesCommandNotSupported(t *testing.T) {
	s := New()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	c := doHandshakeCmd(t, ln.Addr().String(), CmdUDPAssoc)
	defer c.Close()
	rep, _ := readReply(t, c)
	if rep != RepCommandNotSupported {
		t.Fatalf("rep = 0x%02x, want 0x%02x", rep, RepCommandNotSupported)
	}
}

func TestUnsupportedAddrTypeRepliesAddrTypeNotSupported(t *testing.T) {
	s := New()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	c := doHandshakeRawAddrType(t, ln.Addr().String(), 0x09)
	defer c.Close()
	rep, _ := readReply(t, c)
	if rep != RepAddrTypeNotSupported {
		t.Fatalf("rep = 0x%02x, want 0x%02x (addr type not supported)", rep, RepAddrTypeNotSupported)
	}
}

func TestDialFailureRepliesNotReset(t *testing.T) {
	// 拨号失败必须回一帧错误码，不能静默断开。
	s := New()
	s.Dial = func(network, address string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	c := doHandshake(t, ln.Addr().String(), ATypIPv4, "127.0.0.1", 9)
	defer c.Close()
	rep, _ := readReply(t, c)
	if rep == RepSucceeded {
		t.Fatalf("拨号失败却返回成功")
	}
}

func TestWrongVersionRejected(t *testing.T) {
	s := New()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	c, _ := net.Dial("tcp", ln.Addr().String())
	defer c.Close()
	c.Write([]byte{0x04, 0x01, MethodNoAuth}) // SOCKS4
	buf := make([]byte, 1)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(buf); err == nil {
		t.Fatalf("SOCKS4 客户端应被关闭，但读到了数据")
	}
}

// ---------------------------------------------------------------------------
// 工具
// ---------------------------------------------------------------------------

func doHandshakeCmd(t *testing.T, addr string, cmd byte) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c.Write([]byte{Version5, 0x01, MethodNoAuth})
	io.ReadFull(c, make([]byte, 2))
	req := []byte{Version5, cmd, 0x00, ATypIPv4, 0, 0, 0, 0, 0, 0}
	c.Write(req)
	return c
}

func doHandshakeRawAddrType(t *testing.T, addr string, atyp byte) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c.Write([]byte{Version5, 0x01, MethodNoAuth})
	io.ReadFull(c, make([]byte, 2))
	req := []byte{Version5, CmdConnect, 0x00, atyp, 1, 2, 3, 4, 0, 80}
	c.Write(req)
	return c
}

func TestReadAddrRoundTrip(t *testing.T) {
	// readAddr 是协议解析的关键点：IPv6 与域名都必须能被 net 侧复用。
	cases := []struct {
		name string
		atyp byte
		host string
		port int
		want string
	}{
		{"ipv4", ATypIPv4, "10.1.2.3", 1080, "10.1.2.3:1080"},
		{"ipv6", ATypIPv6, "2001:db8::1", 443, "[2001:db8::1]:443"},
		{"domain", ATypDomain, "a.example.com", 80, "a.example.com:80"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var frame []byte
			switch tc.atyp {
			case ATypIPv4:
				frame = append(frame, net.ParseIP(tc.host).To4()...)
			case ATypIPv6:
				frame = append(frame, net.ParseIP(tc.host).To16()...)
			case ATypDomain:
				frame = append(frame, byte(len(tc.host)))
				frame = append(frame, []byte(tc.host)...)
			}
			var pb [2]byte
			binary.BigEndian.PutUint16(pb[:], uint16(tc.port))
			frame = append(frame, pb[:]...)

			got, err := readAddr(bytes.NewReader(frame), tc.atyp)
			if err != nil {
				t.Fatalf("readAddr: %v", err)
			}
			if got != tc.want {
				t.Fatalf("readAddr = %q, want %q", got, tc.want)
			}
			// 必须能被 net.SplitHostPort 还原，否则 Dial 会失败。
			if _, p, err := net.SplitHostPort(got); err != nil || p != strconv.Itoa(tc.port) {
				t.Fatalf("SplitHostPort(%q) = %q, %v", got, p, err)
			}
		})
	}
}

func TestSplitAddr(t *testing.T) {
	if n, a := splitAddr("unix:/tmp/x.sock"); n != "unix" || a != "/tmp/x.sock" {
		t.Fatalf("splitAddr(unix) = %q, %q", n, a)
	}
	if n, a := splitAddr("127.0.0.1:1080"); n != "tcp" || a != "127.0.0.1:1080" {
		t.Fatalf("splitAddr(tcp) = %q, %q", n, a)
	}
}

func TestConcurrentConnections(t *testing.T) {
	// 并发连接不能互相干扰（共享写路径 / 死锁）。
	//
	// 注意：这里**不能**复用 doHandshake —— 它在内部调用 t.Fatalf 与
	// t.Cleanup。从多个 goroutine 里调 t.Fatalf 是 Go 明令禁止的
	// （它会在错误的 goroutine 上 runtime.Goexit），t.Cleanup 的并发注册
	// 也是数据竞争。这类测试初看能过，实则会随机假失败，把门禁变成噪声，
	// 因此并发用例必须自包含：错误用返回值往上传，不用 t.Fatal。
	upstream := newFakeUpstream(t, "R")
	s := New()
	s.Dial = func(network, address string) (net.Conn, error) {
		return net.Dial(network, upstream.addr())
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { s.Close(); ln.Close() })
	go s.Serve(ln)

	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := oneRoundTrip(ln.Addr().String()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("并发连接失败: %v", err)
		}
	}
}

// oneRoundTrip 走完一次握手 + 一次数据往返，错误经返回值传出。
func oneRoundTrip(addr string) error {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := c.Write([]byte{Version5, 0x01, MethodNoAuth}); err != nil {
		return err
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(c, method); err != nil {
		return err
	}
	if method[1] != MethodNoAuth {
		return errRep
	}

	if _, err := c.Write([]byte{Version5, CmdConnect, 0x00, ATypIPv4, 1, 2, 3, 4, 0, 80}); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil {
		return err
	}
	if _, err := readAddr(c, head[3]); err != nil {
		return err
	}
	if head[1] != RepSucceeded {
		return errRep
	}

	if _, err := c.Write([]byte("x")); err != nil {
		return err
	}
	buf := make([]byte, 8)
	if _, err := c.Read(buf); err != nil {
		return err
	}
	return nil
}

var errRep = &repError{}

type repError struct{}

func (e *repError) Error() string { return "rep != succeeded" }
