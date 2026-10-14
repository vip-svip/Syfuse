// store.go 是脚本的持久化存储（与 module.Store 同构：目录即索引）。
// 每个脚本一个 JSON 条目，含原文/类型/开关/触发条件/命中计数。
package script

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Entry 是存储中的一个脚本条目。
type Entry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    Type   `json:"type"`
	Enabled bool   `json:"enabled"`
	Order   int    `json:"order"`
	Match   Match  `json:"match"`          // 触发条件（http/rule 类型用）
	Cron    string `json:"cron,omitempty"` // cron 类型用
	Source  string `json:"source"`         // 脚本原文
	Hits    int64  `json:"hits"`           // 命中次数（调试控制台"命中计数"）
}

// DiskStore 是脚本落盘存储（区别于 engine.go 的 $persistentStore 后端
// 接口 Store）。目录即索引。
type DiskStore struct {
	dir string
}

// OpenDiskStore 打开（不存在则创建）脚本存储目录。
func OpenDiskStore(dir string) (*DiskStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("script: 存储目录为空")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("script: 创建存储目录: %w", err)
	}
	return &DiskStore{dir: dir}, nil
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func slug(name string) string {
	s := slugRe.ReplaceAllString(strings.TrimSpace(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "script"
	}
	return s
}

// Save 保存脚本（同名 = 更新，保留 enabled/order/hits）。缺 #!name 之类
// 元数据不属于脚本语法 —— 脚本名显式传入，不解析注释头。
func (s *DiskStore) Save(name string, typ Type, src string, match Match, cron string) (*Entry, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("script: 名字不能为空")
	}
	if !typ.Valid() {
		return nil, fmt.Errorf("script: 未知类型 %q", typ)
	}
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("script: 脚本内容不能为空")
	}
	e := &Entry{
		ID:      slug(name),
		Name:    name,
		Type:    typ,
		Enabled: true,
		Match:   match,
		Cron:    cron,
		Source:  src,
	}
	if old, err := s.Get(e.ID); err == nil && old != nil {
		e.Enabled, e.Order, e.Hits = old.Enabled, old.Order, old.Hits
	}
	if err := s.save(e); err != nil {
		return nil, err
	}
	return e, nil
}

func (s *DiskStore) path(id string) string { return filepath.Join(s.dir, slug(id)+".json") }

func (s *DiskStore) save(e *Entry) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(e.ID), data, 0o600)
}

// Get 按 ID 取；不存在返回 (nil, nil)。
func (s *DiskStore) Get(id string) (*Entry, error) {
	data, err := os.ReadFile(s.path(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("script: 条目 %s 损坏: %w", id, err)
	}
	return &e, nil
}

// List 全部条目按 Order 升序、同序号按 ID。
func (s *DiskStore) List() ([]*Entry, error) {
	matches, err := filepath.Glob(filepath.Join(s.dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []*Entry
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("script: 条目 %s 损坏: %w", p, err)
		}
		out = append(out, &e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Delete 删除；不存在报错。
func (s *DiskStore) Delete(id string) error {
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return fmt.Errorf("script: 脚本 %q 不存在", id)
	}
	return err
}

// SetEnabled 启用/停用。
func (s *DiskStore) SetEnabled(id string, enabled bool) (*Entry, error) {
	e, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, fmt.Errorf("script: 脚本 %q 不存在", id)
	}
	e.Enabled = enabled
	if err := s.save(e); err != nil {
		return nil, err
	}
	return e, nil
}

// SetOrder 重排。
func (s *DiskStore) SetOrder(ids []string) error {
	for i, id := range ids {
		e, err := s.Get(id)
		if err != nil {
			return err
		}
		if e == nil {
			return fmt.Errorf("script: 脚本 %q 不存在", id)
		}
		e.Order = i
		if err := s.save(e); err != nil {
			return err
		}
	}
	return nil
}

// IncHits 命中计数 +n（调试控制台"命中次数"）。
func (s *DiskStore) IncHits(id string, n int64) error {
	e, err := s.Get(id)
	if err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("script: 脚本 %q 不存在", id)
	}
	e.Hits += n
	return s.save(e)
}
