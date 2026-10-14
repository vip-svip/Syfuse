// fix.go 实现 leak.fix（M12 收尾）：从检测报告生成修复计划并应用
// 配置级修复。
//
// 修复分两级：
//   - 配置级（本包可直接应用）：把防泄漏键写入 syfuse.conf
//     （PROXY_LEAK=1、DNS_UPSTREAM 改加密 DoH、DNS_IPV6_DISABLE 等）；
//   - 动作级（只能提示用户）：如"系统设置里关 IPv6"——返回建议文本，
//     绝不假装已修复。
//
// 纯函数 FixPlan(report) 生成计划（可测）；ApplyConf 把配置级项
// upsert 进 conf 文件（键存在改值，不存在追加）。
package leak

import (
	"fmt"
	"os"
	"strings"
)

// Fix 是一条修复项。
type Fix struct {
	// Key 是修复类别（conf 键名或 action）。
	Key string `json:"key"`
	// Value 是配置级修复的目标值；动作级为空。
	Value string `json:"value,omitempty"`
	// Reason 是为什么修（对应哪个检测项的什么结论）。
	Reason string `json:"reason"`
	// Applied 表示是否已应用（配置级 true；动作级 false）。
	Applied bool `json:"applied"`
}

// fixChecker 抽象"从报告里读某维度结论"，便于测试注入。
type fixChecker struct {
	results []RunResult
}

// failOrWarn 返回维度结论状态（fail/warn 才算坏）。
func (f fixChecker) failOrWarn(dim string) (string, bool) {
	for _, r := range f.results {
		if r.Dimension == dim && (r.Status == Fail || r.Status == Warn) {
			return string(r.Status), true
		}
	}
	return "", false
}

// FixPlan 从检测报告生成修复计划。
// 规则（与防泄漏语义一一对应）：
//   - DNS 明文/解析异常        → DNS_UPSTREAM 改规范默认加密 DoH
//   - IPv6 泄漏风险            → DNS_IPV6_DISABLE=1（配置级）
//   - QUIC 未阻断              → 生成 QUIC 阻断过滤规则建议（动作级，
//     规则文本由调用方塞进 filter 引擎）
//   - 系统层（私有 DNS 等）    → 动作级提示
func FixPlan(report Report) []Fix {
	fc := fixChecker{results: report.Results}
	var out []Fix

	if st, bad := fc.failOrWarn("dns"); bad {
		out = append(out, Fix{
			Key: "DNS_UPSTREAM_ENC", Value: "https://dns.alidns.com/dns-query",
			Reason:  "DNS 层结论 " + st + "：上游必须走加密 DoH（规范 6.4 硬指标）",
			Applied: false,
		})
	}
	if st, bad := fc.failOrWarn("ipv6"); bad {
		out = append(out, Fix{
			Key: "DNS_IPV6_DISABLE", Value: "1",
			Reason: "IPv6 结论 " + st + "：关闭 IPv6 解析防 v6 旁路",
		})
	}
	if st, bad := fc.failOrWarn("quic"); bad {
		out = append(out, Fix{
			Key: "filter.rule", Value: "||^udp/443^$important",
			Reason: "QUIC 结论 " + st + "：阻断 UDP/443 迫使流量回落 TCP（可被代理）",
		})
	}
	if st, bad := fc.failOrWarn("system"); bad {
		out = append(out, Fix{
			Key: "action:private_dns", Value: "",
			Reason: "系统层结论 " + st + "：需要在系统设置里配置私人 DNS（动作级，引擎不可代劳）",
		})
	}
	return out
}

// ApplyConf 把配置级修复（Applied 目标 = true 且 Key 是大写配置键）
// upsert 进 conf：键存在改值，不存在追加；文件末尾无换行则补。
// 返回实际应用的键列表。
func ApplyConf(confPath string, fixes []Fix) ([]string, error) {
	var applied []string
	var confFixes []Fix
	for _, fx := range fixes {
		if fx.Value == "" || fx.Key == "filter.rule" || strings.HasPrefix(fx.Key, "action:") {
			continue // 动作级/非配置键
		}
		confFixes = append(confFixes, fx)
	}
	if len(confFixes) == 0 {
		return nil, nil
	}
	data, err := os.ReadFile(confPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	for _, fx := range confFixes {
		replaced := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, fx.Key+"=") ||
				strings.HasPrefix(trimmed, "#"+fx.Key+"=") {
				lines[i] = fx.Key + "=" + fx.Value
				replaced = true
				break
			}
		}
		if !replaced {
			lines = append(lines, fx.Key+"="+fx.Value)
		}
		applied = append(applied, fx.Key)
	}
	out := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	if err := os.WriteFile(confPath, []byte(out), 0o600); err != nil {
		return nil, fmt.Errorf("leak: 写配置失败: %w", err)
	}
	return applied, nil
}
