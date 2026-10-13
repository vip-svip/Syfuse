package dns

// 测试替身与公共辅助。理念（与 internal/outbound 的测试一致）：
// 用**真实的本地 socket**（UDP/TCP/TLS 监听）驱动被测代码，不 mock 任何
// 接口 —— mock 只能证明"代码按我设想的方式调用了我设想的接口"，
// 真实 socket 才能证明协议Actually在线上是通的。

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// dnsResponder 把收到的原始查询报文变换成应答报文。
// 返回 nil 表示不回包（模拟丢包，用于测超时/故障切换）。
type dnsResponder func(query []byte) []byte

// fakeUpstream 是在**同一端口**上同时监听 UDP 与 TCP 的本地假 DNS 上游。
//
// 为什么必须同端口：真实 DNS 的 UDP/TCP 共用 53，TC=1 时客户端就是拿
// 同一个 host:port 换 TCP 重问的。不同端口的话，被测代码"换 TCP 连同一
// 地址"的行为根本测不到。
type fakeUpstream struct {
	t       *testing.T
	addr    string
	udpConn net.PacketConn
	tcpLn   net.Listener

	udpHits atomic.Int32
	tcpHits atomic.Int32

	respond dnsResponder
}

func startFakeUpstream(t *testing.T, respond dnsResponder) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{t: t, respond: respond}
	// 随机选口再双协议绑定（失败重试）：两个独立随机口之间没有"同端口"
	// 的保证，而先绑一个再抢同端口 TCP 有极小概率被系统分给别人。
	var lastErr error
	for i := 0; i < 50; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(40000))
		if err != nil {
			t.Fatalf("取随机端口失败: %v", err)
		}
		port := 20000 + int(n.Int64())
		uc, err := net.ListenPacket("udp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			lastErr = err
			continue
		}
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			uc.Close()
			lastErr = err
			continue
		}
		f.udpConn, f.tcpLn, f.addr = uc, ln, "127.0.0.1:"+strconv.Itoa(port)
		lastErr = nil
		break
	}
	if lastErr != nil || f.udpConn == nil {
		t.Fatalf("无法绑定本地假上游（50 次重试后仍失败）: %v", lastErr)
	}
	t.Cleanup(func() {
		f.udpConn.Close()
		f.tcpLn.Close()
	})

	go f.serveUDP()
	go f.serveTCP()
	return f
}

func (f *fakeUpstream) serveUDP() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := f.udpConn.ReadFrom(buf)
		if err != nil {
			return // 监听器已关（t.Cleanup），正常退场
		}
		f.udpHits.Add(1)
		// 拷贝一份再交给 responder：buf 会被下一次读覆盖。
		resp := f.respond(append([]byte(nil), buf[:n]...))
		if resp == nil {
			continue // 丢包：什么都不回
		}
		if _, err := f.udpConn.WriteTo(resp, addr); err != nil {
			return
		}
	}
}

func (f *fakeUpstream) serveTCP() {
	for {
		c, err := f.tcpLn.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			var hdr [2]byte
			if _, err := io.ReadFull(c, hdr[:]); err != nil {
				return
			}
			q := make([]byte, int(binary.BigEndian.Uint16(hdr[:])))
			if _, err := io.ReadFull(c, q); err != nil {
				return
			}
			f.tcpHits.Add(1)
			resp := f.respond(q)
			if resp == nil {
				return
			}
			binary.BigEndian.PutUint16(hdr[:], uint16(len(resp)))
			_, _ = c.Write(append(hdr[:], resp...))
		}(c)
	}
}

// echoResponse 基于收到的原始查询构造一份应答：沿用查询的 ID 与问题段，
// 填入给定 RCODE 与 extra flags、给定的 answer 段。
//
// 内部用被测代码自己的 Parse/Pack 完成"读查询→拼应答"，这是测试替身的
// 常规做法（编码正确性另有 message_test.go 的独立用例锁）；真正手工逐
// 字节构造的报文只出现在畸形输入/压缩指针环那类攻击面测试里 —— 那些
// 场景被测编码器根本产不出需要的字节。
func echoResponse(t *testing.T, query []byte, rcode uint16, answers []RR) []byte {
	t.Helper()
	q, err := ParseMessage(query)
	if err != nil {
		t.Errorf("假上游解析查询失败: %v", err)
		return nil
	}
	resp := &Message{
		ID:        q.ID,
		Flags:     flagQR | flagRA | flagRD | rcode,
		Questions: q.Questions,
		Answers:   answers,
	}
	b, err := resp.Pack()
	if err != nil {
		t.Errorf("假上游打包应答失败: %v", err)
		return nil
	}
	return b
}

// aRR 是一条 A 记录的速记构造。
func aRR(name, ipv4 string, ttl uint32) RR {
	ip := net.ParseIP(ipv4)
	if ip == nil {
		panic("测试里写了非法 IPv4 字面量: " + ipv4)
	}
	return RR{Name: name, Type: TypeA, Class: ClassINET, TTL: ttl, IP: ip.To4()}
}

// aaaaRR 是一条 AAAA 记录的速记构造。
func aaaaRR(name, ipv6 string, ttl uint32) RR {
	ip := net.ParseIP(ipv6)
	if ip == nil {
		panic("测试里写了非法 IPv6 字面量: " + ipv6)
	}
	return RR{Name: name, Type: TypeAAAA, Class: ClassINET, TTL: ttl, IP: ip.To16()}
}

// newTestCert 生成一张给 127.0.0.1 签的自签证书（DoT/httptest 场景用）。
// 只在测试里出现，不染指生产代码路径。
func newTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成测试私钥失败: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"127.0.0.1", "localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发测试证书失败: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeClock 是可编程时钟，配合 Cache.now / FakeIPPool.now 注入，
// 让过期测试不靠 sleep 猜时间（sleep 在 CI 上是偶发失败的常客）。
type fakeClock struct {
	t  time.Time
	mu chan struct{} // 占位，保持结构体可扩展；当前逻辑单测内串行访问
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
