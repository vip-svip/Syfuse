package dns

// 本文件实现 fake-ip 池：从配置的 CIDR 段分配地址，维护 域名↔地址 双向
// 映射，带过期回收。
//
// fake-ip 在代理里的角色：对选择了"假 IP"策略的域名，解析器不真的去上游
// 问，而是从池里发一个本段假地址；之后该地址上的连接被数据面按 域名
// 逆向映射接管。定位随 Syfuse 模块优先的架构：
//
//	Resolver.Query  → 上层判定该域名要走 fake-ip →  FakeIPPool.Allocate
//	数据面看到发往假地址的连接 →  FakeIPPool.Reverse 取回域名 → 走代理
//
// 本文件只负责任务的两半（分配/回收 + 双向映射），不感知数据面。

import (
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// ErrFakeIPExhausted 池里没有可用地址了。
//
// 必须显式报错而不是"复用/凑合"：两个域名拿到同一个 fake-ip 意味着其中
// 一个域名的连接会被送到另一个域名的代理路上 —— 症状是"打开 A 网站看到
// B 网站的内容/证书错误"，且只在池子快满时偶发，是最难查的一类 bug。
var ErrFakeIPExhausted = errors.New("dns: fake-ip 池已耗尽，没有可用地址可分配")

// fake-ip 生命周期的默认值与夹紧边界（语义同 Cache，理由见 cache.go）。
const (
	defaultFakeIPTTL    = 10 * time.Minute
	defaultFakeIPMinTTL = 5 * time.Second
	defaultFakeIPMaxTTL = 24 * time.Hour
)

type fakeIPEntry struct {
	ip        net.IP
	domain    string
	expiresAt time.Time
}

// FakeIPPool 是并发安全的 fake-ip 池。
type FakeIPPool struct {
	mu sync.Mutex

	subnet *net.IPNet
	v4     bool
	base   *big.Int // 网络地址的整数值
	// 可用偏移区间 [startOff, endOff)：IPv4 跳过网络地址(0)与广播地址
	// (最后一个)，因为把这两个地址发给用户连接会撞上链路层的怪异行为。
	startOff *big.Int
	endOff   *big.Int
	usable   *big.Int // = endOff - startOff
	next     *big.Int // 扫描游标（下一个待检查的偏移）

	ipToEntry  map[string]*fakeIPEntry // key：ip.String()
	domainToIP map[string]net.IP       // key：规范域名（小写无尾点）
	live       int                     // 存活条目数（过期未回收的不算）

	// 生命周期字段：调用方传 ttl<=0 时用 DefaultTTL；一律再按
	// [MinTTL, MaxTTL] 夹紧。
	DefaultTTL time.Duration
	MinTTL     time.Duration
	MaxTTL     time.Duration

	// now 是时钟注入点，测试用假时钟确定性推进过期。
	now func() time.Time
}

// NewFakeIPPool 从 CIDR 构造池，如 "198.18.0.0/16"。
//
// 主机位被忽略：传 "198.18.0.5/16" 与 "198.18.0.0/16" 等价，从
// 网络地址之后开始分配。理由是配置里写主机位几乎总是笔误，悄悄按
// 192.0.2.5 起步分配反而让人对不上"到底哪个地址给了谁"。
func NewFakeIPPool(cidr string) (*FakeIPPool, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("dns: fake-ip CIDR %q 解析失败：%w", cidr, err)
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 && bits != 128 {
		return nil, fmt.Errorf("dns: fake-ip CIDR %q 的地址族不支持（仅 IPv4/IPv6）", cidr)
	}

	p := &FakeIPPool{
		subnet:     ipnet,
		v4:         bits == 32,
		base:       new(big.Int).SetBytes(ipnet.IP),
		ipToEntry:  make(map[string]*fakeIPEntry),
		domainToIP: make(map[string]net.IP),
		DefaultTTL: defaultFakeIPTTL,
		MinTTL:     defaultFakeIPMinTTL,
		MaxTTL:     defaultFakeIPMaxTTL,
		now:        time.Now,
	}
	// 地址总数 = 2^(bits-ones)，用 big.Int：IPv6 的 /64 有 2^64 个地址，
	// int64 会溢出。IPv4 的网络/广播地址跳过，IPv6 无此概念，从 0 起。
	size := new(big.Int).Lsh(big.NewInt(1), uint(bits-ones))
	if p.v4 {
		p.startOff = big.NewInt(1)
		p.endOff = new(big.Int).Sub(size, big.NewInt(1)) // 不含广播
	} else {
		p.startOff = big.NewInt(0)
		p.endOff = new(big.Int).Set(size)
	}
	p.usable = new(big.Int).Sub(p.endOff, p.startOff)
	if p.usable.Sign() <= 0 {
		return nil, fmt.Errorf("dns: fake-ip 网段 %s 可用主机地址不足（/31、/32 没有可分配地址）", cidr)
	}
	p.next = new(big.Int).Set(p.startOff)
	return p, nil
}

// Allocate 给域名分配（或取回）一个 fake-ip。
//
//   - 域名已有**存活**映射：刷新过期时间并返回同一地址（同域名同地址，
//     这是 fake-ip 能被数据面长期接管的前提）；
//   - 域名有映射但已过期：陈旧映射先清掉再走正常分配 —— 那个地址可能
//     已被别的域名回收复用，不清就会把两个域名映射到同一地址；
//   - 全新域名：从游标处向后找第一个空闲偏移分配。
//
// ttl<=0 时用 DefaultTTL；最终按 [MinTTL, MaxTTL] 夹紧。
func (p *FakeIPPool) Allocate(domain string, ttl time.Duration) (net.IP, error) {
	d, err := normalizeDomain(domain)
	if err != nil {
		return nil, err
	}
	exp := p.now().Add(p.clampTTL(ttl))

	p.mu.Lock()
	defer p.mu.Unlock()

	if ip, ok := p.domainToIP[d]; ok {
		if e := p.ipToEntry[ip.String()]; e != nil && p.now().Before(e.expiresAt) {
			e.expiresAt = exp
			return cloneIP(e.ip), nil
		}
		// 陈旧/已过期映射：地址可能已被他人回收，映射作废重走分配。
		delete(p.domainToIP, d)
	}

	// 计数器看着满了也要先回收一轮：过期未清理的僵尸条目会占着 live 计数，
	// 小池（如 /30 只有 2 个可用地址）下不清就会把其实空闲的池误报耗尽。
	if p.liveAtLeastUsable() {
		p.reapExpiredLocked()
		if p.liveAtLeastUsable() {
			return nil, ErrFakeIPExhausted
		}
	}

	// 从游标向后找空闲偏移。步数上界 live+2：在 live+1 个连续不同偏移里
	// 必有一个空闲（鸽笼原理，且 live<usable 保证这段窗口不回绕自身）。
	// 顺序分配时游标紧跟分配点，实测一步命中；交错回收最坏走 live 步。
	steps := p.live + 2
	for i := 0; i < steps; i++ {
		ip := p.ipAt(p.next)
		key := ip.String()
		if e, used := p.ipToEntry[key]; used {
			if p.now().Before(e.expiresAt) {
				p.advanceLocked()
				continue // 活着，跳过
			}
			p.reapLocked(e) // 过期：地址即刻可用（顺手回收，不留垃圾）
		}
		p.ipToEntry[key] = &fakeIPEntry{ip: cloneIP(ip), domain: d, expiresAt: exp}
		p.domainToIP[d] = cloneIP(ip)
		p.live++
		p.advanceLocked()
		return cloneIP(ip), nil
	}
	return nil, ErrFakeIPExhausted
}

// Lookup 取域名当前的 fake-ip；无映射或已过期返回 nil。
// 纯查询：不刷新过期时间（续期请走 Allocate）。
func (p *FakeIPPool) Lookup(domain string) net.IP {
	d, err := normalizeDomain(domain)
	if err != nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ip, ok := p.domainToIP[d]
	if !ok {
		return nil
	}
	e := p.ipToEntry[ip.String()]
	if e == nil || !p.now().Before(e.expiresAt) {
		p.reapLocked(e)
		return nil
	}
	return cloneIP(e.ip)
}

// Reverse 是 Lookup 的逆映射：假地址 → 域名；无映射或已过期返回 ""。
func (p *FakeIPPool) Reverse(ip net.IP) string {
	if ip == nil {
		return ""
	}
	key := cloneIP(ip).String()
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.ipToEntry[key]
	if !ok {
		return ""
	}
	if !p.now().Before(e.expiresAt) {
		p.reapLocked(e)
		return ""
	}
	return e.domain
}

// Release 显式回收一个地址的映射（连接拆除/域名换策略时调用）。
// 幂等：回收不存在的地址什么都不做。
func (p *FakeIPPool) Release(ip net.IP) {
	if ip == nil {
		return
	}
	key := cloneIP(ip).String()
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.ipToEntry[key]; ok {
		p.reapLocked(e)
	}
}

// Len 返回存活映射数。
func (p *FakeIPPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.live
}

// Subnet 返回池所在网段的字符串形式（日志/调试用）。
func (p *FakeIPPool) Subnet() string { return p.subnet.String() }

// ipAt 计算偏移 off 对应的地址（base+off）。
func (p *FakeIPPool) ipAt(off *big.Int) net.IP {
	n := new(big.Int).Add(p.base, off)
	if p.v4 {
		return net.IP(n.FillBytes(make([]byte, 4)))
	}
	return net.IP(n.FillBytes(make([]byte, 16)))
}

// advanceLocked 把游标推进一步，越界回绕到起点。
func (p *FakeIPPool) advanceLocked() {
	p.next.Add(p.next, big.NewInt(1))
	if p.next.Cmp(p.endOff) >= 0 {
		p.next.Set(p.startOff)
	}
}

// reapLocked 拆除一条映射并维护 live 计数。
//
// domainToIP 的删除带比对守卫：只有指向**同一个地址**的映射才删，
// 避免误删该域名后来重新分配到的新映射。
func (p *FakeIPPool) reapLocked(e *fakeIPEntry) {
	if e == nil {
		return
	}
	delete(p.ipToEntry, e.ip.String())
	if cur, ok := p.domainToIP[e.domain]; ok && cur.String() == e.ip.String() {
		delete(p.domainToIP, e.domain)
	}
	p.live--
}

// liveAtLeastUsable 判断存活条目数是否已占满池子。
// 走 big.Int 而不是 int64：IPv6 的 usable 可以是 2^64，int64 装不下。
func (p *FakeIPPool) liveAtLeastUsable() bool {
	return big.NewInt(int64(p.live)).Cmp(p.usable) >= 0
}

// reapExpiredLocked 全量清一遍过期条目。只在"计数器看着满了"时调用
// （错误路径上的一次 O(n) 清扫），常态下过期条目由各处顺手回收。
func (p *FakeIPPool) reapExpiredLocked() {
	now := p.now()
	for _, e := range p.ipToEntry {
		if !now.Before(e.expiresAt) {
			p.reapLocked(e)
		}
	}
}

func (p *FakeIPPool) clampTTL(d time.Duration) time.Duration {
	if d <= 0 {
		d = p.DefaultTTL
		if d <= 0 {
			d = defaultFakeIPTTL
		}
	}
	min, max := p.MinTTL, p.MaxTTL
	if min <= 0 {
		min = defaultFakeIPMinTTL
	}
	if max <= 0 {
		max = defaultFakeIPMaxTTL
	}
	return clampTTL(d, min, max)
}

// normalizeDomain 把域名规范化为小写无尾点形式；空名报错。
// fake-ip 只管普通域名，根域/空串没有分配意义。
func normalizeDomain(domain string) (string, error) {
	d := strings.TrimSpace(domain)
	d = strings.ToLower(d)
	d = strings.TrimSuffix(d, ".")
	if d == "" {
		return "", fmt.Errorf("dns: fake-ip 域名 %q 为空", domain)
	}
	return d, nil
}

// cloneIP 复制一份 IP：池内与池外共享同一个 net.IP 底层数组的话，
// 调用方一次就地修改（比如 To4/Mask 误用）就会污染整个池的键。
func cloneIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	return append(net.IP(nil), ip...)
}
