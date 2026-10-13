// Package module 实现 M7a 模块系统的 Go 侧内核：
// 模块文件解析（元数据头 + 分段）、行级/分段级开关、
// 多模块合并（冲突检测）与序列化往返。
//
// 模块语法（对齐 Shadowrocket/Stash 生态，见规范 5.3）：
//
//	#!name=名字
//	#!desc=描述
//	#!author=作者        （可扩展：version/icon/homepage/update）
//	[Segment]            （General/Rule/Host/URL Rewrite/...）
//	key=value            （启用行）
//	# key=value          （# 前缀 = 停用行，保留原文以便重新启用）
package module

import (
	"fmt"
	"strings"
)

// Module 是一个解析后的模块。
type Module struct {
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	Author   string `json:"author,omitempty"`
	Version  string `json:"version,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Homepage string `json:"homepage,omitempty"`
	Update   string `json:"update,omitempty"` // 订阅 URL
	// Segments 保序：同一文件内分段按出现顺序。
	Segments []Segment `json:"segments"`
}

// Segment 是一个分段及其行。
type Segment struct {
	Name  string `json:"name"`
	Lines []Line `json:"lines"`
}

// Line 是分段内的一行。Raw 保留原文（不含停用前缀），
// Disabled 表示该行当前停用。
type Line struct {
	Raw      string `json:"raw"`
	Disabled bool   `json:"disabled"`
}

// KnownSegments 是规范 5.3 允许的分段名（大小写不敏感比较）。
var KnownSegments = []string{
	"General", "Rule", "Host", "URL Rewrite", "Header Rewrite",
	"Body Rewrite", "Map Local", "MITM", "Script", "Proxy Group",
}

// ParseErrors 是解析过程中非致命问题的集合。
type ParseErrors struct {
	Warnings []string `json:"warnings"`
}

// Parse 解析模块文本。非致命问题（未知分段、缺元数据）记入 warnings，
// 不阻断解析 —— 模块编辑器要能打开半成品。
func Parse(text string) (*Module, ParseErrors) {
	var pe ParseErrors
	m := &Module{}
	var cur *Segment
	seenName, seenDesc := false, false

	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "#!"):
			k, v := splitKV(strings.TrimPrefix(trimmed, "#!"))
			switch k {
			case "name":
				m.Name, seenName = v, true
			case "desc":
				m.Desc, seenDesc = v, true
			case "author":
				m.Author = v
			case "version":
				m.Version = v
			case "icon":
				m.Icon = v
			case "homepage":
				m.Homepage = v
			case "update":
				m.Update = v
			default:
				pe.Warnings = append(pe.Warnings,
					fmt.Sprintf("第 %d 行：未知元数据 %q", i+1, k))
			}
		default:
			// 停用行：以 # 开头且去掉 # 后像一行内容（非注释注释）。
			content, disabled := stripDisable(line)
			if !disabled && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
				m.Segments = append(m.Segments, Segment{Name: name})
				cur = &m.Segments[len(m.Segments)-1]
				if !knownSegment(name) {
					pe.Warnings = append(pe.Warnings,
						fmt.Sprintf("第 %d 行：未知分段 [%s]", i+1, name))
				}
				continue
			}
			if cur == nil {
				if content == "" || strings.HasPrefix(content, "#") {
					continue // 分段外的纯注释忽略
				}
				pe.Warnings = append(pe.Warnings,
					fmt.Sprintf("第 %d 行：内容出现在任何分段之前，已忽略", i+1))
				continue
			}
			cur.Lines = append(cur.Lines, Line{Raw: content, Disabled: disabled})
		}
	}
	if !seenName {
		pe.Warnings = append(pe.Warnings, "缺少 #!name= 元数据")
	}
	if !seenDesc {
		pe.Warnings = append(pe.Warnings, "缺少 #!desc= 元数据")
	}
	return m, pe
}

// stripDisable 判断一行是否为停用行。
// 停用约定：行首 #（可带空格）后跟非空内容且不是 [分段] 头。
// 返回 (去前缀内容, 是否停用)。纯注释（#! 或空注释）不算停用行。
func stripDisable(line string) (string, bool) {
	if !strings.HasPrefix(strings.TrimSpace(line), "#") {
		return strings.TrimSpace(line), false
	}
	body := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
	if body == "" || strings.HasPrefix(body, "!") {
		return body, false // 空注释 / 元数据
	}
	if strings.HasPrefix(body, "[") && strings.HasSuffix(body, "]") {
		// 停用的分段头 —— 原样返回，标记停用。
		return body, true
	}
	return body, true
}

func splitKV(s string) (string, string) {
	if i := strings.Index(s, "="); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return strings.TrimSpace(s), ""
}

func knownSegment(name string) bool {
	for _, k := range KnownSegments {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

// ToggleLine 启用/停用指定分段的第 idx 行（0 基）。
func (m *Module) ToggleLine(segName string, idx int, enabled bool) error {
	seg, err := m.segment(segName)
	if err != nil {
		return err
	}
	if idx < 0 || idx >= len(seg.Lines) {
		return fmt.Errorf("module: 分段 %s 第 %d 行不存在", segName, idx)
	}
	seg.Lines[idx].Disabled = !enabled
	return nil
}

// ToggleSegment 启用/停用整个分段（停用 = 所有行加停用前缀，
// 分段头本身保留，保证序列化往返后结构不变）。
func (m *Module) ToggleSegment(segName string, enabled bool) error {
	seg, err := m.segment(segName)
	if err != nil {
		return err
	}
	for i := range seg.Lines {
		seg.Lines[i].Disabled = !enabled
	}
	return nil
}

func (m *Module) segment(name string) (*Segment, error) {
	for i := range m.Segments {
		if strings.EqualFold(m.Segments[i].Name, name) {
			return &m.Segments[i], nil
		}
	}
	return nil, fmt.Errorf("module: 分段 %q 不存在", name)
}

// Serialize 还原为模块文本（与 Parse 构成往返）。
// 启用行原样输出；停用行加 "# " 前缀。
func (m *Module) Serialize() string {
	var b strings.Builder
	if m.Name != "" {
		b.WriteString("#!name=" + m.Name + "\n")
	}
	if m.Desc != "" {
		b.WriteString("#!desc=" + m.Desc + "\n")
	}
	for k, v := range map[string]string{
		"author": m.Author, "version": m.Version, "icon": m.Icon,
		"homepage": m.Homepage, "update": m.Update,
	} {
		if v != "" {
			b.WriteString("#!" + k + "=" + v + "\n")
		}
	}
	for _, seg := range m.Segments {
		b.WriteString("\n[" + seg.Name + "]\n")
		for _, l := range seg.Lines {
			if l.Disabled {
				b.WriteString("# " + l.Raw + "\n")
			} else {
				b.WriteString(l.Raw + "\n")
			}
		}
	}
	return b.String()
}
