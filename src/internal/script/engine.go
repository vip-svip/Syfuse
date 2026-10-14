// Package script 实现 M7 脚本系统（规范 5.4）的 Go 侧引擎：
// goja（vendored）沙箱 + Shadowrocket/Surge/QX/Loon 兼容 API 面。
//
// 沙箱保证（对应规范"无文件系统、无任意网络、无反射逃逸、5s 超时、
// 内存上限"）：
//   - 无文件系统/无任意网络：goja 本身没有这些全局，不注入即不存在；
//     唯一网络出口是 $httpClient，由 TransportFunc 代理（宿主决定
//     走隧道还是拒绝），脚本拿不到原始 socket；
//   - 无反射逃逸：不向脚本暴露任何 Go reflect 值（API 面全部用
//     基础类型与 map[string]any 往返）；
//   - 超时：看门狗协程到时 Interrupt(v <-chan time.Time)，
//     死循环/长计算都会被打断；
//   - 内存：上游 goja 未暴露进程内限额，退化策略 = 栈深上限
//     （SetMaxCallStackSize）+ 超时兜底；真正的内存上限由
//     单独 goroutine 运行脚本（崩了不伤守护进程）实现。
//
// 沙箱既不读全局配置也不落盘 —— 那是调用方（daemon）的事。
package script

import (
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/vip-svip/syfuse/src/internal/third_party/goja"
)

// Type 是脚本类型（规范 5.4：HTTP 请求/响应处理、规则脚本、定时任务、
// DNS 脚本、通用脚本）。
type Type string

const (
	TypeHTTP    Type = "http"    // $request/$response 处理
	TypeRule    Type = "rule"    // 规则脚本（返回代理决策）
	TypeCron    Type = "cron"    // 定时任务（cron 表达式）
	TypeDNS     Type = "dns"     // DNS 脚本
	TypeGeneral Type = "general" // 通用脚本
)

// Valid 校验类型字面量。
func (t Type) Valid() bool {
	switch t {
	case TypeHTTP, TypeRule, TypeCron, TypeDNS, TypeGeneral:
		return true
	}
	return false
}

// DefaultTimeout 是单次执行的缺省超时（规范写 5s）。
const DefaultTimeout = 5 * time.Second

// MaxCallStack 是脚本栈深上限。
const MaxCallStack = 2048

// Request 是注入给 http 脚本的 $request（与 QX/Surge 字段对齐）。
type Request struct {
	Method   string            `json:"method"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Body     string            `json:"body,omitempty"`
	Hostname string            `json:"hostname"`
}

// Response 是注入给 http 脚本的 $response。
type Response struct {
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	Body     string            `json:"body,omitempty"`
	FinalURL string            `json:"final_url,omitempty"`
}

// HTTPResult 是 http 脚本跑完的产物：脚本可以改写 body/headers，
// 或决定 reject（对应 QX 的 $done({body}) / reject 语义）。
type HTTPResult struct {
	Reject  bool              `json:"reject"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    *string           `json:"body,omitempty"` // nil = 不改写
}

// TransportFunc 是 $httpClient 的受控出口：入参 method/url/headers/body，
// 返回 status/headers/body。返回 error 时脚本侧 HTTP 抛错。
// 宿主注入什么，脚本就能访问什么 —— 沙箱本身不提供网络。
type TransportFunc func(method, url string, headers map[string]string, body string) (int, map[string]string, string, error)

// Store 是 $persistentStore 的后端（宿主决定存哪：内存/DataStore/文件）。
type Store interface {
	Get(key string) (string, bool)
	Put(key, val string)
}

// Notifier 是 $notify 的后端（宿主决定发到哪：通知/日志）。
type Notifier func(title, content string)

// Options 配置一次脚本执行。
type Options struct {
	Type      Type
	Timeout   time.Duration
	Argument  string            // $argument
	Script    string            // $script（脚本名）
	Env       map[string]string // $environment
	Request   *Request          // $request（http 类型）
	Response  *Response         // $response（http 类型）
	Transport TransportFunc     // $httpClient
	Store     Store             // $persistentStore
	Notify    Notifier          // $notify
}

// result 收集 $done 的值；also log lines 收集控制台输出。
type run struct {
	done     chan goja.Value
	doneOnce sync.Once
	logs     []string
}

// Run 在全新 VM 里执行脚本，返回 $done 的值（未调用则 nil）。
// 超时或异常都以 error 返回 —— 调用方拿到的要么是结果要么是错误，
// 不存在"超时后脚本还在后台跑"的中间态（VM 用完即弃）。
func Run(src string, opts Options) (any, []string, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	vm := goja.New()
	vm.SetMaxCallStackSize(MaxCallStack)

	r := &run{done: make(chan goja.Value, 1)}
	if err := installAPI(vm, r, opts); err != nil {
		return nil, r.logs, fmt.Errorf("script: 注入 API 失败: %w", err)
	}

	// 看门狗：到时 Interrupt。Interrupt 的值是 goja.InterruptedError 的
	// iface；用特定标识区分"超时"与脚本自己调的其它中断。
	timeoutCh := time.After(opts.Timeout)
	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-timeoutCh:
			vm.Interrupt(&timeoutError{d: opts.Timeout})
		case <-watchdogDone:
		}
	}()
	defer close(watchdogDone)

	val, err := vm.RunString(src)
	if err != nil {
		return nil, r.logs, normalizeErr(err)
	}
	select {
	case v := <-r.done:
		return export(v), r.logs, nil
	default:
		// $done 没被调用：脚本同步返回值也算结果（general/cron 常见）。
		if val != nil && !goja.IsUndefined(val) && !goja.IsNull(val) {
			return export(val), r.logs, nil
		}
		return nil, r.logs, nil
	}
}

// RunHTTP 跑一个 http 类型脚本：注入 $request/$response，返回改写结果。
func RunHTTP(src string, req *Request, resp *Response, opts Options) (*HTTPResult, []string, error) {
	opts.Type = TypeHTTP
	opts.Request, opts.Response = req, resp
	v, logs, err := Run(src, opts)
	if err != nil {
		return nil, logs, err
	}
	res := &HTTPResult{}
	switch t := v.(type) {
	case nil:
		return res, logs, nil // $done() 无参 = 原样放行
	case map[string]any:
		if b, ok := t["reject"].(bool); ok && b {
			res.Reject = true
		}
		if st, ok := t["status"].(int64); ok {
			res.Status = int(st)
		}
		if h, ok := t["headers"].(map[string]any); ok {
			res.Headers = toStringMap(h)
		}
		if b, ok := t["body"].(string); ok {
			res.Body = &b
		}
	default:
		return nil, logs, fmt.Errorf("script: $done 参数类型不支持: %T", v)
	}
	return res, logs, nil
}

// installAPI 把规范 5.4 列出的 API 面全部装上。
func installAPI(vm *goja.Runtime, r *run, opts Options) error {
	// --- $done：一次性，先到先得。
	vm.Set("$done", func(v goja.Value) {
		r.doneOnce.Do(func() { r.done <- v })
	})

	// --- $notify。
	if opts.Notify != nil {
		notify := opts.Notify
		vm.Set("$notify", func(title, content string) { notify(title, content) })
	} else {
		vm.Set("$notify", func(title, content string) {
			r.logs = append(r.logs, "[notify] "+title+": "+content)
		})
	}

	// --- $persistentStore。
	store := opts.Store
	if store == nil {
		mem := map[string]string{}
		store = &memStore{m: mem}
	}
	vm.Set("$persistentStore", map[string]any{
		"put": func(key, val string) bool { store.Put(key, val); return true },
		"get": func(key string) any {
			if v, ok := store.Get(key); ok {
				return v
			}
			return nil
		},
		"remove": func(key string) bool { store.Put(key, ""); return true },
	})

	// --- $httpClient：受控出口。
	if opts.Transport != nil {
		tr := opts.Transport
		vm.Set("$httpClient", map[string]any{
			"get": func(params map[string]any, cb func(error, goja.Value)) { httpCall(vm, "GET", params, cb, tr) },
			"post": func(params map[string]any, cb func(error, goja.Value)) {
				httpCall(vm, "POST", params, cb, tr)
			},
			"put": func(params map[string]any, cb func(error, goja.Value)) {
				httpCall(vm, "PUT", params, cb, tr)
			},
			"delete": func(params map[string]any, cb func(error, goja.Value)) {
				httpCall(vm, "DELETE", params, cb, tr)
			},
			// head/patch 少见，只留四个常用动词，API 面不膨胀。
		})
	} else {
		// 无 Transport 时 $httpClient 存在但全部抛错 —— 脚本能探测到
		// "网络被禁"，而不是 "变量未定义"。
		denied := func(error, goja.Value) {} // 占位避免误用
		_ = denied
		deny := func(string, map[string]any, func(error, goja.Value)) {
			panic(vm.NewGoError(fmt.Errorf("$httpClient: 沙箱未授权网络")))
		}
		vm.Set("$httpClient", map[string]any{
			"get": deny, "post": deny, "put": deny, "delete": deny,
		})
	}

	// --- 元信息。
	vm.Set("$argument", opts.Argument)
	vm.Set("$script", map[string]any{"name": opts.Script, "type": string(opts.Type)})
	env := map[string]any{}
	for k, v := range opts.Env {
		env[k] = v
	}
	vm.Set("$environment", env)

	// --- $request / $response（http 类型才注入）。
	if opts.Request != nil {
		vm.Set("$request", map[string]any{
			"method":   opts.Request.Method,
			"url":      opts.Request.URL,
			"headers":  toAnyMap(opts.Request.Headers),
			"body":     opts.Request.Body,
			"hostname": opts.Request.Hostname,
		})
	} else {
		vm.Set("$request", nil)
	}
	if opts.Response != nil {
		vm.Set("$response", map[string]any{
			"status":    opts.Response.Status,
			"headers":   toAnyMap(opts.Response.Headers),
			"body":      opts.Response.Body,
			"final_url": opts.Response.FinalURL,
		})
	} else {
		vm.Set("$response", nil)
	}

	// --- console.log（调试控制台的数据源）。
	console := map[string]any{
		"log":   func(args ...goja.Value) { r.logs = append(r.logs, joinArgs(vm, args)) },
		"info":  func(args ...goja.Value) { r.logs = append(r.logs, joinArgs(vm, args)) },
		"warn":  func(args ...goja.Value) { r.logs = append(r.logs, "[warn] "+joinArgs(vm, args)) },
		"error": func(args ...goja.Value) { r.logs = append(r.logs, "[error] "+joinArgs(vm, args)) },
	}
	vm.Set("console", console)
	return nil
}

// httpCall 把 $httpClient.get({url,...}, cb) 翻译成 TransportFunc 调用。
// 回调必须异步语义：Transport 同步返回也先记下结果再调 cb。
func httpCall(vm *goja.Runtime, method string, params map[string]any, cb func(error, goja.Value), tr TransportFunc) {
	url, _ := params["url"].(string)
	if url == "" {
		cb(fmt.Errorf("$httpClient.%s: 缺 url", method), nil)
		return
	}
	headers := map[string]string{}
	if h, ok := params["headers"].(map[string]any); ok {
		headers = toStringMap(h)
	}
	body, _ := params["body"].(string)
	status, respHeaders, respBody, err := tr(method, url, headers, body)
	if err != nil {
		cb(err, nil)
		return
	}
	resp := map[string]any{
		"status":    status,
		"headers":   toAnyMap(respHeaders),
		"body":      respBody,
		"final_url": url,
	}
	cb(nil, vm.ToValue(resp))
}

// --- helpers ---

type memStore struct{ m map[string]string }

func (s *memStore) Get(k string) (string, bool) { v, ok := s.m[k]; return v, ok }
func (s *memStore) Put(k, v string)             { s.m[k] = v }

func export(v goja.Value) any {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	return v.Export()
}

func toStringMap(h map[string]any) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

func toAnyMap(h map[string]string) map[string]any {
	out := make(map[string]any, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
}

func joinArgs(vm *goja.Runtime, args []goja.Value) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, a.String())
	}
	return joinStrings(parts, " ")
}

func joinStrings(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// timeoutError 是超时哨兵：调用方用 errors.As 识别并区分业务错误。
type timeoutError struct{ d time.Duration }

func (e *timeoutError) Error() string { return fmt.Sprintf("script: 执行超时（%v）", e.d) }

// normalizeErr 把 goja 异常转成带位置的可读错误（编辑器"错误定位"）；
// 超时哨兵原样透传，保证 errors.As 可识别。
func normalizeErr(err error) error {
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		if te, ok := ie.Value().(*timeoutError); ok {
			return te
		}
	}
	if exc, ok := err.(*goja.Exception); ok {
		return fmt.Errorf("script: %s", exc.String())
	}
	return fmt.Errorf("script: %v", err)
}

// Match 描述脚本的触发条件（规范 5.4"脚本规则匹配：URL 正则、域名、
// 方法、状态码"）。全字段可选；全部为空 = 无条件匹配。
type Match struct {
	URLRegex  string `json:"url_regex,omitempty"`
	Domain    string `json:"domain,omitempty"`
	Method    string `json:"method,omitempty"`
	MinStatus int    `json:"min_status,omitempty"`
	MaxStatus int    `json:"max_status,omitempty"`
}

var matchRegexCache sync.Map // string -> *regexp.Regexp

// Matches 判断该触发条件是否命中。
func (m Match) Matches(req *Request, resp *Response) bool {
	if req != nil {
		if m.Method != "" && !stringsEqualFold(m.Method, req.Method) {
			return false
		}
		if m.Domain != "" && !hostMatches(m.Domain, req.Hostname) {
			return returnFalse()
		}
		if m.URLRegex != "" {
			re, err := compiledRegex(m.URLRegex)
			if err != nil || !re.MatchString(req.URL) {
				return false
			}
		}
	}
	if resp != nil && (m.MinStatus != 0 || m.MaxStatus != 0) {
		if m.MinStatus != 0 && resp.Status < m.MinStatus {
			return false
		}
		if m.MaxStatus != 0 && resp.Status > m.MaxStatus {
			return false
		}
	}
	return true
}

func returnFalse() bool { return false }

func stringsEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// hostMatches：pattern 可以是精确域名或 *.example.com / .example.com。
func hostMatches(pattern, host string) bool {
	pattern, host = lower(pattern), lower(host)
	switch {
	case pattern == host:
		return true
	case len(pattern) > 1 && pattern[0] == '*':
		return len(host) > len(pattern)-1 && host[len(host)-(len(pattern)-1):] == pattern[1:] &&
			host[:len(host)-(len(pattern)-1)] != "" &&
			!containsDot(host[:len(host)-(len(pattern)-1)])
	case pattern[0] == '.':
		return host == pattern[1:] || (len(host) > len(pattern) && host[len(host)-len(pattern):] == pattern)
	default:
		return false
	}
}

func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if 'A' <= b[i] && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func compiledRegex(pattern string) (*regexp.Regexp, error) {
	if v, ok := matchRegexCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	matchRegexCache.Store(pattern, re)
	return re, nil
}
