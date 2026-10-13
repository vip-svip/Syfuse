package dns

// 本文件实现多上游解析器：UDP/TCP/DoT/DoH，顺序故障切换或竞速，
// UDP 截断(TC=1)自动换 TCP 重问，并如实上报 SERVFAIL/NXDOMAIN。
//
// 设计取舍先说清楚：
//
//   - 顺序模式（默认）按上游列表逐个尝试，任一"可接受应答"即刻返回；
//     不可接受（传输失败/解析失败/溯源校验失败/SERVFAIL/REFUSED）才切下一个。
//     NXDOMAIN 是权威的"不存在"答复，切上游只会浪费时间且可能拿到不同
//     视图的答案，因此与 NOERROR 一样立即返回。
//   - 竞速模式同时向所有被选上游发问，第一个可接受应答获胜，其余通过
//     context 取消。代价是每个查询的出口流量/连接数翻倍；换来的是单点
//     慢上游不再拖累总延迟。默认关闭，由 Resolver.Race 打开。
//   - SERVFAIL/REFUSED 触发切换，但若所有上游都只能给出这类应答，最后
//     一份应答会连错误一起上抛：调用方拿到的 (msg, err) 里 msg.RCode()
//     如实呈现失败原因，绝不吞掉再猜一个"看起来像成功"的空答案。

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 协议标识（与配置层约定的小写字符串）。字符串而不是自定义类型：
// 配置里直接写字面量，少一层转换；校验已在 ParseUpstream 收敛。
const (
	ProtoUDP = "udp"
	ProtoTCP = "tcp"
	ProtoDoT = "dot"
	ProtoDoH = "doh"
	ProtoDoQ = "doq"
)

const (
	// maxDoHBody：DoH 应答体读取上限。DNS 报文本身受 64KB 长度前缀约束，
	// 多留一个量级是给合法 padding；超出即可判为异常，防止恶意上游
	// 用一个无限响应体打爆内存。
	maxDoHBody = 1 << 20

	// defaultUDPTimeout：UDP 一次尝试默认 2s。UDP 没有握手，"慢"就是丢包，
	// 等满 3s 只会拖长总延迟；TCP/DoT/DoH 有握手代价，给 3s。
	defaultUDPTimeout = 2 * time.Second
	defaultTCPTimeout = 3 * time.Second
	defaultDoTTimeout = 3 * time.Second
	defaultDoHTimeout = 3 * time.Second
)

var (
	// ErrNoUpstreams 一个上游都没配。
	ErrNoUpstreams = errors.New("dns: 未配置任何上游")

	// ErrDoQUnsupported 配置了 doq(QUIC) 上游。**刻意不支持**：标准库没有
	// QUIC 实现，做它必然引入第三方依赖，与全仓"零第三方依赖"约束冲突
	// （tests/run_kernel.sh 有断言守着）。用可 errors.Is 的错误而不是静默
	// 跳过，好让配置错误在第一次查询就暴露，而不是表现成"某些域名解析不了"
	// 这种要查半天的症状。
	ErrDoQUnsupported = errors.New("dns: DoQ(QUIC) 暂不支持：标准库未内置 QUIC，为避免第三方依赖不予实现，请改用 DoT/DoH")

	// ErrAllUpstreamsFailed 所有上游都没给出可接受的应答。
	ErrAllUpstreamsFailed = errors.New("dns: 所有上游均未能给出可接受的应答")
)

// Upstream 描述一个上游 DNS 服务器。
type Upstream struct {
	Protocol string // udp / tcp / dot / doh / doq
	Address  string // udp/tcp/dot/doq："host:port"；doh：完整 URL
	Timeout  time.Duration

	// Default 标记"主力上游"：只要有任一上游 Default=true，本次查询就只在
	// Default 之间进行（它们之间照样故障切换/竞速）；非 Default 的全部落选。
	// 全都没有 Default 时视为全部可用。这样配置可以写"主力+备用"，平时不
	// 打扰备用（备用往往计费/画像/日志语义都不同）。
	Default bool
}

// String 渲染成日志/错误消息里的形式（带协议前缀，避免同端口的 udp 与
// dot 上游在日志里长得一样、分不清是谁出的错）。
func (u Upstream) String() string {
	return u.Protocol + "://" + u.Address
}

// ParseUpstream 从配置字符串解析上游。支持：
//
//	8.8.8.8 / 8.8.8.8:5353      → udp（省略端口补 53）
//	udp://… / tcp://…            → 显式 UDP/TCP（省略端口补 53）
//	tls://dns.google             → DoT（省略端口补 853）
//	dot://…                      → tls:// 的别名
//	https://dns.google/dns-query → DoH（保留完整 URL 供 HTTP 使用）
//	quic://dns.adguard.com       → DoQ（可解析，查询时返回 ErrDoQUnsupported）
//
// 刻意不做的事：不解析 DoH/DoT 地址里的主机名 —— 引导解析（bootstrap）
// 属于上层配置职责（模块侧已知晓 bootstrapped IP），包里做只会把一份职责
// 复制成两份、两处都可能错。
func ParseUpstream(spec string) (Upstream, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return Upstream{}, errors.New("dns: 上游描述为空")
	}
	var up Upstream
	if i := strings.Index(s, "://"); i >= 0 {
		scheme := strings.ToLower(s[:i])
		rest := s[i+3:]
		switch scheme {
		case "udp":
			up.Protocol = ProtoUDP
		case "tcp":
			up.Protocol = ProtoTCP
		case "tls", "dot":
			up.Protocol = ProtoDoT
		case "https":
			up.Protocol = ProtoDoH
		case "quic", "doq":
			up.Protocol = ProtoDoQ
		default:
			return Upstream{}, fmt.Errorf("dns: 无法识别的上游协议 %q（期望 udp/tcp/tls/dot/https/quic）", scheme)
		}
		if rest == "" {
			return Upstream{}, fmt.Errorf("dns: 上游 %q 缺少协议前缀后的地址", spec)
		}
		s = rest
	} else {
		up.Protocol = ProtoUDP
	}

	switch up.Protocol {
	case ProtoDoH:
		// Address 必须是完整 URL（上面把 scheme 吃掉了，这里拼回去），
		// http.NewRequest 要能直接用。
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return Upstream{}, fmt.Errorf("dns: DoH 上游 URL %q 非法", s)
		}
		up.Address = "https://" + s
		up.Timeout = defaultDoHTimeout
	default:
		if !strings.Contains(s, ":") {
			port := "53"
			if up.Protocol == ProtoDoT || up.Protocol == ProtoDoQ {
				port = "853"
			}
			s = net.JoinHostPort(s, port)
		}
		if host, port, err := net.SplitHostPort(s); err != nil || host == "" || port == "" {
			return Upstream{}, fmt.Errorf("dns: 上游地址 %q 不是合法的 host:port", s)
		}
		up.Address = s
		switch up.Protocol {
		case ProtoUDP:
			up.Timeout = defaultUDPTimeout
		case ProtoTCP:
			up.Timeout = defaultTCPTimeout
		case ProtoDoT, ProtoDoQ:
			up.Timeout = defaultDoTTimeout
		}
	}
	return up, nil
}

// Resolver 是多上游 DNS 解析器。
//
// 并发安全：所有字段只在构造后读取，Query 本身无共享可变状态
// （查询 ID 每次现取随机数），可多 goroutine 并发调用。
type Resolver struct {
	Upstreams []Upstream

	// Race 打开竞速模式：同时向所有被选上游发问，第一个可接受应答获胜。
	// 关闭（默认）时按列表顺序逐个尝试、失败即切换。
	Race bool

	// TotalTimeout 是整个 Query（含所有上游尝试与 TC 重问）的总预算；
	// 0 表示不额外设限，只用 ctx 与各上游自己的 Timeout。
	TotalTimeout time.Duration

	// Cache 可选应答缓存；nil 表示不缓存。 resolver 与缓存是组合关系：
	// 上层可以只为特定 qtype 开缓存，或实现自己的缓存策略。
	Cache *Cache

	// Hosts 可选的静态 hosts 表；nil 表示不使用。非 nil 时，命中
	// （域名在表内且有对应地址族的地址）的查询由 Hosts.answer 本地构造
	// 应答直接返回：不问上游、不经缓存、不经 fake-ip。
	Hosts *Hosts

	// TLSConfig 是 DoT 拨号用的 tls.Config 基础（会被 Clone 后使用）。
	// nil 时用系统根证书 + 自动 SNI。测试可注入自签 CA / InsecureSkipVerify。
	TLSConfig *tls.Config

	// HTTPClient 是 DoH 使用的客户端；nil 时用包级默认客户端。
	// 请求超时由 ctx 控制，故不设 Client.Timeout —— 设了会和 ctx 叠加，
	// 出现"谁先到算谁"的难查行为。测试注入自定义 Transport（RootCAs 或
	// InsecureSkipVerify）即可对接 httptest.NewTLSServer。
	HTTPClient *http.Client
}

// NewResolver 构造解析器。变参形式让"单上游/双上游"的调用点都不用
// 现造切片。
func NewResolver(upstreams ...Upstream) *Resolver {
	return &Resolver{Upstreams: upstreams}
}

// defaultDoHClient 是 DoH 的包级默认客户端（无超时，由 ctx 控时长）。
var defaultDoHClient = &http.Client{}

// Query 向解析器发起一次查询。
//
// 返回值的语义契约（调用方务必按此理解，这是"不撒谎"的关键）：
//
//   - (msg, nil)：拿到了协议完整的应答。**必须检查 msg.RCode()** ——
//     NXDOMAIN 等"查无此名"会以应答里的 RCODE 如实呈现，不是 error。
//   - (msg, err)：所有上游都不可接受。若至少有一个上游给过协议完整的
//     应答（如 SERVFAIL），msg 就是其中最后一份，msg.RCode() 告诉你是
//     哪种失败；err 包裹 ErrAllUpstreamsFailed（可用 errors.Is 判定）。
//   - (nil, err)：连一份能解析的应答都没拿到（传输全失败/超时/取消）。
func (r *Resolver) Query(ctx context.Context, name string, qtype uint16) (*Message, error) {
	if ctx == nil {
		// 防御来自 gomobile 绑定/JNI 侧的 nil ctx：Done() 会 panic，
		// 而"上层忘了传 ctx"不该让守护进程崩。
		ctx = context.Background()
	}
	req, err := NewQuery(newQueryID(), name, qtype)
	if err != nil {
		return nil, err
	}
	qname := req.Questions[0].Name

	// hosts 静态表优先于一切网络路径（含缓存）：见 hosts.go 头注释。
	// 放在缓存之前不冲突 —— 缓存里只会躺着上游曾给出的答案，而 hosts 是
	// 管理员拍板的映射，理应压过它；本地构造应答零网络往返，也无需
	// TotalTimeout 兜底，故顺序在预算计算之前。
	//
	// 同时排在"无上游"检查之前：只配 hosts、不配任何上游（纯静态解析
	// 部署）是合法形态，命中的域名不应被 ErrNoUpstreams 挡住。
	if resp := r.Hosts.answer(req); resp != nil {
		return resp, nil
	}
	if len(r.Upstreams) == 0 {
		return nil, ErrNoUpstreams
	}

	if r.TotalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.TotalTimeout)
		defer cancel()
	}

	// 缓存优先。GET 到即返回克隆（见 Cache.Get），绝不给调用方缓存内对象的
	// 引用 —— 那会让一次误改污染后续所有命中。
	if r.Cache != nil {
		if hit := r.Cache.Get(qname, qtype); hit != nil {
			return hit, nil
		}
	}

	sel := r.selectedUpstreams()
	var (
		msg *Message
	)
	if r.Race {
		msg, err = r.queryRace(ctx, sel, req)
	} else {
		msg, err = r.querySequential(ctx, sel, req)
	}

	// 只有"真的查到了且不带错误语义"的应答才进缓存。
	// 不缓存 SERVFAIL/NXDOMAIN：前者是上游故障，缓存它等于把一次故障
	// 复制成持续故障；后者的负面缓存需要 SOA TTL 语义，本单元不做，
	// 把决策留给上层策略层。缓存失败只损失性能，不影响查询结果本身。
	if err == nil && r.Cache != nil && msg.RCode() == RCodeNoError && len(msg.Answers) > 0 {
		_ = r.Cache.Put(qname, qtype, msg)
	}

	if err != nil {
		return msg, fmt.Errorf("dns: 查询 %s %s 失败：%w", qname, qtypeName(qtype), err)
	}
	return msg, nil
}

// selectedUpstreams 选出本次查询使用的上游集合。规则见 Upstream.Default。
func (r *Resolver) selectedUpstreams() []Upstream {
	var sel []Upstream
	for _, up := range r.Upstreams {
		if up.Default {
			sel = append(sel, up)
		}
	}
	if len(sel) == 0 {
		return r.Upstreams
	}
	return sel
}

// newQueryID 每次现取 16bit 随机 ID。
//
// 为什么不用原子自增：ID 同时是应答溯源校验的依据（见 validateResponse），
// 可预测的 ID 会显著降低伪造应答的成本。crypto/rand 两次读取的代价
// 相对一次 DNS 查询（毫秒级 RTT）可以忽略；只有它出问题（几乎不会）
// 才退化为时间低位，并保证非零。
func newQueryID() uint16 {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint16(time.Now().UnixNano() & 0xFFFF)
	}
	return binary.BigEndian.Uint16(b[:])
}

// acceptableRCode 判断一个 RCODE 是"可接受应答"还是"上游失败"。
// SERVFAIL/REFUSED 是这个上游答不出来，换一个也许能答；其余 RCODE
// （NOERROR/NXDOMAIN/FORMERR/NOTIMP…）都是协议意义上的有效答复，
// 如实上抛 —— 尤其 NXDOMAIN，切上游只会浪费时间还可能拿到不一致的答案。
func acceptableRCode(rc uint16) bool {
	return rc != RCodeServFail && rc != RCodeRefused
}

// finishQuery 汇总一次查询的结局：有"最后一份协议完整但被拒"的应答就
// 连着它一起返回，否则只返回错误。
func finishQuery(lastBad *Message, reasons []string) (*Message, error) {
	if lastBad != nil {
		return lastBad, fmt.Errorf("%w（%s；最后一份应答 RCODE=%s）",
			ErrAllUpstreamsFailed, strings.Join(reasons, "；"), rcodeName(lastBad.RCode()))
	}
	return nil, fmt.Errorf("%w（%s）", ErrAllUpstreamsFailed, strings.Join(reasons, "；"))
}

// querySequential 顺序模式：逐个上游尝试，可接受应答即刻返回。
func (r *Resolver) querySequential(ctx context.Context, sel []Upstream, req *Message) (*Message, error) {
	var (
		lastBad *Message
		reasons []string
	)
	for _, up := range sel {
		// 每个上游的预算 = min(它自己的 Timeout, ctx 剩余时间)。
		// 这样"总超时"是真正的总预算，不会被一个配了长超时的上游耗穿。
		attempt := up.Timeout
		if attempt <= 0 {
			attempt = defaultTCPTimeout
		}
		if dl, ok := ctx.Deadline(); ok {
			if remain := time.Until(dl); remain < attempt {
				attempt = remain
			}
		}
		if attempt <= 0 {
			reasons = append(reasons, "总超时已用尽")
			break
		}

		resp, err := r.exchange(ctx, up, req, attempt)
		if err != nil {
			reasons = append(reasons, fmt.Sprintf("%s：%v", up, err))
			// 查询被取消/总超时到了就没必要再试下一个上游：每个都会因
			// 同一个原因瞬间失败，只是把日志写长。
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("dns: 查询被取消或超时：%w", ctxErr)
			}
			continue
		}
		if !acceptableRCode(resp.RCode()) {
			lastBad = resp
			reasons = append(reasons, fmt.Sprintf("%s：应答 RCODE=%s", up, rcodeName(resp.RCode())))
			continue
		}
		return resp, nil
	}
	return finishQuery(lastBad, reasons)
}

// queryRace 竞速模式：所有被选上游同时发问。
//
// "最先返回者胜"精确化为：第一个**可接受**应答胜（与顺序模式的判定完全
// 一致，行为可预测）；SERVFAIL/REFUSED 不配获胜，等其余上游的结果。
// 代价：最坏情况（大家都晚）要等最慢那个，与顺序模式无异；收益：常态下
// 不再被最慢上游拖住。输家的连接通过 rctx 取消后自行关闭。
func (r *Resolver) queryRace(ctx context.Context, sel []Upstream, req *Message) (*Message, error) {
	type raceResult struct {
		up  Upstream
		msg *Message
		err error
	}
	ch := make(chan raceResult, len(sel))
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, up := range sel {
		go func(up Upstream) {
			attempt := up.Timeout
			if attempt <= 0 {
				attempt = defaultTCPTimeout
			}
			// 竞速下每个上游的预算用各自 Timeout；总预算由 ctx（外层
			// TotalTimeout）兜底，收集循环会在 ctx 结束时收摊。
			msg, err := r.exchange(rctx, up, req, attempt)
			ch <- raceResult{up: up, msg: msg, err: err}
		}(up)
	}

	var (
		lastBad *Message
		reasons []string
	)
	for i := 0; i < len(sel); i++ {
		select {
		case res := <-ch:
			if res.err == nil && acceptableRCode(res.msg.RCode()) {
				return res.msg, nil
			}
			if res.err != nil {
				reasons = append(reasons, fmt.Sprintf("%s：%v", res.up, res.err))
			} else {
				lastBad = res.msg
				reasons = append(reasons, fmt.Sprintf("%s：应答 RCODE=%s", res.up, rcodeName(res.msg.RCode())))
			}
		case <-ctx.Done():
			reasons = append(reasons, "总超时到达："+ctx.Err().Error())
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("dns: 查询被取消或超时：%w", ctxErr)
			}
			return finishQuery(lastBad, reasons)
		}
	}
	return finishQuery(lastBad, reasons)
}

// exchange 向上游发起一次完整交换（含 UDP 的 TC→TCP 重问）。
// attempt 是本次尝试的绝对预算（已由调用方按 ctx 剩余时间裁剪）。
func (r *Resolver) exchange(ctx context.Context, up Upstream, req *Message, attempt time.Duration) (*Message, error) {
	switch up.Protocol {
	case ProtoUDP:
		return r.exchangeUDP(ctx, up, req, attempt)
	case ProtoTCP, ProtoDoT:
		return r.exchangeStream(ctx, up, req, attempt, up.Protocol == ProtoDoT)
	case ProtoDoH:
		return r.exchangeDoH(ctx, up, req, attempt)
	case ProtoDoQ:
		return nil, fmt.Errorf("上游 %s 使用 DoQ(QUIC)：%w", up, ErrDoQUnsupported)
	default:
		return nil, fmt.Errorf("上游 %s 的协议 %q 无法识别（可用 ParseUpstream 解析配置）", up, up.Protocol)
	}
}

// exchangeUDP：UDP 交换；应答带 TC=1 时自动换同一地址的 TCP 重问。
//
// TC→TCP 是真实世界的高频路径：任何超过 512 字节且未协商 EDNS0 的应答
// 都会被上游截断。不处理的后果是"某些网站解析拿到半个 A 记录集"，
// 时好时坏极难查。测试里有专门用例锁住这条路径。
func (r *Resolver) exchangeUDP(ctx context.Context, up Upstream, req *Message, attempt time.Duration) (*Message, error) {
	start := time.Now()
	d := net.Dialer{Timeout: attempt}
	conn, err := d.DialContext(ctx, "udp", up.Address)
	if err != nil {
		return nil, fmt.Errorf("UDP 拨号失败：%w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(earlierOf(start.Add(attempt), ctxDeadline(ctx)))

	q, err := req.Pack()
	if err != nil {
		return nil, fmt.Errorf("打包查询失败：%w", err)
	}
	if _, err := conn.Write(q); err != nil {
		return nil, fmt.Errorf("UDP 写入失败：%w", err)
	}
	buf := make([]byte, maxUDPSize)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("UDP 读取失败：%w", err)
	}
	resp, err := ParseMessage(buf[:n])
	if err != nil {
		return nil, fmt.Errorf("解析应答失败：%w", err)
	}
	if err := validateResponse(req, resp); err != nil {
		return nil, fmt.Errorf("应答未通过溯源校验：%w", err)
	}
	if resp.Truncated() {
		// 换 TCP 重问。预算重新给一个完整 attempt：已经花掉的 UDP 时间
		// 是沉没成本，而"剩下的预算"可能只剩几毫秒、连 TCP 握手都不够。
		// 总预算仍由外层 ctx 兜底，不会无限放大。
		return r.exchangeStream(ctx, up, req, attempt, false)
	}
	return resp, nil
}

// exchangeStream：TCP/DoT 交换（两字节长度前缀分帧，RFC 1035 §4.2.2）。
// useTLS 为 true 时按 DoT 拨号。
func (r *Resolver) exchangeStream(ctx context.Context, up Upstream, req *Message, attempt time.Duration, useTLS bool) (*Message, error) {
	start := time.Now()
	d := &net.Dialer{Timeout: attempt}
	var (
		conn net.Conn
		err  error
	)
	if useTLS {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if r.TLSConfig != nil {
			cfg = r.TLSConfig.Clone()
			if cfg.MinVersion == 0 {
				cfg.MinVersion = tls.VersionTLS12
			}
		}
		// ServerName 留空时 crypto/tls 会从地址里的主机名自动推导，
		// IP 字面量则按规则省略 SNI —— 与 net/http 的行为一致。
		conn, err = tls.DialWithDialer(d, "tcp", up.Address, cfg)
		if err != nil {
			return nil, fmt.Errorf("DoT TLS 握手失败： %w", err)
		}
	} else {
		conn, err = d.DialContext(ctx, "tcp", up.Address)
		if err != nil {
			return nil, fmt.Errorf("TCP 拨号失败：%w", err)
		}
	}
	defer conn.Close()
	_ = conn.SetDeadline(earlierOf(start.Add(attempt), ctxDeadline(ctx)))

	q, err := req.Pack()
	if err != nil {
		return nil, fmt.Errorf("打包查询失败：%w", err)
	}
	var frame [2]byte
	binary.BigEndian.PutUint16(frame[:], uint16(len(q)))
	if _, err := conn.Write(append(frame[:], q...)); err != nil {
		return nil, fmt.Errorf("TCP 写入失败：%w", err)
	}
	if _, err := io.ReadFull(conn, frame[:]); err != nil {
		return nil, fmt.Errorf("TCP 读取长度前缀失败：%w", err)
	}
	n := int(binary.BigEndian.Uint16(frame[:]))
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, fmt.Errorf("TCP 读取报文体（%d 字节）失败：%w", n, err)
	}
	resp, err := ParseMessage(body)
	if err != nil {
		return nil, fmt.Errorf("解析应答失败：%w", err)
	}
	if err := validateResponse(req, resp); err != nil {
		return nil, fmt.Errorf("应答未通过溯源校验：%w", err)
	}
	return resp, nil
}

// exchangeDoH：RFC 8484 DoH，POST application/dns-message。
//
// 只实现 POST 不实现 GET：GET 要把 base64url 塞进 URL，会撞上代理链各层
// 的 URL 长度限制与查询串日志留存，而本包的使用场景（代理内核）里 POST
// 没有这些副作用。超时全部走 ctx，不动 Client.Timeout（见字段注释）。
func (r *Resolver) exchangeDoH(ctx context.Context, up Upstream, req *Message, attempt time.Duration) (*Message, error) {
	body, err := req.Pack()
	if err != nil {
		return nil, fmt.Errorf("打包查询失败：%w", err)
	}
	actx, cancel := context.WithTimeout(ctx, attempt)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(actx, http.MethodPost, up.Address, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造 HTTP 请求失败：%w", err)
	}
	httpReq.Header.Set("Content-Type", "application/dns-message")
	httpReq.Header.Set("Accept", "application/dns-message")

	client := r.HTTPClient
	if client == nil {
		client = defaultDoHClient
	}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("DoH 请求失败：%w", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH 上游返回 HTTP %d", httpResp.StatusCode)
	}
	// 对 Content-Type 宽容：有些实现漏设；状态码 200 且体能解析就认。
	// 反方向和严格派的风险不对称：宁可放过一个怪服务器，也不要因为
	// 一个 header 拼写让用户"某些 DoH 上游完全不能用"。
	data, err := io.ReadAll(io.LimitReader(httpResp.Body, maxDoHBody))
	if err != nil {
		return nil, fmt.Errorf("读取 DoH 应答体失败：%w", err)
	}
	resp, err := ParseMessage(data)
	if err != nil {
		return nil, fmt.Errorf("解析应答失败：%w", err)
	}
	if err := validateResponse(req, resp); err != nil {
		return nil, fmt.Errorf("应答未通过溯源校验：%w", err)
	}
	return resp, nil
}

// validateResponse 应答溯源校验：ID 必须与请求一致、QR 位必须置起、
// 问题段必须逐字回显我们问的问题。
//
// 这不是洁癖而是安全边界：DNS over UDP 明文且无认证，路径上的攻击者
// 可以抢在真上游之前伪造应答。ID + 问题段回显是最低成本的校验，能挡掉
// 大部分乱猜/缓存投毒尝试 —— 挡不住精心构造的 16bit ID 碰撞，但那需要
// 每秒上万个包的运气，且 DoT/DoH 场景根本不存在这个面。
func validateResponse(req, resp *Message) error {
	if resp.ID != req.ID {
		return fmt.Errorf("ID 不匹配（请求 0x%04x，应答 0x%04x）", req.ID, resp.ID)
	}
	if !resp.IsResponse() {
		return errors.New("应答未置 QR 位（这不是一份应答）")
	}
	if len(resp.Questions) != len(req.Questions) {
		return fmt.Errorf("问题段数量不一致（请求 %d，应答 %d）", len(req.Questions), len(resp.Questions))
	}
	for i := range req.Questions {
		rq, aq := req.Questions[i], resp.Questions[i]
		if rq.Type != aq.Type || rq.Class != aq.Class ||
			!strings.EqualFold(rq.Name, aq.Name) {
			// EqualFold：大小写在 DNS 里不敏感，服务端有权改写成别的
			// 大小写回显；逐字节比较会误杀合法应答。
			return fmt.Errorf("第 %d 个问题与请求不一致（请求 %s %s，应答 %s %s）",
				i+1, rq.Name, qtypeName(rq.Type), aq.Name, qtypeName(aq.Type))
		}
	}
	return nil
}

// earlierOf / ctxDeadline 是连接 deadline 计算的小工具：
// deadline 取 min(自身预算, ctx 截止时间)，谁先到谁生效，
// 避免"ctx 早就到期但连接还在傻等"的悬挂读取。
func earlierOf(a, b time.Time) time.Time {
	if !b.IsZero() && b.Before(a) {
		return b
	}
	return a
}

func ctxDeadline(ctx context.Context) time.Time {
	if dl, ok := ctx.Deadline(); ok {
		return dl
	}
	return time.Time{}
}
