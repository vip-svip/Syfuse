// Package dns 提供 Syfuse 内核的 DNS 解析能力：多上游（UDP/TCP/DoT/DoH）
// 应答缓存、fake-ip 地址池与超时竞速。
//
// 先读这段边界说明再改代码：
//
//   - 本包只用标准库（与全仓库一致，tests/run_kernel.sh 有断言守着"无
//     require / 无 go.sum"）。DoH 走 net/http，DoT 走 crypto/tls，DNS
//     报文用 encoding/binary 手拼 —— 每个第三方依赖都是一条供应链审计面，
//     代理内核宁可有界地重复造轮子。
//   - DoQ(QUIC) 明确不支持：标准库没有 QUIC，实现它必然引入第三方依赖，
//     与上一条冲突。配置到 doq 上游时 Query 返回可 errors.Is 识别的
//     ErrDoQUnsupported，而不是静默失败。
//   - 本包只解析/重发 A/AAAA/CNAME（及 NS/PTR）记录；其他 RR 类型以
//     原始 RDATA 透传。理由：本仓库的用途（代理分流/fake-ip/嗅探）只需要
//     这几类，完整实现 60+ RR 类型的编解码是几十倍代码量与测试面，
//     收益却接近零。透传至少保证"不弄丢不认识的回答"。
//   - 报文解码的畸形输入一律返回 error，绝不 panic：DNS 是暴露面最大的
//     明文协议之一，应答来自不可控的上游/网络，一个 panic 就能打挂守护
//     进程。所有越界读都有显式长度检查（见 parseName/parseRR）。
package dns

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// 常用 RR 类型与类别（只列本包用得到的，不追求 RFC 全集）。
const (
	TypeA     uint16 = 1
	TypeNS    uint16 = 2
	TypeCNAME uint16 = 5
	TypeSOA   uint16 = 6
	TypePTR   uint16 = 12
	TypeMX    uint16 = 15
	TypeTXT   uint16 = 16
	TypeAAAA  uint16 = 28
	TypeSRV   uint16 = 33
	TypeOPT   uint16 = 41

	ClassINET uint16 = 1
)

// RCode 只定义真正常见的几个；其余值按 TYPE 一样格式化成数字。
const (
	RCodeNoError  uint16 = 0
	RCodeFormErr  uint16 = 1
	RCodeServFail uint16 = 2
	RCodeNXDomain uint16 = 3
	RCodeNotImp   uint16 = 4
	RCodeRefused  uint16 = 5
)

// header 标志位与报文尺寸限制。
const (
	flagQR uint16 = 1 << 15 // 0=查询 1=应答
	flagAA uint16 = 1 << 10 // 权威应答
	flagTC uint16 = 1 << 9  // 被截断：UDP 装不下，客户端应换 TCP 重问
	flagRD uint16 = 1 << 8  // 期望递归
	flagRA uint16 = 1 << 7  // 服务器愿意递归
	flagZ  uint16 = 1 << 6  // 保留，必须为 0

	rcodeMask uint16 = 0x000F

	headerLen = 12 // ID(2)+FLAGS(2)+四段计数(8)

	// maxNameLen：RFC 1035 §2.3.4 规定的域名线上总长上限（含长度字节与根标签）。
	maxNameLen = 255
	// maxNameJumps：压缩指针最多跳转次数。正常报文一跳到底，128 是给深层
	// 共享标签的宽上限；真正的环防御靠 parseName 里"指针必须指向更早偏移"。
	maxNameJumps = 128

	// maxUDPSize：读 UDP 应答的缓冲区。未启用 EDNS0 的传统应答不超过 512
	// 字节，这里留到 4KB 是给带了少量扩展字段的上游留余量。不跟着 64KB
	// 走的原因：UDP 数据报超过 MTU 会被 IP 层分片，大部分内核会直接丢，
	// 所以真实世界里"大应答"必然走 TC→TCP 路径，而不是更大的 UDP 缓冲。
	maxUDPSize = 4096
)

// Message 是一条 DNS 报文（查询或应答）。
//
// 计数（QDCOUNT 等）不落字段：解析时按头部声明的段数驱动，发出时由
// len(切片) 现算。少一处状态就少一处"计数与实际段不一致"的 bug。
type Message struct {
	ID    uint16
	Flags uint16

	Questions   []Question
	Answers     []RR
	Authorities []RR
	Additionals []RR
}

// Question 是问题段的一条记录。
type Question struct {
	Name  string
	Type  uint16
	Class uint16
}

// RR 是一条资源记录。
//
// RData 恒为**原始 RDATA 字节**（从报文里拷贝出来的副本，不引用底层
// buffer —— 引用会让"解析后的报文"和"复用的读缓冲"产生别名，改一处
// 抖一处）。IP/Target 是解析期顺带展开的便利字段：
//
//   - A/AAAA → IP（4/16 字节）
//   - CNAME/NS/PTR → Target（**已展开**的域名，压缩指针已跟随完毕）
//
// 只填这几个类型的理由：本包的使用场景（代理分流）只消费这些记录；
// MX/TXT 等一律只保留 RData 原样透传，够用且不必为它们写测试矩阵。
type RR struct {
	Name   string
	Type   uint16
	Class  uint16
	TTL    uint32
	IP     net.IP // Type A/AAAA 时非 nil
	Target string // Type CNAME/NS/PTR 时非空（展开后的名字）
	RData  []byte // 原始 RDATA（已从报文 buffer 拷贝）
}

// 报文头部标志的读取入口。写侧不提供 setter：查询报文由 NewQuery 构造，
// 应答的 Flags 只在测试与假上游里手工填，多一套 setter 就多一条走偏的路。
func (m *Message) RCode() uint16    { return m.Flags & rcodeMask }
func (m *Message) IsResponse() bool { return m.Flags&flagQR != 0 }
func (m *Message) Truncated() bool  { return m.Flags&flagTC != 0 }

// qtypeName / rcodeName 把数字渲染成日志里能看懂的名字。
//
//	TYPE65 这种未知值格式化成数字而不是报错：上游报个罕见类型不该让
//
// 我们的错误处理链路本身出错。
func qtypeName(t uint16) string {
	switch t {
	case TypeA:
		return "A"
	case TypeAAAA:
		return "AAAA"
	case TypeCNAME:
		return "CNAME"
	case TypeNS:
		return "NS"
	case TypeSOA:
		return "SOA"
	case TypePTR:
		return "PTR"
	case TypeMX:
		return "MX"
	case TypeTXT:
		return "TXT"
	case TypeSRV:
		return "SRV"
	case TypeOPT:
		return "OPT"
	default:
		return fmt.Sprintf("TYPE%d", t)
	}
}

func rcodeName(rc uint16) string {
	switch rc {
	case RCodeNoError:
		return "NOERROR"
	case RCodeFormErr:
		return "FORMERR"
	case RCodeServFail:
		return "SERVFAIL"
	case RCodeNXDomain:
		return "NXDOMAIN"
	case RCodeNotImp:
		return "NOTIMP"
	case RCodeRefused:
		return "REFUSED"
	default:
		return fmt.Sprintf("RCODE%d", rc)
	}
}

// normalizeName 把用户/配置给的域名规范化成缓存与查询用的规范形：
// 去首尾空白、转小写、去掉末尾的点。空名（含单独的 "."）归一为 "."（根域）。
//
// 小写化是安全动作不是美化：DNS 名大小写不敏感，若不做归一，缓存里
// "Example.com" 与 "example.com" 会是两条记录 —— 同一域名两份 TTL，
// fake-ip 也会给出两个地址（最难查的那类 bug）。
func normalizeName(name string) (string, error) {
	n := strings.TrimSpace(name)
	n = strings.ToLower(n)
	// 只掉一个尾点："a.." 里第二个点是空 label，应由 encodeName 报错，
	// 不能被这里悄悄吃掉。
	n = strings.TrimSuffix(n, ".")
	if n == "" {
		return ".", nil
	}
	if _, err := encodeName(n); err != nil {
		return "", err
	}
	return n, nil
}

// NewQuery 构造一条标准查询。RD=1：家用/移动网络里没有递归服务器
// 不可用，所以默认要求上游递归。
func NewQuery(id uint16, name string, qtype uint16) (*Message, error) {
	qname, err := normalizeName(name)
	if err != nil {
		return nil, err
	}
	return &Message{
		ID:    id,
		Flags: flagRD,
		Questions: []Question{
			{Name: qname, Type: qtype, Class: ClassINET},
		},
	}, nil
}

// encodeName 把点分域名编码成 label 序列（不压缩）。
//
// 写侧刻意不做压缩：压缩只在一条报文里出现重复后缀时才有收益，而本包
// 写报文只发生在"构造查询"（单条问题段）与测试替身，省掉压缩算法与其
// 测试面，收益为零。报文里出现的压缩一律只由 parseName 解析。
func encodeName(name string) ([]byte, error) {
	if name == "." || name == "" {
		return []byte{0}, nil // 根域：单个零长度标签
	}
	out := make([]byte, 0, len(name)+2)
	total := 1 // 结尾根标签也要计入 255 上限
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 {
			return nil, fmt.Errorf("dns: 域名 %q 含空 label（连续两个点或以点开头）", name)
		}
		if len(label) > 63 {
			return nil, fmt.Errorf("dns: 域名 %q 的 label 长度 %d 超过 63 字节上限", name, len(label))
		}
		out = append(out, byte(len(label)))
		out = append(out, label...)
		total += 1 + len(label)
		if total > maxNameLen {
			return nil, fmt.Errorf("dns: 域名 %q 编码后总长超过 %d 字节上限", name, maxNameLen)
		}
	}
	out = append(out, 0)
	return out, nil
}

// parseName 从 buf[off:] 解出一个域名，返回 (规范显示名, 名字结束后下一个
// 字节的偏移, error)。支持 label 压缩指针的**解析**。
//
// 指针环防御（这是 DNS 实现最经典的攻击面，务必先读懂再改）：
//
//	指针的 target 必须**严格小于指针自身所在的偏移**。合法编码里压缩指针
//	只会指向"更早出现过的同名后缀"，沿跳转链偏移必然严格递减，因此环在
//	数学上不可能存在；反过来，任何自指（ptr==pos）或指向前方（ptr>pos）
//	的报文都是畸形/攻击报文，一条规则同时否决三类坏输入。再叠一层
//	maxNameJumps 跳数上限与 maxNameLen 总长上限，双重保险。
func parseName(buf []byte, off int) (string, int, error) {
	var (
		labels []string
		total  int // 已累积的 label 线长度（含各自的长度字节）
		pos    = off
		end    = -1 // 名字在报文流里结束的位置（跳过指针链）
		jumps  int
	)
	for {
		if pos >= len(buf) {
			return "", 0, errors.New("dns: 域名意外越界（报文在名字中间被截断）")
		}
		b := buf[pos]
		switch b & 0xC0 {
		case 0x00: // 普通 label
			if b == 0 { // 根标签，名字结束
				pos++
				if end < 0 {
					end = pos
				}
				if len(labels) == 0 {
					return ".", end, nil
				}
				if total+1 > maxNameLen {
					return "", 0, fmt.Errorf("dns: 域名总长 %d 超过 %d 字节上限", total+1, maxNameLen)
				}
				return strings.Join(labels, "."), end, nil
			}
			if pos+1+int(b) > len(buf) {
				return "", 0, fmt.Errorf("dns: label 长度 %d 的数据越界（报文被截断）", b)
			}
			total += 1 + int(b)
			if total+1 > maxNameLen {
				return "", 0, fmt.Errorf("dns: 域名总长超过 %d 字节上限", maxNameLen)
			}
			labels = append(labels, string(buf[pos+1:pos+1+int(b)]))
			pos += 1 + int(b)
		case 0xC0: // 压缩指针
			if pos+1 >= len(buf) {
				return "", 0, errors.New("dns: 压缩指针自身被截断（缺第二个字节）")
			}
			ptr := int(b&0x3F)<<8 | int(buf[pos+1])
			if end < 0 {
				// 名字在报文流里的结束点 = 第一个指针之后；
				// 后续 label 都是通过跳转在别处读到的，不占当前流位置。
				end = pos + 2
			}
			if ptr >= pos {
				return "", 0, fmt.Errorf("dns: 压缩指针指向非法位置 %d（自身位于 %d，疑似指针环/前向指针）", ptr, pos)
			}
			jumps++
			if jumps > maxNameJumps {
				return "", 0, fmt.Errorf("dns: 压缩指针跳转次数超过 %d", maxNameJumps)
			}
			pos = ptr
		default: // 0x40 / 0x80：保留位，合法报文不会出现
			return "", 0, fmt.Errorf("dns: label 长度字节 0x%02x 的高两位是保留值", b)
		}
	}
}

// parseRR 解析一条资源记录。返回的记录里所有字节都是从 buf 拷贝的副本。
func parseRR(buf []byte, off int, section string, idx int) (RR, int, error) {
	var rr RR
	name, next, err := parseName(buf, off)
	if err != nil {
		return rr, 0, fmt.Errorf("dns: %s 第 %d 条的 NAME：%w", section, idx+1, err)
	}
	if next+10 > len(buf) {
		return rr, 0, fmt.Errorf("dns: %s 第 %d 条的固定头部（TYPE/CLASS/TTL/RDLENGTH）不完整", section, idx+1)
	}
	rr.Name = name
	rr.Type = binary.BigEndian.Uint16(buf[next : next+2])
	rr.Class = binary.BigEndian.Uint16(buf[next+2 : next+4])
	rr.TTL = binary.BigEndian.Uint32(buf[next+4 : next+8])
	rdlen := int(binary.BigEndian.Uint16(buf[next+8 : next+10]))
	dataStart := next + 10
	if dataStart+rdlen > len(buf) {
		return rr, 0, fmt.Errorf("dns: %s 第 %d 条 RDLENGTH=%d 超出报文剩余 %d 字节", section, idx+1, rdlen, len(buf)-dataStart)
	}
	raw := buf[dataStart : dataStart+rdlen]
	switch rr.Type {
	case TypeA:
		if rdlen != 4 {
			return rr, 0, fmt.Errorf("dns: %s 第 %d 条 A 记录 RDLENGTH=%d，应为 4", section, idx+1, rdlen)
		}
		rr.RData = append([]byte(nil), raw...)
		rr.IP = net.IP(append([]byte(nil), raw...))
	case TypeAAAA:
		if rdlen != 16 {
			return rr, 0, fmt.Errorf("dns: %s 第 %d 条 AAAA 记录 RDLENGTH=%d，应为 16", section, idx+1, rdlen)
		}
		rr.RData = append([]byte(nil), raw...)
		rr.IP = net.IP(append([]byte(nil), raw...))
	case TypeCNAME, TypeNS, TypePTR:
		// RDATA 是域名：必须在这里跟随压缩指针展开成 Target，
		// 因为指针是相对**整条报文**的偏移，脱离 buf 后无法再解。
		target, _, err := parseName(buf, dataStart)
		if err != nil {
			return rr, 0, fmt.Errorf("dns: %s 第 %d 条的 RDATA 域名：%w", section, idx+1, err)
		}
		rr.Target = target
		rr.RData = append([]byte(nil), raw...)
	default:
		// 不认识的类型：原样保留 RDATA（只透传，不理解语义）。
		rr.RData = append([]byte(nil), raw...)
	}
	return rr, dataStart + rdlen, nil
}

func parseRRs(buf []byte, off, count int, section string) ([]RR, int, error) {
	if count == 0 {
		return nil, off, nil
	}
	rrs := make([]RR, 0, count)
	for i := 0; i < count; i++ {
		rr, next, err := parseRR(buf, off, section, i)
		if err != nil {
			return nil, 0, err
		}
		rrs = append(rrs, rr)
		off = next
	}
	return rrs, off, nil
}

// ParseMessage 解析一条完整 DNS 报文。
//
// 安全姿态：所有输入被视为敌对。畸形输入（截断、RDLENGTH 越界、压缩指针
// 成环、保留位置位、尾部多字节、段数与声明不符）一律返回 error；
// 宁可拒绝一条真上游的怪应答，也不能让畸形数据穿透到分流决策。
func ParseMessage(buf []byte) (*Message, error) {
	if len(buf) < headerLen {
		return nil, fmt.Errorf("dns: 报文过短：%d 字节，不足头部 %d 字节", len(buf), headerLen)
	}
	m := &Message{
		ID:    binary.BigEndian.Uint16(buf[0:2]),
		Flags: binary.BigEndian.Uint16(buf[2:4]),
	}
	qd := int(binary.BigEndian.Uint16(buf[4:6]))
	an := int(binary.BigEndian.Uint16(buf[6:8]))
	ns := int(binary.BigEndian.Uint16(buf[8:10]))
	ar := int(binary.BigEndian.Uint16(buf[10:12]))

	off := headerLen
	for i := 0; i < qd; i++ {
		name, next, err := parseName(buf, off)
		if err != nil {
			return nil, fmt.Errorf("dns: 第 %d 个问题：%w", i+1, err)
		}
		if next+4 > len(buf) {
			return nil, fmt.Errorf("dns: 第 %d 个问题缺少 QTYPE/QCLASS", i+1)
		}
		m.Questions = append(m.Questions, Question{
			Name:  name,
			Type:  binary.BigEndian.Uint16(buf[next : next+2]),
			Class: binary.BigEndian.Uint16(buf[next+2 : next+4]),
		})
		off = next + 4
	}

	var err error
	if m.Answers, off, err = parseRRs(buf, off, an, "answer"); err != nil {
		return nil, err
	}
	if m.Authorities, off, err = parseRRs(buf, off, ns, "authority"); err != nil {
		return nil, err
	}
	if m.Additionals, off, err = parseRRs(buf, off, ar, "additional"); err != nil {
		return nil, err
	}
	if off != len(buf) {
		// 声明段全部解析完却还剩字节：要么是伪造报文，要么是我们数错了段。
		// 任何一种都不该放行 —— 放行的后果是后续按这个错误偏移读到的
		// "记录"参与分流决策。
		return nil, fmt.Errorf("dns: 报文尾部有 %d 个多余字节", len(buf)-off)
	}
	return m, nil
}

// Pack 把报文编码成字节。写侧**不做 name 压缩**（理由见 encodeName）。
// 重新编码 CNAME/NS/PTR 时用展开后的 Target 重打，绝不重放原始 RDATA ——
// 原始 RDATA 里可能含压缩指针，脱离原报文偏移后就是废字节。
func (m *Message) Pack() ([]byte, error) {
	if len(m.Questions) > 65535 || len(m.Answers) > 65535 ||
		len(m.Authorities) > 65535 || len(m.Additionals) > 65535 {
		return nil, errors.New("dns: 某一段记录数超过 65535，无法编码")
	}
	out := make([]byte, headerLen, headerLen+len(m.Questions)*16+len(m.Answers)*40)
	binary.BigEndian.PutUint16(out[0:2], m.ID)
	binary.BigEndian.PutUint16(out[2:4], m.Flags)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(m.Questions)))
	binary.BigEndian.PutUint16(out[6:8], uint16(len(m.Answers)))
	binary.BigEndian.PutUint16(out[8:10], uint16(len(m.Authorities)))
	binary.BigEndian.PutUint16(out[10:12], uint16(len(m.Additionals)))

	for _, q := range m.Questions {
		nb, err := encodeName(q.Name)
		if err != nil {
			return nil, fmt.Errorf("dns: 问题 %q：%w", q.Name, err)
		}
		out = append(out, nb...)
		out = binary.BigEndian.AppendUint16(out, q.Type)
		out = binary.BigEndian.AppendUint16(out, q.Class)
	}
	for _, sec := range []struct {
		name string
		rrs  []RR
	}{{"answer", m.Answers}, {"authority", m.Authorities}, {"additional", m.Additionals}} {
		for _, rr := range sec.rrs {
			nb, err := encodeName(rr.Name)
			if err != nil {
				return nil, fmt.Errorf("dns: %s 记录 %q：%w", sec.name, rr.Name, err)
			}
			out = append(out, nb...)
			out = binary.BigEndian.AppendUint16(out, rr.Type)
			out = binary.BigEndian.AppendUint16(out, rr.Class)
			out = binary.BigEndian.AppendUint32(out, rr.TTL)
			rd, err := rr.rdata()
			if err != nil {
				return nil, fmt.Errorf("dns: %s 记录 %q：%w", sec.name, rr.Name, err)
			}
			if len(rd) > 65535 {
				return nil, fmt.Errorf("dns: %s 记录 %q 的 RDATA 长度 %d 超过 65535", sec.name, rr.Name, len(rd))
			}
			out = binary.BigEndian.AppendUint16(out, uint16(len(rd)))
			out = append(out, rd...)
		}
	}
	return out, nil
}

// rdata 给出编码用的 RDATA。已解码的类型从结构化字段重建，
// 未解码的类型原样吐出。
func (rr *RR) rdata() ([]byte, error) {
	switch rr.Type {
	case TypeA:
		switch {
		case len(rr.RData) == 4:
			return rr.RData, nil
		case rr.IP != nil:
			v4 := rr.IP.To4()
			if v4 == nil {
				return nil, fmt.Errorf("dns: A 记录的 IP %v 不是 IPv4 地址", rr.IP)
			}
			return v4, nil
		default:
			return nil, errors.New("dns: A 记录既无 RData 也无 IP")
		}
	case TypeAAAA:
		switch {
		case len(rr.RData) == 16:
			return rr.RData, nil
		case rr.IP != nil:
			if v16 := rr.IP.To16(); v16 != nil {
				return v16, nil
			}
			return nil, fmt.Errorf("dns: AAAA 记录的 IP %v 非法", rr.IP)
		default:
			return nil, errors.New("dns: AAAA 记录既无 RData 也无 IP")
		}
	case TypeCNAME, TypeNS, TypePTR:
		if rr.Target != "" {
			return encodeName(rr.Target) // 用展开名重编码，杜绝重放压缩指针
		}
		if len(rr.RData) == 0 {
			// CNAME 之类本质是"另一个域名"，没有目标名就是构造错误。
			// 放过它只会让下游收到一条指向根域的畸形记录。
			return nil, fmt.Errorf("dns: %s 记录没有目标名（Target 与 RData 均为空）", qtypeName(rr.Type))
		}
		return rr.RData, nil
	default:
		return rr.RData, nil
	}
}

// clone 深拷贝一份报文。缓存命中时返回副本，调用方改不到缓存里的记录；
// 反过来，入缓存的报文也改成副本，上游 buffer 复用不会抖到缓存。
func (m *Message) clone() *Message {
	if m == nil {
		return nil
	}
	c := *m
	c.Questions = append(make([]Question, 0, len(m.Questions)), m.Questions...)
	c.Answers = cloneRRs(m.Answers)
	c.Authorities = cloneRRs(m.Authorities)
	c.Additionals = cloneRRs(m.Additionals)
	return &c
}

func cloneRRs(in []RR) []RR {
	if in == nil {
		return nil
	}
	out := make([]RR, len(in))
	for i, rr := range in {
		out[i] = rr
		if rr.RData != nil {
			out[i].RData = append([]byte(nil), rr.RData...)
		}
		if rr.IP != nil {
			out[i].IP = append(net.IP(nil), rr.IP...)
		}
	}
	return out
}

// clampTTL 把 TTL 夹紧到 [min, max]。独立成函数而不散在调用点：
// 夹紧语义必须只有一份实现，缓存与 fake-ip 池共用，策略改动只改一处。
func clampTTL(d, min, max time.Duration) time.Duration {
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	return d
}
