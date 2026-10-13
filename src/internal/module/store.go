// store.go 是模块的持久化存储：每个模块一个 JSON 文件，
// 文件名 = 模块 ID（名称 slug）。目录即索引，无需单独索引文件——
// 少一个可损坏的中间层。
package module

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Entry 是存储中的一个模块条目：原文 + 开关 + 排序。
type Entry struct {
	ID      string `json:"id"`   // 名称 slug，即文件名
	Name    string `json:"name"` // #!name=
	Enabled bool   `json:"enabled"`
	Order   int    `json:"order"` // 用户手动排序
	Text    string `json:"text"`  // 模块原文（Parse/Serialize 往返无损）
}

// Store 落盘在 dir（如 <data>/modules）。
type Store struct {
	dir string
}

// OpenStore 打开（不存在则创建）模块存储目录。
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("module: 存储目录为空")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("module: 创建存储目录: %w", err)
	}
	return &Store{dir: dir}, nil
}

// slug 把模块名转成文件名安全的 ID。
var slugRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func slug(name string) string {
	s := slugRe.ReplaceAllString(strings.TrimSpace(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "module"
	}
	return s
}

// Import 解析并保存模块文本。同名模块覆盖（更新语义，失败保留旧版
// 由调用方保证：先不删旧文件再写入）。返回条目。
func (s *Store) Import(text string) (*Entry, error) {
	m, pe := Parse(text)
	if m.Name == "" {
		return nil, fmt.Errorf("module: 缺少 #!name=，无法导入")
	}
	e := &Entry{
		ID:      slug(m.Name),
		Name:    m.Name,
		Enabled: true,
		Text:    text,
	}
	// 同名导入 = 更新：保留原有 enabled/order。
	if old, err := s.Get(e.ID); err == nil && old != nil {
		e.Enabled, e.Order = old.Enabled, old.Order
	}
	if err := s.save(e); err != nil {
		return nil, err
	}
	_ = pe // 告警由调用方按需取 Parse 自己看；导入不因告警失败
	return e, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, slug(id)+".json")
}

func (s *Store) save(e *Entry) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(e.ID), data, 0o600)
}

// Get 按 ID 取条目；不存在返回 (nil, nil)。
func (s *Store) Get(id string) (*Entry, error) {
	data, err := os.ReadFile(s.path(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("module: 条目 %s 损坏: %w", id, err)
	}
	return &e, nil
}

// List 返回全部条目，按 Order 升序、同序号按 ID。
func (s *Store) List() ([]*Entry, error) {
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
			return nil, fmt.Errorf("module: 条目 %s 损坏: %w", p, err)
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

// Delete 按 ID 删除。
func (s *Store) Delete(id string) error {
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return fmt.Errorf("module: 模块 %q 不存在", id)
	}
	return err
}

// SetEnabled 启用/停用模块。
func (s *Store) SetEnabled(id string, enabled bool) (*Entry, error) {
	e, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, fmt.Errorf("module: 模块 %q 不存在", id)
	}
	e.Enabled = enabled
	if err := s.save(e); err != nil {
		return nil, err
	}
	return e, nil
}

// SetOrder 重排模块（ids 的顺序即新顺序）。
func (s *Store) SetOrder(ids []string) error {
	for i, id := range ids {
		e, err := s.Get(id)
		if err != nil {
			return err
		}
		if e == nil {
			return fmt.Errorf("module: 模块 %q 不存在", id)
		}
		e.Order = i
		if err := s.save(e); err != nil {
			return err
		}
	}
	return nil
}

// MergeEnabled 把所有启用模块按当前顺序合并（冲突检测见 Merge）。
// 预览导入差异时用 MergeAll(true) 也走这条路。
func (s *Store) MergeEnabled() (*MergeResult, error) {
	entries, err := s.List()
	if err != nil {
		return nil, err
	}
	var mods []*Module
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		m, _ := Parse(e.Text)
		if m != nil {
			mods = append(mods, m)
		}
	}
	return Merge(mods)
}
