package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 平台能力探测（重要，别删）
// ---------------------------------------------------------------------------
//
// 守护进程的目标平台是 **Android（Linux）**，但本文件**不按 GOOS 跳过**。
//
// 原因：同一个操作系统名之下，能力可以完全不同 —— Windows 10 1803+ 已支持
// AF_UNIX；WSL1 与 WSL2、受限容器、被安全策略关掉 socket 的环境又各不相同。
// 按平台名跳过会**掩盖真实回归**：断言在能跑的机器上被跳过，等于没有断言，
// 而且看报告时"skip"和"pass"长得一样容易，缺口会被忘掉。
//
// 所以改成**探测实际能力**：能跑就跑，跑不了才跳过，并把探测到的具体错误
// 写进跳过信息（而不是"因为你是 Windows"）。
//
// 本机实测（Windows + Go 1.23）：
//
//	AF_UNIX listen / dial / 回环读写 …… 可用（原注释称 bind 报 invalid argument，不成立）
//	os.Stat().Mode()&os.ModeSocket …… 可识别
//	os.Chmod 后的 0600 权限位    …… 不可用，读回恒为 0666（目录 0777）
//
// 即：套接字相关断言在本机应当**真实执行**；只有权限位断言才需要跳过。

// probeUnixSocket 探测当前环境能否真正使用 AF_UNIX 套接字。
// 只做 listen 不够 —— 有些环境能 bind 但连不上，因此走一次完整回环。
func probeUnixSocket(t *testing.T) error {
	t.Helper()
	dir, err := os.MkdirTemp("", "syfuse-uds-probe-")
	if err != nil {
		return fmt.Errorf("建临时目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "probe.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("net.Listen(\"unix\") 失败: %w", err)
	}
	defer func() { _ = ln.Close() }()

	server := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			server <- err
			return
		}
		defer func() { _ = c.Close() }()
		buf := make([]byte, 8)
		n, err := c.Read(buf)
		if err != nil {
			server <- err
			return
		}
		_, err = c.Write(buf[:n])
		server <- err
	}()

	c, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("net.Dial(\"unix\") 失败: %w", err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Write([]byte("ping")); err != nil {
		return fmt.Errorf("回环写失败: %w", err)
	}
	select {
	case err := <-server:
		if err != nil {
			return fmt.Errorf("回环读失败: %w", err)
		}
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("AF_UNIX 回环超时")
	}
}

// probePosixPerms 探测当前环境是否真的实现 POSIX 权限位。
// 判定标准与断言一致：Chmod(0600) 之后读回的必须是 0600。
func probePosixPerms(t *testing.T) error {
	t.Helper()
	dir, err := os.MkdirTemp("", "syfuse-perm-probe-")
	if err != nil {
		return fmt.Errorf("建临时目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "probe.token")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("创建探测文件失败: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("Chmod(0600) 失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("关闭探测文件失败: %w", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("Stat 失败: %w", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		return fmt.Errorf("Chmod(0600) 后读回 %o —— 该环境不实现 POSIX 权限位", perm)
	}
	return nil
}

// requireUnixSocket 在环境不支持 AF_UNIX 时跳过，并说明探测到的原因。
func requireUnixSocket(t *testing.T) {
	t.Helper()
	if err := probeUnixSocket(t); err != nil {
		t.Skipf("跳过：本环境无法使用 AF_UNIX 套接字（探测结果：%v）。"+
			"目标平台 Android/Linux 上此断言必须通过，不得据此判定通过。", err)
	}
}

// requirePosixPerms 在环境不实现 POSIX 权限位时跳过，并说明探测到的原因。
func requirePosixPerms(t *testing.T) {
	t.Helper()
	if err := probePosixPerms(t); err != nil {
		t.Skipf("跳过：本环境不实现 POSIX 权限位（探测结果：%v）。"+
			"目标平台 Android/Linux 上此断言必须通过，不得据此判定通过。", err)
	}
}

// helperBindEnv 是"制造崩溃残留"的辅助进程开关。
//
// Go 的 UnixListener.Close() 会 unlink socket 文件，所以**用 Close 造不出
// 遗留文件**（这正是原 TestListen_CleansStaleSocket 空转的原因）。真实崩溃
// 残留的形态是：进程 bind 成功后直接死掉，文件留在磁盘上。
// 因此这里 fork 本测试二进制、只 bind 不 Close 就 os.Exit，精确复现该形态。
const helperBindEnv = "SYFUSE_IPC_TEST_BIND"

func TestMain(m *testing.M) {
	if path := os.Getenv(helperBindEnv); path != "" {
		ln, err := net.Listen("unix", path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "helper: listen %s: %v\n", path, err)
			os.Exit(3)
		}
		_ = ln // 故意不 Close：模拟进程崩溃，socket 文件留在磁盘上
		os.Exit(0)
	}
	os.Exit(m.Run())
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
	requirePosixPerms(t)
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

// TestListen_CreatesSocketAndRemovesOnClose 断言 socket 文件的生命周期。
// 只依赖 AF_UNIX 能力，不依赖权限位 —— 因此在 Windows 上也会真实执行。
func TestListen_CreatesSocketAndRemovesOnClose(t *testing.T) {
	requireUnixSocket(t)
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
	if st.Mode()&os.ModeSocket == 0 {
		t.Errorf("创建出来的不是 socket 文件，mode=%v", st.Mode())
	}

	// 关闭后不得留下 socket 文件 —— 这是"零残留"承诺的一部分
	if err := s.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Error("关闭后 socket 文件必须被删除")
	}
}

// TestListen_SocketPermissionsAre0600 单独断言 socket 的权限位。
// 与上一个测试分开，是为了让"权限位不可用"的环境只跳过这一条，
// 而不是把整个 socket 生命周期断言一起跳过。
func TestListen_SocketPermissionsAre0600(t *testing.T) {
	requireUnixSocket(t)
	requirePosixPerms(t)
	dir := t.TempDir()
	sock := filepath.Join(dir, "control.sock")
	s, err := NewServer(sock, filepath.Join(dir, "api.token"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Listen(); err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer func() { _ = s.Close() }()

	st, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("socket 文件未创建: %v", err)
	}
	// 0600：只有 root 与同 uid 可连，否则同机任意应用都能接管控制通道
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket 权限 = %o, 期望 600", perm)
	}
}

// TestListen_CleansStaleSocket 断言上次崩溃遗留的 socket 文件会被清理。
//
// 关键：遗留文件必须由**真的崩溃**造出来。Go 的 UnixListener.Close() 会
// unlink 文件，所以"Close 掉 listener 但保留文件"这种写法根本留不下东西，
// 断言会空转通过（原实现即如此）。这里 fork 一个只 bind、不 Close 就退出的
// 辅助进程，精确复现崩溃残留。
func TestListen_CleansStaleSocket(t *testing.T) {
	requireUnixSocket(t)
	dir := t.TempDir()
	sock := filepath.Join(dir, "control.sock")

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperBindEnv+"="+sock)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("辅助进程失败（测试前提不成立）: %v\n%s", err, out)
	}

	st, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("辅助进程未留下 socket 文件，测试前提不成立: %v", err)
	}
	if st.Mode()&os.ModeSocket == 0 {
		t.Fatalf("遗留物不是 socket 文件，mode=%v", st.Mode())
	}

	// 此刻文件在磁盘上但无人监听：不清理的话 bind 会直接失败。
	second, err := NewServer(sock, filepath.Join(dir, "api.token"))
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Listen(); err != nil {
		t.Fatalf("遗留 socket 未被清理，二次启动失败: %v", err)
	}
	defer func() { _ = second.Close() }()

	// 真的能连上，才算证明是"新监听"而不是残留假象
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("清理后无法连接: %v", err)
	}
	_ = c.Close()
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
