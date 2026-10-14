package script

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// 基础执行：$done 值回传、console 日志、$argument/$script/$environment。
func TestRun_Basics(t *testing.T) {
	src := `
console.log('hi', 1, true);
$notify('标题', '内容');
$done($argument + '-' + $environment.suffix + '-' + $script.name);
`
	var notified []string
	v, logs, err := Run(src, Options{
		Argument: "A",
		Script:   "测试脚本",
		Env:      map[string]string{"suffix": "S"},
		Notify:   func(title, content string) { notified = append(notified, title+"|"+content) },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if v != "A-S-测试脚本" {
		t.Fatalf("结果 = %v", v)
	}
	if len(logs) == 0 || logs[0] != "hi 1 true" {
		t.Fatalf("logs = %v", logs)
	}
	if len(notified) != 1 || notified[0] != "标题|内容" {
		t.Fatalf("notify = %v", notified)
	}
}

// $done 未调用时取脚本返回值；都没有则 nil。
func TestRun_NoDone(t *testing.T) {
	v, _, err := Run("42", Options{})
	if err != nil || v != int64(42) { // goja 数字导出为 int64
		t.Fatalf("返回值路径: %v(%T) %v", v, v, err)
	}
	v2, _, _ := Run("var x = 1;", Options{})
	if v2 != nil {
		t.Fatalf("无返回应 nil: %v", v2)
	}
}

// 超时：死循环被打断，错误信息带"超时"。
func TestRun_Timeout(t *testing.T) {
	start := time.Now()
	_, _, err := Run("while(true){}", Options{Timeout: 300 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("应超时: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("超时未生效，耗时 %v", time.Since(start))
	}
}

// 沙箱：无文件系统/网络全局；异常带位置信息。
func TestRun_Sandbox(t *testing.T) {
	_, _, err := Run("require('fs')", Options{})
	if err == nil || !strings.Contains(err.Error(), "ReferenceError") {
		t.Fatalf("require 应未定义: %v", err)
	}
	_, _, err = Run(`throw new Error("boom")`, Options{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("异常应透出: %v", err)
	}
}

// $httpClient：有 Transport 时可用；无 Transport 时抛"未授权网络"。
func TestRun_HttpClient(t *testing.T) {
	called := ""
	src := `
$httpClient.get({url: 'https://x.example/a', headers: {h: 'v'}}, function(err, resp) {
	if (err) { $done('ERR:' + err); return; }
	$done(resp.status + ':' + resp.body + ':' + resp.headers['x-tag']);
});
`
	v, _, err := Run(src, Options{
		Transport: func(method, url string, headers map[string]string, body string) (int, map[string]string, string, error) {
			called = method + " " + url + " " + headers["h"]
			return 200, map[string]string{"x-tag": "T"}, "内容", nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called != "GET https://x.example/a v" {
		t.Fatalf("transport 收到: %q", called)
	}
	if v != "200:内容:T" {
		t.Fatalf("resp = %v", v)
	}

	// 无 Transport：同步抛"沙箱未授权网络"，脚本用 try/catch 感知。
	v3, _, err3 := Run(`
try { $httpClient.get({url:'http://x'}, function(e){}); $done('ok'); }
catch (e) { $done(String(e).indexOf('未授权') >= 0 ? 'denied' : 'other:' + e); }
`, Options{})
	if err3 != nil {
		t.Fatalf("deny: %v", err3)
	}
	if v3 != "denied" {
		t.Fatalf("deny 结果 = %v", v3)
	}
}

// $persistentStore：内存后端 + 注入后端两种路径。
func TestRun_PersistentStore(t *testing.T) {
	src := `$persistentStore.put('k','v'); $done($persistentStore.get('k'));`
	v, _, err := Run(src, Options{})
	if err != nil || v != "v" {
		t.Fatalf("mem store: %v %v", v, err)
	}
	custom := mapStore{}
	v2, _, _ := Run(src, Options{Store: &custom})
	if v2 != "v" || custom["k"] != "v" {
		t.Fatalf("custom store: %v %v", v2, custom)
	}
}

type mapStore map[string]string

func (m *mapStore) Get(k string) (string, bool) { v, ok := (*m)[k]; return v, ok }
func (m *mapStore) Put(k, v string)             { (*m)[k] = v }

// RunHTTP：改写 body、reject、headers、status。
func TestRunHTTP(t *testing.T) {
	// 改写 body。
	res, _, err := RunHTTP(
		`$done({body: $response.body.replace('bad','good'), headers: {'x-a':'1'}});`,
		&Request{Method: "GET", URL: "https://x/", Hostname: "x"},
		&Response{Status: 200, Body: "bad content"},
		Options{})
	if err != nil {
		t.Fatalf("RunHTTP: %v", err)
	}
	if res.Reject || res.Body == nil || *res.Body != "good content" || res.Headers["x-a"] != "1" {
		t.Fatalf("改写结果: %+v", res)
	}

	// reject。
	res2, _, _ := RunHTTP(`$done({reject: true});`, &Request{}, nil, Options{})
	if !res2.Reject {
		t.Fatalf("reject: %+v", res2)
	}

	// 无参放行。
	res3, _, _ := RunHTTP(`$done();`, &Request{}, &Response{}, Options{})
	if res3.Reject || res3.Body != nil {
		t.Fatalf("放行: %+v", res3)
	}
}

// Match：方法/域名/URL 正则/状态码窗口。
func TestMatch(t *testing.T) {
	req := &Request{Method: "GET", URL: "https://a.example.com/api/list", Hostname: "a.example.com"}
	resp := &Response{Status: 404}

	cases := []struct {
		name string
		m    Match
		want bool
	}{
		{"空条件", Match{}, true},
		{"方法命中", Match{Method: "get"}, true},   // 大小写不敏感
		{"方法不中", Match{Method: "POST"}, false}, //
		{"域名精确", Match{Domain: "a.example.com"}, true},
		{"域名子域", Match{Domain: "*.example.com"}, true},
		{"域名其它", Match{Domain: "b.example.com"}, false},
		{"URL正则", Match{URLRegex: `/api/.*list`}, true},
		{"URL正则不中", Match{URLRegex: `^https://z/`}, false},
		{"状态码窗口", Match{MinStatus: 400, MaxStatus: 499}, true},
		{"状态码窗口外", Match{MinStatus: 500}, false},
		{"组合全过", Match{Method: "GET", Domain: "*.example.com", URLRegex: "list"}, true},
		{"组合一票否决", Match{Method: "GET", Domain: "b.com"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.Matches(req, resp); got != tc.want {
				t.Fatalf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

// 脚本类型合法性。
func TestTypeValid(t *testing.T) {
	for _, v := range []Type{"http", "rule", "cron", "dns", "general"} {
		if !v.Valid() {
			t.Fatalf("%q 应合法", v)
		}
	}
	if Type("nope").Valid() {
		t.Fatal("未知类型应非法")
	}
}

// 超时错误是可识别的哨兵（调用方据此区分业务错误）。
func TestRun_TimeoutSentinel(t *testing.T) {
	_, _, err := Run("while(true){}", Options{Timeout: 100 * time.Millisecond})
	var sentinel *timeoutError
	if !errors.As(err, &sentinel) {
		t.Fatalf("应为 timeoutError: %T %v", err, err)
	}
}
