package syfuse

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseConfig_MinimalValid(t *testing.T) {
	cfg, err := ParseConfig(`{"schemaVersion":1,"routingMode":"rule","nodes":["ss://x@1.1.1.1:1"]}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.RoutingMode != RoutingModeRule {
		t.Errorf("routingMode = %q", cfg.RoutingMode)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("logLevel 应默认 warn，实际 %q", cfg.LogLevel)
	}
}

func TestParseConfig_DefaultsApplied(t *testing.T) {
	cfg, err := ParseConfig(`{}`)
	if err != nil {
		t.Fatalf("空对象应能解析并补默认值: %v", err)
	}
	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion 默认值 = %d, 期望 %d", cfg.SchemaVersion, SchemaVersion)
	}
	if cfg.RoutingMode != RoutingModeRule {
		t.Errorf("routingMode 默认值 = %q, 期望 rule", cfg.RoutingMode)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("logLevel 默认值 = %q", cfg.LogLevel)
	}
}

func TestParseConfig_RejectsHigherSchemaVersion(t *testing.T) {
	// 这是**安全**行为而不是严格性洁癖：新配置里可能有旧内核理解不了的分流规则，
	// 勉强解析会让流量走错方向，而且用户不会收到任何提示。
	_, err := ParseConfig(`{"schemaVersion":2,"routingMode":"rule"}`)
	if err == nil {
		t.Fatalf("更高的 schemaVersion 必须被拒绝")
	}
	if !strings.Contains(err.Error(), "schemaVersion") {
		t.Errorf("错误信息应指明 schemaVersion: %v", err)
	}
}

func TestParseConfig_AcceptsLowerSchemaVersion(t *testing.T) {
	// 向后兼容：旧配置必须能读。否则每次升级内核都会让用户配置失效。
	cfg, err := ParseConfig(`{"schemaVersion":1,"routingMode":"global"}`)
	if err != nil {
		t.Fatalf("同版本配置解析失败: %v", err)
	}
	if cfg.RoutingMode != RoutingModeGlobal {
		t.Errorf("routingMode = %q", cfg.RoutingMode)
	}
}

func TestParseConfig_InvalidRoutingMode(t *testing.T) {
	_, err := ParseConfig(`{"schemaVersion":1,"routingMode":"teleport"}`)
	if err == nil {
		t.Fatalf("非法 routingMode 必须被拒绝")
	}
	if !strings.Contains(err.Error(), "routingMode") {
		t.Errorf("错误信息应指明 routingMode: %v", err)
	}
}

func TestParseConfig_InvalidLogLevel(t *testing.T) {
	_, err := ParseConfig(`{"schemaVersion":1,"logLevel":"verbose"}`)
	if err == nil {
		t.Fatalf("非法 logLevel 必须被拒绝")
	}
}

func TestParseConfig_InvalidJSON(t *testing.T) {
	_, err := ParseConfig(`{not json}`)
	if err == nil {
		t.Fatalf("非法 JSON 必须被拒绝")
	}
}

func TestParseConfig_EmptyRejected(t *testing.T) {
	if _, err := ParseConfig(""); err == nil {
		t.Fatalf("空配置必须被拒绝")
	}
	if _, err := ParseConfig("   \n\t "); err == nil {
		t.Fatalf("纯空白配置必须被拒绝")
	}
}

func TestParseConfig_StripsCommentsAndBlankLinesFromNodes(t *testing.T) {
	raw := `{"schemaVersion":1,"nodes":["ss://a@1.1.1.1:1","# 注释行","","  ","ss://b@2.2.2.2:2"]}`
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(cfg.Nodes) != 2 {
		t.Errorf("节点数 = %d, 期望 2（注释与空行应被剔除）: %v", len(cfg.Nodes), cfg.Nodes)
	}
}

func TestParseConfig_FakeIPRequiresRange(t *testing.T) {
	_, err := ParseConfig(`{"schemaVersion":1,"dns":{"fakeIp":true}}`)
	if err == nil {
		t.Fatalf("启用 fakeIp 但未给 fakeIpRange 时必须被拒绝")
	}
	if !strings.Contains(err.Error(), "fakeIpRange") {
		t.Errorf("错误信息应指明 fakeIpRange: %v", err)
	}

	// 给了 range 就应该通过
	if _, err := ParseConfig(`{"schemaVersion":1,"dns":{"fakeIp":true,"fakeIpRange":"198.18.0.0/15"}}`); err != nil {
		t.Errorf("合法 fakeIp 配置被拒绝: %v", err)
	}
}

func TestParseConfig_EmptyDNSServerRejected(t *testing.T) {
	_, err := ParseConfig(`{"schemaVersion":1,"dns":{"servers":["8.8.8.8",""]}}`)
	if err == nil {
		t.Fatalf("空的 dns server 项必须被拒绝")
	}
}

func TestParseConfig_UnknownFieldsAreIgnored(t *testing.T) {
	// 前向兼容：更新版本写入的未知字段不应让旧内核拒绝整份配置。
	// （版本号提升时才需要显式拒绝，那由 schemaVersion 判断负责。）
	raw := `{"schemaVersion":1,"routingMode":"rule","futureField":{"a":1},"anotherNew":"x"}`
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("未知字段不应导致解析失败: %v", err)
	}
	if cfg.RoutingMode != RoutingModeRule {
		t.Errorf("已知字段应正常解析")
	}
}

func TestValidateConfigJSON_RoundTrip(t *testing.T) {
	in := `{"schemaVersion":1,"routingMode":"global","nodes":["ss://a@1.1.1.1:1"],"logLevel":"info"}`
	out, err := ValidateConfigJSON(in)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if m["routingMode"] != "global" {
		t.Errorf("规范化输出丢了字段: %v", m)
	}
	if m["logLevel"] != "info" {
		t.Errorf("logLevel 被改写: %v", m)
	}
}

func TestValidateConfigJSON_PropagatesError(t *testing.T) {
	if _, err := ValidateConfigJSON(`{"schemaVersion":99}`); err == nil {
		t.Fatalf("高版本 schema 应校验失败")
	}
}

func TestConfig_StringHasNoSecretFields(t *testing.T) {
	// String() 用于日志与诊断，必须只输出**计数**，不能输出节点 URI（含凭据）。
	// 注意：JSON 必须用字符串拼接构造 —— 反引号原始字符串里的 "+" 会被当成 JSON 字面量。
	raw := `{"schemaVersion":1,"nodes":["ss://` + b64url("aes-256-gcm:secret") + `@1.1.1.1:1"]}`
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	s := cfg.String()
	if strings.Contains(s, "1.1.1.1") {
		t.Errorf("Config.String() 泄漏了节点地址: %s", s)
	}
	if strings.Contains(s, "secret") {
		t.Errorf("Config.String() 泄漏了凭据: %s", s)
	}
	if !strings.Contains(s, "nodes=1") {
		t.Errorf("Config.String() 应包含节点计数: %s", s)
	}
}
