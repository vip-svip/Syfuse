// trie.go 是域名 Trie：按标签（label）逆序插入，支持"含子域"匹配。
//
// 为什么按标签逆序：DNS 域名匹配是"后缀语义"（||ads.example.com^ 要拦
// a.ads.example.com），按 label 逆序走 Trie（com → example → ads），
// 终结节点即命中 —— O(标签数) 且天然支持通配首段（*.x.com 存为通配节点）。
package filter

import "strings"

// trieFlags 是一个域名的拦截/放行标志。
type trieFlags struct {
	block bool
	allow bool
}

// trieNode 是 Trie 节点。
type trieNode struct {
	children map[string]*trieNode
	wildcard *trieNode // 首段通配（*.x.com 的 * 层）
	term     bool      // 本节点是某个规则域名的终结
	block    bool
	allow    bool
	// important 标志：存在带 $important 的同向规则。
	importantBlock bool
	importantAllow bool
}

func (n *trieNode) insert(domain string, f trieFlags) {
	node := n.descend(domain, true)
	if node == nil {
		return
	}
	node.term = true
	if f.block {
		node.block = true
	}
	if f.allow {
		node.allow = true
	}
}

func (n *trieNode) markImportant(domain string, allow bool) {
	node := n.descend(domain, true)
	if node == nil {
		return
	}
	if allow {
		node.importantAllow = true
	} else {
		node.importantBlock = true
	}
}

// descend 按 label 逆序走到 domain 对应节点；create=true 时沿途建节点。
// 首段为 * 时落到 wildcard 子树（*.x.com 拦一切子域）。
func (n *trieNode) descend(domain string, create bool) *trieNode {
	labels := strings.Split(domain, ".")
	cur := n
	for i := len(labels) - 1; i >= 0; i-- {
		label := labels[i]
		if label == "*" && i == 0 {
			if cur.wildcard == nil {
				if !create {
					return nil
				}
				cur.wildcard = &trieNode{}
			}
			cur = cur.wildcard
			continue
		}
		if cur.children == nil {
			if !create {
				return nil
			}
			cur.children = map[string]*trieNode{}
		}
		child, ok := cur.children[label]
		if !ok {
			if !create {
				return nil
			}
			child = &trieNode{}
			cur.children[label] = child
		}
		cur = child
	}
	return cur
}

// lookup 返回域名及其全部父域中"最具体的命中标志集合"。
// 语义：||x.com^ 拦 x.com 与其所有子域；若同时有 ||a.x.com^ 与
// @@||x.com^，则子域 a.x.com 既有 allow（继承自父）又有 block（自身），
// 由优先级链（调用方）裁决。
type lookupResult struct {
	node *trieNode
	// chain 是从最具体到最泛的命中节点链（用于优先级裁决）。
	chain []*trieNode
}

func (n *trieNode) lookup(domain string) *lookupResult {
	var chain []*trieNode
	cur := n
	labels := strings.Split(domain, ".")
	for i := len(labels) - 1; i >= 0; i-- {
		child, ok := cur.children[labels[i]]
		if !ok {
			// 尝试通配层：*.parent 存在时把它当作命中。
			if cur.wildcard != nil {
				chain = append(chain, cur.wildcard)
			}
			break
		}
		cur = child
		if cur.term {
			chain = append(chain, cur)
		}
	}
	if len(chain) == 0 {
		return nil
	}
	return &lookupResult{node: chain[0], chain: chain}
}

// match 在 compiled 上执行优先级链裁决。
//
//	重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中
//
// 多规则同级别命中时取"最具体"（Trie 链首）。exact/rewrites 先查精确表。
func (c *compiled) match(host string, ctx *matchCtx) MatchResult {
	if host == "" {
		return MatchResult{Decision: NoMatch}
	}
	// 1) hosts 重写 / dnsmasq local 精确表。
	if v, ok := c.rewrites[host]; ok {
		return MatchResult{Decision: Rewritten, Rewrite: v, Rule: host}
	}
	// 2) hosts 拦截 / dnsmasq address 精确表。
	if _, ok := c.exact[host]; ok {
		return MatchResult{Decision: Blocked, Rule: host}
	}
	// 3) P1 片段（正则/作用域）——先算好，与 Trie 结果并链。
	pImpAllow, pImpBlock, pAllow, pBlock, pRule := c.matchP1(host, ctx)

	// 4) Trie（||锚定 + 纯域名 + 子域继承）。
	lr := c.domainTrie.lookup(host)
	var allow, block, impAllow, impBlock bool
	rule := pRule
	if lr != nil {
		// 聚合链上全部标志：任一节点有 important* 则提升到对应级别；
		// 普通级别同向合并。
		for _, node := range lr.chain {
			impAllow = impAllow || node.importantAllow
			impBlock = impBlock || node.importantBlock
			allow = allow || node.allow
			block = block || node.block
		}
		if pRule == "" {
			rule = "||" + host + "^"
		}
	}
	// P1 片段并入同一优先级链。
	impAllow = impAllow || pImpAllow
	impBlock = impBlock || pImpBlock
	allow = allow || pAllow
	block = block || pBlock
	switch {
	case impAllow:
		return MatchResult{Decision: AllowedImportant, Rule: rule}
	case impBlock:
		return MatchResult{Decision: BlockedImportant, Rule: rule}
	case allow:
		return MatchResult{Decision: Allowed, Rule: rule}
	case block:
		return MatchResult{Decision: Blocked, Rule: rule}
	}
	return MatchResult{Decision: NoMatch}
}
