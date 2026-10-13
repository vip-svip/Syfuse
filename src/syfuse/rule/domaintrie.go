package rule

// DomainTrie 是域名的反向标签 Trie（suffix trie）。
//
// 用途：把 `||example.com^` 一类域名锚定规则编译进 Trie，使 DNS 层的单次查询
// 从"线性扫描 N 条规则"降为"按标签深度 O(标签数)"。这是 5.4 性能目标
// （百万级规则、单次匹配 p99 < 1ms）的基础结构。
//
// 反向存储（`com.example.ads` → `ads` → `example` → `com`）使后缀匹配天然成立：
// 查 `a.b.example.com` 时会依次经过 `com`、`example`、`b`、`a`，路径上挂的规则全部命中。
type DomainTrie[T any] struct {
	root       *domainNode[T]
	valueCount int
}

type domainNode[T any] struct {
	children map[string]*domainNode[T]
	values   []T
}

func newDomainNode[T any]() *domainNode[T] {
	return &domainNode[T]{children: make(map[string]*domainNode[T], 2)}
}

// NewDomainTrie 创建一个空 Trie。
func NewDomainTrie[T any]() *DomainTrie[T] {
	return &DomainTrie[T]{root: newDomainNode[T]()}
}

// Put 挂载一条规则到指定域名。
func (t *DomainTrie[T]) Put(domain string, value T) {
	labels := labelsOf(domain)
	if len(labels) == 0 {
		return
	}
	node := t.root
	for _, label := range labels {
		next, ok := node.children[label]
		if !ok {
			next = newDomainNode[T]()
			node.children[label] = next
		}
		node = next
	}
	node.values = append(node.values, value)
	t.valueCount++
}

// Lookup 查出所有后缀匹配的 value。
//
// 返回顺序为**最长匹配优先**：更具体的规则（`ads.example.com`）排在
// 更宽泛的规则（`example.com`）之前。引擎最终仍按优先级模型裁决，
// 此顺序只影响链路展示的可读性。
func (t *DomainTrie[T]) Lookup(host string) []T {
	labels := labelsOf(host)
	if len(labels) == 0 {
		return nil
	}
	node := t.root
	buckets := make([][]T, 0, len(labels)+1)
	if len(node.values) > 0 {
		buckets = append(buckets, node.values)
	}
	for _, label := range labels {
		next, ok := node.children[label]
		if !ok {
			break
		}
		node = next
		if len(node.values) > 0 {
			buckets = append(buckets, node.values)
		}
	}
	if len(buckets) == 0 {
		return nil
	}
	total := 0
	for _, b := range buckets {
		total += len(b)
	}
	out := make([]T, 0, total)
	for i := len(buckets) - 1; i >= 0; i-- {
		out = append(out, buckets[i]...)
	}
	return out
}

// Size 返回已挂载的 value 总数（不是节点数）。
func (t *DomainTrie[T]) Size() int { return t.valueCount }

// labelsOf 把域名切成"反向标签"序列。
func labelsOf(domain string) []string {
	s := trimSuffixDot(trimSpace(toLowerASCII(domain)))
	if s == "" {
		return nil
	}
	parts := splitByte(s, '.')
	out := make([]string, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			out = append(out, parts[i])
		}
	}
	return out
}

// HostMatchesDomain 判断 host 是否等于 domain 或为其子域。
//
// 注意这是**标签级**匹配：`notexample.com` 不匹配 `example.com`，
// 避免用字符串 endsWith 造成的越界放行/拦截。
func HostMatchesDomain(host, domain string) bool {
	h := trimSuffixDot(toLowerASCII(host))
	d := trimPrefixDots(trimSuffixDot(toLowerASCII(domain)))
	if d == "" {
		return false
	}
	if h == d {
		return true
	}
	return len(h) > len(d) && hasSuffix(h, d) && h[len(h)-len(d)-1] == '.'
}

func anyDomainMatch(host string, domains []string) bool {
	for _, d := range domains {
		if HostMatchesDomain(host, d) {
			return true
		}
	}
	return false
}

func trimPrefixDots(s string) string {
	for len(s) > 0 && s[0] == '.' {
		s = s[1:]
	}
	return s
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// splitByte 按单字节分隔符切分，保留空段（与 Kotlin String.split 的默认行为一致）。
func splitByte(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
