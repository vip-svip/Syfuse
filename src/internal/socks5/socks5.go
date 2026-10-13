// Package socks5 实现 RFC 1928 的 SOCKS5 服务端，作为本守护进程的**本地入站**。
//
// # 为什么需要它
//
// 守护进程此前只有 IPC（UDS 控制通道），没有任何"把外部流量接进来"的入口。
// 没有入站，内核再强也只是一个死进程：用户选了节点、点了连接，
// 却没有任何字节流经它。本包补上这一环，而且是**今天就能用**的那一环：
// 任何应用把代理指向本服务，流量就会流经守护进程并走所选节点出去。
//
// # 为什么先做 SOCKS5 而不是 TUN
//
// TUN 模式需要一个用户态 TCP/IP 栈（gVisor / lwIP 级别）把原始 IP 包
// 还原成流。那是 sing-box、mihomo 内核里各有一万行量级的组件，
// 在本模块"零第三方依赖"的约束下从零写不现实。
//
// 而 SOCKS5 入站是**同一块数据的另一侧**：sing-box 的架构正是
// `TUN(netstack) → inbound(socks/http) → outbound(协议)`。
// 先把 inbound 做扎实并验证，后续上 TUN 时只需要替换最前面那段。
//
// # 零依赖
//
// 只用标准库 net。不使用 golang.org/x/net 等任何第三方包 ——
// tests/run_kernel.sh 对 go.mod 的 `require` 与 go.sum 有断言。
package socks5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// SOCKS5 版本号。
const Version5 = 0x05

// 认证方法（RFC 1928 第 3 节）。
const (
	MethodNoAuth   = 0x00 // 无需认证
	MethodUserPass = 0x02 // 用户名/口令（RFC 1929）
	MethodNone     = 0xFF // 无可接受的方法
)

// 命令。
const (
	CmdConnect  = 0x01
	CmdBind     = 0x02
	CmdUDPAssoc = 0x03
)

// 地址类型。
const (
	ATypIPv4   = 0x01
	ATypDomain = 0x03
	ATypIPv6   = 0x06
)

// 应答字段（RFC 1928 第 6 节）。**必须如实返回**，
// 调用方（客户端）会据此判断"是目标拒绝还是协议出错"。
const (
	RepSucceeded            = 0x00
	RepGeneralFailure       = 0x01
	RepNotAllowedByRuleset  = 0x02
	RepNetworkUnreachable   = 0x03
	RepHostUnreachable      = 0x04
	RepConnectionRefused    = 0x05
	RepTTLExpired           = 0x06
	RepCommandNotSupported  = 0x07
	RepAddrTypeNotSupported = 0x08
)

// 读超时。只用于握手阶段：握手完成后双向流转发不设超时，
// 长连接（WebSocket、流媒体）不能被读超时掐断。
const handshakeTimeout = 10 * time.Second

var (
	// ErrAuthFailed 用户名/口令校验失败。
	ErrAuthFailed = errors.New("socks5: 认证失败")
	// ErrNoAcceptableMethod 客户端提出的认证方法本服务都不支持。
	ErrNoAcceptableMethod = errors.New("socks5: 无可接受的认证方法")
)

// Credentials 登录凭据校验。返回 false 表示拒绝。
//
// 传 nil 表示只允许"无需认证"——本服务默认监听回环/本地 UDS，
// 不假设存在远端攻击者，但也**不主动放开**：需要口令时由调用方注入。
type Credentials func(user, password string) bool

// Server 是一个 SOCKS5 服务端。
type Server struct {
	// Auth 可选的认证校验。nil 时只通告 MethodNoAuth。
	Auth Credentials

	// Dial 拨号函数。默认 net.Dial。抽出来是为了测试时注入假拨号，
	// 避免单元测试真的连外网。
	Dial func(network, address string) (net.Conn, error)

	// Logger 可选。nil 时不记录。
	Logger func(format string, args ...any)

	// MaxHandshakeBytes 单次握手上限，防止对端发无限数据耗尽内存。
	MaxHandshakeBytes int

	mu       sync.Mutex
	closed   bool
	listener net.Listener
	wg       sync.WaitGroup
}

// New 构造一个默认配置的服务。
func New() *Server {
	return &Server{
		Dial:              net.Dial,
		MaxHandshakeBytes: 4 * 1024,
	}
}

// ListenAndServe 在 addr 上监听并服务，直到 Close。
//
// addr 形如 `127.0.0.1:1080` 或 `unix:/path/to.sock`。
// 监听单位是调用方的选择 —— 守护进程默认用 UDS（不暴露 TCP 端口，
// 同机其他应用无法蹭代理）。
func (s *Server) ListenAndServe(addr string) error {
	network, address := splitAddr(addr)
	ln, err := net.Listen(network, address)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve 在已有监听器上服务。
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("socks5: 服务已关闭")
	}
	s.listener = ln
	s.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			// 瞬时错误（对端在 accept 前断开、fd 用尽等）继续服务，
			// 不因单次失败退出整个入站。
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(conn)
		}()
	}
}

// log 记录（Logger 为 nil 时静默）。
func (s *Server) log(format string, args ...any) {
	if s.Logger != nil {
		s.Logger(format, args...)
	}
}

// Close 停止接受新连接并等待在途连接结束。
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.listener
	s.mu.Unlock()
	var err error
	if ln != nil {
		err = ln.Close()
	}
	s.wg.Wait()
	return err
}

// Addr 返回实际监听地址；未监听时返回空串。
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// handle 服务一个客户端连接。
//
// 注意收尾方式：**协商/请求阶段出错时用 finish，不能用裸 Close**。
// 原因见 finish 的注释 —— 裸 Close 会让刚写好的错误应答被 RST 冲掉。
func (s *Server) handle(client net.Conn) {
	if err := client.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		finish(client)
		return
	}

	if err := s.negotiateAuth(client); err != nil {
		finish(client)
		return
	}

	target, err := s.readRequest(client)
	if err != nil {
		finish(client)
		return
	}

	// 握手完成，撤掉截止时间：长连接不能被掐断。
	if err := client.SetDeadline(time.Time{}); err != nil {
		return
	}

	dial := s.Dial
	if dial == nil {
		dial = net.Dial
	}
	upstream, err := dial("tcp", target)
	if err != nil {
		// 如实区分"目标拒了"与"网络不可达" —— 客户端要靠这个区分
		// "这个节点挂了"与"我的网络断了"，笼统返回 general 会让排障变成测。
		if werr := s.writeReply(client, classifyDialError(err), nil); werr != nil {
			s.log("写失败应答出错: %v", werr)
		}
		finish(client)
		return
	}
	defer upstream.Close()

	if err := s.writeReply(client, RepSucceeded, upstream.LocalAddr()); err != nil {
		return
	}

	relay(client, upstream)
}

// finish 协议出错后的收尾。
//
// # 为什么不能裸 Close
//
// Windows 的 TCP 栈有一条行为：**close 时若接收缓冲里还有未读数据，
// 就发 RST 而不是 FIN**。而协议出错的场景恰恰是这个状态 ——
// 我们只读掉了请求的前 4 字节，剩下的（错误的地址字段、多余字节）
// 还原样躺在缓冲里。此时裸 Close：
//
//	客户端先读到 RST → 连接被重置 → **刚写好的错误应答还没被读走就没了**；
//	用户侧看到的现象是"连接被重置"，而不是"地址类型不支持"。
//
// 这就是"报错报不出来"的标准成因，也是本函数存在的唯一理由。
//
// # 做法
//
//  1. CloseWrite —— 发 FIN 让客户端读到 EOF，FIN 不会冲掉已写数据；
//  2. 限时 drain —— 把缓冲里剩下的请求字节读掉，消掉 RST 的触发条件；
//  3. 再 Close。
//
// 第 2 步的时限必须短（200ms）：它只是排空，不该拖长一个已经失败的连接。
func finish(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, _ = io.Copy(io.Discard, c)
	_ = c.Close()
}

// relay 双向转发，直到任一方向结束。
//
// 语义要点（这些是"连接看起来通了但几秒后卡死"类 bug 的根因）：
//   - 两个方向各自一个 goroutine，互不等候；
//   - 任一方向结束后**必须关掉另一侧**，否则对端会一直等 EOF，
//     表现为"下载停在 99%"；
//   - 用 io.Copy 的返回判断结束，不假设读错误一定是异常 ——
//     EOF 与"连接被重置"都是正常的结束原因。
func relay(client, upstream net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(upstream, client)
		// 客户端不再发数据：让上游看到 EOF，它才能收尾。
		if c, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(client, upstream)
		if c, ok := client.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}()

	wg.Wait()
}

// negotiateAuth 完成方法协商与可选的用户口令认证。
func (s *Server) negotiateAuth(client net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil {
		return err
	}
	if header[0] != Version5 {
		return fmt.Errorf("socks5: 版本号 %d 不是 5", header[0])
	}
	nMethods := int(header[1])
	if nMethods == 0 || nMethods > 255 {
		return errors.New("socks5: 认证方法数量非法")
	}
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(client, methods); err != nil {
		return err
	}

	var chosen byte = MethodNone
	allowNoAuth := s.Auth == nil
	for _, m := range methods {
		if m == MethodNoAuth && allowNoAuth {
			chosen = MethodNoAuth
			break
		}
		if m == MethodUserPass && s.Auth != nil {
			chosen = MethodUserPass
			break
		}
	}
	if _, err := client.Write([]byte{Version5, chosen}); err != nil {
		return err
	}
	if chosen == MethodNone {
		return ErrNoAcceptableMethod
	}
	if chosen == MethodUserPass {
		return s.authUserPass(client)
	}
	return nil
}

// authUserPass 处理 RFC 1929 的用户名/口令认证。
func (s *Server) authUserPass(client net.Conn) error {
	ver := make([]byte, 2)
	if _, err := io.ReadFull(client, ver); err != nil {
		return err
	}
	if ver[0] != 0x01 {
		return fmt.Errorf("socks5: 认证子协商版本 %d 非法", ver[0])
	}
	userLen := int(ver[1])
	user := make([]byte, userLen)
	if _, err := io.ReadFull(client, user); err != nil {
		return err
	}
	pwLenBuf := make([]byte, 1)
	if _, err := io.ReadFull(client, pwLenBuf); err != nil {
		return err
	}
	password := make([]byte, int(pwLenBuf[0]))
	if _, err := io.ReadFull(client, password); err != nil {
		return err
	}

	ok := s.Auth != nil && s.Auth(string(user), string(password))
	if !ok {
		// 失败也要按协议回一帧（0x01 = 失败），不能直接断开 ——
		// 直接断开会让客户端看到"连接被重置"而不是"密码错了"。
		client.Write([]byte{0x01, 0x01})
		return ErrAuthFailed
	}
	_, err := client.Write([]byte{0x01, 0x00})
	return err
}

// readRequest 读取 CONNECT 请求，返回要拨的目标地址。
func (s *Server) readRequest(client net.Conn) (string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(client, head); err != nil {
		return "", err
	}
	if head[0] != Version5 {
		return "", fmt.Errorf("socks5: 请求版本号 %d 非法", head[0])
	}
	cmd := head[1]
	addr, err := readAddr(client, head[3])
	if err != nil {
		// 地址解析失败同样要回一帧。直接断开会让客户端看到
		// "连接被重置"，无法与网络故障区分。
		if werr := s.writeReply(client, RepAddrTypeNotSupported, nil); werr != nil {
			s.log("写地址类型应答出错: %v", werr)
		}
		return "", err
	}

	// BIND 与 UDP ASSOCIATE 本服务不支持，如实返回 command not supported。
	// 伪装成通用失败会让客户端误以为是网络问题。
	if cmd != CmdConnect {
		if err := s.writeReply(client, RepCommandNotSupported, nil); err != nil {
			return "", err
		}
		return "", fmt.Errorf("socks5: 不支持的命令 0x%02x", cmd)
	}
	return addr, nil
}

// readAddr 按 SOCKS5 地址格式读取 host:port。
func readAddr(r io.Reader, atyp byte) (string, error) {
	switch atyp {
	case ATypIPv4:
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		ip := net.IP(buf[:4])
		port := binary.BigEndian.Uint16(buf[4:6])
		return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), nil

	case ATypIPv6:
		buf := make([]byte, 16+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", err
		}
		ip := net.IP(buf[:16])
		port := binary.BigEndian.Uint16(buf[16:18])
		return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), nil

	case ATypDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return "", err
		}
		n := int(lenBuf[0])
		if n == 0 {
			return "", errors.New("socks5: 域名为空")
		}
		domain := make([]byte, n)
		if _, err := io.ReadFull(r, domain); err != nil {
			return "", err
		}
		portBuf := make([]byte, 2)
		if _, err := io.ReadFull(r, portBuf); err != nil {
			return "", err
		}
		port := binary.BigEndian.Uint16(portBuf)
		// 用 JoinHostPort 而不是手工拼：域名含冒号（IPv6 字面量）时
		// 手工拼出来的串 net.Dial 解析不了。
		return net.JoinHostPort(string(domain), strconv.Itoa(int(port))), nil

	default:
		return "", fmt.Errorf("socks5: 不支持的地址类型 0x%02x", atyp)
	}
}

// writeReply 写应答帧。boundAddr 为 nil 时填全零地址。
func (s *Server) writeReply(w io.Writer, rep byte, boundAddr net.Addr) error {
	host, portStr := "0.0.0.0", "0"
	if boundAddr != nil {
		h, p, err := net.SplitHostPort(boundAddr.String())
		if err == nil {
			host, portStr = h, p
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		port = 0
	}

	ip := net.ParseIP(host)
	var frame []byte
	switch {
	case ip == nil:
		// 域名形式的 bound 地址：按域名编码。
		frame = append(frame, Version5, rep, 0x00, ATypDomain, byte(len(host)))
		frame = append(frame, []byte(host)...)
	case ip.To4() != nil:
		frame = append(frame, Version5, rep, 0x00, ATypIPv4)
		frame = append(frame, ip.To4()...)
	default:
		frame = append(frame, Version5, rep, 0x00, ATypIPv6)
		frame = append(frame, ip.To16()...)
	}
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	frame = append(frame, portBytes[:]...)
	_, err = w.Write(frame)
	return err
}

// classifyDialError 把拨号错误映射为 SOCKS5 应答码。
func classifyDialError(err error) byte {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "dial" {
			return RepNetworkUnreachable
		}
	}
	// 连接被拒是"目标明确说不"，与"根本到不了"是两回事。
	msg := err.Error()
	if containsAny(msg, "connection refused", "拒接", "拒绝连接") {
		return RepConnectionRefused
	}
	return RepGeneralFailure
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// splitAddr 把 "unix:/path" 与 "host:port" 统一成 (network, address)。
func splitAddr(addr string) (string, string) {
	const prefix = "unix:"
	if len(addr) > len(prefix) && addr[:len(prefix)] == prefix {
		return "unix", addr[len(prefix):]
	}
	return "tcp", addr
}
