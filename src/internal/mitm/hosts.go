// hosts.go 实现解密主机列表（规范 5.5"解密主机列表：通配符、逐主机开关"）。
//
// 匹配语义：
//   - "example.com"      精确匹配该主机
//   - "*.example.com"    匹配任意一层子域（a.example.com ✓,
//     a.b.example.com ✗ —— 与证书 SAN 的通配符语义一致）
//   - ".example.com"     匹配 example.com 及任意深度子域（宽松模式）
//   - "!" 前缀           排除项，优先级最高（!bad.example.com 永不解密）
package mitm

import (
	"fmt"
	"strings"
)

// HostList 是解密主机列表。
type HostList struct {
	entries []hostEntry
}

type hostEntry struct {
	pattern string
	exclude bool
}

// ParseHostList 从文本构建列表：每行一个 pattern，# 开头为注释，空行跳过。
func ParseHostList(text string) (*HostList, error) {
	hl := &HostList{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := hl.Add(line); err != nil {
			return nil, fmt.Errorf("mitm: 主机列表第 %d 行 %q：%w", i+1, line, err)
		}
	}
	return hl, nil
}

// Add 追加一条 pattern。
func (h *HostList) Add(pattern string) error {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return fmt.Errorf("空 pattern")
	}
	exclude := false
	if strings.HasPrefix(p, "!") {
		exclude, p = true, strings.TrimSpace(p[1:])
		if p == "" {
			return fmt.Errorf("! 后必须跟主机名")
		}
	}
	if p != "*" && strings.Count(p, "*") > 1 {
		return fmt.Errorf("多个 * 不支持")
	}
	if i := strings.Index(p, "*"); i >= 0 && i != 0 {
		return fmt.Errorf("* 只能作为前缀（*.example.com）")
	}
	h.entries = append(h.entries, hostEntry{pattern: strings.ToLower(p), exclude: exclude})
	return nil
}

// ShouldDecrypt 判断主机是否应解密。排除项永远优先；
// 列表为空时返回 false（按规范 5.5"默认全关"的精神，抓包按需开）。
func (h *HostList) ShouldDecrypt(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	// 末位排除优先：先查排除，再查允许。
	for _, e := range h.entries {
		if e.exclude && matchHost(e.pattern, host) {
			return false
		}
	}
	for _, e := range h.entries {
		if !e.exclude && matchHost(e.pattern, host) {
			return true
		}
	}
	return false
}

// Patterns 返回全部 pattern（含 ! 前缀），供 UI 展示。
func (h *HostList) Patterns() []string {
	out := make([]string, 0, len(h.entries))
	for _, e := range h.entries {
		p := e.pattern
		if e.exclude {
			p = "!" + p
		}
		out = append(out, p)
	}
	return out
}

// matchHost 按 pattern 类型匹配（host 已小写）。
func matchHost(pattern, host string) bool {
	switch {
	case pattern == host:
		return true
	case strings.HasPrefix(pattern, "*."):
		// 只匹配一层子域：*.example.com 匹配 a.example.com，不匹配 a.b.c。
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(host, suffix) && !strings.Contains(host[:len(host)-len(suffix)], ".")
	case strings.HasPrefix(pattern, "."):
		// 宽松：.example.com 匹配 example.com 与任意深度子域。
		return host == pattern[1:] || strings.HasSuffix(host, pattern)
	default:
		return false
	}
}
