package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vip-svip/syfuse/src/internal/filter"
)

// config.reload 热重载语义：改磁盘上的过滤规则文件 → reload →
// 新规则生效（引擎指针级换新）；notify 规则同步换新。
func TestConfigReload(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "filter_rules.txt"),
		[]byte("||old.example^\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "notify_keywords.conf"),
		[]byte("旧词\n"), 0o600)
	p := Paths{DataDir: dir}

	// 首次加载：old 生效。
	if got := filterEngineFor(p).Match("old.example").Decision.String(); got != "blocked" {
		t.Fatalf("初始规则未生效: %s", got)
	}
	// 改磁盘：换新规则。
	os.WriteFile(filepath.Join(dir, "filter_rules.txt"),
		[]byte("||new.example^\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "notify_keywords.conf"),
		[]byte("新词\n"), 0o600)

	h := handleConfigReload(p)
	raw, err := h(json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	resp := raw.(map[string]any)
	fs := resp["filter"].(filter.Stats)
	if fs.BlockRules != 1 {
		t.Fatalf("重载后过滤统计: %+v", fs)
	}

	// 新规则生效、旧规则失效。
	if got := filterEngineFor(p).Match("new.example").Decision.String(); got != "blocked" {
		t.Fatalf("新规则未生效: %s", got)
	}
	if got := filterEngineFor(p).Match("old.example").Decision.String(); got != "nomatch" {
		t.Fatalf("旧规则应失效: %s", got)
	}
	// notify 换新。
	nf := notifyFilterFor(p)
	if _, hit := nf.ShouldFilter("com.x", "包含新词的标题", ""); !hit {
		t.Fatal("notify 新规则未生效")
	}
	if _, hit := nf.ShouldFilter("com.x", "包含旧词的标题", ""); hit {
		t.Fatal("notify 旧规则应失效")
	}
}
