package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vip-svip/syfuse/src/internal/conn"
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

// twoNodeConfig 是一份**能真正启动**的规则模式配置：
// 两个 ss 节点 + 一条规则。规则非空是硬要求 —— 规则模式给空规则集
// 会让所有流量落到默认动作，用户以为分流生效了其实没有（见
// syfuse.newRuleRouter 的拒绝逻辑）。
const twoNodeConfig = `{
  "schemaVersion": 1,
  "routingMode": "rule",
  "nodes": [
    "ss://YWVzLTI1Ni1nY206cHc@127.0.0.1:8388#节点B",
    "ss://YWVzLTI1Ni1nY206cHc@127.0.0.1:8389#节点A"
  ],
  "rules": "||example.com^\n",
  "logLevel": "info"
}`

// TestProxies_StartBuildsRealTunnel 核心断言（M3 之后的真相）：
// start 必须建出真隧道并报 running + socksAddr。
//
// 历史对照：这里曾经断言 `state == "not_linked"`（内核宿主未接通）。
// 现在隧道已实现，那条断言反映的是**过去**的事实，留着就是假绿 ——
// 所以它被替换成"真的在跑"而不是被删掉了事。
func TestProxies_StartBuildsRealTunnel(t *testing.T) {
	p := writeConfigPack(t, twoNodeConfig, "")
	pm := newProxyManager()
	t.Cleanup(func() { _, _ = handleProxiesStop(pm)(nil) })

	raw, err := handleProxiesStart(pm, p)(nil)
	if err != nil {
		t.Fatalf("start 返回错误: %v", err)
	}
	m := raw.(map[string]any)

	if m["state"] != "running" {
		t.Errorf("隧道建成后 state 应为 running，得到 %v（error=%v）", m["state"], m["error"])
	}
	if m["running"] != true {
		t.Errorf("running 应为 true，得到 %v", m["running"])
	}
	if m["kernelLinked"] != true {
		t.Errorf("kernelLinked 应为 true，得到 %v", m["kernelLinked"])
	}
	// socksAddr 是上层（TUN/iptables）唯一知道"流量送哪去"的依据
	addr, _ := m["socksAddr"].(string)
	if addr == "" {
		t.Fatalf("必须报回 socksAddr，否则上层无处送流量")
	}
	// 端口可能是系统分配的：真的能连上才算数。
	c, dialErr := net.DialTimeout("tcp", addr, 3*time.Second)
	if dialErr != nil {
		t.Fatalf("socksAddr %s 连不上: %v", addr, dialErr)
	}
	c.Close()
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

// TestProxies_ReloadRebuildsTunnelWhenRunning 运行中 reload 必须真的换隧道：
// 新配置的监听地址要生效（否则用户改了 listen 却连到旧端口）。
func TestProxies_ReloadRebuildsTunnelWhenRunning(t *testing.T) {
	p := writeConfigPack(t, twoNodeConfig, "")
	pm := newProxyManager()
	t.Cleanup(func() { _, _ = handleProxiesStop(pm)(nil) })

	first := mustStart(t, pm, p)
	addr1 := first["socksAddr"].(string)

	// 换一份配置：同样两个节点，但规则不同（用来证明配置真的被换了）。
	reloaded := `{
  "schemaVersion": 1,
  "routingMode": "rule",
  "nodes": [
    "ss://YWVzLTI1Ni1nY206cHc@127.0.0.1:8388#节点B",
    "ss://YWVzLTI1Ni1nY206cHc@127.0.0.1:8389#节点A"
  ],
  "rules": "||changed.example^\n",
  "logLevel": "info"
}`
	if err := os.WriteFile(filepath.Join(p.DataDir, "config.json"), []byte(reloaded), 0o600); err != nil {
		t.Fatalf("写配置包: %v", err)
	}
	raw, err := handleProxiesReload(pm, p)(nil)
	if err != nil {
		t.Fatalf("reload 出错: %v", err)
	}
	m := raw.(map[string]any)
	if m["running"] != true {
		t.Errorf("运行中 reload 后应保持 running，得到 %v", m["running"])
	}
	if m["reloaded"] != true {
		t.Errorf("应答应带 reloaded=true")
	}
	addr2, _ := m["socksAddr"].(string)
	if addr2 == "" {
		t.Fatalf("运行中 reload 必须报回 socksAddr")
	}
	// 旧隧道必须已经关掉：留着就是一个没人知道的僵尸监听端。
	if c, dialErr := net.DialTimeout("tcp", addr1, time.Second); dialErr == nil {
		c.Close()
		t.Errorf("reload 后旧地址 %s 仍在监听 —— 旧隧道没有关闭", addr1)
	}
}

// TestProxies_ReloadWhenStoppedDoesNotCreateTunnel 未运行时 reload
// **不得**建隧道：那会造成"有监听端在收流量、UI 显示未连接"的分裂。
func TestProxies_ReloadWhenStoppedDoesNotCreateTunnel(t *testing.T) {
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
	if m["state"] != "stopped" {
		t.Errorf("未运行时 reload 后 state 应为 stopped，得到 %v", m["state"])
	}
	if _, ok := m["socksAddr"]; ok {
		t.Errorf("未运行时 reload 不得报 socksAddr（那意味着起了隧道）")
	}
	// 节点表必须真的刷新了 —— 否则 reload 是空操作。
	if m["nodeCount"] != 2 {
		t.Errorf("reload 应重新解析出 2 个节点，得到 %v", m["nodeCount"])
	}
}

// mustStart 走一次 start 并断言成功，返回应答 map。
func mustStart(t *testing.T, pm *proxyManager, p Paths) map[string]any {
	t.Helper()
	raw, err := handleProxiesStart(pm, p)(nil)
	if err != nil {
		t.Fatalf("start 返回错误: %v", err)
	}
	m := raw.(map[string]any)
	if m["state"] != "running" {
		t.Fatalf("start 后应为 running，得到 %v（error=%v）", m["state"], m["error"])
	}
	return m
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

// TestProxies_ConnectionsTraffic 端到端验证 M9 两个数据出口：
// 真隧道里跑一条真实连接，connections 必须报出这条记录、traffic 必须报出
// 同源统计。engine 未创建时两个方法必须返回空结果而不是错误
// （界面该显示"没有数据"，不是接口故障）。
func TestProxies_ConnectionsTraffic(t *testing.T) {
	// 1) 引擎未创建：空结果，不报错。
	pm0 := newProxyManager()
	connResp, err := handleConnections(pm0)(nil)
	if err != nil {
		t.Fatalf("引擎未创建时 connections 报错: %v", err)
	}
	if m := connResp.(map[string]any); len(m["records"].([]any)) != 0 {
		t.Fatalf("引擎未创建时 records 应为空，得到 %d 条", len(m["records"].([]conn.Record)))
	}
	if _, err := handleTraffic(pm0)(nil); err != nil {
		t.Fatalf("引擎未创建时 traffic 报错: %v", err)
	}

	// 2) 真隧道 + 真连接。
	// 本地 echo 上游（直连模式拨得到）。
	up, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen 上游: %v", err)
	}
	defer up.Close()
	go func() {
		for {
			c, e := up.Accept()
			if e != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()

	cfg := fmt.Sprintf(`{"schemaVersion":1,"routingMode":"direct","nodes":[],"logLevel":"info"}`)
	p := writeConfigPack(t, cfg, "")
	pm := newProxyManager()
	t.Cleanup(func() { _, _ = handleProxiesStop(pm)(nil) })
	m := mustStart(t, pm, p)
	addr, _ := m["socksAddr"].(string)
	if addr == "" {
		t.Fatalf("必须报回 socksAddr")
	}

	// 最小 SOCKS5 客户端：问候 → 连接目标 → 收到成功应答。
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("连入站: %v", err)
	}
	defer c.Close()
	host, portStr, _ := net.SplitHostPort(up.Addr().String())
	port, _ := strconv.Atoi(portStr)
	if _, err := c.Write([]byte{5, 1, 0}); err != nil {
		t.Fatalf("问候写出: %v", err)
	}
	hello := make([]byte, 2)
	if _, err := io.ReadFull(c, hello); err != nil || hello[0] != 5 || hello[1] != 0 {
		t.Fatalf("问候应答异常: %v %v", hello, err)
	}
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = append(req, byte(port>>8), byte(port&0xff))
	if _, err := c.Write(req); err != nil {
		t.Fatalf("连接请求写出: %v", err)
	}
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != 0 {
		t.Fatalf("连接应答异常: %v %v", rep, err)
	}
	// 真的发几个字节再关，让记录里有非零字节数。
	if _, err := c.Write([]byte("hi")); err != nil {
		t.Fatalf("业务写出: %v", err)
	}
	buf := make([]byte, 2)
	_, _ = io.ReadFull(c, buf)
	c.Close()

	// 3) connections 必须报出这条真实记录。
	waitForCond(t, "connections 记账", func() bool {
		resp, err := handleConnections(pm)(nil)
		if err != nil {
			return false
		}
		st := resp.(map[string]any)["stats"].(conn.Stats)
		return st.ClosedConnections == 1
	})
	resp, err := handleConnections(pm)(nil)
	if err != nil {
		t.Fatalf("connections: %v", err)
	}
	recs := resp.(map[string]any)["records"].([]conn.Record)
	if len(recs) != 1 {
		t.Fatalf("应记 1 条连接，得到 %d", len(recs))
	}
	rec := recs[0]
	if rec.DstHost != host || rec.DstPort != port {
		t.Errorf("记录目标 = %s:%d, want %s:%d", rec.DstHost, rec.DstPort, host, port)
	}
	if rec.BytesIn < 2 {
		t.Errorf("BytesIn = %d, want >= 2", rec.BytesIn)
	}

	// 4) traffic 与 connections 同源。
	traffic, err := handleTraffic(pm)(nil)
	if err != nil {
		t.Fatalf("traffic: %v", err)
	}
	st := traffic.(conn.Stats)
	if st.TotalConnections != 1 || st.TotalBytesIn != rec.BytesIn {
		t.Errorf("traffic 与 connections 不同源：%+v vs %+v", st, rec)
	}
}

// waitForCond 与 dataplane 的 waitFor 同构：轮询等待异步关账可见。
func waitForCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时", what)
}
