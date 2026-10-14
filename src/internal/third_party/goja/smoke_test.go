// Package goja_test 是 goja 移植的冒烟测试：
// 验证 vendored 引擎在零第三方依赖模块内真实可用。
package goja_test

import (
	"strings"
	"testing"

	"github.com/vip-svip/syfuse/src/internal/third_party/goja"
)

// 基础求值：算术、字符串、JSON、正则（打到 vendored regexp2）。
func TestGojaSmoke(t *testing.T) {
	vm := goja.New()
	cases := []struct{ name, script, want string }{
		{"算术", "1 + 2 * 3", "7"},
		{"字符串", "'a' + 'b'.toUpperCase()", "aB"},
		{"模板串", "`x=${1+1}`", "x=2"},
		{"JSON", "JSON.parse('{\"a\":1}').a + 1", "2"},
		{"正则", "'foobar'.replace(/foo/, 'baz')", "bazbar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := vm.RunString(tc.script)
			if err != nil {
				t.Fatalf("RunString(%q): %v", tc.script, err)
			}
			if got := v.String(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// 脚本系统沙箱语义的前置验证：
//   - 无文件系统/网络等全局（eval 中引用应抛 ReferenceError）；
//   - 运行时中断可用（5s 超时的实现基础）；
//   - $done 回调式 API 可用 Go 函数注入。
func TestGojaSandboxPrereq(t *testing.T) {
	vm := goja.New()

	// 1) 沙箱没有这些全局。
	_, err := vm.RunString("require('fs')")
	if err == nil || !strings.Contains(err.Error(), "ReferenceError") {
		t.Fatalf("require 应未定义，得到: %v", err)
	}

	// 2) 中断。
	vm.Interrupt("timeout")
	if _, err := vm.RunString("while(true){}"); err == nil {
		t.Fatal("死循环应被中断")
	}
	vm = goja.New() // 中断后不能复用，换新 VM

	// 3) $done 注入。
	var doneArg string
	vm.Set("$done", func(arg string) { doneArg = arg })
	if _, err := vm.RunString("$done('ok')"); err != nil {
		t.Fatalf("$done: %v", err)
	}
	if doneArg != "ok" {
		t.Fatalf("doneArg = %q", doneArg)
	}

	// 4) $persistentStore 语义演示（get/put 内存键值）。
	store := map[string]string{}
	vm.Set("$persistentStore", map[string]any{
		"put": func(key, val string) bool { store[key] = val; return true },
		"get": func(key string) any { return store[key] },
	})
	if _, err := vm.RunString("$persistentStore.put('k','v') && $persistentStore.get('k') === 'v'"); err != nil {
		t.Fatalf("persistentStore: %v", err)
	}
	if store["k"] != "v" {
		t.Fatalf("store = %v", store)
	}
}

// 编译错误定位（脚本编辑器"错误定位"功能的基础）。
func TestGojaSyntaxError(t *testing.T) {
	vm := goja.New()
	_, err := vm.RunString("function f( {")
	if err == nil {
		t.Fatal("语法错误应返回 err")
	}
	var se *goja.Exception
	if !errorsAs(err, &se) {
		t.Fatalf("应为 *goja.Exception，得到 %T", err)
	}
	if !strings.Contains(err.Error(), "SyntaxError") {
		t.Fatalf("错误应含 SyntaxError: %v", err)
	}
}

// errorsAs 小工具（避免只为一个断言 import errors 就全文件改名）。
func errorsAs(err error, target **goja.Exception) bool {
	for err != nil {
		if e, ok := err.(*goja.Exception); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
