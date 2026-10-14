// scripts.go 是脚本管理的 UDS 处理器（与 modules.go 同构）。
// 存储在 <DataDir>/scripts；scripts.run 是调试控制台"单脚本试跑"的最小
// 可用路径：不落盘、直接执行传入的源码（或已存脚本 ID）并返回结果+日志。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vip-svip/syfuse/src/internal/backup"
	"github.com/vip-svip/syfuse/src/internal/filter"
	"github.com/vip-svip/syfuse/src/internal/hook"
	"github.com/vip-svip/syfuse/src/internal/logx"
	"github.com/vip-svip/syfuse/src/internal/notify"

	"github.com/vip-svip/syfuse/src/internal/ipc"
	"github.com/vip-svip/syfuse/src/internal/script"
)

func scriptStore(p Paths) (*script.DiskStore, error) {
	return script.OpenDiskStore(filepath.Join(p.DataDir, "scripts"))
}

// scriptScheduler 是 daemon 内共享的 cron 调度器（cmdDaemon 起循环，
// UDS 方法读执行历史——同一个实例才能看到同一份记录）。
var (
	scriptSchedulerOnce sync.Once
	scriptScheduler     *script.Scheduler
)

// schedulerFor 返回（并按需创建）daemon 级调度器。
func schedulerFor(p Paths, log *logx.Logger) *script.Scheduler {
	scriptSchedulerOnce.Do(func() {
		store, err := scriptStore(p)
		if err != nil {
			log.Error("脚本调度器初始化失败: %v", err)
			return
		}
		scriptScheduler = script.NewScheduler(store, func(title, content string) {
			log.Info("[脚本通知] %s: %s", title, content)
		})
	})
	return scriptScheduler
}

// StartScriptScheduler 起每分钟一次的调度循环（daemon 生命周期内常驻）。
// 返回 stop 函数；循环内的错误只记日志，绝不让调度器拖垮守护进程。
func StartScriptScheduler(p Paths, log *logx.Logger) (stop func()) {
	sch := schedulerFor(p, log)
	if sch == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				runs, err := sch.RunDue()
				if err != nil {
					log.Error("cron 调度失败: %v", err)
					continue
				}
				for _, r := range runs {
					if r.Err != "" {
						log.Error("cron 脚本 %s 失败: %s", r.Name, r.Err)
					} else {
						log.Info("cron 脚本 %s 执行完成 耗时=%s", r.Name, r.Elapsed.Round(time.Millisecond))
					}
				}
			}
		}
	}()
	return func() { close(done) }
}

// handleScriptsRecent 暴露 cron 执行历史（调试控制台）。
func handleScriptsRecent(p Paths, log *logx.Logger) ipc.Handler {
	return func(json.RawMessage) (any, error) {
		sch := schedulerFor(p, log)
		if sch == nil {
			return map[string]any{"recent": []script.CronRun{}}, nil
		}
		recent := sch.Recent()
		if recent == nil {
			recent = []script.CronRun{}
		}
		return map[string]any{"recent": recent}, nil
	}
}

// handleScriptsList 列出全部脚本（含原文，供编辑器）。
func handleScriptsList(p Paths) ipc.Handler {
	return func(json.RawMessage) (any, error) {
		s, err := scriptStore(p)
		if err != nil {
			return nil, err
		}
		list, err := s.List()
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []*script.Entry{}
		}
		return map[string]any{"scripts": list}, nil
	}
}

// handleScriptsSave 保存/更新脚本。
func handleScriptsSave(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		var req struct {
			Name   string       `json:"name"`
			Type   string       `json:"type"`
			Source string       `json:"source"`
			Match  script.Match `json:"match"`
			Cron   string       `json:"cron"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("scripts.save: 参数解析失败: %w", err)
		}
		s, err := scriptStore(p)
		if err != nil {
			return nil, err
		}
		e, err := s.Save(req.Name, script.Type(req.Type), req.Source, req.Match, req.Cron)
		if err != nil {
			return nil, err
		}
		return map[string]any{"entry": e}, nil
	}
}

// handleScriptsDelete / toggle / order 与 modules.* 同构。
func handleScriptsDelete(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("scripts.delete: %w", err)
		}
		if req.ID == "" {
			return nil, fmt.Errorf("scripts.delete: 缺 id")
		}
		s, err := scriptStore(p)
		if err != nil {
			return nil, err
		}
		if err := s.Delete(req.ID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": req.ID}, nil
	}
}

func handleScriptsToggle(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		var req struct {
			ID      string `json:"id"`
			Enabled *bool  `json:"enabled"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("scripts.toggle: %w", err)
		}
		if req.ID == "" || req.Enabled == nil {
			return nil, fmt.Errorf("scripts.toggle: 需要 id 与 enabled")
		}
		s, err := scriptStore(p)
		if err != nil {
			return nil, err
		}
		e, err := s.SetEnabled(req.ID, *req.Enabled)
		if err != nil {
			return nil, err
		}
		return map[string]any{"entry": e}, nil
	}
}

func handleScriptsOrder(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		var req struct {
			IDS []string `json:"ids"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("scripts.order: %w", err)
		}
		if len(req.IDS) == 0 {
			return nil, fmt.Errorf("scripts.order: 缺 ids")
		}
		s, err := scriptStore(p)
		if err != nil {
			return nil, err
		}
		if err := s.SetOrder(req.IDS); err != nil {
			return nil, err
		}
		list, err := s.List()
		if err != nil {
			return nil, err
		}
		return map[string]any{"scripts": list}, nil
	}
}

// scripts.run 入参：{source?, id?, type?, argument?, timeout_ms?}。
// 有 source 直接跑（试跑未保存的草稿）；有 id 从存储取。
func handleScriptsRun(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		var req struct {
			Source    string           `json:"source"`
			ID        string           `json:"id"`
			Type      string           `json:"type"`
			Argument  string           `json:"argument"`
			TimeoutMs int64            `json:"timeout_ms"`
			Request   *script.Request  `json:"request"`
			Response  *script.Response `json:"response"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, fmt.Errorf("scripts.run: 参数解析失败: %w", err)
			}
		}
		src, typ := req.Source, script.Type(req.Type)
		if src == "" && req.ID != "" {
			s, err := scriptStore(p)
			if err != nil {
				return nil, err
			}
			e, err := s.Get(req.ID)
			if err != nil {
				return nil, err
			}
			if e == nil {
				return nil, fmt.Errorf("scripts.run: 脚本 %q 不存在", req.ID)
			}
			src, typ = e.Source, e.Type
		}
		if src == "" {
			return nil, fmt.Errorf("scripts.run: 需要 source 或 id")
		}

		opts := script.Options{
			Type:     typ,
			Argument: req.Argument,
			Script:   "试跑",
		}
		if req.TimeoutMs > 0 {
			opts.Timeout = timeDuration(req.TimeoutMs)
		}
		var res *script.HTTPResult
		var logs []string
		var err error
		if req.Request != nil || req.Response != nil {
			res, logs, err = script.RunHTTP(src, req.Request, req.Response, opts)
		} else {
			var v any
			v, logs, err = script.Run(src, opts)
			if err == nil {
				return map[string]any{"result": v, "logs": logs}, nil
			}
		}
		if err != nil {
			var te interface{ Error() string }
			if te = err; te != nil {
				// 超时也是错误，直接返回给调试台显示。
				return map[string]any{"error": err.Error(), "logs": logs}, nil
			}
		}
		if res != nil {
			return map[string]any{"http_result": res, "logs": logs}, nil
		}
		return map[string]any{"logs": logs}, nil
	}
}

// timeDuration 毫秒转 time.Duration。
func timeDuration(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}

// filterEngine 是 daemon 级过滤引擎单例（后续接规则源加载；当前从
// <DataDir>/filter_rules.txt 读规则集，无文件即空规则集）。
var (
	filterEngineOnce sync.Once
	filterEngine     *filter.Engine
)

func filterEngineFor(p Paths) *filter.Engine {
	filterEngineOnce.Do(func() {
		filterEngine = &filter.Engine{}
		if data, err := os.ReadFile(filepath.Join(p.DataDir, "filter_rules.txt")); err == nil {
			filterEngine.Load(string(data))
		}
	})
	return filterEngine
}

// handleFilterStats 实现 M10 的 filter.stats：规则集统计口径。
func handleFilterStats(p Paths) ipc.Handler {
	return func(json.RawMessage) (any, error) {
		return filterEngineFor(p).Stats(), nil
	}
}

// handleBackupCreate 实现 M13 的 backup.create：
// 打包 config/rules/scripts/modules 四个目录到 <DataDir>/backups/。
// 入参 {dirs?: ["config",...]} 可自选子集，缺省全四。
func handleBackupCreate(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		var req struct {
			Dirs []string `json:"dirs"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &req); err != nil {
				return nil, fmt.Errorf("backup.create: %w", err)
			}
		}
		if len(req.Dirs) == 0 {
			req.Dirs = []string{"config", "rules", "scripts", "modules"}
		}
		zipPath, mf, err := backup.Create(p.DataDir, req.Dirs, buildVersion)
		if err != nil {
			return nil, err
		}
		return map[string]any{"zip": zipPath, "manifest": mf}, nil
	}
}

// handleBackupRestore 实现 M13 的 backup.restore：入参 {zip: "<路径>"}。
// zip 路径必须位于 <DataDir>/backups/ 内（防任意文件读取面扩大）。
func handleBackupRestore(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		var req struct {
			Zip string `json:"zip"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("backup.restore: %w", err)
		}
		if req.Zip == "" {
			return nil, fmt.Errorf("backup.restore: 缺 zip 路径")
		}
		allowedRoot, _ := filepath.Abs(filepath.Join(p.DataDir, "backups"))
		zipAbs, err := filepath.Abs(req.Zip)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(zipAbs, allowedRoot+string(filepath.Separator)) &&
			zipAbs != allowedRoot {
			return nil, fmt.Errorf("backup.restore: 备份包必须位于 %s", allowedRoot)
		}
		mf, err := backup.Restore(p.DataDir, zipAbs)
		if err != nil {
			return nil, err
		}
		return map[string]any{"manifest": mf}, nil
	}
}

// notifyFilterFor 是 daemon 级通知净化器单例（<DataDir>/notify_keywords.conf）。
var (
	notifyFilterOnce sync.Once
	notifyFilter     *notify.Filter
	notifyStats      *notify.Stats
)

func notifyFilterFor(p Paths) *notify.Filter {
	notifyFilterOnce.Do(func() {
		text := ""
		if data, err := os.ReadFile(filepath.Join(p.DataDir, "notify_keywords.conf")); err == nil {
			text = string(data)
		}
		notifyFilter, notifyStats = notify.Parse(text)
	})
	return notifyFilter
}

// handleNotifyTest 实现 M13 的 notify.test：给定包名/标题/正文，
// 返回净化决策（调试控制台"单条试跑"）。
func handleNotifyTest(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		var req struct {
			Pkg     string `json:"pkg"`
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("notify.test: %w", err)
		}
		f := notifyFilterFor(p)
		m, hit := f.ShouldFilter(req.Pkg, req.Title, req.Content)
		resp := map[string]any{"matched": hit}
		if m != nil {
			resp["rule"] = m.Rule
			resp["line"] = m.Line
		}
		if notifyStats != nil {
			resp["stats"] = *notifyStats
		}
		return resp, nil
	}
}

// handleHookStatus 实现 M11 的 hook.status：框架检测与降级说明。
// Android 真机探测 /data/adb/... 真实路径；非 Android 环境（沙盒/测试）
// 全部不存在 → 返回空列表 + 降级说明，语义与"无 root 设备"一致。
func handleHookStatus() ipc.Handler {
	return func(json.RawMessage) (any, error) {
		d, err := hook.NewDetector(func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		})
		if err != nil {
			return nil, err
		}
		return d.Detect(), nil
	}
}
