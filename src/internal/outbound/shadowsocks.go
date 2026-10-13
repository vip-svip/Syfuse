package outbound

// ---------------------------------------------------------------------------
// Shadowsocks（AEAD）出站
// ---------------------------------------------------------------------------
//
// # 协议（与 shadowsocks/go-shadowsocks2 的 shadowaead 包逐条对齐）
//
//	连接建立后，客户端先发 salt（SaltSize 字节，crypto/rand 随机），
//	之后是任意多条记录，每条：
//
//	    [2 字节大端长度（密文）][长度明文+tag][载荷密文][载荷 tag]
//
//	两个 AEAD 操作（先封长度、再封载荷）各消耗一个 nonce；nonce 是小端
//	计数，从全零开始，每次操作 +1，溢出回绕。读侧按掩码 0x3FFF 取长度
//	（高 2 位保留，参考实现的 payloadSizeMask）。
//
//	主密钥 psk  = EVP_BytesToKey-MD5(password, keySize)
//	会话子密钥   = HKDF-SHA1(psk, salt, info="ss-subkey", keySize)
//
//	除此之外，TCP 流的**第一个记录**是 SOCKS5 格式的目标地址
//	（ATYP + DST.ADDR + DST.PORT）。参考 tcp.go 的 tcpLocal：先
//	rc.Write(tgt) 再 relay；服务端 tcpRemote 用 socks.ReadAddr 解出
//	转发目标。少了这一帧，真实节点不知道把解密后的流量送去哪里。
//
// # 与参考实现的差异（逐条写明理由）
//
//   - 用标准库 crypto/hkdf（Go 1.24 起进入标准库，工具链 go1.26.9
//     实测可用）而不是手写 expand：它与参考的
//     golang.org/x/crypto/hkdf + sha1.New 语义逐字节一致（测试里用
//     手写 expand 与 Node/OpenSSL 双路交叉验证）。x/crypto 被零依赖
//     约束禁止。
//   - 支持 aes-192-gcm：SIP003 与 ss-libev / v2ray 都有它；go-shadowsocks2
//     master 的 aeadList 只列了三个，属于漏列而非有意排除。
//   - 不复制 internal.AddSalt/CheckSalt 的 salt 过滤器：那是**服务端**
//     侧重放检测（bloomring）。本拨号器是客户端角色，crypto/rand 产出
//     128 位以上 salt，碰撞概率可忽略。
//   - cipher 名大小写不敏感（参考用 ToUpper，等效）。
//   - 空口令在拨号前直接拒绝：节点没配口令是误配，不该 KDF 出一个
//     "能连上但服务端必拒"的会话。
//   - legacy 流加密显式拒绝并给专用错误（参考返回笼统的
//     ErrCipherNotSupported）——见 ErrNonAEADCipher。
//   - 不做 UDP：本包 Dialer 只服务 tcp；参考的 shadowaead/packet.go
//     是另一条独立代码路径。
//   - 不串行化并发写（与参考一致）：使用方式是每条连接一个方向的
//     io.Copy 泵，单写者。

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/vip-svip/syfuse/src/internal/third_party/chacha20poly1305/chacha20poly1305"
)

// ssMaxPayload 单条记录的明文上限（参考 shadowaead/stream.go 的
// payloadSizeMask = 0x3FFF = 16383）。长度字段高 2 位保留，读侧掩掉。
const ssMaxPayload = 0x3FFF

// ssSubkeyInfo HKDF 的 info 字段（参考 shadowaead/cipher.go）。
const ssSubkeyInfo = "ss-subkey"

// ---------------------------------------------------------------------------
// 错误哨兵
// ---------------------------------------------------------------------------

// ErrNonAEADCipher 节点配了 Shadowsocks 的**流加密**（legacy stream cipher：
// cfb/ctr/ofb/rc4/不带 poly1305 的 chacha20 等）。
//
// 这类加密没有完整性保护，可被重放与主动探测，SIP004 起被主流实现废弃。
// 明确报错而不是"想别的办法"：静默回落明文或直连，会把"节点配置不可用"
// 伪装成"连上了但没流量"，那种故障形态比报错难查十倍。
var ErrNonAEADCipher = errors.New("outbound: ss 使用了已废弃的非 AEAD 流加密")

// ErrUnsupportedCipher cipher 名不在受支持的 AEAD 表内：拼错、2022 版
// 方法（2022-blake3-*，密钥派生与握手机制都不同）、或标准库无法构造的
// AEAD（如 xchacha20-ietf-poly1305，XChaCha20 不在 crypto/cipher 里）。
var ErrUnsupportedCipher = errors.New("outbound: 不支持的 ss 加密方式")

// ErrMissingPassword ss 节点没配口令。
var ErrMissingPassword = errors.New("outbound: ss 节点缺少口令")

// ---------------------------------------------------------------------------
// 口令派生
// ---------------------------------------------------------------------------

// ssKdf 口令 → 主密钥：原版 Shadowsocks 的 EVP_BytesToKey 变体
// （MD5 链式，参考 core/cipher.go 的 kdf）。
//
// 这是 SIP003 为 AEAD 规定的口令派生：ss-libev / shadowsocks-android /
// Outline 逐字节一致。换成"口令直接当密钥"或 PBKDF2 都会与真实节点
// 不兼容——那种不兼容只会在"连上后第一个记录就被拒"时暴露。
func ssKdf(password string, keyLen int) []byte {
	var b, prev []byte
	h := md5.New()
	for len(b) < keyLen {
		h.Write(prev)
		h.Write([]byte(password))
		b = h.Sum(b)
		prev = b[len(b)-h.Size():]
		h.Reset()
	}
	return b[:keyLen]
}

// ssSubkey 主密钥 + salt → 会话子密钥：HKDF-SHA1，info="ss-subkey"。
//
// 每次连接的 salt 不同 ⇒ 子密钥不同 ⇒ 同一条明文在不同连接上的密文
// 不同，这是 ss 抗流量关联的基础。
func ssSubkey(psk, salt []byte, keyLen int) ([]byte, error) {
	return hkdf.Key(sha1.New, psk, salt, ssSubkeyInfo, keyLen)
}

// ---------------------------------------------------------------------------
// 加密方式表
// ---------------------------------------------------------------------------

// ssCipherDef 一个受支持的 AEAD 加密方式。
type ssCipherDef struct {
	keySize int
	newAEAD func(key []byte) (cipher.AEAD, error)
}

// saltSize 与参考 shadowaead/cipher.go 的 metaCipher.SaltSize 一致：
// 密钥 ≤16 字节时 salt 取 16 字节，否则与密钥等长。本表的密钥长度
// 只有 16/24/32 三种，因此这里恒等于 keySize。
func (d *ssCipherDef) saltSize() int {
	if d.keySize > 16 {
		return d.keySize
	}
	return 16
}

func aesGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// ssAEADCiphers 受支持的 AEAD 加密方式表（SIP003）。
//
// chacha20-ietf-poly1305 即 RFC 8439 的 ChaCha20-Poly1305：**标准库里
// 没有这个包**（它属于 golang.org/x/crypto，而本模块被"零第三方依赖"
// 约束禁止引入外部模块），所以那份实现被原样移植到了
// internal/third_party/chacha20poly1305 —— 移植过程只改了两件事：
// import 路径、以及剥掉汇编构建标签（汇编路径全部未启用，纯 Go 分支）。
// RFC 8439 官方向量的逐字节验证见该包的 vendor_vectors_test.go。
// 以前这里是一份手写的 AEAD，已删除：同一份语义有两份实现，
// 其中一份没有权威向量锚定时，"两份一致"什么都证明不了。
var ssAEADCiphers = map[string]*ssCipherDef{
	"aes-128-gcm":            {keySize: 16, newAEAD: aesGCM},
	"aes-192-gcm":            {keySize: 24, newAEAD: aesGCM},
	"aes-256-gcm":            {keySize: 32, newAEAD: aesGCM},
	"chacha20-ietf-poly1305": {keySize: 32, newAEAD: chacha20poly1305.New},
}

// ssLegacyCiphers 常见的已废弃流加密名单。
//
// 列出来不是为了支持，而是为了把错误从"不认识这个字符串"变成"这是
// 已废弃的不安全加密方式"——前者会让人以为是拼写错误。
var ssLegacyCiphers = map[string]bool{
	"aes-128-cfb": true, "aes-192-cfb": true, "aes-256-cfb": true,
	"aes-128-cfb1": true, "aes-192-cfb1": true, "aes-256-cfb1": true,
	"aes-128-cfb8": true, "aes-192-cfb8": true, "aes-256-cfb8": true,
	"aes-128-ctr": true, "aes-192-ctr": true, "aes-256-ctr": true,
	"aes-128-ofb": true, "aes-192-ofb": true, "aes-256-ofb": true,
	"bf-cfb": true, "cast5-cfb": true, "des-cfb": true, "idea-cfb": true,
	"seed-cfb":         true,
	"camellia-128-cfb": true, "camellia-192-cfb": true, "camellia-256-cfb": true,
	"chacha20": true, "chacha20-ietf": true, "salsa20": true,
	"rc4": true, "rc4-md5": true, "rc4-md5-6": true,
}

// pickSsCipher 把配置里的加密方式名解析成表项。
//
// 校验放在**拨号之前**：节点配错加密方式必须立刻可见，而不是连上去
// 之后才在第一个记录上失败——后者看起来像"网络问题"。
func pickSsCipher(name string) (*ssCipherDef, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil, fmt.Errorf("%w: 加密方式不能为空（需要 aes-256-gcm / chacha20-ietf-poly1305 等 AEAD）", ErrUnsupportedCipher)
	}
	if def, ok := ssAEADCiphers[key]; ok {
		return def, nil
	}
	if ssLegacyCiphers[key] {
		return nil, fmt.Errorf("%w: %q 是无完整性保护的流加密，请换用 aes-256-gcm / chacha20-ietf-poly1305", ErrNonAEADCipher, name)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnsupportedCipher, name)
}

// ---------------------------------------------------------------------------
// 目标地址编码
// ---------------------------------------------------------------------------

// ssSocksAddr 把 host:port 编码成 SOCKS5 地址（参考 socks/socks.go 的
// ParseAddr）：ATYP(1) + DST.ADDR + DST.PORT(2)。
//
// ATYP: 0x01=IPv4, 0x03=域名, 0x04=IPv6。域名按字面量编码（不做解析），
// 与参考一致：域名由 ss 服务端解析，客户端多解析一次反而可能和服务端
// 看到不同的 A 记录。
func ssSocksAddr(address string) ([]byte, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("ss: 目标端口 %q 非法", portStr)
	}
	var out []byte
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			out = make([]byte, 1+net.IPv4len+2)
			out[0] = 0x01
			copy(out[1:], v4)
		} else {
			out = make([]byte, 1+net.IPv6len+2)
			out[0] = 0x04
			copy(out[1:], ip.To16())
		}
	} else {
		if len(host) > 255 {
			return nil, errors.New("ss: 目标域名过长")
		}
		out = make([]byte, 1+1+len(host)+2)
		out[0] = 0x03
		out[1] = byte(len(host))
		copy(out[2:], host)
	}
	out[len(out)-2] = byte(port >> 8)
	out[len(out)-1] = byte(port)
	return out, nil
}

// ---------------------------------------------------------------------------
// AEAD 流封装
// ---------------------------------------------------------------------------

// ssIncrementNonce 小端递增 nonce，溢出回绕（参考 stream.go 的 increment）。
func ssIncrementNonce(nonce []byte) {
	for i := range nonce {
		nonce[i]++
		if nonce[i] != 0 {
			return
		}
	}
}

// ssConn 把一条 TCP 连接封装成 ss AEAD 流。
//
// 读写两侧各自惰性初始化（首次 Read/Write 时才生成或读取 salt），与参考
// 的 streamConn 一致：连接建立后不主动发任何字节，直到真有数据要过。
type ssConn struct {
	net.Conn

	psk []byte       // 主密钥（口令 KDF 而来）
	def *ssCipherDef // 加密方式

	// addr 是目标地址的 SOCKS5 编码，作为第一个记录发出。
	addr     []byte
	addrSent bool

	// 写侧状态（惰性初始化）
	wAEAD  cipher.AEAD
	wNonce []byte
	wbuf   []byte

	// 读侧状态（惰性初始化）
	rAEAD    cipher.AEAD
	rNonce   []byte
	rbuf     []byte
	leftover []byte // 上一条记录未被读完的余量（指向 rbuf）
}

func (c *ssConn) initWriter() error {
	salt := make([]byte, c.def.saltSize())
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("ss: 生成 salt 失败: %w", err)
	}
	subkey, err := ssSubkey(c.psk, salt, c.def.keySize)
	if err != nil {
		return fmt.Errorf("ss: 派生会话密钥失败: %w", err)
	}
	aead, err := c.def.newAEAD(subkey)
	if err != nil {
		return fmt.Errorf("ss: 构造 AEAD 失败: %w", err)
	}
	// salt 必须是连接上的第一个字节（参考 stream.go 的 initWriter）。
	if _, err := c.Conn.Write(salt); err != nil {
		return err
	}
	c.wAEAD = aead
	c.wNonce = make([]byte, aead.NonceSize())
	c.wbuf = make([]byte, 0, 2+2*aead.Overhead()+ssMaxPayload)
	return nil
}

// writeChunks 把 p 切成 ≤ ssMaxPayload 的记录写出去。
// 返回值是**已消费的 p 字节数**，出错时用于如实记账。
func (c *ssConn) writeChunks(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		n := len(p)
		if n > ssMaxPayload {
			n = ssMaxPayload
		}
		buf := c.wbuf[:0]
		buf = append(buf, byte(n>>8), byte(n))
		buf = c.wAEAD.Seal(buf[:0], c.wNonce, buf[:2], nil) // 就地把长度加密
		ssIncrementNonce(c.wNonce)
		buf = c.wAEAD.Seal(buf, c.wNonce, p[:n], nil) // 追加加密后的载荷
		ssIncrementNonce(c.wNonce)
		if _, err := c.Conn.Write(buf); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

func (c *ssConn) Write(p []byte) (int, error) {
	if c.wAEAD == nil {
		if err := c.initWriter(); err != nil {
			return 0, err
		}
	}
	// 第一个记录固定是目标地址，服务端据此决定转发目标
	// （参考 tcp.go：rc.Write(tgt) 在 relay 之前）。
	if !c.addrSent {
		if _, err := c.writeChunks(c.addr); err != nil {
			return 0, err
		}
		c.addrSent = true
	}
	return c.writeChunks(p)
}

func (c *ssConn) initReader() error {
	salt := make([]byte, c.def.saltSize())
	if _, err := io.ReadFull(c.Conn, salt); err != nil {
		return err
	}
	subkey, err := ssSubkey(c.psk, salt, c.def.keySize)
	if err != nil {
		return fmt.Errorf("ss: 派生会话密钥失败: %w", err)
	}
	aead, err := c.def.newAEAD(subkey)
	if err != nil {
		return fmt.Errorf("ss: 构造 AEAD 失败: %w", err)
	}
	c.rAEAD = aead
	c.rNonce = make([]byte, aead.NonceSize())
	c.rbuf = make([]byte, 2+ssMaxPayload+aead.Overhead())
	return nil
}

// readRecord 解出下一条记录，返回**指向内部缓冲**的明文：有效期到下
// 一次 readRecord 之前。Read 的 leftover 机制保证这一点（余量未排空
// 不会读下一条）。
func (c *ssConn) readRecord() ([]byte, error) {
	ov := c.rAEAD.Overhead()

	head := c.rbuf[:2+ov]
	if _, err := io.ReadFull(c.Conn, head); err != nil {
		return nil, err
	}
	if _, err := c.rAEAD.Open(head[:0], c.rNonce, head, nil); err != nil {
		return nil, fmt.Errorf("ss: 记录长度解密失败: %w", err)
	}
	ssIncrementNonce(c.rNonce)
	size := (int(head[0])<<8 | int(head[1])) & ssMaxPayload

	body := c.rbuf[:size+ov]
	if _, err := io.ReadFull(c.Conn, body); err != nil {
		return nil, err
	}
	plain, err := c.rAEAD.Open(body[:0], c.rNonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("ss: 记录解密失败（口令或加密方式不匹配、或流被篡改）: %w", err)
	}
	ssIncrementNonce(c.rNonce)
	return plain, nil
}

func (c *ssConn) Read(b []byte) (int, error) {
	if c.rAEAD == nil {
		if err := c.initReader(); err != nil {
			return 0, err
		}
	}
	// 先把上一条记录没读完的余量吐出去（参考 reader.Read）。
	if len(c.leftover) > 0 {
		n := copy(b, c.leftover)
		c.leftover = c.leftover[n:]
		return n, nil
	}
	rec, err := c.readRecord()
	if err != nil {
		return 0, err
	}
	n := copy(b, rec)
	if n < len(rec) { // b 装不下，剩下的留到下次 Read
		c.leftover = rec[n:]
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// 拨号器
// ---------------------------------------------------------------------------

// SsDialer 以 Shadowsocks(AEAD) 为出站。
type SsDialer struct {
	Server   string
	Port     int
	Password string
	Cipher   string
	Timeout  time.Duration
}

func (d *SsDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("ss 出站只支持 tcp，收到 %q", network)
	}
	def, err := pickSsCipher(d.Cipher)
	if err != nil {
		return nil, err
	}
	if d.Password == "" {
		return nil, ErrMissingPassword
	}
	addr, err := ssSocksAddr(address)
	if err != nil {
		return nil, err
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
	return &ssConn{
		Conn: conn,
		psk:  ssKdf(d.Password, def.keySize),
		def:  def,
		addr: addr,
	}, nil
}

func (d *SsDialer) Name() string { return "ss" }
