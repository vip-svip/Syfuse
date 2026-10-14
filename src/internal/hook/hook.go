// Package hook 实现 M11 的 Hook 框架检测（规范 5.7"引擎检测与选择"）：
// Zygisk / ReZygisk / Zygisk Next / LSPosed / APatch / Shamiko 的存在性
// 探测与状态上报。
//
// 设计：检测完全依赖"路径存在性"注入（exists func(path) bool），
// 引擎本身不做任何 I/O —— 沙盒测试确定性，Android 侧由 daemon 把
// /data/adb/... 的 os.Stat 接进来。无框架时返回空列表（UI 据此显示
// "无 Hook 框架，功能降级到 VPN 层等价方案"，规范 6.5 三级降级）。
package hook

import (
	"fmt"
	"sort"
)

// Framework 是已知的 Hook 框架。
type Framework string

const (
	FwZygisk     Framework = "zygisk"      // Magisk Zygisk
	FwReZygisk   Framework = "rezygisk"    // ReZygisk
	FwZygiskNext Framework = "zygisk_next" // Zygisk Next
	FwLSPosed    Framework = "lsposed"     // LSPosed
	FwAPatch     Framework = "apatch"      // APatch
	FwShamiko    Framework = "shamiko"     // Shamiko（隐藏用，非注入框架但影响状态）
	FwKernelSU   Framework = "kernelsu"    // KernelSU（root 形态，影响可用性）
	FwMagisk     Framework = "magisk"      // Magisk 本体
)

// Detector 是一次检测的执行器。
type Detector struct {
	exists func(path string) bool
}

// NewDetector 构造；exists 不能为 nil。
func NewDetector(exists func(path string) bool) (*Detector, error) {
	if exists == nil {
		return nil, fmt.Errorf("hook: exists 探测函数不能为 nil")
	}
	return &Detector{exists: exists}, nil
}

// probeDef 是框架 → 特征路径表。
// 多条路径任一存在即认为框架存在（不同 root 方案路径不同）。
var probeDef = []struct {
	fw    Framework
	name  string
	paths []string
}{
	{FwMagisk, "Magisk", []string{
		"/data/adb/magisk",
		"/sbin/.magisk",
	}},
	{FwKernelSU, "KernelSU", []string{
		"/data/adb/ksu",
		"/data/adb/kernelsu",
	}},
	{FwAPatch, "APatch", []string{
		"/data/adb/ap",
		"/data/adb/apatch",
	}},
	{FwZygisk, "Zygisk", []string{
		"/data/adb/modules/zygisk",
		"/data/adb/.magisk/zygisk",
	}},
	{FwZygiskNext, "Zygisk Next", []string{
		"/data/adb/modules/zygisksu",
		"/data/adb/modules/zygisk_next",
	}},
	{FwReZygisk, "ReZygisk", []string{
		"/data/adb/modules/rezygisk",
	}},
	{FwLSPosed, "LSPosed", []string{
		"/data/adb/lspd",
		"/data/adb/modules/lsposed",
	}},
	{FwShamiko, "Shamiko", []string{
		"/data/adb/modules/shamiko",
	}},
}

// Status 是 hook.status UDS 的应答体。
type Status struct {
	// Detected 是检测到的框架（按探测表顺序）。
	Detected []Detected `json:"detected"`
	// InjectionCapable 表示存在可用的注入框架
	//（Zygisk 系任一或 LSPosed）。
	InjectionCapable bool `json:"injection_capable"`
	// RootProvider 是判定的 root 形态（none/magisk/kernelsu/apatch）。
	RootProvider string `json:"root_provider"`
	// DegradedMode 是降级说明：有注入能力为空；否则给等价方案说明。
	DegradedMode string `json:"degraded_mode,omitempty"`
}

// Detected 是单个框架的检测结果。
type Detected struct {
	Framework Framework `json:"framework"`
	Name      string    `json:"name"`
	// Evidence 是命中的特征路径（证据，日志可见）。
	Evidence string `json:"evidence"`
}

// Detect 执行全部探测。
func (d *Detector) Detect() Status {
	st := Status{RootProvider: "none"}
	for _, pd := range probeDef {
		for _, p := range pd.paths {
			if d.exists(p) {
				st.Detected = append(st.Detected, Detected{
					Framework: pd.fw, Name: pd.name, Evidence: p,
				})
				switch pd.fw {
				case FwMagisk:
					st.RootProvider = "magisk"
				case FwKernelSU:
					if st.RootProvider == "none" {
						st.RootProvider = "kernelsu"
					}
				case FwAPatch:
					st.RootProvider = "apatch"
				case FwZygisk, FwZygiskNext, FwReZygisk, FwLSPosed:
					st.InjectionCapable = true
				}
				break // 该框架命中即停，换下一个框架
			}
		}
	}
	if !st.InjectionCapable {
		st.DegradedMode = "无可用注入框架：MITM 信任注入改走用户证书引导，" +
			"代理重定向走 VPN 层等价方案（功能不缺失，精度降低）"
	}
	return st
}

// FrameworkNames 返回检测到的框架名列表（排序，便于测试与展示）。
func (s Status) FrameworkNames() []string {
	out := make([]string, 0, len(s.Detected))
	for _, d := range s.Detected {
		out = append(out, string(d.Framework))
	}
	sort.Strings(out)
	return out
}
