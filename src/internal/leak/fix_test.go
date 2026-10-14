package leak

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reportWith(results ...RunResult) Report {
	return Report{Results: results, Overall: "leak"}
}

func rr(dim, status string) RunResult {
	return RunResult{Dimension: dim, Status: Status(status)}
}

// FixPlan：坏维度 → 对应修复项；干净维度不产生。
func TestFixPlan(t *testing.T) {
	rep := reportWith(
		rr("dns", "fail"), rr("ipv6", "warn"), rr("quic", "fail"),
		rr("system", "warn"), rr("stun", "pass"), rr("ip_echo", "skip"),
	)
	plan := FixPlan(rep)
	keys := map[string]bool{}
	for _, fx := range plan {
		keys[fx.Key] = true
		if fx.Reason == "" {
			t.Fatalf("%s 缺 Reason", fx.Key)
		}
	}
	for _, want := range []string{"DNS_UPSTREAM_ENC", "DNS_IPV6_DISABLE", "filter.rule", "action:private_dns"} {
		if !keys[want] {
			t.Fatalf("缺修复项 %s: %v", want, keys)
		}
	}
	// 全干净 → 无计划。
	plan2 := FixPlan(reportWith(rr("dns", "pass"), rr("system", "pass")))
	if len(plan2) != 0 {
		t.Fatalf("干净报告不应有计划: %v", plan2)
	}
}

// ApplyConf：键存在改值、被注释键激活、不存在追加；动作级跳过。
func TestApplyConf(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "syfuse.conf")
	os.WriteFile(conf, []byte("DNS_ENABLE=1\n#DNS_UPSTREAM_ENC=\nOTHER=x\n"), 0o600)

	fixes := []Fix{
		{Key: "DNS_UPSTREAM_ENC", Value: "https://dns.alidns.com/dns-query"},
		{Key: "DNS_IPV6_DISABLE", Value: "1"},
		{Key: "action:private_dns", Value: ""},
		{Key: "filter.rule", Value: "||^udp/443^"},
	}
	applied, err := ApplyConf(conf, fixes)
	if err != nil {
		t.Fatalf("ApplyConf: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied = %v", applied)
	}
	data, _ := os.ReadFile(conf)
	s := string(data)
	if !strings.Contains(s, "DNS_UPSTREAM_ENC=https://dns.alidns.com/dns-query") {
		t.Fatalf("注释键应被激活: %s", s)
	}
	if !strings.Contains(s, "DNS_IPV6_DISABLE=1") {
		t.Fatalf("应追加新键: %s", s)
	}
	if !strings.Contains(s, "OTHER=x") {
		t.Fatal("既有无关键不应被破坏")
	}
	if strings.Contains(s, "action:") {
		t.Fatal("动作级不应写入 conf")
	}
}

// conf 不存在时创建。
func TestApplyConfCreate(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "syfuse.conf")
	applied, err := ApplyConf(conf, []Fix{{Key: "DNS_IPV6_DISABLE", Value: "1"}})
	if err != nil || len(applied) != 1 {
		t.Fatalf("创建路径: %v %v", applied, err)
	}
	data, _ := os.ReadFile(conf)
	if !strings.HasSuffix(string(data), "DNS_IPV6_DISABLE=1\n") {
		t.Fatalf("内容: %q", string(data))
	}
}

// 空 plan 不写文件。
func TestApplyConfNoFixes(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "syfuse.conf")
	applied, err := ApplyConf(conf, nil)
	if err != nil || applied != nil {
		t.Fatalf("空计划: %v %v", applied, err)
	}
	if _, err := os.Stat(conf); !os.IsNotExist(err) {
		t.Fatal("不应创建文件")
	}
}
