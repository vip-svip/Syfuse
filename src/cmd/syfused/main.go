// Command syfused 是 Syfuse 的守护进程与命令行工具。
//
// 双重身份：
//   - 不带子命令（或 `daemon`）时以守护进程方式常驻，提供 UDS 控制通道；
//   - 带子命令时是一次性 CLI，供 `action.sh` 菜单、App 与用户手工调用。
//
// 子命令命名对齐提示词 17 部分（保留 SY-Fusion 的 `fetch/ca/proxy/serve/token`
// 命名，并扩展 `daemon/hook/leak/restore` 与规则引擎子命令）。
//
// **诚实约定**：本版本处于 M1（模块骨架）。未实现的子命令一律返回
// 明确的"未实现 + 计划里程碑"，并以非零退出码结束。
// 绝不打印"已完成"然后什么都不做 —— 那是提示词 2.4 第 11 条明令禁止的。
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vip-svip/syfuse/src/internal/ipc"
	"github.com/vip-svip/syfuse/src/internal/logx"
	"github.com/vip-svip/syfuse/src/internal/manifest"
	"github.com/vip-svip/syfuse/src/syfuse"
)

// moduleID 必须与 module/module.prop 的 id 一致。
const moduleID = "syfuse"

// 版本信息。构建时可用 -ldflags "-X main.buildVersion=..." 覆盖。
var (
	buildVersion = "0.1.0"
	buildCommit  = "unknown"
	buildTime    = "unknown"
)

// Paths 是运行时路径集合。
//
// 全部可用环境变量覆盖 —— 不是为了"灵活"，而是为了让
// tests/run_restore_check.sh 能在临时目录里跑完整的
// 安装→卸载→残留检查流程，而不必真的往 /data/adb 写东西。
type Paths struct {
	DataDir    string // 数据根目录
	ConfPath   string // syfuse.conf
	LogDir     string // 日志目录
	SockPath   string // 控制 socket
	TokenPath  string // 访问令牌
	Manifest   string // 还原清单
	PidFile    string // PID 文件
	RulesDir   string // 规则目录
	CertDir    string // 证书目录
	BackupDir  string // 备份目录
	WebrootDir string // 局域网面板静态资源
}

// DefaultPaths 返回默认路径（可用 SYFUSE_DATA_DIR 整体重定向）。
func DefaultPaths() Paths {
	base := os.Getenv("SYFUSE_DATA_DIR")
	if base == "" {
		base = "/data/adb/syfuse"
	}
	return Paths{
		DataDir:    base,
		ConfPath:   filepath.Join(base, "syfuse.conf"),
		LogDir:     filepath.Join(base, "log"),
		SockPath:   filepath.Join(base, "control.sock"),
		TokenPath:  filepath.Join(base, "api.token"),
		Manifest:   filepath.Join(base, "manifest", "installed.json"),
		PidFile:    filepath.Join(base, "syfused.pid"),
		RulesDir:   filepath.Join(base, "rules"),
		CertDir:    filepath.Join(base, "cert"),
		BackupDir:  filepath.Join(base, "backup"),
		WebrootDir: filepath.Join(base, "webroot"),
	}
}

// exitNotImplemented 是统一的"未实现"出口。
//
// 存在的意义：让"还没做"这件事**有统一的形状**，不会有人顺手写个
// 空实现蒙混过去。退出码 78 是 sysexits.h 的 EX_CONFIG 之外的约定值，
// 这里固定用 78 表示"该功能尚未实现"，脚本可据此区分于普通错误（1）。
const exitNotImplemented = 78

func notImplemented(cmd, milestone, detail string) int {
	fmt.Fprintf(os.Stderr, "syfused: 子命令 %q 尚未实现\n", cmd)
	fmt.Fprintf(os.Stderr, "  计划里程碑: %s\n", milestone)
	fmt.Fprintf(os.Stderr, "  说明: %s\n", detail)
	fmt.Fprintf(os.Stderr, "  退出码 %d = 未实现（区别于普通错误 1）\n", exitNotImplemented)
	return exitNotImplemented
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	paths := DefaultPaths()

	if len(args) == 0 {
		return cmdDaemon(paths)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "daemon":
		return cmdDaemon(paths)
	case "serve":
		return cmdServe(paths)
	case "token":
		return cmdToken(paths, rest)
	case "status":
		return cmdStatus(paths)
	case "restore":
		return cmdRestore(paths, rest)
	case "rules":
		return cmdRules(paths, rest)
	case "version", "-v", "--version":
		fmt.Printf("syfused %s (commit %s, built %s)\n", buildVersion, buildCommit, buildTime)
		fmt.Printf("module id: %s\n", moduleID)
		fmt.Printf("协议版本: %d\n", ipc.ProtocolVersion)
		fmt.Printf("规则引擎: %s\n", syfuse.Version())
		return 0
	case "help", "-h", "--help":
		usage()
		return 0

	// 以下子命令在提示词 17 部分有定义，但属于后续里程碑。
	// 这里保留名字并明确报未实现，而不是让调用方拿到 "unknown command"
	// 而误以为是自己拼错了。
	case "fetch":
		return notImplemented("fetch", "M7（模块与脚本）/ M10（过滤内核）",
			"下载并更新规则集、脚本、模块订阅。需要 HTTP 客户端与哈希校验，随规则引擎一起交付。")
	case "ca":
		return notImplemented("ca", "M8（MITM 与抓包）",
			"生成/导出/安装/轮换根证书。需要密码学库与系统信任库写入路径。")
	case "proxy":
		return notImplemented("proxy", "M8（MITM 与抓包）",
			"启动 HTTP/HTTPS 入站中间人代理。")
	case "hook":
		return notImplemented("hook", "M11（Hook 与特权）",
			"Hook 框架检测与装载。需要 Zygisk/LSPosed 交互层。")
	case "leak":
		return notImplemented("leak", "M12（防泄漏）",
			"DNS/IPv6/WebRTC/QUIC 泄漏检测与一键修复。")
	default:
		fmt.Fprintf(os.Stderr, "syfused: 未知子命令 %q\n\n", cmd)
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Syfuse 守护进程与命令行工具

用法: syfused [子命令] [参数]

守护与状态
  daemon                以守护进程常驻（默认；不带子命令时等同于此）
  serve                 前台运行 UDS 控制服务（调试用）
  status                查询运行状态（走 UDS，失败则报告未运行）
  token [--rotate]      显示当前访问令牌 / 重新生成
  restore [--apply]     按还原清单执行还原（默认 --dry-run 只打印计划）

规则引擎（与 App 共用同一实现，语义由 tests/corpus 两侧共同守住）
  rules paths           打印规则相关路径
  rules stats           打印规则集与还原清单统计
  rules build           编译规则集并报告（内存内编译；产物落盘属 M5）
  rules syntax          规则体检：逐行诊断
  rules verify          同 syntax，但存在 ERROR 时退出码为 1
  rules test <host|url> [资源类型]
                        对单个请求跑一次匹配，打印完整候选链路

其它
  version               版本信息
  help                  本帮助

尚未实现（会明确报错，不会静默成功）
  fetch  ca  proxy  hook  leak
  rules testfile  rules clear
`)
}

// ---------------------------------------------------------------------------
// daemon / serve
// ---------------------------------------------------------------------------

func newLogger(paths Paths) *logx.Logger {
	return logx.New(logx.Options{
		Dir:   paths.LogDir,
		Level: logx.ParseLevel(os.Getenv("SYFUSE_LOG_LEVEL")),
	})
}

func cmdDaemon(paths Paths) int {
	log := newLogger(paths)
	log.Info("syfused 启动 version=%s commit=%s", buildVersion, buildCommit)

	if err := os.MkdirAll(paths.DataDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "syfused: 无法创建数据目录 %s: %v\n", paths.DataDir, err)
		return 1
	}

	// 单实例保护：PID 文件存在且进程存活则拒绝再起一个。
	// 注意必须**验证进程存活**，不能只看文件在不在 ——
	// 上次崩溃留下的 PID 文件会让守护永远起不来。
	if pid, alive := readLivePid(paths.PidFile); alive {
		fmt.Fprintf(os.Stderr, "syfused: 已有实例在运行 (pid=%d)，拒绝重复启动\n", pid)
		return 1
	}
	if err := writePid(paths.PidFile); err != nil {
		fmt.Fprintf(os.Stderr, "syfused: 写 PID 文件失败: %v\n", err)
		return 1
	}
	defer os.Remove(paths.PidFile)

	srv, err := buildServer(paths, log)
	if err != nil {
		log.Error("构建控制服务失败: %v", err)
		return 1
	}
	if err := srv.Listen(); err != nil {
		log.Error("监听 %s 失败: %v", paths.SockPath, err)
		return 1
	}
	log.Info("控制通道就绪 socket=%s 方法数=%d", paths.SockPath, len(srv.Methods()))

	go func() {
		if err := srv.Serve(); err != nil {
			log.Error("控制服务退出: %v", err)
		}
	}()

	// cron 脚本调度循环（M7）：每分钟扫一次到点任务。
	stopScheduler := StartScriptScheduler(paths, log)
	defer stopScheduler()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	started := time.Now()
	for {
		select {
		case s := <-sigCh:
			if s == syscall.SIGHUP {
				// SIGHUP 约定为重载配置。当前没有可热重载的配置（M5 规则引擎接入后启用），
				// 但**必须**处理这个信号：默认行为是终止进程，那会让
				// "reload" 变成 "stop"，与用户预期相反。
				log.Info("收到 SIGHUP：当前无可热重载项（规则引擎接入后启用），进程继续运行")
				continue
			}
			log.Info("收到 %s，正在退出", s)
			_ = srv.Close()
			log.Info("已退出，运行时长 %s", time.Since(started).Round(time.Second))
			return 0
		case <-tick.C:
			// 存活心跳。当前没有内核子进程可监护（M3 接入），
			// 因此这里只记录自身存活，**不假装**在监护什么。
			log.Debug("心跳 uptime=%s", time.Since(started).Round(time.Second))
		}
	}
}

func cmdServe(paths Paths) int {
	log := newLogger(paths)
	log.Info("syfused serve（前台模式）")
	srv, err := buildServer(paths, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "syfused: %v\n", err)
		return 1
	}
	if err := srv.Listen(); err != nil {
		fmt.Fprintf(os.Stderr, "syfused: 监听失败: %v\n", err)
		return 1
	}
	defer srv.Close()
	fmt.Printf("监听 %s，token 见 %s\n按 Ctrl-C 退出\n", paths.SockPath, paths.TokenPath)
	if err := srv.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "syfused: %v\n", err)
		return 1
	}
	return 0
}

// buildServer 注册全部控制方法。
func buildServer(paths Paths, log *logx.Logger) (*ipc.Server, error) {
	srv, err := ipc.NewServer(paths.SockPath, paths.TokenPath)
	if err != nil {
		return nil, err
	}

	srv.Handle("status", func(json.RawMessage) (any, error) {
		m, _ := manifest.Load(paths.Manifest, moduleID, buildVersion)
		return map[string]any{
			"daemonRunning":   true,
			"daemonVersion":   buildVersion,
			"daemonCommit":    buildCommit,
			"moduleVersion":   buildVersion,
			"protocolVersion": ipc.ProtocolVersion,
			"uptimeSeconds":   int(time.Since(processStart).Seconds()),
			"manifestEntries": len(m.Entries),
			"methods":         srv.Methods(),
		}, nil
	})

	srv.Handle("version", func(json.RawMessage) (any, error) {
		return map[string]any{
			"daemon":          buildVersion,
			"commit":          buildCommit,
			"built":           buildTime,
			"protocol":        ipc.ProtocolVersion,
			"ruleEngine":      syfuse.Version(),
			"supportedProtos": syfuse.SupportedProtocols(),
		}, nil
	})

	srv.Handle("manifest.summary", func(json.RawMessage) (any, error) {
		m, err := manifest.Load(paths.Manifest, moduleID, buildVersion)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"summary":  m.Summary(),
			"total":    len(m.Entries),
			"problems": m.Verify(),
		}, nil
	})

	srv.Handle("token.rotate", func(json.RawMessage) (any, error) {
		tok, err := srv.RotateToken()
		if err != nil {
			return nil, err
		}
		// 只回传长度与后 4 位，不回传完整 token —— 结果可能被写进日志。
		return map[string]any{"rotated": true, "tokenLen": len(tok), "tokenSuffix": tok[len(tok)-4:]}, nil
	})

	srv.Handle("log.tail", func(params json.RawMessage) (any, error) {
		var p struct {
			Lines int `json:"lines"`
		}
		_ = json.Unmarshal(params, &p)
		if p.Lines <= 0 {
			p.Lines = 100
		}
		lines, err := tailFile(filepath.Join(paths.LogDir, "syfused.log"), p.Lines)
		if err != nil {
			return map[string]any{"lines": []string{}, "note": err.Error()}, nil
		}
		return map[string]any{"lines": lines}, nil
	})

	// 规则引擎：语义实现已与 Kotlin 侧 core:rule 同源（共用 tests/corpus
	// 与 tests/expect.tsv 作跨语言预言机）。规则**来源管理**（订阅下载、
	// 自动更新）与编译产物落盘仍属 M5/M10，故 proxies/config.reload 等
	// 仍留在下面的待实现表里。
	srv.Handle("rules.test", handleRulesTest(paths))
	srv.Handle("rules.syntax", handleRulesSyntax(paths))
	srv.Handle("rules.stats", handleRulesStats(paths))
	srv.Handle("rules.build", handleRulesBuild(paths))

	// 代理控制：真实实现（配置校验 + 节点解析 + 选择 + TCP 可达性测速）。
	// proxies.* 全部由 proxyManager 驱动真实引擎：start 会建立本地
	// SOCKS5 入站隧道并在应答里报回实际监听地址（socksAddr）。
	pm := newProxyManager()
	srv.Handle("proxies", handleProxiesStatus(pm))
	srv.Handle("proxies.status", handleProxiesStatus(pm))
	srv.Handle("proxies.start", handleProxiesStart(pm, paths))
	srv.Handle("proxies.stop", handleProxiesStop(pm))
	srv.Handle("proxies.reload", handleProxiesReload(pm, paths))
	srv.Handle("proxies.select", handleProxiesSelect(pm))
	srv.Handle("proxies.urlTest", handleProxiesURLTest(pm))

	// 数据与统计（M9）：来自引擎的连接跟踪器，与 dataplane 同源。
	srv.Handle("connections", handleConnections(pm))
	srv.Handle("traffic", handleTraffic(pm))

	// 防泄漏（M12）：一键自检。
	srv.Handle("leak.check", handleLeakCheck())

	// 脚本系统（M7）：试跑 + 存储管理，语义见 internal/script。
	srv.Handle("scripts.run", handleScriptsRun(paths))
	srv.Handle("scripts.list", handleScriptsList(paths))
	srv.Handle("scripts.save", handleScriptsSave(paths))
	srv.Handle("scripts.delete", handleScriptsDelete(paths))
	srv.Handle("scripts.toggle", handleScriptsToggle(paths))
	srv.Handle("scripts.order", handleScriptsOrder(paths))
	srv.Handle("scripts.recent", handleScriptsRecent(paths, log))

	// 模块管理（M7a）：存储在 <DataDir>/modules，语义见 internal/module。
	srv.Handle("modules.list", handleModulesList(paths))
	srv.Handle("modules.import", handleModulesImport(paths))
	srv.Handle("modules.delete", handleModulesDelete(paths))
	srv.Handle("modules.toggle", handleModulesToggle(paths))
	srv.Handle("modules.order", handleModulesOrder(paths))
	srv.Handle("modules.merge", handleModulesMerge(paths))

	// 尚未实现的方法：注册但返回明确错误。
	// 为什么注册而不是不注册：让 App 拿到"未实现 + 计划"，
	// 而不是 "unknown_method" —— 后者会被误当成版本不匹配。
	for _, m := range []struct{ name, milestone string }{
		{"filter.stats", "M10"},
		{"hook.status", "M11"}, {"leak.fix", "M12"},
		{"config.reload", "M5"},
	} {
		name, milestone := m.name, m.milestone
		srv.Handle(name, func(json.RawMessage) (any, error) {
			return nil, fmt.Errorf("方法 %q 尚未实现（计划里程碑 %s）", name, milestone)
		})
	}
	return srv, nil
}

var processStart = time.Now()

// ---------------------------------------------------------------------------
// token / status
// ---------------------------------------------------------------------------

func cmdToken(paths Paths, args []string) int {
	rotate := false
	for _, a := range args {
		if a == "--rotate" {
			rotate = true
		}
	}
	if rotate {
		srv, err := ipc.NewServer(paths.SockPath, paths.TokenPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "syfused: %v\n", err)
			return 1
		}
		tok, err := srv.RotateToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "syfused: %v\n", err)
			return 1
		}
		fmt.Printf("令牌已重新生成（长度 %d）。\n", len(tok))
		fmt.Println("注意：已连接的 App 需要重新获取令牌。")
		return 0
	}
	tok, err := ipc.LoadOrCreateToken(paths.TokenPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "syfused: %v\n", err)
		return 1
	}
	fmt.Printf("令牌文件: %s\n", paths.TokenPath)
	fmt.Printf("令牌: %s\n", tok)
	return 0
}

func cmdStatus(paths Paths) int {
	pid, alive := readLivePid(paths.PidFile)
	fmt.Printf("数据目录   : %s\n", paths.DataDir)
	fmt.Printf("socket     : %s\n", paths.SockPath)
	if !alive {
		fmt.Println("守护进程   : 未运行")
		return 1
	}
	fmt.Printf("守护进程   : 运行中 (pid=%d)\n", pid)
	m, err := manifest.Load(paths.Manifest, moduleID, buildVersion)
	if err != nil {
		fmt.Printf("还原清单   : 读取失败 - %v\n", err)
		return 0
	}
	fmt.Printf("还原清单   : %d 条 %v\n", len(m.Entries), m.Summary())
	if probs := m.Verify(); len(probs) > 0 {
		fmt.Printf("清单问题   : %d 条\n", len(probs))
		for _, p := range probs {
			fmt.Printf("  - [%s] %s\n", p.EntryID, p.Reason)
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// restore
// ---------------------------------------------------------------------------

func cmdRestore(paths Paths, args []string) int {
	apply := false
	for _, a := range args {
		if a == "--apply" {
			apply = true
		}
	}

	m, err := manifest.Load(paths.Manifest, moduleID, buildVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "syfused: %v\n", err)
		return 1
	}
	if len(m.Entries) == 0 {
		fmt.Println("还原清单为空，无需还原。")
		return 0
	}

	if probs := m.Verify(); len(probs) > 0 {
		fmt.Fprintf(os.Stderr, "还原清单存在问题，拒绝执行还原（先修清单，否则可能留下残留）：\n")
		for _, p := range probs {
			fmt.Fprintf(os.Stderr, "  - [%s] %s\n", p.EntryID, p.Reason)
		}
		return 1
	}

	plan := m.RestorePlan()
	fmt.Printf("还原计划（逆序执行，共 %d 条）:\n", len(plan))
	for i, e := range plan {
		fmt.Printf("  %2d. [%s] %s\n      target=%s source=%s\n", i+1, e.Kind, e.ID, e.Target, e.Source)
	}

	if !apply {
		fmt.Println("\n以上为 --dry-run（默认）。加 --apply 才会真正执行。")
		fmt.Println("真机上完整的还原由 module/uninstall.sh 负责（含挂载与证书）；")
		fmt.Println("本子命令处理清单中可安全删除的文件系统条目。")
		return 0
	}

	var failed int
	for _, e := range plan {
		if err := restoreEntry(e); err != nil {
			fmt.Fprintf(os.Stderr, "  还原失败 [%s] %s: %v\n", e.Kind, e.ID, err)
			failed++
			continue
		}
		fmt.Printf("  已还原 [%s] %s\n", e.Kind, e.ID)
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d 条还原失败。**不得**报告为已完全还原。\n", failed)
		return 1
	}
	if err := m.Save(paths.Manifest); err != nil {
		fmt.Fprintf(os.Stderr, "syfused: 清空清单失败: %v\n", err)
		return 1
	}
	m.Entries = nil
	_ = m.Save(paths.Manifest)
	fmt.Println("\n全部条目已还原，清单已清空。")
	return 0
}

// restoreEntry 执行单条还原。
//
// 只处理**文件系统层面可安全删除**的类型。挂载、证书、hosts、iptables
// 需要 root 与系统工具，由 module/uninstall.sh 承担 —— 那里能保证
// 命令在真机上可用，而 Go 侧 fork 系统命令会引入平台差异。
// 这里遇到这些类型返回明确的"交给 uninstall.sh"而不是假装成功。
func restoreEntry(e manifest.Entry) error {
	switch e.Kind {
	case manifest.KindDir:
		if e.Target == "" || e.Target == "/" {
			return errors.New("拒绝删除空路径或根路径")
		}
		return os.RemoveAll(e.Target)
	case manifest.KindFile:
		if e.Target == "" {
			return errors.New("拒绝删除空路径")
		}
		if err := os.Remove(e.Target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case manifest.KindMount, manifest.KindCert, manifest.KindHosts,
		manifest.KindIptables, manifest.KindProp, manifest.KindSELinux, manifest.KindAPK:
		return fmt.Errorf("类型 %s 需要 root 与系统工具，请由 module/uninstall.sh 处理（本命令不代劳）", e.Kind)
	default:
		return fmt.Errorf("未知类型 %s", e.Kind)
	}
}

// ---------------------------------------------------------------------------
// rules
// ---------------------------------------------------------------------------

func cmdRules(paths Paths, args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "paths":
		// 真实实现：把路径打出来，供脚本与 App 使用。
		// 输出**必须**包含仓库根下的路径与运行时路径两类，否则
		// action.sh 与 App 会各自猜路径，迟早不一致。
		rows := [][2]string{
			{"运行时规则目录", paths.RulesDir},
			{"规则主配置", filepath.Join(paths.RulesDir, "user_rules.txt")},
			{"黑名单", filepath.Join(paths.RulesDir, "blacklist.conf")},
			{"白名单", filepath.Join(paths.RulesDir, "whitelist.conf")},
			{"规则源列表", filepath.Join(paths.RulesDir, "lists.conf")},
			{"订阅源", filepath.Join(paths.RulesDir, "sources.conf")},
			{"编译产物", filepath.Join(paths.RulesDir, "compiled.bin")},
			{"证书目录", paths.CertDir},
			{"备份目录", paths.BackupDir},
			{"还原清单", paths.Manifest},
		}
		for _, r := range rows {
			exists := "缺失"
			if _, err := os.Stat(r[1]); err == nil {
				exists = "存在"
			}
			fmt.Printf("%-14s %-56s [%s]\n", r[0], r[1], exists)
		}
		return 0

	case "stats":
		m, err := manifest.Load(paths.Manifest, moduleID, buildVersion)
		if err != nil {
			fmt.Fprintf(os.Stderr, "syfused: %v\n", err)
			return 1
		}
		summary := m.Summary()
		keys := make([]string, 0, len(summary))
		for k := range summary {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Printf("还原清单条目: %d\n", len(m.Entries))
		for _, k := range keys {
			fmt.Printf("  %-10s %d\n", k, summary[k])
		}
		fmt.Printf("\n规则引擎版本: %s\n", syfuse.Version())
		fmt.Printf("支持协议: %s\n", strings.Join(syfuse.SupportedProtocols(), ", "))
		// 规则条数：真实编译一次再报，不再打印"未实现"。
		printRuleStats(paths)
		return 0

	case "build", "syntax", "verify":
		return cmdRulesReport(paths, sub)

	case "test":
		return cmdRulesTest(paths, args[1:])

	case "testfile", "clear":
		return notImplemented("rules "+sub, "M5（规则与分流）/ M10（过滤内核）",
			"rules testfile 需要规则集文件格式与期望表的约定（随订阅管理交付）；"+
				"rules clear 需要编译产物落盘（compiled.bin），两者都在 M5。"+
				"在此之前规则语义本身已可用：见 rules test / syntax / verify / build。")

	case "":
		fmt.Fprintln(os.Stderr, "用法: syfused rules <paths|stats|build|syntax|verify|test|testfile|clear>")
		return 2

	default:
		fmt.Fprintf(os.Stderr, "syfused: 未知 rules 子命令 %q\n", sub)
		return 2
	}
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func writePid(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
}

// readLivePid 读取 PID 文件并检查进程是否真的存活。
//
// 只看文件存在是不够的：上次崩溃留下的 PID 文件会让守护永远无法启动。
// 用 signal 0 探测进程存在性（不发送任何信号）。
func readLivePid(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return pid, false
	}
	return pid, true
}

func tailFile(path string, n int) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}
