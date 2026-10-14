// scripts.go 是脚本管理的 UDS 处理器（与 modules.go 同构）。
// 存储在 <DataDir>/scripts；scripts.run 是调试控制台"单脚本试跑"的最小
// 可用路径：不落盘、直接执行传入的源码（或已存脚本 ID）并返回结果+日志。
package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/vip-svip/syfuse/src/internal/ipc"
	"github.com/vip-svip/syfuse/src/internal/script"
)

func scriptStore(p Paths) (*script.DiskStore, error) {
	return script.OpenDiskStore(filepath.Join(p.DataDir, "scripts"))
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
