// probe.go 是 Probe 的生产实现：真实网络探测。
// 每个方法都设短超时，探测失败返回 error（由检测逻辑归为 Skip/Warn），
// 绝不阻塞自检流程。
package leak

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// RealProbe 直连真实网络执行探测。
type RealProbe struct {
	// Timeout 是单次探测超时；零值取 5 秒。
	Timeout time.Duration
	client  *http.Client
}

// NewRealProbe 构造生产探针。
func NewRealProbe() *RealProbe {
	return &RealProbe{}
}

func (p *RealProbe) timeout() time.Duration {
	if p.Timeout <= 0 {
		return 5 * time.Second
	}
	return p.Timeout
}

func (p *RealProbe) httpClient() *http.Client {
	if p.client == nil {
		p.client = &http.Client{
			Timeout: p.timeout(),
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			},
		}
	}
	return p.client
}

// DNSUpstream 通过向本机 resolver 发起一次解析并读取 /etc/resolv.conf
// 风格的上游来判定。Android 侧真实来源由调用方注入；这里给出
// 基于系统 resolver 的保守实现：读 resolv.conf 的 nameserver。
func (p *RealProbe) DNSUpstream() (string, error) {
	up, err := readResolvNameserver()
	if err != nil {
		return "", err
	}
	if up == "" {
		return "", fmt.Errorf("leak: 未找到 nameserver")
	}
	return up, nil
}

// ResolveAAAA 对 host 解析 AAAA 记录。
func (p *RealProbe) ResolveAAAA(host string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout())
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip6", host)
	if err != nil {
		return false, err
	}
	return len(ips) > 0, nil
}

// STUNBinding 发最小 STUN Binding Request 并解析 XOR-MAPPED-ADDRESS。
func (p *RealProbe) STUNBinding(server string) (string, error) {
	host, port, err := net.SplitHostPort(server)
	if err != nil {
		host, port = server, "3478"
	}
	addr := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("udp", addr, p.timeout())
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(p.timeout()))

	// RFC 5389 最小 Binding Request：type=0x0001, len=0, magic cookie, 12 字节事务 ID。
	msgID := []byte{0x73, 0x79, 0x66, 0x75, 0x73, 0x65, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:], 0x0001)
	binary.BigEndian.PutUint32(req[4:], 0x2112A442)
	copy(req[8:], msgID)
	if _, err := conn.Write(req); err != nil {
		return "", err
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return parseXorMappedAddress(buf[:n], msgID)
}

// parseXorMappedAddress 从 STUN 响应中解析 XOR-MAPPED-ADDRESS /
// MAPPED-ADDRESS 属性。
func parseXorMappedAddress(resp, msgID []byte) (string, error) {
	if len(resp) < 20 {
		return "", fmt.Errorf("leak: STUN 响应过短")
	}
	length := int(binary.BigEndian.Uint16(resp[2:]))
	if 20+length > len(resp) {
		length = len(resp) - 20
	}
	off := 20
	for off+4 <= 20+length {
		attrType := binary.BigEndian.Uint16(resp[off:])
		attrLen := int(binary.BigEndian.Uint16(resp[off+2:]))
		val := resp[off+4 : min(off+4+attrLen, len(resp))]
		switch attrType {
		case 0x0020: // XOR-MAPPED-ADDRESS
			if len(val) >= 8 && val[1] == 0x01 { // IPv4
				ip := make(net.IP, 4)
				for i := 0; i < 4; i++ {
					ip[i] = val[4+i] ^ msgID[i]
				}
				return ip.String(), nil
			}
		case 0x0001: // MAPPED-ADDRESS
			if len(val) >= 8 && val[1] == 0x01 {
				return net.IP(val[4:8]).String(), nil
			}
		}
		off += 4 + attrLen + ((4 - attrLen%4) % 4)
	}
	return "", fmt.Errorf("leak: 响应中无映射地址属性")
}

// QUICReachable 探测 UDP 443 直连：向目标发一个 QUIC Initial 大小的
// UDP 包并等待任何回包；此处以"能否对公共 443 完成一次 UDP 往返"为准。
// 使用 1.1.1.1:443 的 DNS-over-QUIC 无效负载探测即可判断可达性。
func (p *RealProbe) QUICReachable() (bool, error) {
	conn, err := net.DialTimeout("udp", "1.1.1.1:443", p.timeout())
	if err != nil {
		return false, nil // 拨号失败 = 不可达（阻断），不算错误
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(p.timeout()))
	// 非 QUIC 负载，正常服务会丢弃或回 ICMP；能收到任何回包即视为 UDP 443 可达。
	if _, err := conn.Write(make([]byte, 64)); err != nil {
		return false, nil
	}
	buf := make([]byte, 128)
	if _, err := conn.Read(buf); err != nil {
		return false, nil // 超时/ICMP 不可达 = 阻断
	}
	return true, nil
}

// PublicIP 请求回显端点取出口 IP。
func (p *RealProbe) PublicIP(endpoint string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "syfuse-leakcheck/1.0")
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("leak: 端点 %s 返回非 IP: %q", endpoint, ip)
	}
	return ip, nil
}

// RouteFor 生产实现依赖 dataplane 的路由决策；在守护进程接线时由
// 调用方注入带 Router 的探针（见 syfused 的 leakProbe 适配器）。
// 独立使用时返回未实现错误（对应维度记 Skip，不算失败）。
func (p *RealProbe) RouteFor(string) (string, error) {
	return "", fmt.Errorf("leak: 独立探针无路由信息")
}

// SystemFlags 独立使用时同样由守护进程接线注入（读 Android 系统状态）。
func (p *RealProbe) SystemFlags() (map[string]bool, error) {
	return nil, fmt.Errorf("leak: 独立探针无系统状态")
}

// readResolvNameserver 从 resolv.conf 风格文件取首个 nameserver。
func readResolvNameserver() (string, error) {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[0] == "nameserver" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("leak: 无 nameserver")
}
