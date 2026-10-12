package syfuse

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Node 是一个代理节点。
//
// 字段刻意用可导出的基本类型，因为本结构要跨越 gomobile 边界，
// 复杂类型（map / interface / 嵌套指针）在绑定层支持很差。
type Node struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Server   string `json:"server"`
	Port     int    `json:"port"`

	// 凭据。
	//
	// 注意：这里**不能**用 `json:"-"`。那样会让 MarshalNodesWithSecrets 也拿不到
	// 凭据，真实连接时就会"密码为空"—— 而且是在运行时才炸。
	// 凭据隔离改由 nodePublic 白名单投影保证（见该类型注释），而不是靠 tag 屏蔽。
	Password string `json:"password,omitempty"`
	UUID     string `json:"uuid,omitempty"`

	// 传输层
	Network string `json:"network,omitempty"` // tcp / ws / grpc / h2 / quic
	TLS     bool   `json:"tls,omitempty"`
	SNI     string `json:"sni,omitempty"`
	Host    string `json:"host,omitempty"`
	Path    string `json:"path,omitempty"`

	// 协议特有
	Cipher  string `json:"cipher,omitempty"`
	AlterID int    `json:"alterId,omitempty"`
	Flow    string `json:"flow,omitempty"`
	Obfs    string `json:"obfs,omitempty"`

	// 传输层细项。
	// 之所以用一串标量字段而不是 map[string]string：gomobile 对 map 字段支持很差，
	// 会让 bind 阶段直接失败。标量字段虽然啰嗦，但能稳定跨过绑定层。
	ObfsPassword string `json:"obfsPassword,omitempty"`
	Insecure     bool   `json:"insecure,omitempty"`

	// ssr 专有：protocol 与 obfs 是两个独立维度，且各带参数。
	SSRProtocol   string `json:"ssrProtocol,omitempty"`
	SSRObfsParam  string `json:"ssrObfsParam,omitempty"`
	SSRProtoParam string `json:"ssrProtoParam,omitempty"`

	// Raw 是原始 URI（**含凭据**）。保留它是为了：解析出错时能向用户指出
	// **具体哪一行**有问题，而不是只报"第 N 个节点无效"。
	// 它同样不在 nodePublic 白名单里，因此不会出现在 UI 用的 JSON 中。
	Raw string `json:"raw,omitempty"`
}

// nodePublic 是 Node 的**去凭据投影**，用于 UI 展示与跨进程传递。
//
// 为什么用白名单投影，而不是"把敏感字段清空"：
// 黑名单式清空（safe[i].Password = ""）在**新增**敏感字段时会静默失效 ——
// 加字段的人不会记得去改清空逻辑，于是凭据就漏出去了，而且没有任何编译错误。
// 白名单投影则相反：新字段默认**不出现**，要漏必须先显式加进来。
// 安全默认值应当是"不输出"，而不是"记得清空"。
type nodePublic struct {
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
	Server        string `json:"server"`
	Port          int    `json:"port"`
	Network       string `json:"network,omitempty"`
	TLS           bool   `json:"tls,omitempty"`
	SNI           string `json:"sni,omitempty"`
	Host          string `json:"host,omitempty"`
	Path          string `json:"path,omitempty"`
	Cipher        string `json:"cipher,omitempty"`
	AlterID       int    `json:"alterId,omitempty"`
	Flow          string `json:"flow,omitempty"`
	Obfs          string `json:"obfs,omitempty"`
	ObfsPassword  string `json:"obfsPassword,omitempty"`
	Insecure      bool   `json:"insecure,omitempty"`
	SSRProtocol   string `json:"ssrProtocol,omitempty"`
	SSRObfsParam  string `json:"ssrObfsParam,omitempty"`
	SSRProtoParam string `json:"ssrProtoParam,omitempty"`
}

func (n Node) public() nodePublic {
	return nodePublic{
		Name:          n.Name,
		Protocol:      n.Protocol,
		Server:        n.Server,
		Port:          n.Port,
		Network:       n.Network,
		TLS:           n.TLS,
		SNI:           n.SNI,
		Host:          n.Host,
		Path:          n.Path,
		Cipher:        n.Cipher,
		AlterID:       n.AlterID,
		Flow:          n.Flow,
		Obfs:          n.Obfs,
		ObfsPassword:  n.ObfsPassword,
		Insecure:      n.Insecure,
		SSRProtocol:   n.SSRProtocol,
		SSRObfsParam:  n.SSRObfsParam,
		SSRProtoParam: n.SSRProtoParam,
	}
}

// Key 返回用于去重的稳定标识。
//
// 不含 Name —— 同一个节点在不同订阅里名字可能不同，那不该算两个节点。
// 含凭据 —— 同服务器同端口但密码不同是两个不同节点。
func (n Node) Key() string {
	secret := n.UUID
	if secret == "" {
		secret = n.Password
	}
	return strings.Join([]string{
		strings.ToLower(n.Protocol),
		strings.ToLower(n.Server),
		strconv.Itoa(n.Port),
		secret,
		strings.ToLower(n.Network),
		n.Path,
		strings.ToLower(n.Obfs),
		strings.ToLower(n.SSRProtocol),
	}, "|")
}

// SupportedProtocols 列出解析器支持的协议。
func SupportedProtocols() []string {
	return []string{"ss", "ssr", "vmess", "vless", "trojan", "hysteria2", "http", "socks5"}
}

// ParseNodeURI 解析单条节点 URI。
func ParseNodeURI(raw string) (Node, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Node{}, fmt.Errorf("空 URI")
	}
	scheme, rest, ok := splitScheme(trimmed)
	if !ok {
		return Node{}, fmt.Errorf("缺少 scheme: %q", truncate(trimmed, 24))
	}

	var (
		n   Node
		err error
	)
	switch scheme {
	case "ss":
		n, err = parseSS(rest)
	case "ssr":
		n, err = parseSSR(rest)
	case "vmess":
		n, err = parseVmess(rest)
	case "vless":
		n, err = parseVless(rest)
	case "trojan":
		n, err = parseTrojan(rest)
	case "hysteria2", "hy2":
		n, err = parseHysteria2(rest)
	case "http", "https":
		n, err = parseHTTPLike(rest, scheme)
	case "socks", "socks5":
		n, err = parseHTTPLike(rest, scheme)
	default:
		return Node{}, fmt.Errorf("不支持的协议: %q", scheme)
	}
	if err != nil {
		return Node{}, err
	}
	n.Raw = trimmed
	if n.Protocol == "" {
		n.Protocol = scheme
	}
	if err := n.validate(); err != nil {
		return Node{}, err
	}
	if n.Name == "" {
		n.Name = fmt.Sprintf("%s-%s:%d", n.Protocol, n.Server, n.Port)
	}
	return n, nil
}

// ParseSubscription 解析一份订阅内容（每行一个 URI）。
//
// 返回值刻意是"部分成功"语义：
//   - 解析成功的节点照常返回；
//   - 失败的行以 error 列表形式一并返回，**不**因为一行坏掉就整体失败。
//
// 理由：真实订阅里出现一两行厂商私有格式是常态。整体失败会让用户
// 完全无法使用订阅，而逐行报告能让他知道到底哪几行有问题。
func ParseSubscription(content string) ([]Node, []error) {
	var (
		nodes []Node
		errs  []error
	)
	seen := make(map[string]struct{})

	for i, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		// 兼容"只有 base64 内容、没有 scheme"的订阅：整体 base64 后再递归。
		if !strings.Contains(line, "://") {
			if decoded, derr := decodeBase64(line); derr == nil && strings.Contains(decoded, "://") {
				sub, subErrs := ParseSubscription(decoded)
				for _, n := range sub {
					if _, dup := seen[n.Key()]; dup {
						continue
					}
					seen[n.Key()] = struct{}{}
					nodes = append(nodes, n)
				}
				for _, e := range subErrs {
					errs = append(errs, fmt.Errorf("第 %d 行（内嵌订阅）: %w", i+1, e))
				}
				continue
			}
		}

		n, err := ParseNodeURI(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("第 %d 行: %w", i+1, err))
			continue
		}
		if _, dup := seen[n.Key()]; dup {
			continue
		}
		seen[n.Key()] = struct{}{}
		nodes = append(nodes, n)
	}
	return nodes, errs
}

// MarshalNodes 把节点序列化为 JSON。
//
// **凭据不出现在输出中**：这是给 UI 展示与跨进程传递用的。
// 需要完整凭据时走 MarshalNodesWithSecrets，那条路径只在建立连接时使用。
func MarshalNodes(nodes []Node) (string, error) {
	safe := make([]nodePublic, len(nodes))
	for i, n := range nodes {
		safe[i] = n.public()
	}
	b, err := json.Marshal(safe)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// MarshalNodesWithSecrets 序列化**含凭据**的节点。
//
// 调用方有责任不把它写进日志、不落明文磁盘、不跨到非受信进程。
func MarshalNodesWithSecrets(nodes []Node) (string, error) {
	b, err := json.Marshal(nodes)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SortNodes 按名称稳定排序，便于 UI 展示与结果可复现（测试需要确定性）。
func SortNodes(nodes []Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Name == nodes[j].Name {
			return nodes[i].Key() < nodes[j].Key()
		}
		return nodes[i].Name < nodes[j].Name
	})
}

// ---------------------------------------------------------------------------
// 各协议解析
// ---------------------------------------------------------------------------

// ss:// 有两种历史格式，必须都支持，否则会有一批老订阅解析不出来。
//
//	SIP002: ss://<base64url(method:password)>@host:port/?plugin=...#tag
//	旧格式:  ss://<base64(method:password@host:port)>#tag
//	明文:    ss://method:password@host:port#tag   （userinfo 可能被 percent-encode）
//
// secretscan:allow 上面是格式说明，不是真实凭据
func parseSS(rest string) (Node, error) {
	body, name := splitFragment(rest)

	// 去掉 SIP002 的插件查询串
	if i := strings.IndexByte(body, '?'); i >= 0 {
		body = body[:i]
	}

	var userinfo, hostport string
	if at := strings.LastIndexByte(body, '@'); at >= 0 {
		// base64 字母表不含 '@'，因此最后一个 '@' 一定是分隔符
		userinfo = body[:at]
		hostport = body[at+1:]
	} else {
		decoded, err := decodeBase64(body)
		if err != nil {
			return Node{}, fmt.Errorf("ss: 无法解码: %w", err)
		}
		at := strings.LastIndexByte(decoded, '@')
		if at < 0 {
			return Node{}, fmt.Errorf("ss: 解码后缺少 '@' 分隔符")
		}
		userinfo = decoded[:at]
		hostport = decoded[at+1:]
	}

	// userinfo 可能是 base64(method:password)，也可能是明文 method:password
	cred := userinfo
	if !strings.Contains(cred, ":") {
		if decoded, err := decodeBase64(cred); err == nil {
			cred = decoded
		}
	}
	method, password, ok := strings.Cut(cred, ":")
	if !ok {
		return Node{}, fmt.Errorf("ss: 凭据格式应为 method:password")
	}
	if dec, err := url.QueryUnescape(password); err == nil {
		password = dec
	}

	server, port, err := splitHostPort(hostport)
	if err != nil {
		return Node{}, fmt.Errorf("ss: %w", err)
	}
	return Node{
		Name:     name,
		Protocol: "ss",
		Server:   server,
		Port:     port,
		Password: password,
		Cipher:   strings.TrimSpace(method),
		Network:  "tcp",
	}, nil
}

// ssr:// 格式：
//
//	ssr://<base64url(host:port:protocol:method:obfs:base64url(password)/?obfsparam=&protoparam=&remarks=)>
func parseSSR(rest string) (Node, error) {
	decoded, err := decodeBase64(rest)
	if err != nil {
		return Node{}, fmt.Errorf("ssr: 无法解码: %w", err)
	}
	// 参数部分在 '?' 之后
	main, query, _ := strings.Cut(decoded, "/?")
	if query == "" {
		main, query, _ = strings.Cut(decoded, "?")
	}
	parts := strings.Split(main, ":")
	if len(parts) < 6 {
		return Node{}, fmt.Errorf("ssr: 字段数不足（需要 6 个，实际 %d）", len(parts))
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return Node{}, fmt.Errorf("ssr: 端口非法: %q", parts[1])
	}
	password, err := decodeBase64(parts[5])
	if err != nil {
		return Node{}, fmt.Errorf("ssr: 密码无法解码: %w", err)
	}

	q, _ := url.ParseQuery(query)
	remarks, _ := decodeBase64(q.Get("remarks"))
	obfsParam, _ := decodeBase64(q.Get("obfsparam"))
	protoParam, _ := decodeBase64(q.Get("protoparam"))

	return Node{
		Name:          remarks,
		Protocol:      "ssr",
		Server:        parts[0],
		Port:          port,
		Password:      password,
		Cipher:        parts[3],
		Obfs:          parts[4],
		SSRProtocol:   parts[2],
		SSRObfsParam:  obfsParam,
		SSRProtoParam: protoParam,
		Network:       "tcp",
	}, nil
}

// vmess:// 是 base64(JSON)。字段名沿用 v2rayN 的短名，这是事实标准。
func parseVmess(rest string) (Node, error) {
	body, nameFromFrag := splitFragment(rest)
	decoded, err := decodeBase64(body)
	if err != nil {
		return Node{}, fmt.Errorf("vmess: 无法解码 base64(JSON): %w", err)
	}
	var v struct {
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port any    `json:"port"`
		ID   string `json:"id"`
		Aid  any    `json:"aid"`
		Scy  string `json:"scy"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
	}
	if err := json.Unmarshal([]byte(decoded), &v); err != nil {
		return Node{}, fmt.Errorf("vmess: JSON 非法: %w", err)
	}
	// port / aid 在不同客户端里有时是字符串有时是数字，必须两种都收。
	port := toInt(v.Port)
	if port == 0 {
		return Node{}, fmt.Errorf("vmess: 端口缺失或为 0")
	}
	if strings.TrimSpace(v.Add) == "" {
		return Node{}, fmt.Errorf("vmess: 服务器地址缺失")
	}
	if strings.TrimSpace(v.ID) == "" {
		return Node{}, fmt.Errorf("vmess: UUID 缺失")
	}

	name := v.PS
	if name == "" {
		name = nameFromFrag
	}
	net := v.Net
	if net == "" {
		net = "tcp"
	}
	return Node{
		Name:     name,
		Protocol: "vmess",
		Server:   v.Add,
		Port:     port,
		UUID:     v.ID,
		AlterID:  toInt(v.Aid),
		Cipher:   v.Scy,
		Network:  net,
		TLS:      v.TLS == "tls" || v.TLS == "true" || v.TLS == "1",
		SNI:      v.SNI,
		Host:     v.Host,
		Path:     v.Path,
	}, nil
}

// vless://uuid@host:port?encryption=none&security=tls&type=ws&path=/&host=#name
func parseVless(rest string) (Node, error) {
	u, err := url.Parse("vless://" + rest)
	if err != nil {
		return Node{}, fmt.Errorf("vless: URI 非法: %w", err)
	}
	if u.User == nil || u.User.Username() == "" {
		return Node{}, fmt.Errorf("vless: 缺少 UUID")
	}
	server, port, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, fmt.Errorf("vless: %w", err)
	}
	q := u.Query()
	security := q.Get("security")
	net := q.Get("type")
	if net == "" {
		net = "tcp"
	}
	return Node{
		Name:     fragmentName(u),
		Protocol: "vless",
		Server:   server,
		Port:     port,
		UUID:     u.User.Username(),
		Cipher:   firstNonEmpty(q.Get("encryption"), "none"),
		Network:  net,
		TLS:      security == "tls" || security == "reality" || security == "xtls",
		SNI:      firstNonEmpty(q.Get("sni"), q.Get("peer")),
		Host:     q.Get("host"),
		Path:     q.Get("path"),
		Flow:     q.Get("flow"),
	}, nil
}

// trojan://password@host:port?sni=&type=&security=#name
func parseTrojan(rest string) (Node, error) {
	u, err := url.Parse("trojan://" + rest)
	if err != nil {
		return Node{}, fmt.Errorf("trojan: URI 非法: %w", err)
	}
	if u.User == nil || u.User.Username() == "" {
		return Node{}, fmt.Errorf("trojan: 缺少密码")
	}
	server, port, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, fmt.Errorf("trojan: %w", err)
	}
	q := u.Query()
	net := q.Get("type")
	if net == "" {
		net = "tcp"
	}
	// trojan 默认走 TLS。security=none 才是不加密（少见，但存在）。
	security := q.Get("security")
	return Node{
		Name:     fragmentName(u),
		Protocol: "trojan",
		Server:   server,
		Port:     port,
		Password: u.User.Username(),
		Network:  net,
		TLS:      security != "none",
		SNI:      firstNonEmpty(q.Get("sni"), q.Get("peer")),
		Host:     q.Get("host"),
		Path:     q.Get("path"),
		Obfs:     q.Get("obfs"),
	}, nil
}

// hysteria2://auth@host:port?sni=&insecure=1&obfs=salamander&obfs-password=#name
// 也接受 hy2:// 别名。
func parseHysteria2(rest string) (Node, error) {
	u, err := url.Parse("hysteria2://" + rest)
	if err != nil {
		return Node{}, fmt.Errorf("hysteria2: URI 非法: %w", err)
	}
	server, port, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, fmt.Errorf("hysteria2: %w", err)
	}
	q := u.Query()
	auth := ""
	if u.User != nil {
		auth = u.User.Username()
		if pw, ok := u.User.Password(); ok && pw != "" {
			auth = auth + ":" + pw
		}
	}
	// hysteria2 基于 QUIC，必然走 TLS
	return Node{
		Name:         fragmentName(u),
		Protocol:     "hysteria2",
		Server:       server,
		Port:         port,
		Password:     auth,
		Network:      "quic",
		TLS:          true,
		SNI:          firstNonEmpty(q.Get("sni"), q.Get("peer")),
		Obfs:         q.Get("obfs"),
		ObfsPassword: q.Get("obfs-password"),
		Insecure:     q.Get("insecure") == "1" || q.Get("insecure") == "true",
	}, nil
}

// socks5://user:pass@host:port  /  http://user:pass@host:port  /  https://...
func parseHTTPLike(rest, scheme string) (Node, error) {
	u, err := url.Parse(scheme + "://" + rest)
	if err != nil {
		return Node{}, fmt.Errorf("%s: URI 非法: %w", scheme, err)
	}
	server, port, err := splitHostPort(u.Host)
	if err != nil {
		return Node{}, fmt.Errorf("%s: %w", scheme, err)
	}
	user, password := "", ""
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}
	// 无密码时把用户名放进 Password 复用（这两个协议只支持 user:pass 一种凭据形态）
	if user != "" && password == "" {
		password = user
		user = ""
	}
	proto := "http"
	if scheme == "socks" || scheme == "socks5" {
		proto = "socks5"
	}
	return Node{
		Name:     fragmentName(u),
		Protocol: proto,
		Server:   server,
		Port:     port,
		UUID:     user,
		Password: password,
		Network:  "tcp",
		TLS:      scheme == "https",
	}, nil
}

// ---------------------------------------------------------------------------
// 工具
// ---------------------------------------------------------------------------

func (n Node) validate() error {
	if strings.TrimSpace(n.Server) == "" {
		return fmt.Errorf("服务器地址为空")
	}
	if n.Port <= 0 || n.Port > 65535 {
		return fmt.Errorf("端口越界: %d", n.Port)
	}
	switch n.Protocol {
	case "vmess", "vless":
		if n.UUID == "" {
			return fmt.Errorf("%s: UUID 为空", n.Protocol)
		}
	case "ss", "ssr", "trojan", "hysteria2":
		if n.Password == "" {
			return fmt.Errorf("%s: 密码为空", n.Protocol)
		}
	}
	return nil
}

func splitScheme(uri string) (scheme, rest string, ok bool) {
	i := strings.Index(uri, "://")
	if i <= 0 {
		return "", "", false
	}
	return strings.ToLower(uri[:i]), uri[i+3:], true
}

// splitFragment 把 "...#name" 拆开，并对 name 做 percent-decode。
func splitFragment(s string) (body, name string) {
	body, frag, found := strings.Cut(s, "#")
	if !found {
		return s, ""
	}
	if dec, err := url.QueryUnescape(frag); err == nil {
		frag = dec
	}
	return body, strings.TrimSpace(frag)
}

func fragmentName(u *url.URL) string {
	if u.Fragment == "" {
		return ""
	}
	if dec, err := url.QueryUnescape(u.Fragment); err == nil {
		return strings.TrimSpace(dec)
	}
	return strings.TrimSpace(u.Fragment)
}

// splitHostPort 支持 IPv6 字面量（形如 [::1]:443）。
func splitHostPort(hostport string) (string, int, error) {
	hostport = strings.TrimSpace(hostport)
	hostport = strings.TrimSuffix(hostport, "/")
	if hostport == "" {
		return "", 0, fmt.Errorf("地址为空")
	}

	host, portStr := "", ""
	bracketed := false
	if strings.HasPrefix(hostport, "[") {
		bracketed = true
		end := strings.LastIndexByte(hostport, ']')
		if end < 0 {
			return "", 0, fmt.Errorf("IPv6 地址缺少 ']': %q", hostport)
		}
		host = hostport[1:end]
		rest := hostport[end+1:]
		if !strings.HasPrefix(rest, ":") {
			return "", 0, fmt.Errorf("IPv6 地址缺少端口: %q", hostport)
		}
		portStr = rest[1:]
	} else {
		idx := strings.LastIndexByte(hostport, ':')
		if idx < 0 {
			return "", 0, fmt.Errorf("缺少端口: %q", hostport)
		}
		host = hostport[:idx]
		portStr = hostport[idx+1:]
	}

	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil {
		return "", 0, fmt.Errorf("端口非数字: %q", portStr)
	}
	if port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("端口越界: %d", port)
	}
	if strings.TrimSpace(host) == "" {
		return "", 0, fmt.Errorf("主机名为空")
	}
	// 未加方括号的 IPv6 是**歧义**的：'2001:db8::1:8388' 无法判断端口从哪里开始。
	// 与其猜，不如拒绝并让用户改对 —— 猜错的后果是连到一个错误的地址。
	// 已加方括号的情况不能走这条判断：括号内本来就该含 ':'。
	if !bracketed && strings.Contains(host, ":") {
		return "", 0, fmt.Errorf("IPv6 地址必须用方括号包裹: %q", hostport)
	}
	return host, port, nil
}

// decodeBase64 依次尝试 URL-safe 与标准字母表、有无 padding 共 4 种组合。
//
// 为什么必须四种都试：不同客户端生成的订阅里 base64 变体混用是常态，
// 只支持一种会让"看起来格式正确"的订阅解析失败，而且失败信息很难理解。
func decodeBase64(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("空 base64")
	}
	// URL 查询串里 '+' 常被编码成空格，先还原
	s = strings.ReplaceAll(s, " ", "+")
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("不是合法的 base64: %q", truncate(s, 16))
}

func toInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0
		}
		return n
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return 0
		}
		return int(n)
	}
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
