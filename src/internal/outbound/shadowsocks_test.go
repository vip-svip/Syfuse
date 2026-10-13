package outbound

// Shadowsocks 出站的测试。
//
// 两个重点：
//  1. 密钥派生用**固定向量**锚定（向量由 Node.js 的 OpenSSL 后端独立
//     计算，与 Go 实现零共享代码）；
//  2. 假服务端是一套**独立的线上解包/封装实现**（测试内手写），不调用
//     产品的 ssConn。往返测试验证的是真实线上字节：salt 前缀、记录
//     分帧、nonce 递增、第一个记录是 SOCKS5 目标地址，任何一处与参考
//     实现不一致都会在服务端的 AEAD Open 上失败。

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"

	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/vip-svip/syfuse/src/internal/third_party/chacha20poly1305/chacha20poly1305"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const ssTestPassword = "ss-test-password"

// ---------------------------------------------------------------------------
// 密钥派生的固定向量
// ---------------------------------------------------------------------------

// ssTestKdf 测试内**独立**复现的口令→主密钥链（EVP_BytesToKey-MD5）。
// 与产品的 ssKdf 算法相同但代码独立，避免"同一份代码验证自己"。
func ssTestKdf(password string, keyLen int) []byte {
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

// ssTestHKDFSHA1 测试内手写的 HKDF-SHA1（extract + expand），
// 用来交叉验证产品路径（标准库 crypto/hkdf）。
func ssTestHKDFSHA1(secret, salt, info []byte, keyLen int) []byte {
	prkHMAC := hmac.New(sha1.New, salt)
	prkHMAC.Write(secret)
	prk := prkHMAC.Sum(nil)

	var out, t []byte
	for i := byte(1); len(out) < keyLen; i++ {
		m := hmac.New(sha1.New, prk)
		m.Write(t)
		m.Write(info)
		m.Write([]byte{i})
		t = m.Sum(nil)
		out = append(out, t...)
	}
	return out[:keyLen]
}

func TestSsKdfMatchesKnownVector(t *testing.T) {
	// 向量来源：Node.js crypto（OpenSSL 后端）独立计算的
	// EVP_BytesToKey-MD5 链。md5("password") = 5f4dcc3b... 是公开常
	// 数值，第二块是 md5(prev||password) 的链式，可人工复算。
	cases := []struct {
		keyLen int
		want   string
	}{
		{16, "5f4dcc3b5aa765d61d8327deb882cf99"},
		{24, "5f4dcc3b5aa765d61d8327deb882cf992b95990a9151374a"},
		{32, "5f4dcc3b5aa765d61d8327deb882cf992b95990a9151374abd8ff8c5a7a0fe08"},
	}
	for _, tc := range cases {
		if got := hex.EncodeToString(ssKdf("password", tc.keyLen)); got != tc.want {
			t.Errorf("ssKdf(%q, %d) = %s, want %s", "password", tc.keyLen, got, tc.want)
		}
		if got := hex.EncodeToString(ssTestKdf("password", tc.keyLen)); got != tc.want {
			t.Errorf("测试内独立链 = %s, want %s（固定向量本身可疑）", got, tc.want)
		}
	}
}

func TestSsSubkeyMatchesKnownVector(t *testing.T) {
	// 固定 salt 与固定口令派生的主密钥，info 固定 "ss-subkey"。
	// 向量由 Node.js crypto.hkdfSync("sha1", ...)（OpenSSL 后端）计算，
	// 与 Go 的标准库 crypto/hkdf 零共享代码。
	salt, err := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	if err != nil {
		t.Fatalf("解码 salt 向量: %v", err)
	}
	cases := []struct {
		keyLen int
		want   string
	}{
		{16, "ed2a618d9490d1701de885d82aa80616"},
		{24, "cc7c589c6ee3a2567d57c9e6c98376dd6e3d3f6573c6edd5"},
		{32, "21b989020f67b371530ef9a3818f30db2dc789e804d948098dc5a91ec6fe382f"},
	}
	for _, tc := range cases {
		psk := ssKdf("password", tc.keyLen)
		got, err := ssSubkey(psk, salt, tc.keyLen)
		if err != nil {
			t.Fatalf("ssSubkey(keyLen=%d): %v", tc.keyLen, err)
		}
		if hex.EncodeToString(got) != tc.want {
			t.Errorf("ssSubkey(keyLen=%d) = %s, want %s", tc.keyLen, hex.EncodeToString(got), tc.want)
		}
		// 交叉验证：测试内手写 expand 必须给出同一结果。
		if w := hex.EncodeToString(ssTestHKDFSHA1(psk, salt, []byte(ssSubkeyInfo), tc.keyLen)); w != tc.want {
			t.Errorf("手写 HKDF-SHA1 expand = %s, want %s", w, tc.want)
		}
	}

	// 再锚一个不经过 KDF 的输入：证明标准库 crypto/hkdf 与 OpenSSL 的
	// HKDF-SHA1 在任意 secret 上一致（该向量两侧实测相同）。
	rawKey, _ := hex.DecodeString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	rawSalt, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	got, err := ssSubkey(rawKey, rawSalt, 32)
	if err != nil {
		t.Fatalf("ssSubkey(raw): %v", err)
	}
	const wantRaw = "b6cdc49e356ceef22e509eca562d3bc034425b5326181a8a5438b6bab13f8aef"
	if hex.EncodeToString(got) != wantRaw {
		t.Errorf("ssSubkey(任意输入) = %s, want %s", hex.EncodeToString(got), wantRaw)
	}
}

// ---------------------------------------------------------------------------
// 独立实现的假 ss 服务端
// ---------------------------------------------------------------------------

var errSSDecrypt = errors.New("ss 测试服务端: 解密失败")

func ssTestIncr(nonce []byte) {
	for i := range nonce {
		nonce[i]++
		if nonce[i] != 0 {
			return
		}
	}
}

func ssTestKeySize(cipherName string) int {
	switch cipherName {
	case "aes-128-gcm":
		return 16
	case "aes-192-gcm":
		return 24
	case "aes-256-gcm", "chacha20-ietf-poly1305":
		return 32
	}
	panic("测试服务端不认识的 cipher: " + cipherName)
}

func ssTestAEAD(cipherName string, key []byte) (cipher.AEAD, error) {
	switch cipherName {
	case "aes-128-gcm", "aes-192-gcm", "aes-256-gcm":
		blk, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(blk)
	case "chacha20-ietf-poly1305":
		// chacha20 原语本身由 RFC 8439 官方向量单独验证
		// （internal/third_party/chacha20poly1305 的 vendor_vectors_test.go，
		// 每个向量的密文逐字节对齐 + 全位篡改必须认证失败），
		// 这里直接用产品构造器，不再另起一份实现。
		return chacha20poly1305.New(key)
	}
	return nil, fmt.Errorf("测试服务端不支持的 cipher %q", cipherName)
}

// ssTestDec / ssTestEnc 是测试侧独立的解包/封装器：
// [2B 大端长度(密文)][tag][载荷密文][tag]，nonce 小端递增从 0 开始。
type ssTestDec struct {
	aead  cipher.AEAD
	nonce []byte
	buf   []byte
}

func ssTestReadRecord(r io.Reader, d *ssTestDec) ([]byte, error) {
	ov := d.aead.Overhead()
	head := d.buf[:2+ov]
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err
	}
	if _, err := d.aead.Open(head[:0], d.nonce, head, nil); err != nil {
		return nil, fmt.Errorf("%w: %v", errSSDecrypt, err)
	}
	ssTestIncr(d.nonce)
	size := int(head[0])<<8 | int(head[1])
	body := d.buf[:size+ov]
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	plain, err := d.aead.Open(body[:0], d.nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSSDecrypt, err)
	}
	ssTestIncr(d.nonce)
	return plain, nil
}

type ssTestEnc struct {
	aead  cipher.AEAD
	nonce []byte
	buf   []byte
}

func (e *ssTestEnc) writeRecord(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n := len(p)
		if n > ssMaxPayload {
			n = ssMaxPayload
		}
		buf := e.buf[:0]
		buf = append(buf, byte(n>>8), byte(n))
		buf = e.aead.Seal(buf[:0], e.nonce, buf[:2], nil)
		ssTestIncr(e.nonce)
		buf = e.aead.Seal(buf, e.nonce, p[:n], nil)
		ssTestIncr(e.nonce)
		if _, err := w.Write(buf); err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}

// ssEchoServer 假 ss 服务端：解包后把数据原样加密回显。
//
// 第一条记录当目标地址**消费掉**（不回显）——真实服务端据此拨目标，
// 地址不会被转发回来。逐条记录计数、salt、地址明文、服务端错误都记
// 进 stats 供断言。
type ssEchoServer struct {
	*fakeServer

	password string
	cipher   string

	mu          sync.Mutex
	salts       [][]byte // 客户端（写侧）salt
	serverSalts [][]byte // 服务端（写侧）salt，按连接顺序与 salts 对齐
	records     int
	firstAddr   []byte
	serverErr   string
}

func startSSEchoServer(t *testing.T, password, cipherName string) *ssEchoServer {
	t.Helper()
	s := &ssEchoServer{password: password, cipher: cipherName}
	s.fakeServer = newFakeServer(t, s.handle)
	return s
}

// handle 按参考格式逐字节解包。返回值语义同 fakeServer 的约定：
// nil 表示正常结束（客户端关闭）。
func (s *ssEchoServer) handle(c net.Conn) error {
	keySize := ssTestKeySize(s.cipher)
	salt := make([]byte, keySize)
	if _, err := io.ReadFull(c, salt); err != nil {
		return err
	}
	s.addSalt(salt)

	// 读侧：客户端 salt 派生的子密钥。
	decAEAD, err := s.ssAEADForSalt(salt)
	if err != nil {
		return err
	}
	dec := &ssTestDec{aead: decAEAD, nonce: make([]byte, decAEAD.NonceSize()), buf: make([]byte, 2+decAEAD.Overhead()+ssMaxPayload)}

	// 写侧：**服务端自己也要发一份 salt**，并且用它派生一套独立的子密钥。
	//
	// 这一步不是礼貌而是协议：参考实现 shadowaead.streamConn.initWriter
	// 在首次写之前先发出自己的 salt，而客户端的 Read 走 initReader，
	// 把连接上收到的前 SaltSize 个字节当服务端 salt 来派生读侧密钥。
	// 也就是说 ss AEAD 是"两个方向各一个 salt"——客户端一个、服务端
	// 一个，两套子密钥互相独立（nonce 计数器各自从 0 开始）。
	//
	// 少了这一步，客户端 initReader 会把服务端第一条记录的头
	// saltSize 个字节当成 salt 吃掉，于是永远读不出回显：症状是
	// "记录长度解密失败"或 i/o timeout，而客户端→服务端半边完全正常
	// ——这个不对称就是本断言的来由（见 TestSsServerSendsOwnSalt）。
	srvSalt := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, srvSalt); err != nil {
		return err
	}
	if bytes.Equal(srvSalt, salt) {
		return errors.New("测试替身：服务端 salt 与客户端 salt 相同")
	}
	if _, err := c.Write(srvSalt); err != nil {
		return err
	}
	s.addServerSalt(srvSalt)

	encAEAD, err := s.ssAEADForSalt(srvSalt)
	if err != nil {
		return err
	}
	enc := &ssTestEnc{aead: encAEAD, nonce: make([]byte, encAEAD.NonceSize()), buf: make([]byte, 0, 2+2*encAEAD.Overhead()+ssMaxPayload)}

	// 第一条记录 = 目标地址：消费掉，不回显。
	addr, err := ssTestReadRecord(c, dec)
	if err != nil {
		if errors.Is(err, errSSDecrypt) {
			s.setServerErr(err)
		}
		return err
	}
	s.countRecord(addr)

	for {
		plain, err := ssTestReadRecord(c, dec)
		if err != nil {
			if errors.Is(err, errSSDecrypt) {
				s.setServerErr(err)
			}
			return err // 客户端关闭或流损坏：结束
		}
		s.countRecord(plain)
		if err := enc.writeRecord(c, plain); err != nil {
			return err
		}
	}
}

// ssAEADForSalt 用给定的 salt 派生写/读侧子密钥并构造 AEAD。
// 读侧传客户端 salt，写侧传服务端自己的 salt。
func (s *ssEchoServer) ssAEADForSalt(salt []byte) (cipher.AEAD, error) {
	keySize := ssTestKeySize(s.cipher)
	subkey := ssTestHKDFSHA1(ssTestKdf(s.password, keySize), salt, []byte(ssSubkeyInfo), keySize)
	return ssTestAEAD(s.cipher, subkey)
}

func (s *ssEchoServer) countRecord(plain []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records++
	if s.firstAddr == nil {
		cp := make([]byte, len(plain))
		copy(cp, plain)
		s.firstAddr = cp
	}
}

func (s *ssEchoServer) recordCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records
}

// waitForRecords 等到服务端至少收到 n 条记录再返回。
//
// 为什么不用 fakeServer.waitIdle：ssEchoServer.handle 是
// "读一条 → 回显一条"的循环，在客户端关闭连接之前它永远是 active 的，
// waitIdle 只会等到 5 秒超时。这里要等的是"服务端确实处理过某件事"，
// 那是一个可判定的条件，而不是"连接结束"。
func waitForRecords(t *testing.T, srv *ssEchoServer, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if srv.recordCount() >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("服务端只收到 %d 条记录，期望至少 %d 条", srv.recordCount(), n)
}

func (s *ssEchoServer) firstAddrSnapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(s.firstAddr))
	copy(cp, s.firstAddr)
	return cp
}

func (s *ssEchoServer) addSalt(salt []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.salts = append(s.salts, salt)
	s.serverSalts = append(s.serverSalts, nil) // 占位，写侧 salt 到达前
}

// addServerSalt 记录服务端（写侧）发出去的 salt。
func (s *ssEchoServer) addServerSalt(salt []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(salt))
	copy(cp, salt)
	if n := len(s.serverSalts); n > 0 {
		s.serverSalts[n-1] = cp
	} else {
		s.serverSalts = append(s.serverSalts, cp)
	}
}

func (s *ssEchoServer) saltsSnapshot() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.salts))
	copy(out, s.salts)
	return out
}

func (s *ssEchoServer) setServerErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serverErr == "" {
		s.serverErr = err.Error()
	}
}

func (s *ssEchoServer) serverErrSnapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverErr
}

// ---------------------------------------------------------------------------
// 拨号器：地址帧
// ---------------------------------------------------------------------------

// ssParseTestAddr 独立解析第一条记录里的 SOCKS5 地址。
func ssParseTestAddr(t *testing.T, b []byte) (atyp byte, host string, port int) {
	t.Helper()
	if len(b) < 1+2 {
		t.Fatalf("地址记录过短: %d 字节", len(b))
	}
	atyp = b[0]
	switch atyp {
	case 0x01:
		if len(b) != 1+4+2 {
			t.Fatalf("IPv4 地址记录长度 = %d, want 7", len(b))
		}
		return atyp, net.IP(b[1:5]).String(), int(b[5])<<8 | int(b[6])
	case 0x03:
		n := int(b[1])
		if len(b) != 1+1+n+2 {
			t.Fatalf("域名地址记录长度 = %d, want %d", len(b), 1+1+n+2)
		}
		return atyp, string(b[2 : 2+n]), int(b[2+n])<<8 | int(b[2+n+1])
	case 0x04:
		if len(b) != 1+16+2 {
			t.Fatalf("IPv6 地址记录长度 = %d, want 19", len(b))
		}
		return atyp, net.IP(b[1:17]).String(), int(b[17])<<8 | int(b[18])
	}
	t.Fatalf("未知 ATYP 0x%02x", atyp)
	return 0, "", 0
}

func TestSsDialerSendsTargetAddressFirst(t *testing.T) {
	// ss 的 TCP 流第一个记录必须是 SOCKS5 格式的目标地址
	// （参考 tcp.go：rc.Write(tgt) 先于 relay）。少这一帧，
	// 真实节点无法知道转发目标。
	cases := []struct {
		name     string
		target   string
		wantAtyp byte
		wantHost string
		wantPort int
	}{
		{"domain", "example.com:443", 0x03, "example.com", 443},
		{"ipv4", "93.184.216.34:80", 0x01, "93.184.216.34", 80},
		{"ipv6", "[2001:db8::1]:8443", 0x04, "2001:db8::1", 8443},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := startSSEchoServer(t, ssTestPassword, "aes-256-gcm")
			host, port := srv.hostPort(t)
			d := &SsDialer{Server: host, Port: port, Password: ssTestPassword, Cipher: "aes-256-gcm"}
			c, err := d.DialContext(context.Background(), "tcp", tc.target)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer c.Close()
			if _, err := c.Write([]byte("x")); err != nil {
				t.Fatalf("write: %v", err)
			}
			// 地址帧之后数据通路也要正常：读回显。
			c.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.ReadFull(c, make([]byte, 1)); err != nil {
				t.Fatalf("地址帧之后的读回显失败: %v", err)
			}
			waitForRecords(t, srv, 2) // 地址帧 + 载荷帧

			atyp, gotHost, gotPort := ssParseTestAddr(t, srv.firstAddrSnapshot())
			if atyp != tc.wantAtyp {
				t.Errorf("ATYP = 0x%02x, want 0x%02x", atyp, tc.wantAtyp)
			}
			if gotHost != tc.wantHost {
				t.Errorf("主机 = %q, want %q", gotHost, tc.wantHost)
			}
			if gotPort != tc.wantPort {
				t.Errorf("端口 = %d, want %d", gotPort, tc.wantPort)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 拨号器：往返与 salt
// ---------------------------------------------------------------------------

// ssDialEcho 拨号、写 msg、把同样长度的回显读回来并逐字节比对。
func ssDialEcho(t *testing.T, srv *ssEchoServer, cipher, msg string) {
	t.Helper()
	host, port := srv.hostPort(t)
	d := &SsDialer{Server: host, Port: port, Password: ssTestPassword, Cipher: cipher}
	c, err := d.DialContext(context.Background(), "tcp", "93.184.216.34:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != msg {
		t.Fatalf("回显 = %q, want %q", buf, msg)
	}
}

func TestSsDialerCipherTableRoundTripAndSalts(t *testing.T) {
	// 表驱动覆盖全部 AEAD cipher：往返 + salt 长度 = 密钥长度 +
	// 两次连接 salt 不同（防重放的基本保证：同主密钥不同会话密钥）。
	cases := []struct {
		name    string
		keySize int
	}{
		{"aes-128-gcm", 16},
		{"aes-192-gcm", 24},
		{"aes-256-gcm", 32},
		{"chacha20-ietf-poly1305", 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := startSSEchoServer(t, ssTestPassword, tc.name)
			ssDialEcho(t, srv, tc.name, "ping-1")
			ssDialEcho(t, srv, tc.name, "ping-2")
			waitForRecords(t, srv, 2) // 地址帧 + 载荷帧

			salts := srv.saltsSnapshot()
			if len(salts) != 2 {
				t.Fatalf("服务端收到 %d 条连接, want 2", len(salts))
			}
			for i, salt := range salts {
				if len(salt) != tc.keySize {
					t.Errorf("连接 %d 的 salt 长度 = %d, want %d（keylen）", i, len(salt), tc.keySize)
				}
			}
			if bytes.Equal(salts[0], salts[1]) {
				t.Fatalf("两次连接的 salt 相同：%x —— 会话密钥可重放", salts[0])
			}
			if serr := srv.serverErrSnapshot(); serr != "" {
				t.Fatalf("服务端不应报错: %s", serr)
			}
		})
	}
}

func TestSsDialerEchoLargePayloadMultiChunk(t *testing.T) {
	// >16KB 的大载荷必须跨多条记录（16383/条）。非 AEAD 时代最常见的
	// 错误来源就是 chunk 边界：nonce 用错、长度掩码漏掉、余量处理错。
	// 100*1024 = 16383*6 + 5502 ⇒ 7 条数据记录，加地址记录共 8 条。
	const payloadLen = 100 * 1024
	srv := startSSEchoServer(t, ssTestPassword, "aes-256-gcm")
	host, port := srv.hostPort(t)
	d := &SsDialer{Server: host, Port: port, Password: ssTestPassword, Cipher: "aes-256-gcm"}
	c, err := d.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	payload := make([]byte, payloadLen)
	for i := range payload {
		payload[i] = byte(i*31 + 7)
	}

	// 写与读并发：回显是逐条产生的，单线程先写 100KB 会把双方的
	// socket 缓冲堵死（经典互锁）。
	werr := make(chan error, 1)
	go func() {
		_, err := c.Write(payload)
		werr <- err
	}()

	got := make([]byte, 0, payloadLen)
	buf := make([]byte, 4096)
	c.SetReadDeadline(time.Now().Add(30 * time.Second))
	for len(got) < payloadLen {
		n, err := c.Read(buf)
		if n > 0 {
			got = append(got, buf[:n]...)
		}
		if err != nil {
			t.Fatalf("读回显失败（已收 %d/%d 字节）: %v", len(got), payloadLen, err)
		}
	}
	if err := <-werr; err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("回显与原文不一致（长度 got=%d want=%d）", len(got), len(payload))
	}
	waitForRecords(t, srv, 1+7) // 1 条地址 + 7 条数据记录

	if n := srv.recordCount(); n != 1+7 {
		t.Fatalf("服务端收到 %d 条记录, want 8（1 条地址 + 7 条数据）", n)
	}
	if serr := srv.serverErrSnapshot(); serr != "" {
		t.Fatalf("服务端不应报错: %s", serr)
	}
}

// ---------------------------------------------------------------------------
// 拨号器：错误路径
// ---------------------------------------------------------------------------

func TestSsDialerWrongPasswordFailsClosed(t *testing.T) {
	// 服务端口令与客户端不同：服务端解开第一条记录（地址帧）就会失败。
	// 关键断言：客户端读回的是错误而不是明文——解密失败绝不能把
	// 明文漏出去，也不能伪装成空响应。
	srv := startSSEchoServer(t, "server-side-password", "aes-256-gcm")
	host, port := srv.hostPort(t)
	d := &SsDialer{Server: host, Port: port, Password: "client-side-password", Cipher: "aes-256-gcm"} // secretscan:allow 测试夹具
	c, err := d.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		// 写失败也算错误冒出来（RST 竞态下可能先炸在写上）。
		srv.waitIdle(t)
		return
	}
	buf := make([]byte, 16)
	n, rerr := c.Read(buf)
	if rerr == nil {
		t.Fatalf("口令不匹配时读必须失败，却拿到 %d 字节: %q", n, buf[:n])
	}
	if n != 0 {
		t.Fatalf("解密失败时不允许漏出明文字节，却读到 %d 字节: %q", n, buf[:n])
	}
	srv.waitIdle(t)
	if serr := srv.serverErrSnapshot(); !strings.Contains(serr, "解密失败") {
		t.Fatalf("服务端应在 AEAD 层报解密失败，实际: %q", serr)
	}
}

func TestSsDialerRejectsNonAEADCiphers(t *testing.T) {
	// 常见 legacy 流加密：必须明确报 ErrNonAEADCipher。
	// Port 用 1（没人监听）：cipher 校验发生在拨号**之前**，
	// 若有人把校验挪到拨号后，这里拿到的会是拨号错误而不是哨兵。
	for _, name := range []string{
		"aes-128-cfb", "aes-192-cfb", "aes-256-cfb",
		"aes-128-ctr", "aes-256-ctr", "aes-256-ofb",
		"rc4", "rc4-md5", "chacha20", "chacha20-ietf",
		"bf-cfb", "camellia-128-cfb", "salsa20",
	} {
		d := &SsDialer{Server: "127.0.0.1", Port: 1, Password: "p", Cipher: name}
		_, err := d.DialContext(context.Background(), "tcp", "example.com:443")
		if !errors.Is(err, ErrNonAEADCipher) {
			t.Errorf("cipher %q: 错误 = %v, want ErrNonAEADCipher", name, err)
		}
	}
}

func TestSsDialerRejectsUnknownCiphers(t *testing.T) {
	// 不在表内：拼错、2022 版方法、标准库做不了的 AEAD。
	// 必须报 ErrUnsupportedCipher 且不能误报成 ErrNonAEADCipher。
	for _, name := range []string{
		"", "   ", "aes-512-gcm", "aesgcm", "AES-GCM",
		"xchacha20-ietf-poly1305",
		"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
	} {
		d := &SsDialer{Server: "127.0.0.1", Port: 1, Password: "p", Cipher: name}
		_, err := d.DialContext(context.Background(), "tcp", "example.com:443")
		if !errors.Is(err, ErrUnsupportedCipher) {
			t.Errorf("cipher %q: 错误 = %v, want ErrUnsupportedCipher", name, err)
		}
		if errors.Is(err, ErrNonAEADCipher) {
			t.Errorf("cipher %q: 不应误报为 ErrNonAEADCipher", name)
		}
	}
}

func TestSsDialerRejectsUDP(t *testing.T) {
	d := &SsDialer{Server: "127.0.0.1", Port: 1, Password: "p", Cipher: "aes-256-gcm"}
	if _, err := d.DialContext(context.Background(), "udp", "10.0.0.1:53"); err == nil {
		t.Fatalf("udp 应被拒绝")
	}
}

func TestSsDialerRequiresPassword(t *testing.T) {
	d := &SsDialer{Server: "127.0.0.1", Port: 1, Password: "", Cipher: "aes-256-gcm"}
	if _, err := d.DialContext(context.Background(), "tcp", "example.com:443"); !errors.Is(err, ErrMissingPassword) {
		t.Fatalf("错误 = %v, want ErrMissingPassword", err)
	}
}

func TestSsDialerSurfacesUpstreamRST(t *testing.T) {
	// 上游 accept 后立刻带未读数据 RST 断开：错误必须冒出来，
	// 不能卡住也不能静默成功。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// 等 50ms 让 dial 确定返回，再带未读数据关闭（发 RST）。
			time.Sleep(50 * time.Millisecond)
			if tc, ok := c.(*net.TCPConn); ok {
				_ = tc.SetLinger(0)
			}
			_ = c.Close()
		}
	}()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("atoi: %v", err)
	}
	d := &SsDialer{Server: "127.0.0.1", Port: port, Password: "p", Cipher: "aes-256-gcm", Timeout: 2 * time.Second}
	c, err := d.DialContext(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("dial 本身应成功（RST 在 accept 之后才发）: %v", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))

	var lastErr error
	for i := 0; i < 3 && lastErr == nil; i++ {
		if _, err := c.Write([]byte("x")); err != nil {
			lastErr = err
			break
		}
		if _, err := c.Read(make([]byte, 4)); err != nil {
			lastErr = err
			break
		}
	}
	if lastErr == nil {
		t.Fatalf("上游 RST 后错误必须冒出来")
	}
}
