package dns

// 本文件实现带 TTL 的应答缓存。
//
// 定位：解析器与缓存是组合关系（Resolver.Cache 可为 nil），本文件不假设
// 调用方是谁。职责只有两件事：
//
//   1. 按 (name, qtype) 索引存放 NOERROR 应答，命中返回 TTL 已递减的克隆；
//   2. 保证"命中的记录一定没过期"（惰性过期：读到期即删，不另起清扫
//      goroutine —— 少一个后台循环就少一类竞态与一个退出时要等的资源）。

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// 缓存默认值与 TTL 夹紧边界。
const (
	defaultCacheMaxEntries = 4096
	defaultCacheMinTTL     = 5 * time.Second
	defaultCacheMaxTTL     = 24 * time.Hour
)

// cacheKey 是缓存索引。
//
// 用结构体键而不是 "name|type" 字符串拼接：结构体键免掉拼接/切分开销，
// 也不会因名字里恰好含分隔符而撞键（"a|1" 与 ("a", 1) 拼出来一样）。
type cacheKey struct {
	name  string
	qtype uint16
}

type cacheEntry struct {
	msg       *Message // 已按夹紧后的 TTL 重写过 TTL 字段的克隆
	storedAt  time.Time
	expiresAt time.Time
}

// Cache 是并发安全的 TTL 应答缓存。
type Cache struct {
	mu      sync.Mutex
	entries map[cacheKey]*cacheEntry

	// MaxEntries 条数上限；0 表示不限制。达到上限时按 evictLocked 淘汰。
	MaxEntries int

	// TTL 夹紧边界（见 clampTTL）。为什么要夹：
	//   - min：上游可能给 TTL=0 的记录，逐出缓存等于没缓存，热点域名
	//     会被打回上游；同时 TTL 极小是 DNS 放大攻击的常见形态。
	//   - max：防止个别上游（或被投毒的上游）用一个超大 TTL 把错误映射
	//     长期钉在缓存里。
	MinTTL time.Duration
	MaxTTL time.Duration

	// now 是时钟注入点，测试用假时钟确定性验证过期，生产为 time.Now。
	now func() time.Time
}

// NewCache 构造默认配置的缓存。
func NewCache() *Cache {
	return &Cache{
		entries:    make(map[cacheKey]*cacheEntry),
		MaxEntries: defaultCacheMaxEntries,
		MinTTL:     defaultCacheMinTTL,
		MaxTTL:     defaultCacheMaxTTL,
		now:        time.Now,
	}
}

// Get 查询缓存。命中返回**克隆**（TTL 已按流逝时间递减）；未命中或已
// 过期返回 nil。过期即删，保证任何一次返回的记录都不会已过期。
func (c *Cache) Get(name string, qtype uint16) *Message {
	qname, err := normalizeName(name)
	if err != nil {
		// 名字连规范形都构不出，不可能命中（入缓时同样过不了 normalize）。
		return nil
	}
	key := cacheKey{name: qname, qtype: qtype}

	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil
	}
	now := c.now()
	if !now.Before(e.expiresAt) {
		delete(c.entries, key) // 惰性过期：命中即清
		return nil
	}
	// 克隆并按流逝时间下调每条 TTL：调用方拿到的是"还能活多久"的诚实值，
	// 且无法透过引用改脏缓存内记录（缓存内 msg 入盘时也是克隆，双向隔离）。
	msg := e.msg.clone()
	elapsed := uint32(now.Sub(e.storedAt) / time.Second)
	for i := range msg.Answers {
		if elapsed >= msg.Answers[i].TTL {
			msg.Answers[i].TTL = 0
			continue
		}
		msg.Answers[i].TTL -= elapsed
	}
	return msg
}

// Put 写入一份应答。
//
// 只接受 NOERROR 且至少一条 answer：
//   - SERVFAIL 是上游故障，缓存它等于把一次故障复制成持续故障；
//   - NXDOMAIN/NODATA 的负面缓存需要 SOA TTL 语义，本单元不做，
//     把决策留给上层策略层。
//
// 写入内容以克隆形式存放（调用方后续改动不会穿透进缓存），且 TTL 字段
// 被重写为夹紧后的值 —— 于是缓存内 min(TTL) 与条目过期时间严格一致，
// Get 递减后不会出现"记录 TTL 归零但条目还活着"的分裂状态。
func (c *Cache) Put(name string, qtype uint16, msg *Message) error {
	qname, err := normalizeName(name)
	if err != nil {
		return err
	}
	if msg == nil {
		return errors.New("dns: 不能缓存空应答")
	}
	if msg.RCode() != RCodeNoError {
		return fmt.Errorf("dns: 拒绝缓存 RCODE=%s 的应答（只缓存 NOERROR）", rcodeName(msg.RCode()))
	}
	if len(msg.Answers) == 0 {
		return errors.New("dns: 拒绝缓存无 answer 的 NODATA 应答")
	}

	stored := msg.clone()
	entryTTL := c.maxTTL()
	for i := range stored.Answers {
		ttl := clampTTL(time.Duration(stored.Answers[i].TTL)*time.Second, c.minTTL(), c.maxTTL())
		stored.Answers[i].TTL = uint32(ttl / time.Second)
		if ttl < entryTTL {
			entryTTL = ttl
		}
	}

	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[cacheKey]*cacheEntry)
	}
	if c.MaxEntries > 0 && len(c.entries) >= c.MaxEntries {
		c.evictLocked(now)
	}
	c.entries[cacheKey{name: qname, qtype: qtype}] = &cacheEntry{
		msg:       stored,
		storedAt:  now,
		expiresAt: now.Add(entryTTL),
	}
	return nil
}

// evictLocked 淘汰一条记录以腾位置。
//
// 策略（最简实现）：单遍扫描，先撞见已过期的就删它（顺带做惰性清理）；
// 没有过期记录就删迭代到的第一条 —— Go 的 map 迭代顺序本身随机，
// 等价于随机淘汰。
//
// 明码标价的代价：随机淘汰可能误删热点域名，导致它回源一次。LRU 需要
// 每条记录挂链表节点 + 每次命中搬移，锁竞争与复杂度都上一个台阶，
// 而在默认 4096 条、命中即走的用法里收益不抵成本。若将来 profile 显示
// 回源率不可接受，换 LRU 时只需改这一个函数（其余逻辑不感知淘汰细节）。
func (c *Cache) evictLocked(now time.Time) {
	var fallback cacheKey
	var haveFallback bool
	for k, e := range c.entries {
		if !now.Before(e.expiresAt) {
			delete(c.entries, k) // 过期的优先清理，删它不损失命中率
			return
		}
		if !haveFallback {
			fallback, haveFallback = k, true
		}
	}
	if haveFallback {
		delete(c.entries, fallback)
	}
}

// Delete / Purge / Len 是面向维护与测试的小工具。
func (c *Cache) Delete(name string, qtype uint16) {
	qname, err := normalizeName(name)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, cacheKey{name: qname, qtype: qtype})
}

func (c *Cache) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[cacheKey]*cacheEntry)
}

// Len 返回当前条目数（可能含尚未被惰性清理的过期条目）。
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *Cache) minTTL() time.Duration {
	if c.MinTTL > 0 {
		return c.MinTTL
	}
	return defaultCacheMinTTL
}

func (c *Cache) maxTTL() time.Duration {
	if c.MaxTTL > 0 {
		return c.MaxTTL
	}
	return defaultCacheMaxTTL
}
