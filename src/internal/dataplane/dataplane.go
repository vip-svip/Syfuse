// Package dataplane 把已实现的入站、规则与出站**接成一条真隧道**。
//
// # 它在链路里的位置
//
//	应用/SOCKS5 客户端 → [本包] → 节点服务器 → 目标
//	                       ├─ 入站：internal/socks5
//	                       ├─ 路由：RuleRouter（本包）
//	                       └─ 出站：internal/outbound
//
// 在这个包出现之前，三块各自都能工作，但没有一条路径能跑通：
// 用户选了节点、点了连接，`proxies.start` 如实返回"尚未实现"。
// 本包补的就是那最后一公分 —— 也是产品从"能收能发的零件"
// 变成"真的能代理"的那一步。
//
// # 为什么入站/出站都保留为独立包
//
// 入站（SOCKS5）与出站（各协议拨号器）都不该知道自己的上游是谁：
// 入站可以是 SOCKS5、HTTP，将来是 TUN；出站可以是 ss、trojan、socks5。
// 把它们接起来的决策放在这里，换任何一端都不用改另一端。
//
// # 零依赖
//
// 只用标准库。tests/run_kernel.sh 对 go.mod 的 require 有断言。
package dataplane

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vip-svip/syfuse/src/internal/conn"
	"github.com/vip-svip/syfuse/src/internal/outbound"
	"github.com/vip-svip/syfuse/src/internal/socks5"
)

// ErrRouterMissing 没有可用的路由决策器。
//
// 显式报错而不是"没有路由就走代理"：规则模式下静默走代理，
// 等于把本该直网的流量也送进节点 —— 用户以为自己在分流，其实没有。
var ErrRouterMissing = errors.New("dataplane: 未提供路由决策器")

// ErrListenAddrMissing 没有配置入站监听地址。
var ErrListenAddrMissing = errors.New("dataplane: 未配置入站监听地址")

// Stats 是数据面的累计计数。
//
// 只用单调递增的原始计数，不做速率换算 —— 速率是展示层的事
// （Kotlin 侧 TrafficMeter 负责 EMA）。内核报原始计数，
// 好处是两边不会因为"速率窗口取多长"而吵起来。
type Stats struct {
	// Connections 是**已建立**的连接数（拨号成功才算）。
	// 拨号失败不计入：把失败算成"已连接"会让统计数字虚高。
	Connections int64 `json:"connections"`

	// TxBytes 客户端 → 节点方向的字节数。
	TxBytes int64 `json:"txBytes"`

	// RxBytes 节点 → 客户端方向的字节数。
	RxBytes int64 `json:"rxBytes"`

	// Blocked 被规则拒绝的连接数。
	Blocked int64 `json:"blocked"`
}

// Options 描述一条隧道怎么搭。
type Options struct {
	// Listen 是本地入站监听地址。
	// 形如 `127.0.0.1:1080` 或 `unix:/path/to.sock`。
	//
	// 默认用回环而不是 0.0.0.0：本产品的入站只服务本机（与 TUN/iptables
	// 重定向配合）。对外开放是另一个决定，必须由用户显式打开
	// （见 Config.AllowLAN），不能由默认值顺手放开。
	Listen string

	// Proxy 是走节点的出站拨号器。不可为 nil。
	Proxy outbound.Dialer

	// Direct 是直连拨号器。nil 时用 outbound.DirectDialer。
	Direct outbound.Dialer

	// Router 做路由决策。nil 时所有连接都走 Proxy（等同全局模式）。
	Router Router

	// DialTimeout 单次拨号超时；0 用 outbound.DialTimeout。
	DialTimeout time.Duration

	// Tracker 可选的连接跟踪器（M9）。nil 时不做逐连接记账（零额外开销）。
	//
	// 接入语义：**拨号成功**才算一条连接（与 counters.connections 同一口径，
	// 被路由拒绝的连接不进 Tracker —— 拦截数看 Stats.Blocked，不混在这里）。
	// 连接关闭时按**每连接**字节记账关账；方向口径与 conn.Record 一致：
	// BytesIn = 客户端→上游（tx），BytesOut = 上游→客户端（rx）。
	Tracker *conn.Tracker

	// Logger 可选。nil 时不记录。
	Logger func(format string, args ...any)
}

// counters 是 Stats 的原子实现。
type counters struct {
	connections atomic.Int64
	tx          atomic.Int64
	rx          atomic.Int64
	blocked     atomic.Int64
}

func (c *counters) snapshot() Stats {
	return Stats{
		Connections: c.connections.Load(),
		TxBytes:     c.tx.Load(),
		RxBytes:     c.rx.Load(),
		Blocked:     c.blocked.Load(),
	}
}

// DataPlane 是一条正在运行（或可启动）的隧道。
type DataPlane struct {
	opts   Options
	router Router

	// ln 由**本结构**持有并负责关闭。
	// 不这么做会留下一个竞态：若把 ln 只交给 socks5.Server（它在 Serve
	// 的 goroutine 里才登记 s.listener），Close 抢先一步执行时 s.listener
	// 还是 nil —— 监听器谁都没关，表现为"Close 返回成功但端口还在听"。
	ln  net.Listener
	srv *socks5.Server

	counts counters

	mu     sync.Mutex
	closed bool
}

// New 按 Options 准备一条隧道，**不开始监听**。
//
// 校验全部前置：缺监听地址、缺 Proxy 拨号器、显式要求 Router 却没有，
// 都在这里报错。理由与内核其他地方一致 —— 启动到一半才发现缺东西，
// 会把"配置有问题"和"运行时故障"混成同一个现象。
func New(opts Options) (*DataPlane, error) {
	if opts.Listen == "" {
		return nil, ErrListenAddrMissing
	}
	if opts.Proxy == nil {
		return nil, errors.New("dataplane: 缺少 Proxy 拨号器")
	}
	if opts.Direct == nil {
		opts.Direct = &outbound.DirectDialer{Timeout: opts.DialTimeout}
	}
	// Router 显式为 nil 是允许的（全局模式），但 Options 里能放一个
	// "空实现"的 Router —— 那种也要拦住，否则规则模式会退化成全局模式。
	if opts.Router != nil {
		if err := opts.Router.validate(); err != nil {
			return nil, err
		}
	}

	return &DataPlane{opts: opts, router: opts.Router}, nil
}

// Start 开始监听并服务，直到 Close。
//
// 监听是**同步**建立、服务在后台跑：调用方必须立刻知道"端口有没有抢到"。
// 抢不到（端口占用、权限）要当场报错，而不是等第一批连接打过来才失败。
func (d *DataPlane) Start() (string, error) {
	network, address := socks5.SplitAddr(d.opts.Listen)
	ln, err := net.Listen(network, address)
	if err != nil {
		return "", err
	}

	srv := socks5.New()
	srv.Logger = d.opts.Logger
	srv.MaxHandshakeBytes = 8 * 1024
	srv.Dial = d.dial

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		ln.Close()
		return "", errors.New("dataplane: 已关闭")
	}
	// 监听器由**本结构**持有并负责关闭。
	//
	// 不这么做会留下一个竞态：若把 ln 只交给 socks5.Server（它在 Serve
	//  goroutine 里才登记 s.listener），Close 抢先一步执行时 s.listener
	// 还是 nil —— 监听器谁都没关，表现为"Close 返回成功但端口还在听"。
	// 这条路径由 TestDataPlaneCloseStopsAccepting 守着。
	d.ln = ln
	d.srv = srv
	d.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), nil
}

// closeListenerQuietly 关闭监听器，并把"已经关过了"当作成功。
//
// 之所以必须容错：Close 会关**同一个**监听器两次 —— 本结构持有的 ln
// 一次，srv.Close() 内部对它登记的同一个 ln 再一次。Serve 的 goroutine
// 与 Close 竞争 s.listener 的赋值，所以"哪一次真正执行了关闭"是不
// 确定的，两次都可能拿到 net.ErrClosed。
//
// net.ErrClosed 不代表关闭失败，只代表"目标已经关着"；而 Close 的
// 语义（不再接受新连接、在途连接收尾）此时已经达成。不接住它的后果
// 是间歇性的 Close 报错，实测约 1/6 概率复现，且只在 Start 之后
// 立刻 Close 时出现 —— 那种时序下这段代码是必经之路。
//
// 变异测试记录（别删）：本函数的 ErrClosed 容错对上述竞态**不是**
// 承重墙 —— DataPlane 先关 ln，先关的那个总是成功，被关两次的
// 永远是 srv.Close 内部那一次，承重的是 Close 里 srv.Close 判断上的
// 同一个容错。这里保留它是为了"万一 Close 的关闭顺序被后人调整"，
// 属于纵深防御，不是当前唯一防线。
func closeListenerQuietly(ln net.Listener) error {
	if ln == nil {
		return nil
	}
	if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

// Close 停止接受新连接并等待在途连接结束。
//
// 顺序有讲究：**先关监听器**（同步，保证不再有新连接进来），
// 再等服务端的在途连接收尾。反过来先等待的话，长连接会把 Close 挂住。
func (d *DataPlane) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	ln, srv := d.ln, d.srv
	d.mu.Unlock()

	var err error
	if e := closeListenerQuietly(ln); e != nil {
		err = e
	}
	if srv != nil {
		// srv.Close 内部会再关一次它登记的同一个监听器，同样要容错。
		if e := srv.Close(); e != nil && err == nil && !errors.Is(e, net.ErrClosed) {
			err = e
		}
	}
	return err
}

// Addr 返回实际监听地址；未启动时返回空串。
func (d *DataPlane) Addr() string {
	d.mu.Lock()
	srv := d.srv
	d.mu.Unlock()
	if srv == nil {
		return ""
	}
	return srv.Addr()
}

// Stats 返回当前累计计数。
func (d *DataPlane) Stats() Stats { return d.counts.snapshot() }

// dial 是SOCKS5 入站的拨号钩子：先做路由决策，再选拨号器。
//
// 这一步是整个产品"分流"语义的所在。顺序不能反：
// 必须先决策再拨号，否则"拒绝"的流量也已经发出去了。
func (d *DataPlane) dial(network, address string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("dataplane: 端口 %q 非数字: %w", portStr, err)
	}

	decision := DecisionProxy
	if d.router != nil {
		decision = d.router.Decide(host, port)
	}

	switch decision {
	case DecisionReject:
		d.counts.blocked.Add(1)
		// 包装 ErrPolicyRejected 让 socks5 入站回"规则集不允许"应答码，
		// 而不是笼统的 general failure —— 客户端要能区分
		// "被策略拦了"与"连不上"（classifyDialError 的第一条判断就是它）。
		return nil, fmt.Errorf("%w: %s", socks5.ErrPolicyRejected,
			net.JoinHostPort(host, portStr))
	case DecisionDirect:
		c, err := d.opts.Direct.DialContext(context.Background(), network, address)
		if err != nil {
			return nil, err
		}
		return d.tracked(c, decision, host, port), nil
	default: // DecisionProxy
		c, err := d.opts.Proxy.DialContext(context.Background(), network, address)
		if err != nil {
			return nil, err
		}
		return d.tracked(c, decision, host, port), nil
	}
}

// tracked 给拨号成功的连接套字节计数并登记进 Tracker（若配置了）。
//
// 全局计数（counts.tx/rx）与逐连接计数**同一层包装里一起做**：
// 拆成两层包装会让 Read/Write 多一次接口转发，而这是数据面最热的路径。
func (d *DataPlane) tracked(c net.Conn, decision Decision, host string, port int) net.Conn {
	d.counts.connections.Add(1)
	cc := &countingConn{Conn: c, tx: &d.counts.tx, rx: &d.counts.rx}
	if d.opts.Tracker == nil {
		return cc
	}
	// 每连接独立计数器：Close 时把真实字节数关账进 Tracker。
	// 不用全局计数器减法推算 —— 并发连接下减法会把别人的流量算到自己头上。
	txC, rxC := &atomic.Int64{}, &atomic.Int64{}
	cc.txConn, cc.rxConn = txC, rxC
	id := d.opts.Tracker.Open(conn.ConnInfo{
		Protocol: "tcp",
		DstHost:  host,
		DstPort:  port,
		Labels:   map[string]string{"decision": decision.String()},
	})
	cc.onClose = func(txn, rxn int64) {
		d.opts.Tracker.Close(id, txn, rxn, "closed")
	}
	return cc
}

// countingConn 给连接套一层字节计数。
//
// # 为什么必须显式转发 CloseWrite/CloseRead
//
// relay 在客户端写完后会调 upstream.CloseWrite()，让上游看到 EOF
// （否则下载会停在 99%）。若只嵌入 net.Conn 接口，CloseWrite 不会出现在
// 方法集里 —— 包装层把 EOF 这一跳吞掉，症状就是"大文件传不完"。
// 这是本函数存在的全部理由。
type countingConn struct {
	net.Conn
	tx *atomic.Int64
	rx *atomic.Int64

	// txConn/rxConn 是**每连接**计数器（接入 Tracker 时才非 nil）。
	// 与全局计数同一处累加，Close 时一起关账，避免两层包装。
	txConn *atomic.Int64
	rxConn *atomic.Int64

	// onClose 在连接真正关闭时收到每连接字节数（接 Tracker 时才非 nil）。
	// closeOnce 保证只关一次账：并发双端关闭时第二次 Close 直接跳过。
	onClose   func(txn, rxn int64)
	closeOnce sync.Once
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.rx.Add(int64(n))
		if c.rxConn != nil {
			c.rxConn.Add(int64(n))
		}
	}
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.tx.Add(int64(n))
		if c.txConn != nil {
			c.txConn.Add(int64(n))
		}
	}
	return n, err
}

// Close 关闭底层连接并把每连接字节数关账进 Tracker。
//
// 关账在底层 Close **之前**：先记账再关闭，保证 Close 返回后
// Snapshot/Stats 立即可见完整字节数（先关后账则有一个窗口期，
// 统计接口会在连接已关闭时报出未关账的数字）。
func (c *countingConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		if c.onClose != nil && c.txConn != nil {
			c.onClose(c.txConn.Load(), c.rxConn.Load())
		}
		err = c.Conn.Close()
	})
	return err
}

// CloseWrite 向下游发 FIN。底层不支持时返回 nil 而不是 Close：
// 这里被调用意味着"客户端已经不再发送"，吞掉它最多让上游多等一会儿；
// 误调用 Close 会把整个连接掐掉，那是另一种故障。
func (c *countingConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// CloseRead 与 CloseWrite 同理。
func (c *countingConn) CloseRead() error {
	if cr, ok := c.Conn.(interface{ CloseRead() error }); ok {
		return cr.CloseRead()
	}
	return nil
}

// countedConnFrom 构造只带全局计数的包装（无 Tracker 路径的兼容入口）。
func (d *DataPlane) counting(c net.Conn, tx, rx *atomic.Int64) net.Conn {
	return &countingConn{Conn: c, tx: tx, rx: rx}
}
