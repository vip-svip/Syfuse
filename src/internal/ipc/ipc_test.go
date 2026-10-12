package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 平台说明（重要，别删）：
//
// 守护进程的目标平台是 **Android（Linux）**。以下两类断言在 Windows 上
// 无法成立，因此按平台跳过，并在此显式说明原因 —— 不静默跳过：
//
//  1. Unix 域套接字（AF_UNIX）：Windows 上 Go 的 net.Listen("unix", ...)
//     受路径长度与版本限制，实测 bind 报 invalid argument。
//  2. Unix 权限位（0600）：Windows 没有 POSIX 权限位，os.Chmod 是近空操作，
//     读回的 Mode().Perm() 恒为 0666。
//
// CI 在 ubuntu-latest 上运行，这两类断言在那里会真正执行。
// 本机（Windows）跳过属于**已知覆盖缺口**，已在报告里登记。
func skipOnWindows(t *testing.T, what string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skipf("跳过 %s：Windows 不支持（目标平台是 Android/Linux；CI 在 ubuntu-latest 上会执行此断言）", what)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	s, err := NewServer(filepath.Join(dir, "control.sock"), filepath.Join(dir, "api.token"))
	if err != nil {
		t.Fatalf("创建服务端失败: %v", err)
	}
	return s
}

func call(t *testing.T, s *Server, req Request) Response {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return s.Dispatch(b)
}

func authed(s *Server, method string) Request {
	return Request{ProtocolVersion: ProtocolVersion, ID: "1", Authorization: s.Token, Method: method}
}

// ---------------------------------------------------------------------------
// token
// ---------------------------------------------------------------------------

func TestLoadOrCreateToken_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "api.token")

	tok, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(tok) != TokenLen*2 {
		t.Errorf("token 长度 = %d, 期望 %d（%d 字节的十六进制）", len(tok), TokenLen*2, TokenLen)
	}
	if strings.Contains(tok, "ghp_") || strings.Contains(tok, "github_pat_") {
		t.Errorf("token 不应带任何已知前缀")
	}
	if !isHex(tok) {
		t.Errorf("token 应为十六进制字符串，实际 %q", tok)
	}

	// 二次调用必须返回同一个（不能每次生成新的，否则已连接的 App 全部失效）
	again, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if again != tok {
		t.Errorf("二次读取应返回同一个 token")
	}
}

func TestTokenFile_PermissionsAre0600(t *testing.T) {
	skipOnWindows(t, "Unix 权限位断言")
	dir := t.TempDir()
	path := filepath.Join(dir, "api.token")
	if _, err := LoadOrCreateToken(path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// 0600 是安全要求：token 泄漏等于控制通道被完全接管
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("token 文件权限 = %o, 期望 600", perm)
	}
}

func TestRotateToken_InvalidatesOld(t *testing.T) {
	s := newTestServer(t)
	old := s.Token

	newTok, err := s.RotateToken()
	if err != nil {
		t.Fatalf("轮换失败: %v", err)
	}
	if newTok == old {
		t.Fatal("轮换后 token 必须改变")
	}

	// 旧 token 必须立即失效
	bad := Request{ProtocolVersion: ProtocolVersion, ID: "1", Authorization: old, Method: "x"}
	if resp := s.Dispatch(mustJSON(t, bad)); resp.Code != "unauthorized" {
		t.Errorf("旧 token 应被拒绝，实际 %+v", resp)
	}
	// 新 token 生效
	s.Handle("x", func(json.RawMessage) (any, error) { return "ok", nil })
	if resp := call(t, s, authed(s, "x")); !resp.OK {
		t.Errorf("新 token 应可用，实际 %+v", resp)
	}
}

// ---------------------------------------------------------------------------
// 鉴权与协议
// ---------------------------------------------------------------------------

func TestDispatch_RejectsMissingToken(t *testing.T) {
	s := newTestServer(t)
	s.Handle("ping", func(json.RawMessage) (any, error) { return "pong", nil })

	resp := call(t, s, Request{ProtocolVersion: ProtocolVersion, ID: "1", Method: "ping"})
	if resp.OK {
		t.Fatal("无 token 必须被拒绝")
	}
	if resp.Code != "unauthorized" {
		t.Errorf("错误码 = %q, 期望 unauthorized", resp.Code)
	}
}

func TestDispatch_RejectsWrongToken(t *testing.T) {
	s := newTestServer(t)
	s.Handle("ping", func(json.RawMessage) (any, error) { return "pong", nil })

	resp := call(t, s, Request{ProtocolVersion: ProtocolVersion, ID: "1", Authorization: "deadbeef", Method: "ping"})
	if resp.OK || resp.Code != "unauthorized" {
		t.Errorf("错误 token 必须被拒绝，实际 %+v", resp)
	}
}

func TestDispatch_RejectsHigherProtocolVersion(t *testing.T) {
	s := newTestServer(t)
	s.Handle("ping", func(json.RawMessage) (any, error) { return "pong", nil })

	// 版本协商：客户端更新时必须拿到明确提示，而不是莫名其妙的方法不存在
	resp := call(t, s, Request{ProtocolVersion: ProtocolVersion + 1, ID: "1", Authorization: s.Token, Method: "ping"})
	if resp.Code != "version_too_new" {
		t.Errorf("错误码 = %q, 期望 version_too_new", resp.Code)
	}
}

func TestDispatch_UnknownMethod(t *testing.T) {
	s := newTestServer(t)
	resp := call(t, s, authed(s, "does.not.exist"))
	if resp.OK || resp.Code != "unknown_method" {
		t.Errorf("未知方法应返回 unknown_method，实际 %+v", resp)
	}
}

func TestDispatch_BadJSON(t *testing.T) {
	s := newTestServer(t)
	resp := s.Dispatch([]byte("{不是 JSON"))
	if resp.OK || resp.Code != "bad_request" {
		t.Errorf("非法 JSON 应返回 bad_request，实际 %+v", resp)
	}
}

func TestDispatch_Success(t *testing.T) {
	s := newTestServer(t)
	s.Handle("echo", func(p json.RawMessage) (any, error) {
		return map[string]string{"got": strings.TrimSpace(string(p))}, nil
	})

	req := authed(s, "echo")
	req.Params = json.RawMessage(`{"n":1}`)
	resp := call(t, s, req)
	if !resp.OK {
		t.Fatalf("应成功，实际 %+v", resp)
	}
	// Result 是 JSON，其中的字符串值会被转义，因此必须反序列化后比较，
	// 不能直接对原始字节做子串匹配。
	var got map[string]string
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("结果不是合法 JSON: %v (%s)", err, resp.Result)
	}
	if got["got"] != `{"n":1}` {
		t.Errorf("参数应原样回传，实际 %q", got["got"])
	}
	if resp.ID != "1" {
		t.Errorf("ID 应原样回传，实际 %q", resp.ID)
	}
}

func TestDispatch_HandlerErrorPropagates(t *testing.T) {
	s := newTestServer(t)
	s.Handle("boom", func(json.RawMessage) (any, error) {
		return nil, ErrUnauthorized // 借一个已知错误
	})
	resp := call(t, s, authed(s, "boom"))
	if resp.OK {
		t.Fatal("handler 返回错误时应 OK=false")
	}
	if resp.Code != "handler_error" {
		t.Errorf("错误码 = %q, 期望 handler_error", resp.Code)
	}
}

// ---------------------------------------------------------------------------
// 方法注册
// ---------------------------------------------------------------------------

func TestHandle_DuplicatePanics(t *testing.T) {
	s := newTestServer(t)
	s.Handle("dup", func(json.RawMessage) (any, error) { return nil, nil })

	defer func() {
		if recover() == nil {
			t.Error("重复注册方法应 panic —— 那是编码错误，不该在运行时静默覆盖")
		}
	}()
	s.Handle("dup", func(json.RawMessage) (any, error) { return nil, nil })
}

func TestMethods_ListsRegistered(t *testing.T) {
	s := newTestServer(t)
	s.Handle("a", func(json.RawMessage) (any, error) { return nil, nil })
	s.Handle("b", func(json.RawMessage) (any, error) { return nil, nil })

	ms := s.Methods()
	if len(ms) != 2 {
		t.Errorf("方法数 = %d, 期望 2", len(ms))
	}
}

// ---------------------------------------------------------------------------
// socket 生命周期
// ---------------------------------------------------------------------------

func TestListen_CreatesSocketWith0600AndRemovesOnClose(t *testing.T) {
	skipOnWindows(t, "Unix 域套接字与权限位断言")
	dir := t.TempDir()
	sock := filepath.Join(dir, "control.sock")
	s, err := NewServer(sock, filepath.Join(dir, "api.token"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Listen(); err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	st, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("socket 文件未创建: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket 权限 = %o, 期望 600", perm)
	}

	// 关闭后不得留下 socket 文件 —— 这是"零残留"承诺的一部分
	if err := s.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Error("关闭后 socket 文件必须被删除")
	}
}

func TestListen_CleansStaleSocket(t *testing.T) {
	skipOnWindows(t, "Unix 域套接字断言")
	dir := t.TempDir()
	sock := filepath.Join(dir, "control.sock")

	// 第一次启动后不 Close，模拟上次异常退出留下的 socket
	first, err := NewServer(sock, filepath.Join(dir, "api.token"))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Listen(); err != nil {
		t.Fatal(err)
	}
	// 手动关掉 listener 但保留文件（模拟崩溃）
	if first.ln != nil {
		_ = first.ln.Close()
	}

	// 第二次启动必须能成功（清理掉遗留 socket）
	second, err := NewServer(sock, filepath.Join(dir, "api.token"))
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Listen(); err != nil {
		t.Fatalf("遗留 socket 未被清理，二次启动失败: %v", err)
	}
	_ = second.Close()
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
