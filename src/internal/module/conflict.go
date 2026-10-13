// conflict.go 实现多模块合并与冲突检测。
//
// 覆盖语义（与规范 5.3 "同段同键覆盖规则要明确并可查看"对应）：
//   - 模块按传入顺序叠加，后面的模块覆盖前面的同键行；
//   - "键" = 行的首个 token（`key=value` 的 key、Host 行的域名、
//     Rewrite 行的匹配表达式），大小写敏感；
//   - 每次覆盖都记录 Conflict，面板可查看"谁覆盖了谁"。
package module

import (
	"fmt"
	"strings"
)

// Conflict 描述一次同键覆盖。
type Conflict struct {
	Segment  string `json:"segment"`
	Key      string `json:"key"`
	Winner   string `json:"winner"`    // 最终生效的模块名
	Loser    string `json:"loser"`     // 被覆盖的模块名
	RawValue string `json:"raw_value"` // 覆盖后的内容
}

// MergeResult 是多模块合并的结果。
type MergeResult struct {
	// Text 是合并后的配置文本（同配置语法，可直接交给规则引擎）。
	Text string
	// Conflicts 按发生顺序列出全部覆盖。
	Conflicts []Conflict
}

// Merge 按顺序合并多个模块，只取启用的行。
// 失败（nil 模块指针）返回错误；空列表返回空文本。
func Merge(mods []*Module) (*MergeResult, error) {
	res := &MergeResult{}
	type entry struct {
		key  string
		line string
		from string
	}
	order := []string{}                      // 分段名保序
	bySeg := map[string][]entry{}            // 分段 -> 键行
	keys := map[string]map[string]struct{}{} // 分段 -> 键集合（保序用 order）

	for _, m := range mods {
		if m == nil {
			return nil, fmt.Errorf("module: Merge 收到 nil 模块")
		}
		for _, seg := range m.Segments {
			name := seg.Name
			if _, ok := bySeg[name]; !ok {
				order = append(order, name)
				bySeg[name] = nil
				keys[name] = map[string]struct{}{}
			}
			for _, l := range seg.Lines {
				if l.Disabled {
					continue // 停用行不参与合并
				}
				k := lineKey(l.Raw)
				if k == "" {
					// 无键行（罕见）原样保留，不参与冲突判定。
					bySeg[name] = append(bySeg[name], entry{key: "", line: l.Raw, from: m.Name})
					continue
				}
				if _, dup := keys[name][k]; dup {
					// 找到旧条目记冲突：新模块覆盖旧模块。
					for i := range bySeg[name] {
						if bySeg[name][i].key == k {
							res.Conflicts = append(res.Conflicts, Conflict{
								Segment: name, Key: k,
								Winner: m.Name, Loser: bySeg[name][i].from,
								RawValue: l.Raw,
							})
							bySeg[name][i].line = l.Raw
							bySeg[name][i].from = m.Name
							break
						}
					}
					continue
				}
				keys[name][k] = struct{}{}
				bySeg[name] = append(bySeg[name], entry{key: k, line: l.Raw, from: m.Name})
			}
		}
	}

	var b strings.Builder
	for _, seg := range order {
		b.WriteString("[" + seg + "]\n")
		for _, e := range bySeg[seg] {
			b.WriteString(e.line + "\n")
		}
		b.WriteString("\n")
	}
	res.Text = b.String()
	return res, nil
}

// lineKey 提取行的键：`=` 前的部分；无 `=` 时取首 token
// （Host 域名、Rewrite 表达式）。
func lineKey(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if i := strings.Index(line, "="); i > 0 {
		return strings.TrimSpace(line[:i])
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
