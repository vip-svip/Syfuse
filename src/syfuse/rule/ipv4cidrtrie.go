package rule

// Ipv4CidrTrie 是 IPv4 CIDR 最长前缀匹配 Trie
// （即 6.1 工程要求中的 "IP Patricia Trie" 的等价实现）。
//
// 用途：IP 网段类规则（网络层封禁 6.4、IP-CIDR 分流 5.4）需要在 O(32) 内完成匹配。
// 用二叉 Trie 而非区间排序表，是为了支持"最长前缀优先"这一语义 ——
// 排序表只能回答"是否命中"，无法回答"命中的是哪一条"。
//
// IPv6 未在此实现：M1 只交付 IPv4 路径，IPv6 记录在未决清单中
// （见 README 未决与风险）。这与 Kotlin 侧的实现范围一致，不是遗漏。
type Ipv4CidrTrie[T any] struct {
	root       *cidrNode[T]
	entryCount int
}

type cidrNode[T any] struct {
	zero  *cidrNode[T]
	one   *cidrNode[T]
	value *T
}

// NewIpv4CidrTrie 创建一个空 Trie。
func NewIpv4CidrTrie[T any]() *Ipv4CidrTrie[T] {
	return &Ipv4CidrTrie[T]{root: &cidrNode[T]{}}
}

// Put 插入一条 CIDR。prefix 省略时按 /32 处理。
// 非法输入返回 false，由调用方报错到行号。
func (t *Ipv4CidrTrie[T]) Put(cidr string, value T) bool {
	slash := indexByte(cidr, '/')
	ipPart := cidr
	prefix := 32
	if slash >= 0 {
		ipPart = cidr[:slash]
		p, ok := parseIntOrNull(cidr[slash+1:])
		if !ok {
			return false
		}
		prefix = p
	}
	if prefix < 0 || prefix > 32 {
		return false
	}
	ip, ok := ParseIpv4(ipPart)
	if !ok {
		return false
	}

	node := t.root
	for bit := 31; bit >= 32-prefix; bit-- {
		set := (ip>>uint(bit))&1 == 1
		if set {
			if node.one == nil {
				node.one = &cidrNode[T]{}
			}
			node = node.one
		} else {
			if node.zero == nil {
				node.zero = &cidrNode[T]{}
			}
			node = node.zero
		}
	}
	v := value
	node.value = &v
	t.entryCount++
	return true
}

// Lookup 做最长前缀匹配。未命中返回 (零值, false)。
func (t *Ipv4CidrTrie[T]) Lookup(ip string) (T, bool) {
	var zero T
	addr, ok := ParseIpv4(ip)
	if !ok {
		return zero, false
	}
	node := t.root
	best := node.value
	for bit := 31; bit >= 0; bit-- {
		set := (addr>>uint(bit))&1 == 1
		if set {
			node = node.one
		} else {
			node = node.zero
		}
		if node == nil {
			break
		}
		if node.value != nil {
			best = node.value
		}
	}
	if best == nil {
		return zero, false
	}
	return *best, true
}

// Size 返回已插入的 CIDR 条数。
func (t *Ipv4CidrTrie[T]) Size() int { return t.entryCount }

// ParseIpv4 严格解析点分十进制 IPv4。
//
// 拒绝前导零以外的任何异常写法，避免把 "1.2.3" 当成合法地址。
// 返回的 uint32 与 Kotlin 侧的 Int 位模式一一对应（Kotlin 用 ushr 做无符号移位）。
func ParseIpv4(text string) (uint32, bool) {
	parts := splitByte(trimSpace(text), '.')
	if len(parts) != 4 {
		return 0, false
	}
	var acc uint32
	for _, part := range parts {
		if part == "" || len(part) > 3 {
			return 0, false
		}
		v := 0
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return 0, false
			}
			v = v*10 + int(part[i]-'0')
		}
		if v > 255 {
			return 0, false
		}
		acc = (acc << 8) | uint32(v)
	}
	return acc, true
}

// parseIntOrNull 模拟 Kotlin 的 String.toIntOrNull：允许可选正负号 + 纯数字。
func parseIntOrNull(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	i := 0
	neg := false
	if s[0] == '+' || s[0] == '-' {
		neg = s[0] == '-'
		i = 1
	}
	if i >= len(s) {
		return 0, false
	}
	acc := 0
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		acc = acc*10 + int(s[i]-'0')
	}
	if neg {
		acc = -acc
	}
	return acc, true
}
