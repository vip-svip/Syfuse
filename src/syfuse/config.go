package syfuse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 路由模式。取值与 Kotlin 侧 domain.model.RoutingMode 一一对应。
const (
	RoutingModeGlobal = "global" // 全部走代理
	RoutingModeRule   = "rule"   // 按规则分流
	RoutingModeDirect = "direct" // 全部直连
)

// SchemaVersion 是当前内核接受的配置 schema 版本。
//
// 读到**更高**版本必须拒绝，而不是尽力解析：新版配置里可能有旧内核
// 无法正确理解的分流规则，勉强解析会导致流量走错方向 —— 那是静默的安全问题。
const SchemaVersion = 1

// Config 是内核配置。
type Config struct {
	SchemaVersion int    `json:"schemaVersion"`
	RoutingMode   string `json:"routingMode"`

	// Nodes 是节点 URI 列表。用 []string 而非 []Node：
	// 配置是**用户可编辑的文本**，保留原始 URI 才能在解析失败时指出是哪一行。
	Nodes []string `json:"nodes"`

	// Rules 是过滤规则原文（hosts / dnsmasq / ABP 混排）。
	// 内核侧只做**传递与校验**，真正的语义编译在 Kotlin 的 core:rule 模块，
	// 因为那里有完整的 Trie 索引与优先级模型，且已被单测覆盖。
	// 让两处各实现一遍规则语义是 bug 的温床。
	Rules string `json:"rules,omitempty"`

	// DNS
	DNS DNSConfig `json:"dns,omitempty"`

	// 日志级别：silent / error / warn / info / debug
	LogLevel string `json:"logLevel,omitempty"`

	// AllowLAN 是否允许局域网设备接入（用于局域网面板）
	AllowLAN bool `json:"allowLan,omitempty"`

	// Listen 是本地入站监听地址，形如 `127.0.0.1:1080` 或 `unix:/path/to.sock`。
	//
	// 默认回环 + 随机端口（`127.0.0.1:0`）：本产品的入站只服务本机
	// （与 TUN/iptables 重定向配合）。对外开放是另一个决定，必须显式配置，
	// 不能由默认值顺手放开。端口留 0 让系统分配，避免"1080 被占用"这种
	// 与业务无关的失败，实际端口由 Engine.SocksAddr() 报给上层。
	Listen string `json:"listen,omitempty"`
}

// DNSConfig 是 DNS 相关配置。
type DNSConfig struct {
	// Servers 上游 DNS。
	Servers []string `json:"servers,omitempty"`
	// FakeIP 是否启用 FakeIP（域名嗅探依赖它）
	FakeIP bool `json:"fakeIp,omitempty"`
	// FakeIPRange 形如 198.18.0.0/15
	FakeIPRange string `json:"fakeIpRange,omitempty"`
	// Hosts 静态解析，形如 ["example.com:1.2.3.4"]
	Hosts []string `json:"hosts,omitempty"`
}

var validLogLevels = map[string]struct{}{
	"silent": {}, "error": {}, "warn": {}, "info": {}, "debug": {},
}

// ParseConfig 解析并校验配置 JSON。
//
// 校验是**严格**的：宁可拒绝启动，也不要带着一份半懂的配置去代理用户流量。
func ParseConfig(configJSON string) (*Config, error) {
	if strings.TrimSpace(configJSON) == "" {
		return nil, fmt.Errorf("配置为空")
	}

	// 先用 Decoder + DisallowUnknownFields 之外的方式：我们要**允许**未知字段，
	// 但要能识别出"来自更高 schema 版本"的配置。
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal([]byte(configJSON), &probe); err != nil {
		return nil, fmt.Errorf("JSON 非法: %w", err)
	}
	if probe.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf(
			"配置 schemaVersion=%d 高于内核支持的 %d；请升级内核后再试（拒绝按旧规则解析新配置）",
			probe.SchemaVersion, SchemaVersion)
	}

	var cfg Config
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("JSON 非法: %w", err)
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) normalize() error {
	if c.SchemaVersion == 0 {
		// 缺省视为 1，而不是 0 —— 0 会被下面的判断当成"来自未来/非法"。
		c.SchemaVersion = SchemaVersion
	}
	if c.SchemaVersion < 1 {
		return fmt.Errorf("schemaVersion 非法: %d", c.SchemaVersion)
	}

	switch c.RoutingMode {
	case "":
		c.RoutingMode = RoutingModeRule
	case RoutingModeGlobal, RoutingModeRule, RoutingModeDirect:
	default:
		return fmt.Errorf("routingMode 非法: %q（可选 %s / %s / %s）",
			c.RoutingMode, RoutingModeGlobal, RoutingModeRule, RoutingModeDirect)
	}

	if c.LogLevel == "" {
		c.LogLevel = "warn"
	}
	if _, ok := validLogLevels[c.LogLevel]; !ok {
		return fmt.Errorf("logLevel 非法: %q", c.LogLevel)
	}

	// 清理节点列表里的空行与注释，避免把"订阅里的一行注释"当成节点
	cleaned := make([]string, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		t := strings.TrimSpace(n)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		cleaned = append(cleaned, t)
	}
	c.Nodes = cleaned

	// DNS 服务器做最基本的形态校验
	for _, s := range c.DNS.Servers {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("dns.servers 含空项")
		}
	}
	if c.DNS.FakeIP && c.DNS.FakeIPRange == "" {
		return fmt.Errorf("启用 fakeIp 时必须指定 fakeIpRange")
	}

	// 局域网开放是危险项：显式要求调用方明确设置，不允许"顺手打开"。
	// 这里不拒绝，只是保证它出现在配置里时是有意的。

	// 入站监听地址：未配置时用回环 + 随机端口。
	if strings.TrimSpace(c.Listen) == "" {
		c.Listen = DefaultListenAddr
	}
	return nil
}

// DefaultListenAddr 是本地入站的默认监听地址。
//
// 回环 + 端口 0（系统分配）：见 [Config.Listen] 的说明。
const DefaultListenAddr = "127.0.0.1:0"

// String 返回不含敏感信息的配置摘要，用于日志与诊断。
func (c *Config) String() string {
	return fmt.Sprintf("schema=%d mode=%s nodes=%d rules=%d字节 dns=%d fakeIp=%v lan=%v",
		c.SchemaVersion, c.RoutingMode, len(c.Nodes), len(c.Rules),
		len(c.DNS.Servers), c.DNS.FakeIP, c.AllowLAN)
}
