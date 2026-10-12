package syfuse

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// 测试用例里的 URI 由编码器**程序化构造**，而不是手写 base64 字面量。
// 理由：手写 base64 很容易抄错一个字符，然后测试"通过"了但测的是错的东西。
// 程序化构造保证被测的是解析逻辑，而不是我的算术。

func b64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func b64std(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

const testUUID = "b831381d-6324-4d53-ad4f-8cda48b30811"

// ---------------------------------------------------------------------------
// ss
// ---------------------------------------------------------------------------

func TestParseSS_SIP002_Base64Userinfo(t *testing.T) {
	uri := "ss://" + b64url("aes-256-gcm:secret pw") + "@1.2.3.4:8388#%E8%8A%82%E7%82%B9A"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Protocol != "ss" {
		t.Errorf("protocol = %q, 期望 ss", n.Protocol)
	}
	if n.Server != "1.2.3.4" || n.Port != 8388 {
		t.Errorf("server:port = %s:%d, 期望 1.2.3.4:8388", n.Server, n.Port)
	}
	if n.Cipher != "aes-256-gcm" {
		t.Errorf("cipher = %q, 期望 aes-256-gcm", n.Cipher)
	}
	if n.Password != "secret pw" {
		t.Errorf("password = %q, 期望 %q", n.Password, "secret pw")
	}
	if n.Name != "节点A" {
		t.Errorf("name = %q, 期望 节点A（percent-decode 后的中文）", n.Name)
	}
}

func TestParseSS_LegacyFullBase64(t *testing.T) {
	uri := "ss://" + b64std("aes-128-gcm:p@ss@2.2.2.2:443") + "#legacy"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Server != "2.2.2.2" || n.Port != 443 {
		t.Errorf("server:port = %s:%d, 期望 2.2.2.2:443", n.Server, n.Port)
	}
	// 密码里含 '@'：必须按**最后一个** '@' 切分，否则密码会被截断
	if n.Password != "p@ss" {
		t.Errorf("password = %q, 期望 p@ss", n.Password)
	}
	if n.Cipher != "aes-128-gcm" {
		t.Errorf("cipher = %q, 期望 aes-128-gcm", n.Cipher)
	}
}

func TestParseSS_PlainUserinfo_PercentEncoded(t *testing.T) {
	// secretscan:allow 测试夹具
	uri := "ss://chacha20-ietf-poly1305:sec%40ret%3A1@3.3.3.3:8388#plain"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Password != "sec@ret:1" {
		t.Errorf("password = %q, 期望 sec@ret:1", n.Password)
	}
}

func TestParseSS_WithPluginQueryIsIgnored(t *testing.T) {
	uri := "ss://" + b64url("aes-256-gcm:pw") + "@4.4.4.4:8388/?plugin=obfs-local%3Bobfs%3Dhttp#withplugin"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Server != "4.4.4.4" || n.Port != 8388 {
		t.Errorf("插件查询串未正确剥离: %s:%d", n.Server, n.Port)
	}
	if n.Name != "withplugin" {
		t.Errorf("name = %q, 期望 withplugin", n.Name)
	}
}

// ---------------------------------------------------------------------------
// ssr
// ---------------------------------------------------------------------------

func TestParseSSR(t *testing.T) {
	inner := "5.5.5.5:8080:auth_aes128_md5:aes-256-cfb:tls1.2_ticket_auth:" +
		b64url("ssrpass") + "/?remarks=" + b64url("SSR节点") +
		"&obfsparam=" + b64url("obfsp") + "&protoparam=" + b64url("protop")
	uri := "ssr://" + b64url(inner)

	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Protocol != "ssr" || n.Server != "5.5.5.5" || n.Port != 8080 {
		t.Errorf("基本字段错: %+v", n)
	}
	if n.Password != "ssrpass" {
		t.Errorf("password = %q, 期望 ssrpass", n.Password)
	}
	if n.Cipher != "aes-256-cfb" {
		t.Errorf("cipher = %q, 期望 aes-256-cfb", n.Cipher)
	}
	if n.SSRProtocol != "auth_aes128_md5" {
		t.Errorf("ssrProtocol = %q", n.SSRProtocol)
	}
	if n.Obfs != "tls1.2_ticket_auth" {
		t.Errorf("obfs = %q", n.Obfs)
	}
	if n.Name != "SSR节点" {
		t.Errorf("name = %q, 期望 SSR节点", n.Name)
	}
	if n.SSRObfsParam != "obfsp" || n.SSRProtoParam != "protop" {
		t.Errorf("参数解析错: obfs=%q proto=%q", n.SSRObfsParam, n.SSRProtoParam)
	}
}

// ---------------------------------------------------------------------------
// vmess
// ---------------------------------------------------------------------------

func TestParseVmess_Base64JSON_PortAsString(t *testing.T) {
	payload := `{"v":"2","ps":"VMess节点","add":"v2.example.com","port":"443",` +
		`"id":"` + testUUID + `","aid":"0","scy":"auto","net":"ws","type":"none",` +
		`"host":"v2.example.com","path":"/ws","tls":"tls","sni":"v2.example.com"}`
	uri := "vmess://" + b64std(payload)

	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Protocol != "vmess" || n.Server != "v2.example.com" || n.Port != 443 {
		t.Errorf("基本字段错: %+v", n)
	}
	if n.UUID != testUUID {
		t.Errorf("uuid = %q", n.UUID)
	}
	if n.Network != "ws" || n.Path != "/ws" {
		t.Errorf("传输层错: net=%q path=%q", n.Network, n.Path)
	}
	if !n.TLS || n.SNI != "v2.example.com" {
		t.Errorf("TLS 字段错: tls=%v sni=%q", n.TLS, n.SNI)
	}
	if n.Name != "VMess节点" {
		t.Errorf("name = %q", n.Name)
	}
}

func TestParseVmess_PortAsNumber(t *testing.T) {
	// 数字型 port/aid 也必须能收 —— 不同客户端生成的类型不一致
	payload := `{"v":"2","ps":"num","add":"n.example.com","port":8443,` +
		`"id":"` + testUUID + `","aid":64,"net":"tcp","tls":""}`
	uri := "vmess://" + b64std(payload)

	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Port != 8443 {
		t.Errorf("port = %d, 期望 8443", n.Port)
	}
	if n.AlterID != 64 {
		t.Errorf("alterId = %d, 期望 64", n.AlterID)
	}
	if n.TLS {
		t.Errorf("tls 应为 false")
	}
}

func TestParseVmess_URLSafeBase64(t *testing.T) {
	// URL-safe 字母表也必须能解 —— 否则会有一批订阅解析失败
	payload := `{"v":"2","ps":"urlsafe","add":"u.example.com","port":"443","id":"` + testUUID + `","net":"tcp"}`
	uri := "vmess://" + b64url(payload)
	if _, err := ParseNodeURI(uri); err != nil {
		t.Fatalf("URL-safe base64 解析失败: %v", err)
	}
}

// ---------------------------------------------------------------------------
// vless / trojan / hysteria2 / socks5
// ---------------------------------------------------------------------------

func TestParseVless(t *testing.T) {
	uri := "vless://" + testUUID + "@v3.example.com:443" +
		"?encryption=none&security=tls&type=ws&path=%2Fws&host=v3.example.com&sni=v3.example.com" +
		"#VLESS%E8%8A%82%E7%82%B9"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Protocol != "vless" || n.UUID != testUUID {
		t.Errorf("基本字段错: %+v", n)
	}
	if n.Server != "v3.example.com" || n.Port != 443 {
		t.Errorf("server:port = %s:%d", n.Server, n.Port)
	}
	if !n.TLS || n.Network != "ws" || n.Path != "/ws" {
		t.Errorf("传输层错: tls=%v net=%q path=%q", n.TLS, n.Network, n.Path)
	}
	if n.Name != "VLESS节点" {
		t.Errorf("name = %q", n.Name)
	}
}

func TestParseVless_RealityCountsAsTLS(t *testing.T) {
	uri := "vless://" + testUUID + "@r.example.com:443?security=reality&type=tcp&flow=xtls-rprx-vision&sni=r.example.com#reality"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !n.TLS {
		t.Errorf("security=reality 应被视为加密传输")
	}
	if n.Flow != "xtls-rprx-vision" {
		t.Errorf("flow = %q", n.Flow)
	}
}

func TestParseTrojan(t *testing.T) {
	uri := "trojan://mypass%21@t.example.com:443?sni=t.example.com&type=tcp#T%E8%8A%82%E7%82%B9"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Protocol != "trojan" {
		t.Errorf("protocol = %q", n.Protocol)
	}
	if n.Password != "mypass!" {
		t.Errorf("password = %q, 期望 mypass!", n.Password)
	}
	// trojan 默认走 TLS
	if !n.TLS {
		t.Errorf("trojan 默认应启用 TLS")
	}
	if n.SNI != "t.example.com" {
		t.Errorf("sni = %q", n.SNI)
	}
}

func TestParseTrojan_SecurityNone(t *testing.T) {
	// secretscan:allow 测试夹具
	uri := "trojan://pw@t2.example.com:8080?security=none&type=tcp#none"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.TLS {
		t.Errorf("security=none 时不应启用 TLS")
	}
}

func TestParseHysteria2(t *testing.T) {
	// secretscan:allow 测试夹具
	uri := "hysteria2://authpass@h.example.com:8443" +
		"?sni=h.example.com&insecure=1&obfs=salamander&obfs-password=obfspw#H%E8%8A%82%E7%82%B9"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Protocol != "hysteria2" || n.Password != "authpass" {
		t.Errorf("基本字段错: %+v", n)
	}
	if n.Network != "quic" {
		t.Errorf("network = %q, 期望 quic", n.Network)
	}
	if !n.TLS || !n.Insecure {
		t.Errorf("tls=%v insecure=%v", n.TLS, n.Insecure)
	}
	if n.Obfs != "salamander" || n.ObfsPassword != "obfspw" {
		t.Errorf("obfs = %q / %q", n.Obfs, n.ObfsPassword)
	}
}

func TestParseHysteria2_AliasHy2(t *testing.T) {
	n, err := ParseNodeURI("hy2://pw@h2.example.com:443?sni=h2.example.com#alias")
	if err != nil {
		t.Fatalf("hy2:// 别名解析失败: %v", err)
	}
	if n.Server != "h2.example.com" {
		t.Errorf("server = %q", n.Server)
	}
}

func TestParseSocks5(t *testing.T) {
	// secretscan:allow 测试夹具
	n, err := ParseNodeURI("socks5://user:pass@127.0.0.1:1080#%E6%9C%AC%E5%9C%B0")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Protocol != "socks5" {
		t.Errorf("protocol = %q", n.Protocol)
	}
	if n.UUID != "user" || n.Password != "pass" {
		t.Errorf("凭据错: user=%q pass=%q", n.UUID, n.Password)
	}
	if n.Name != "本地" {
		t.Errorf("name = %q", n.Name)
	}
}

func TestParseHTTP_HTTPS(t *testing.T) {
	n, err := ParseNodeURI("https://u:p@proxy.example.com:3128#%E4%BB%A3%E7%90%86")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Protocol != "http" {
		t.Errorf("protocol = %q", n.Protocol)
	}
	if !n.TLS {
		t.Errorf("https 应置 TLS")
	}
}

// ---------------------------------------------------------------------------
// IPv6
// ---------------------------------------------------------------------------

func TestParseIPv6Literal(t *testing.T) {
	uri := "ss://" + b64url("aes-256-gcm:pw") + "@[2001:db8::1]:8388#v6"
	n, err := ParseNodeURI(uri)
	if err != nil {
		t.Fatalf("IPv6 解析失败: %v", err)
	}
	if n.Server != "2001:db8::1" {
		t.Errorf("server = %q, 期望 2001:db8::1", n.Server)
	}
	if n.Port != 8388 {
		t.Errorf("port = %d", n.Port)
	}
}

func TestParseIPv6_MissingBracketRejected(t *testing.T) {
	_, err := ParseNodeURI("ss://" + b64url("aes-256-gcm:pw") + "@2001:db8::1:8388#bad")
	if err == nil {
		t.Fatalf("未加方括号的 IPv6 应当被拒绝，但解析成功了")
	}
}

// ---------------------------------------------------------------------------
// 错误路径
// ---------------------------------------------------------------------------

func TestParseNodeURI_Errors(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want string // 错误信息应包含的片段
	}{
		{"空字符串", "", "空 URI"},
		{"缺少 scheme", "1.2.3.4:8388", "缺少 scheme"},
		{"未知协议", "quantum://x@h:1", "不支持的协议"},
		{"ss 缺端口", "ss://" + b64url("aes-256-gcm:pw") + "@1.2.3.4", "缺少端口"},
		{"ss 端口越界", "ss://" + b64url("aes-256-gcm:pw") + "@1.2.3.4:99999", "端口越界"},
		{"ss 端口非数字", "ss://" + b64url("aes-256-gcm:pw") + "@1.2.3.4:abc", "端口非数字"},
		{"ss 凭据缺冒号", "ss://" + b64url("nocolon") + "@1.2.3.4:8388", "凭据格式"},
		{"vmess 非 JSON", "vmess://" + b64std("not json"), "JSON 非法"},
		{"vmess 缺 UUID", "vmess://" + b64std(`{"add":"a.com","port":"443"}`), "UUID 缺失"},
		{"vmess 缺端口", "vmess://" + b64std(`{"add":"a.com","id":"`+testUUID+`"}`), "端口缺失"},
		{"vmess 缺服务器", "vmess://" + b64std(`{"port":"443","id":"`+testUUID+`"}`), "服务器地址缺失"},
		{"vless 缺 UUID", "vless://@h.com:443", "缺少 UUID"},
		{"trojan 缺密码", "trojan://@h.com:443", "缺少密码"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseNodeURI(c.uri)
			if err == nil {
				t.Fatalf("期望报错（含 %q），但解析成功了", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("错误信息 = %q, 期望包含 %q", err.Error(), c.want)
			}
		})
	}
}

func TestParseNodeURI_DefaultName(t *testing.T) {
	n, err := ParseNodeURI("ss://" + b64url("aes-256-gcm:pw") + "@9.9.9.9:8388")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if n.Name != "ss-9.9.9.9:8388" {
		t.Errorf("默认名称 = %q", n.Name)
	}
}

// ---------------------------------------------------------------------------
// 订阅解析
// ---------------------------------------------------------------------------

func TestParseSubscription_PartialSuccess(t *testing.T) {
	content := strings.Join([]string{
		"# 这是注释，应被忽略",
		"",
		"ss://" + b64url("aes-256-gcm:pw1") + "@1.1.1.1:8388#ok1",
		"这不是一个合法的节点",
		"ss://" + b64url("aes-256-gcm:pw2") + "@2.2.2.2:8388#ok2",
		"vmess://" + b64std("{坏 JSON}"),
		"ss://" + b64url("aes-256-gcm:pw3") + "@3.3.3.3:8388#ok3",
	}, "\n")

	nodes, errs := ParseSubscription(content)

	if len(nodes) != 3 {
		t.Errorf("成功节点数 = %d, 期望 3", len(nodes))
	}
	if len(errs) != 2 {
		t.Errorf("错误数 = %d, 期望 2（一行非法 scheme + 一行坏 JSON）", len(errs))
	}
	// 关键语义：坏行不能让整份订阅失败
	if len(nodes) == 0 {
		t.Fatalf("部分失败语义被破坏：坏行导致全部节点丢失")
	}
	// 错误信息必须能定位到行号
	for _, e := range errs {
		if !strings.Contains(e.Error(), "行") {
			t.Errorf("错误信息缺少行号定位: %v", e)
		}
	}
}

func TestParseSubscription_Dedup(t *testing.T) {
	line := "ss://" + b64url("aes-256-gcm:pw") + "@1.1.1.1:8388#nameA"
	lineSameKeyDifferentName := "ss://" + b64url("aes-256-gcm:pw") + "@1.1.1.1:8388#nameB"
	lineDifferentPassword := "ss://" + b64url("aes-256-gcm:pw2") + "@1.1.1.1:8388#nameC"

	nodes, errs := ParseSubscription(strings.Join([]string{
		line, lineSameKeyDifferentName, lineDifferentPassword,
	}, "\n"))
	if len(errs) != 0 {
		t.Fatalf("不应有错误: %v", errs)
	}
	// 名字不同但连接参数相同 -> 去重；密码不同 -> 保留
	if len(nodes) != 2 {
		t.Errorf("去重后节点数 = %d, 期望 2（名字不同应去重，密码不同应保留）", len(nodes))
	}
}

func TestParseSubscription_Base64WrappedSubscription(t *testing.T) {
	inner := strings.Join([]string{
		"ss://" + b64url("aes-256-gcm:pw1") + "@1.1.1.1:8388#a",
		"ss://" + b64url("aes-256-gcm:pw2") + "@2.2.2.2:8388#b",
	}, "\n")
	// 很多订阅服务返回的是"整体 base64 的一坨"，必须支持
	content := b64std(inner)

	nodes, errs := ParseSubscription(content)
	if len(errs) != 0 {
		t.Fatalf("整体 base64 订阅解析出错: %v", errs)
	}
	if len(nodes) != 2 {
		t.Errorf("节点数 = %d, 期望 2", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// 序列化与凭据隔离
// ---------------------------------------------------------------------------

func TestMarshalNodes_NoSecretsLeak(t *testing.T) {
	nodes := []Node{
		// secretscan:allow 以下均为测试夹具，不是真实凭据
		{Name: "a", Protocol: "ss", Server: "1.1.1.1", Port: 8388, Password: "SUPER_SECRET_PW", Cipher: "aes-256-gcm",
			Raw: "ss://SUPER_SECRET_PW@1.1.1.1:8388"},
		{Name: "b", Protocol: "vmess", Server: "2.2.2.2", Port: 443, UUID: "SUPER_SECRET_UUID"},
	}
	out, err := MarshalNodes(nodes)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if strings.Contains(out, "SUPER_SECRET_PW") {
		t.Errorf("MarshalNodes 泄漏了密码: %s", out)
	}
	if strings.Contains(out, "SUPER_SECRET_UUID") {
		t.Errorf("MarshalNodes 泄漏了 UUID: %s", out)
	}
	// Raw 含完整 URI，也含凭据 —— 同样必须被白名单挡掉
	if strings.Contains(out, "ss://") {
		t.Errorf("MarshalNodes 泄漏了原始 URI（含凭据）: %s", out)
	}
	if !strings.Contains(out, "1.1.1.1") {
		t.Errorf("非敏感字段应保留: %s", out)
	}

	// 反向验证：带凭据的版本**必须**包含它们，否则连接时拿不到密码。
	// 这一条曾经真的失败过：Password 原本标了 json:"-"，导致带凭据的序列化
	// 也是空的 —— 那会在建立连接时表现为"密码为空"，且只在运行时才暴露。
	full, err := MarshalNodesWithSecrets(nodes)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(full, "SUPER_SECRET_PW") {
		t.Errorf("MarshalNodesWithSecrets 应包含密码: %s", full)
	}
	if !strings.Contains(full, "SUPER_SECRET_UUID") {
		t.Errorf("MarshalNodesWithSecrets 应包含 UUID: %s", full)
	}
}

func TestNodePublic_WhitelistCoversAllNonSecretFields(t *testing.T) {
	// 防止 nodePublic 漏字段：如果新增了非敏感字段却忘了加进投影，
	// UI 会莫名其妙少显示一项。这里用"填满所有字段"的方式做一次覆盖检查。
	n := Node{
		Name: "n", Protocol: "vless", Server: "s.example.com", Port: 443,
		// secretscan:allow 测试夹具，用于验证凭据隔离，不是真实凭据
		Password: "SECRETPASSWORDVALUE", UUID: "SECRETUUIDVALUE",
		Network: "ws", TLS: true, SNI: "sni", Host: "host", Path: "/p",
		Cipher: "none", AlterID: 7, Flow: "xtls-rprx-vision", Obfs: "salamander",
		ObfsPassword: "SECRETOBFSPW", Insecure: true,
		SSRProtocol: "auth_aes128_md5", SSRObfsParam: "op", SSRProtoParam: "pp",
		Raw: "SECRETRAWURI",
	}
	b, err := json.Marshal(n.public())
	if err != nil {
		t.Fatalf("投影序列化失败: %v", err)
	}
	out := string(b)

	mustContain := []string{
		`"name":"n"`, `"protocol":"vless"`, `"server":"s.example.com"`, `"port":443`,
		`"network":"ws"`, `"tls":true`, `"sni":"sni"`, `"host":"host"`, `"path":"/p"`,
		`"cipher":"none"`, `"alterId":7`, `"flow":"xtls-rprx-vision"`,
		`"obfs":"salamander"`, `"obfsPassword":"SECRETOBFSPW"`, `"insecure":true`,
		`"ssrProtocol":"auth_aes128_md5"`, `"ssrObfsParam":"op"`, `"ssrProtoParam":"pp"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("nodePublic 投影缺少字段 %s\n实际: %s", want, out)
		}
	}
	mustNotContain := []string{"SECRETPASSWORDVALUE", "SECRETUUIDVALUE", "SECRETRAWURI"}
	for _, bad := range mustNotContain {
		if strings.Contains(out, bad) {
			t.Errorf("nodePublic 投影泄漏了 %q: %s", bad, out)
		}
	}
}

func TestMarshalNodes_IsValidJSON(t *testing.T) {
	nodes := []Node{{Name: "a", Protocol: "ss", Server: "1.1.1.1", Port: 8388}}
	out, err := MarshalNodes(nodes)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var back []map[string]any
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if len(back) != 1 {
		t.Errorf("反序列化后长度 = %d", len(back))
	}
}

func TestSortNodes_Deterministic(t *testing.T) {
	mk := func(names ...string) []Node {
		out := make([]Node, 0, len(names))
		for i, n := range names {
			out = append(out, Node{Name: n, Protocol: "ss", Server: "1.1.1.1", Port: 1000 + i})
		}
		return out
	}
	a := mk("丙", "甲", "乙")
	b := mk("乙", "丙", "甲")
	SortNodes(a)
	SortNodes(b)

	// 断言"确定性"而不是"某个具体顺序"：输入顺序不同的两份相同数据，
	// 排序后必须得到同一个序列。这才是可复现性的实质。
	seq := func(ns []Node) string {
		parts := make([]string, len(ns))
		for i, n := range ns {
			parts[i] = n.Name
		}
		return strings.Join(parts, ",")
	}
	if seq(a) != seq(b) {
		t.Errorf("排序不确定: %q vs %q", seq(a), seq(b))
	}
	// 并且确实按名称有序（用 Go 的字节序，与 sort 实现一致）
	for i := 1; i < len(a); i++ {
		if a[i-1].Name > a[i].Name {
			t.Errorf("排序结果无序: %q 出现在 %q 之前", a[i-1].Name, a[i].Name)
		}
	}
}

func TestSupportedProtocols_MatchesParsers(t *testing.T) {
	// 防止"声称支持但解析器里没有分支"这类不一致
	for _, p := range SupportedProtocols() {
		switch p {
		case "ss", "ssr", "vmess", "vless", "trojan", "hysteria2", "http", "socks5":
		default:
			t.Errorf("SupportedProtocols 声称支持 %q，但解析器没有对应分支", p)
		}
	}
}
