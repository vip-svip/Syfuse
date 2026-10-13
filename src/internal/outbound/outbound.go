// Package outbound 实现守护进程的**出站拨号器**：把一条已建立的连接
// 送到目标服务器的最后一段。
//
// # 它在链路里的位置
//
//	客户端 → SOCKS5 入站(inbound) → [本包] → 节点服务器 → 目标
//
// 入站负责"把外部流量接进来"，本包负责"按节点协议把它送出去"。
// 两者之间由 DataPlane 的拨号函数衔接 —— 所以本包只暴露一个
// [Dialer] 接口，不关心流量是从 TUN 来的、从 SOCKS5 来的、
// 还是从 HTTP 入站来的。
//
// # 为什么先做这几种
//
// 每种协议的真实用户占比差得很远，按占比排：
//
//	Shadowsocks(AEAD)  最常见，纯 stdlib 可做（crypto/aes、crypto/hkdf；
//	                   chacha20-ietf-poly1305 标准库没有对应包，按 RFC 8439 自实现，见 chacha20poly1305.go）
//	Trojan             TLS + 极简帧，crypto/tls 在 stdlib 里
//	SOCKS5 / HTTP      上游本身是 socks5/http 代理时用（链式代理、自建网关）
//	vmess / vless      需要自研分帧与 AES-CBC/GCM 组装，工作量数倍于上面几种
//
// 前三种均已实现并验证（各自文件的测试含协议帧逐字节断言与自环往返），
// vmess/vless 留作独立单元。
// 这不是"挑简单的做"，而是**让尽可能多的真实节点今天就能通**。
//
// # 零依赖
//
// 只用标准库。tests/run_kernel.sh 对 go.mod 的 require 有断言。
package outbound

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"
)

// ErrUnsupportedProtocol 节点协议没有对应拨号器。
var ErrUnsupportedProtocol = errors.New("outbound: 不支持的节点协议")

// DialTimeout 单次拨号的默认超时。
//
// 取 10s 而不是更长：TCP 握手在大多数网络下 1s 内完成，
// 超时设太长会让"节点挂了"的反馈慢到用户以为 App 卡死。
// TLS 握手额外的时间由 DialTimeout + TLSHandshakeTimeout 分担。
const (
	DialTimeout         = 10 * time.Second
	TLSHandshakeTimeout = 10 * time.Second
)

// Dialer 按给定协议拨号到目标地址。
//
// address 形如 `host:port`（由入站侧解析后传入，协议细节与本包无关）。
type Dialer interface {
	// DialContext 拨号并对（若协议要求）完成握手。
	DialContext(ctx context.Context, network, address string) (net.Conn, error)

	// Name 用于日志与排障：显示"这个节点走的是哪条出站路径"。
	Name() string
}

// Config 描述一个节点的出站参数。
//
// 字段与 Kotlin 侧 ProxyNode 对齐（protocol/server/port/credential）。
// 刻意用扁平结构而不是 map[string]string：后者会把"字段名拼错"
// 从编译期错误推迟到运行期，而这里的每个字段都会被写进协议帧。
type Config struct {
	Protocol string // ss / trojan / socks5 / http / direct
	Server   string
	Port     int

	// Password 是 ss/trojan 的口令（vmess 的 UUID 不在此列）。
	Password string

	// Cipher 是 ss 的加密方式，如 aes-128-gcm / chacha20-ietf-poly1305。
	Cipher string

	// SNI 与 Insecure 用于 trojan 的 TLS。
	SNI      string
	Insecure bool
}

// ---------------------------------------------------------------------------
// 直连
// ---------------------------------------------------------------------------

// DirectDialer 直连，不经过任何代理。
//
// 存在的意义不只是"直连模式"：它还是**测试与排障的基线**。
// 判断"是节点挂了还是我的链路错了"时，先用它对比一次就够了。
type DirectDialer struct {
	Timeout time.Duration
}

func (d *DirectDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DialTimeout
	}
	dialer := &net.Dialer{Timeout: timeout}
	return dialer.DialContext(ctx, network, address)
}

func (d *DirectDialer) Name() string { return "direct" }

// ---------------------------------------------------------------------------
// SOCKS5 上游
// ---------------------------------------------------------------------------

// Socks5Dialer 以上游 SOCKS5 服务器为出站（链式代理 / 自建网关）。
type Socks5Dialer struct {
	Server   string
	Port     int
	Username string
	Password string
	Timeout  time.Duration
}

func (d *Socks5Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("socks5 出站只支持 tcp，收到 %q", network)
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DialTimeout
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(d.Server, itoa(d.Port)))
	if err != nil {
		return nil, err
	}

	if err := socks5Handshake(conn, address, d.Username, d.Password, timeout); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (d *Socks5Dialer) Name() string { return "socks5" }

// socks5Handshake 走完方法协商 + CONNECT，读取应答。
//
// 单独抽成函数（而不是复用 internal/socks5 的服务端代码）：
// 客户端侧只发不收认证、只写读答，逻辑与服务端不同源；
// 硬凑成共享代码会让两边互相牵制。
func socks5Handshake(conn net.Conn, target, user, pass string, timeout time.Duration) error {
	_ = conn.SetDeadline(time.Now().Add(timeout))

	var method byte = 0x00
	if user != "" {
		method = 0x02
	}
	if _, err := conn.Write([]byte{0x05, 0x01, method}); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := readFull(conn, resp); err != nil {
		return err
	}
	if resp[1] == 0xFF {
		return errors.New("socks5 上游拒绝了全部认证方法")
	}

	if resp[1] == 0x02 {
		auth := []byte{0x01, byte(len(user))}
		auth = append(auth, []byte(user)...)
		auth = append(auth, byte(len(pass)))
		auth = append(auth, []byte(pass)...)
		if _, err := conn.Write(auth); err != nil {
			return err
		}
		authResp := make([]byte, 2)
		if _, err := readFull(conn, authResp); err != nil {
			return err
		}
		if authResp[1] != 0x00 {
			return errors.New("socks5 上游认证失败")
		}
	}

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return err
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
		if len(host) > 255 {
			return errors.New("socks5 域名过长")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	var portBytes [2]byte
	portBytes[0] = byte(atoi(portStr) >> 8)
	portBytes[1] = byte(atoi(portStr))
	req = append(req, portBytes[:]...)
	if _, err := conn.Write(req); err != nil {
		return err
	}

	head := make([]byte, 4)
	if _, err := readFull(conn, head); err != nil {
		return err
	}
	if head[1] != 0x00 {
		return fmt.Errorf("socks5 上游应答 0x%02x", head[1])
	}
	// bound 地址必须读走，否则残留字节会被当成业务数据。
	if err := skipAddr(conn, head[3]); err != nil {
		return err
	}
	// 握手完成后撤掉截止时间：长连接不能被掐断。
	return conn.SetDeadline(time.Time{})
}

// ---------------------------------------------------------------------------
// HTTP CONNECT 上游
// ---------------------------------------------------------------------------

// HttpConnectDialer 以上游 HTTP 代理为出站（CONNECT 隧道）。
type HttpConnectDialer struct {
	Server   string
	Port     int
	Username string
	Password string
	Timeout  time.Duration
}

func (d *HttpConnectDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("http 出站只支持 tcp，收到 %q", network)
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DialTimeout
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(d.Server, itoa(d.Port)))
	if err != nil {
		return nil, err
	}
	if err := httpConnect(conn, address, d.Username, d.Password, timeout); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (d *HttpConnectDialer) Name() string { return "http" }

// httpConnect 发 CONNECT 并校验应答。
//
// 只认 2xx：3xx 会让代理把客户端重定向到别处，那对隧道语义是错的；
// 4xx/5xx 必须是失败。读状态行后把响应头读完（可能有 body，隧道下没有）。
func httpConnect(conn net.Conn, target, user, pass string, timeout time.Duration) error {
	_ = conn.SetDeadline(time.Now().Add(timeout))

	req := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if user != "" {
		// Basic 认证。凭据只在这条本地/上游链路上出现一次。
		req += "Proxy-Authorization: Basic " + basicAuth(user, pass) + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		return err
	}

	line, err := readLine(conn)
	if err != nil {
		return err
	}
	// 形如 "HTTP/1.1 200 Connection established"
	if len(line) < 12 || line[:7] != "HTTP/1." {
		return fmt.Errorf("http 上游应答异常: %q", line)
	}
	code := line[9:12]
	if code[0] != '2' {
		return fmt.Errorf("http 上游拒绝 CONNECT: %s", line)
	}

	// 读完剩余的响应头，直到空行。
	for {
		h, err := readLine(conn)
		if err != nil {
			return err
		}
		if h == "" {
			break
		}
	}
	return conn.SetDeadline(time.Time{})
}

// ---------------------------------------------------------------------------
// Trojan 出站
// ---------------------------------------------------------------------------

// TrojanDialer 以 Trojan 协议为出站。
//
// # 协议帧（按 trojan-gfw/trojan-go 的行为）
//
//	TLS 连接建立后，客户端发：
//	  hex(SHA224(password)) CRLF
//	  <CRLF>                         // trojan 请求：CMD=CONNECT, 地址, 端口
//	  <CRLF>                         // 载荷开始
//	之后是原始 TCP 流。
//
// 这个协议本身极简，复杂度全在 TLS 上 —— 而 crypto/tls 是 stdlib，
// 所以整个拨号器不需要任何第三方依赖。
type TrojanDialer struct {
	Server   string
	Port     int
	Password string
	SNI      string
	Insecure bool
	Timeout  time.Duration
}

func (d *TrojanDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("trojan 出站只支持 tcp，收到 %q", network)
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DialTimeout
	}
	dialer := &net.Dialer{Timeout: timeout}
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(d.Server, itoa(d.Port)))
	if err != nil {
		return nil, err
	}

	sni := d.SNI
	if sni == "" {
		sni = d.Server
	}
	tlsConn := tls.Client(raw, &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: d.Insecure, // 用户显式开启 allowInsecure 时才为真
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2", "http/1.1"},
	})
	// TLS 握手要给独立超时：它与 TCP 拨号是两段不同的等待。
	handshakeCtx, cancel := context.WithTimeout(ctx, TLSHandshakeTimeout)
	defer cancel()
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		raw.Close()
		return nil, err
	}

	if err := writeTrojanRequest(tlsConn, d.Password, address); err != nil {
		tlsConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (d *TrojanDialer) Name() string { return "trojan" }

// writeTrojanRequest 写 trojan 的 CONNECT 请求帧。
func writeTrojanRequest(conn net.Conn, password, address string) error {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}

	frame := make([]byte, 0, 128)
	frame = append(frame, []byte(hexSha224(password))...)
	frame = append(frame, '\r', '\n')

	// trojan 请求行：CMD(1) + 地址类型(1) + 地址 + 端口(2) + CRLF
	frame = append(frame, 0x01) // CMD = CONNECT
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			frame = append(frame, 0x01)
			frame = append(frame, v4...)
		} else {
			frame = append(frame, 0x04)
			frame = append(frame, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return errors.New("trojan 域名过长")
		}
		frame = append(frame, 0x03, byte(len(host)))
		frame = append(frame, []byte(host)...)
	}
	var portBytes [2]byte
	portBytes[0] = byte(atoi(portStr) >> 8)
	portBytes[1] = byte(atoi(portStr))
	frame = append(frame, portBytes[:]...)
	frame = append(frame, '\r', '\n', '\r', '\n')

	_, err = conn.Write(frame)
	return err
}

// ---------------------------------------------------------------------------
// 工厂
// ---------------------------------------------------------------------------

// NewDialer 按节点配置选出拨号器。
//
// 未知协议返回 [ErrUnsupportedProtocol] 而不是回落到直连 ——
// 回落会让"这个节点的协议还不支持"伪装成"节点连上了但没流量"，
// 那种故障形态比明确的报错难查十倍。
func NewDialer(cfg Config) (Dialer, error) {
	switch cfg.Protocol {
	case "direct", "":
		return &DirectDialer{}, nil
	case "socks5", "socks":
		return &Socks5Dialer{Server: cfg.Server, Port: cfg.Port, Username: "", Password: cfg.Password}, nil
	case "http", "https":
		return &HttpConnectDialer{Server: cfg.Server, Port: cfg.Port, Username: "", Password: cfg.Password}, nil
	case "trojan", "trojan-go":
		return &TrojanDialer{
			Server: cfg.Server, Port: cfg.Port, Password: cfg.Password,
			SNI: cfg.SNI, Insecure: cfg.Insecure,
		}, nil
	case "ss", "shadowsocks":
		// "shadowsocks" 是同一协议的完整拼写：节点配置里两种都有人写。
		return &SsDialer{Server: cfg.Server, Port: cfg.Port, Password: cfg.Password, Cipher: cfg.Cipher}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProtocol, cfg.Protocol)
	}
}
