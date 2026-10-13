// modules.go 是 M7a 模块管理的 UDS 处理器。
// 存储在 <DataDir>/modules，每模块一个 JSON 条目（见 internal/module 的 Store）。
// 参数通过 JSON.RawMessage 传入（UDS 层不做结构化解码，这里自己解）。
package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/vip-svip/syfuse/src/internal/ipc"
	"github.com/vip-svip/syfuse/src/internal/module"
)

// moduleStore 按 Paths 打开模块存储；打不开返回错误给调用方。
func moduleStore(p Paths) (*module.Store, error) {
	return module.OpenStore(filepath.Join(p.DataDir, "modules"))
}

// 解析最小请求体：{"id": "..."} / {"text": "..."} / {"ids": [...]}。
type idReq struct {
	ID      string   `json:"id"`
	Enabled *bool    `json:"enabled"`
	Text    string   `json:"text"`
	IDS     []string `json:"ids"`
}

func decodeReq(raw json.RawMessage) (*idReq, error) {
	var r idReq
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("modules: 参数解析失败: %w", err)
		}
	}
	return &r, nil
}

// handleModulesList 列出全部模块（含原文，供编辑器打开）。
func handleModulesList(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		s, err := moduleStore(p)
		if err != nil {
			return nil, err
		}
		list, err := s.List()
		if err != nil {
			return nil, err
		}
		if list == nil {
			list = []*module.Entry{}
		}
		return map[string]any{"modules": list}, nil
	}
}

// handleModulesImport 导入/更新模块（text 必填，缺 #!name= 报错）。
func handleModulesImport(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		req, err := decodeReq(raw)
		if err != nil {
			return nil, err
		}
		if req.Text == "" {
			return nil, fmt.Errorf("modules.import: 缺少 text")
		}
		s, err := moduleStore(p)
		if err != nil {
			return nil, err
		}
		e, err := s.Import(req.Text)
		if err != nil {
			return nil, err
		}
		return map[string]any{"entry": e}, nil
	}
}

// handleModulesDelete 删除模块（id 必填）。
func handleModulesDelete(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		req, err := decodeReq(raw)
		if err != nil {
			return nil, err
		}
		if req.ID == "" {
			return nil, fmt.Errorf("modules.delete: 缺少 id")
		}
		s, err := moduleStore(p)
		if err != nil {
			return nil, err
		}
		if err := s.Delete(req.ID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": req.ID}, nil
	}
}

// handleModulesToggle 启用/停用模块（id + enabled 必填）。
func handleModulesToggle(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		req, err := decodeReq(raw)
		if err != nil {
			return nil, err
		}
		if req.ID == "" || req.Enabled == nil {
			return nil, fmt.Errorf("modules.toggle: 需要 id 与 enabled")
		}
		s, err := moduleStore(p)
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

// handleModulesOrder 重排（ids 数组顺序即新顺序）。
func handleModulesOrder(p Paths) ipc.Handler {
	return func(raw json.RawMessage) (any, error) {
		req, err := decodeReq(raw)
		if err != nil {
			return nil, err
		}
		if len(req.IDS) == 0 {
			return nil, fmt.Errorf("modules.order: 缺少 ids")
		}
		s, err := moduleStore(p)
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
		return map[string]any{"modules": list}, nil
	}
}

// handleModulesMerge 把启用模块合并成配置文本，带冲突列表。
// 这是"模块沙箱预览（导入前显示它将改动什么）"的数据源。
func handleModulesMerge(p Paths) ipc.Handler {
	return func(json.RawMessage) (any, error) {
		s, err := moduleStore(p)
		if err != nil {
			return nil, err
		}
		res, err := s.MergeEnabled()
		if err != nil {
			return nil, err
		}
		if res.Conflicts == nil {
			res.Conflicts = []module.Conflict{}
		}
		return map[string]any{"text": res.Text, "conflicts": res.Conflicts}, nil
	}
}
