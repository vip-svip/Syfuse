package hook

import (
	"sort"
	"testing"
)

func existsOf(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

// 全空：无框架 → 降级说明，root=none。
func TestDetect_None(t *testing.T) {
	d, _ := NewDetector(existsOf())
	st := d.Detect()
	if len(st.Detected) != 0 || st.RootProvider != "none" {
		t.Fatalf("空环境: %+v", st)
	}
	if st.InjectionCapable {
		t.Fatal("空环境不应有注入能力")
	}
	if st.DegradedMode == "" {
		t.Fatal("无框架必须给降级说明")
	}
}

// Magisk + Zygisk + LSPosed：root=magisk，注入可用，无降级。
func TestDetect_Full(t *testing.T) {
	d, _ := NewDetector(existsOf(
		"/data/adb/magisk",
		"/data/adb/modules/zygisk",
		"/data/adb/lspd",
	))
	st := d.Detect()
	if got := st.FrameworkNames(); len(got) != 3 || got[0] != "lsposed" || got[2] != "zygisk" {
		t.Fatalf("框架列表: %v", got)
	}
	if st.RootProvider != "magisk" {
		t.Fatalf("root = %s", st.RootProvider)
	}
	if !st.InjectionCapable || st.DegradedMode != "" {
		t.Fatalf("应有注入能力且无降级: %+v", st)
	}
	// 证据路径。
	for _, det := range st.Detected {
		if det.Evidence == "" {
			t.Fatalf("%s 缺证据路径", det.Name)
		}
	}
}

// APatch 优先级高于 KernelSU（探测表顺序决定）。
func TestDetect_RootPriority(t *testing.T) {
	d, _ := NewDetector(existsOf("/data/adb/ksu", "/data/adb/ap"))
	if got := d.Detect().RootProvider; got != "apatch" {
		t.Fatalf("root = %s, want apatch", got)
	}
	// 仅 KernelSU。
	d2, _ := NewDetector(existsOf("/data/adb/ksu"))
	if got := d2.Detect().RootProvider; got != "kernelsu" {
		t.Fatalf("root = %s, want kernelsu", got)
	}
}

// Zygisk Next 单独存在也具备注入能力（不需要原生 Zygisk）。
func TestDetect_ZygiskNext(t *testing.T) {
	d, _ := NewDetector(existsOf("/data/adb/modules/zygisksu"))
	st := d.Detect()
	if !st.InjectionCapable {
		t.Fatalf("Zygisk Next 应具备注入能力: %+v", st)
	}
}

// nil exists 拒绝构造。
func TestNewDetector_Nil(t *testing.T) {
	if _, err := NewDetector(nil); err == nil {
		t.Fatal("nil exists 应报错")
	}
}

// FrameworkNames 排序稳定（供 UI 对比）。
func TestFrameworkNamesSorted(t *testing.T) {
	d, _ := NewDetector(existsOf(
		"/data/adb/modules/shamiko", "/data/adb/magisk", "/data/adb/lspd"))
	names := d.Detect().FrameworkNames()
	if !sort.StringsAreSorted(names) {
		t.Fatalf("应有序: %v", names)
	}
}
