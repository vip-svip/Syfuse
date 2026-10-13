package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件守住 `proxies.*` 控制方法的**行为**。
//
// 为什么需要它：这些方法此前全是"未实现"占位。改成真实现后，
// 最危险的不是"抛错"，而是**假装成功** —— 比如 start 在配置根本没读到
// 的情况下也返回 running:true。下面每一条都针对一种"假装"。

// writeConfigPack 在临时目录里落一份 syfuse.conf 与配置包，返回 Paths。
func writeConfigPack(t *testing.T, configJSON, confExtra string) Paths {
	t.Helper()
	p := tempPaths(t)
	confPath := filepath.Join(p.DataDir, "syfuse.conf")
	packPath := filepath.Join(p.DataDir, "config.json")
	p.ConfPath = confPath

	if err := os.WriteFile(packPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("写配置包失败: %v", err)
	}
	conf := "CONFIG_PATH=" + packPath + "\n" + confExtra
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatalf("写 syfuse.conf 失败: %v", err)
	}
	return p
}

const twoNodeConfig = `{
  "schemaVersion": 1,
  "routingMode": "rule",
  "nodes": [
    "ss://YWVzLTI1Ni1nY206cHc@127.0.0.1:8388#节点B",
    "ss://YWVzLTI1Ni1nY206cHc@127.0.0.1:8389#节点A"
  ],
  "logLevel": "info"
}`

func TestProxies_StartReportsNotLinkedNotRunning(t *testing.T) {
	// 核心诚实性断言：内核宿主未接通时，start **不得**报 running。
	p := writeConfigPack(t, twoNodeConfig, "")
	pm := newProxyManager()

	raw, err := handleProxiesStart(pm, p)(nil)
	if err != nil {
		t.Fatalf("start 返回错误: %v", err)
	}
	m := raw.(map[string]any)

	if m["state"] != "not_linked" {
		t.Errorf("内核未接通时 state 应为 not_linked，得到 %v", m["state"])
	}
	if m["running"] != false {
		t.Errorf("内核未接通时 running 必须为 false，得到 %v", m["running"])
	}
	if m["kernelLinked"] != false {
		t.Errorf("kernelLinked 应为 false，得到 %v", m["kernelLinked"])
	}
	// 节点解析是真的做了，所以数量必须对
	if m["nodeCount"] != 2 {
		t.Errorf("节点数应为 2，得到 %v", m["nodeCount"])
	}
}

func TestProxies_StartFailsLoudlyOnMissingConfig(t *testing.T) {
	// 配置读不到时必须**报错**，不能返回一个空配置让引擎"启动成功"。
	p := tempPaths(t)
	p.ConfPath = filepath.Join(p.DataDir, "syfuse.conf")
	if err := os.WriteFile(p.ConfPath, []byte("CONFIG_PATH=/nonexistent/nope.json\n"), 0o600); err != nil {
		t.Fatalf("写 conf 失败: %v", err)
	}
	pm := newProxyManager()

	if _, err := handleProxiesStart(pm, p)(nil); err == nil {
		t.Fatal("配置包不存在时必须返回错误，而不是静默用空配置启动")
	}

	// conf 文件本身也不存在时同样必须报错
	p2 := tempPaths(t)
	p2.ConfPath = filepath.Join(p2.DataDir, "does-not-exist.conf")
	if _, err := handleProxiesStart(pm, p2)(nil); err == nil {
		t.Fatal("syfuse.conf 不存在时必须返回错误")
	}
}

func TestProxies_StartRejectsInvalidConfigAsFailure(t *testing.T) {
	// 配置格式错（不是"读不到"）必须报 failed，与 not_linked 区分开。
	p := writeConfigPack(t, `{"routingMode":"rule","nodes":[`, "")
	pm := newProxyManager()

	raw, err := handleProxiesStart(pm, p)(nil)
	if err != nil {
		t.Fatalf("handler 本身不应返回错误（状态里体现）: %v", err)
	}
	m := raw.(map[string]any)
	if m["state"] != "failed" {
		t.Errorf("非法配置应报 failed，得到 %v", m["state"])
	}
	if _, ok := m["error"]; !ok {
		t.Error("failed 状态下必须带 error 说明")
	}
	if m["running"] != false {
		t.Errorf("失败时 running 必须为 false，得到 %v", m["running"])
	}
}

func TestProxies_SelectRejectsUnknownNode(t *testing.T) {
	p := writeConfigPack(t, twoNodeConfig, "")
	pm := newProxyManager()
	if _, err := handleProxiesStart(pm, p)(nil); err != nil {
		t.Fatalf("先 start 失败: %v", err)
	}

	// 选一个存在的：OK
	okParams, _ := json.Marshal(map[string]string{"node": "节点A"})
	if _, err := handleProxiesSelect(pm)(okParams); err != nil {
		t.Fatalf("选择存在的节点不应失败: %v", err)
	}

	// 选一个不存在的：必须失败（否则 UI 会显示"已选中 X"而 X 不存在）
	badParams, _ := json.Marshal(map[string]string{"node": "不存在的节点"})
	if _, err := handleProxiesSelect(pm)(badParams); err == nil {
		t.Fatal("选择不存在的节点必须报错，不能静默接受")
	}

	// 空名字必须失败
	emptyParams, _ := json.Marshal(map[string]string{"node": "  "})
	if _, err := handleProxiesSelect(pm)(emptyParams); err == nil {
		t.Fatal("空节点名必须报错")
	}
}

func TestProxies_StatusBeforeStartIsStopped(t *testing.T) {
	pm := newProxyManager()
	raw, err := handleProxiesStatus(pm)(nil)
	if err != nil {
		t.Fatalf("status 出错: %v", err)
	}
	m := raw.(map[string]any)
	if m["state"] != "stopped" {
		t.Errorf("未启动时 state 应为 stopped，得到 %v", m["state"])
	}
	if m["running"] != false {
		t.Errorf("未启动时 running 必须 false，得到 %v", m["running"])
	}
	if m["nodeCount"] != 0 {
		t.Errorf("未启动时 nodeCount 应为 0，得到 %v", m["nodeCount"])
	}
}

func TestProxies_StopResetsRunning(t *testing.T) {
	p := writeConfigPack(t, twoNodeConfig, "")
	pm := newProxyManager()
	if _, err := handleProxiesStart(pm, p)(nil); err != nil {
		t.Fatalf("start 失败: %v", err)
	}

	raw, err := handleProxiesStop(pm)(nil)
	if err != nil {
		t.Fatalf("stop 出错: %v", err)
	}
	m := raw.(map[string]any)
	if m["running"] != false {
		t.Errorf("stop 后 running 必须 false，得到 %v", m["running"])
	}
	if m["state"] != "stopped" {
		t.Errorf("stop 后 state 应为 stopped，得到 %v", m["state"])
	}

	// 再查 status 也必须一致 —— 不能只有本次应答对，内部状态没改
	raw2, _ := handleProxiesStatus(pm)(nil)
	if raw2.(map[string]any)["state"] != "stopped" {
		t.Error("stop 之后 status 仍应报 stopped")
	}
}

func TestProxies_ReloadDoesNotTurnStoppedIntoRunning(t *testing.T) {
	// reload 只换配置，不改变"运行与否"。
	// 若它顺手把 running 置 true，一次配置刷新就会把 UI 变成"已连接"。
	p := writeConfigPack(t, twoNodeConfig, "")
	pm := newProxyManager()

	raw, err := handleProxiesReload(pm, p)(nil)
	if err != nil {
		t.Fatalf("reload 出错: %v", err)
	}
	m := raw.(map[string]any)
	if m["running"] != false {
		t.Errorf("未运行时的 reload 不得把 running 置 true，得到 %v", m["running"])
	}
	if m["reloaded"] != true {
		t.Errorf("应答应带 reloaded=true，得到 %v", m["reloaded"])
	}
	if m["nodeCount"] != 2 {
		t.Errorf("reload 应重新解析出 2 个节点，得到 %v", m["nodeCount"])
	}
}

func TestProxies_URLTestRejectsUnknownNode(t *testing.T) {
	p := writeConfigPack(t, twoNodeConfig, "")
	pm := newProxyManager()
	if _, err := handleProxiesStart(pm, p)(nil); err != nil {
		t.Fatalf("start 失败: %v", err)
	}

	params, _ := json.Marshal(map[string]any{"nodes": []string{"不存在"}})
	if _, err := handleProxiesURLTest(pm)(params); err == nil {
		t.Fatal("对不存在的节点测速必须报错，不能返回一个假延迟")
	}
}

func TestProxies_URLTestReportsRealFailureNotFakeZero(t *testing.T) {
	// 关键断言：连不上的目标必须报 delayMs=-1 + 原因，
	// **不能**报 0（0 会被 UI 显示成"0 毫秒"，看着像极快）。
	//
	// 用一个必然连不上的地址：127.0.0.1 上一个几乎不可能被占用的高位端口。
	cfg := `{
      "schemaVersion": 1,
      "routingMode": "rule",
      "nodes": ["ss://YWVzLTI1Ni1nY206cHc@127.0.0.1:1#死端口"]
    }`
	p := writeConfigPack(t, cfg, "")
	pm := newProxyManager()
	if _, err := handleProxiesStart(pm, p)(nil); err != nil {
		t.Fatalf("start 失败: %v", err)
	}

	params, _ := json.Marshal(map[string]any{"timeoutMs": 1500})
	raw, err := handleProxiesURLTest(pm)(params)
	if err != nil {
		t.Fatalf("urlTest 出错: %v", err)
	}
	m := raw.(map[string]any)
	results, ok := m["results"].([]map[string]any)
	if !ok || len(results) != 1 {
		t.Fatalf("应有 1 条结果，得到 %#v", m["results"])
	}
	r := results[0]
	if r["delayMs"] != -1 {
		t.Errorf("连不上时 delayMs 必须为 -1，得到 %v", r["delayMs"])
	}
	errStr, _ := r["error"].(string)
	if strings.TrimSpace(errStr) == "" {
		t.Error("失败结果必须带原因，不能只给一个 -1")
	}
}

func TestProxies_StatusIsJSONSerializable(t *testing.T) {
	// 守护进程会把应答 marshal 成 JSON 发出去。含不可序列化值时
	// 会在**运行时**才炸，而单元测试若只断言 map 内容则发现不了。
	pm := newProxyManager()
	raw, err := handleProxiesStatus(pm)(nil)
	if err != nil {
		t.Fatalf("status 出错: %v", err)
	}
	if _, err := json.Marshal(raw); err != nil {
		t.Fatalf("status 应答无法序列化: %v", err)
	}
}

func TestReadSimpleConfIgnoresUnknownKeysAndComments(t *testing.T) {
	// 6.4 要求 syfuse.conf 接受 SY 与 SY-Fusion 的**并集**键：
	// 陌生的键必须被忽略而不是报错。同时注释与空行要跳过。
	p := tempPaths(t)
	confPath := filepath.Join(p.DataDir, "k.conf")
	content := "" +
		"# 这是注释\n" +
		"\n" +
		"DNS_ENABLE=1\n" +
		"SY_FUSION_ONLY_KEY=whatever\n" +
		"  SPACED_KEY = value with spaces  \n" +
		"NOEQUALS_LINE\n" +
		"=emptyleft\n"
	if err := os.WriteFile(confPath, []byte(content), 0o600); err != nil {
		t.Fatalf("写 conf 失败: %v", err)
	}

	kv, err := readSimpleConf(confPath)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if kv["DNS_ENABLE"] != "1" {
		t.Errorf("DNS_ENABLE = %q, want 1", kv["DNS_ENABLE"])
	}
	if kv["SY_FUSION_ONLY_KEY"] != "whatever" {
		t.Errorf("未知键必须被接受而不是报错，得到 %q", kv["SY_FUSION_ONLY_KEY"])
	}
	if kv["SPACED_KEY"] != "value with spaces" {
		t.Errorf("值两侧空白应被裁掉，得到 %q", kv["SPACED_KEY"])
	}
	if _, ok := kv["NOEQUALS_LINE"]; ok {
		t.Error("非 KEY=value 形态的行不应进入结果")
	}
	if _, ok := kv[""]; ok {
		t.Error("空键不应进入结果")
	}
}
