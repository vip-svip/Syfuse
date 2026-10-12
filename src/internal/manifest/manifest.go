// Package manifest 实现「还原清单」机制。
//
// 这是本项目的还原性硬约束（提示词 3.5）：
//   - 所有系统级改动必须登记到 /data/adb/syfuse/manifest/installed.json
//   - uninstall.sh 与 App 内「完全卸载」必须能还原全部改动
//   - **任何无法自动还原的操作不允许实现**
//
// 最后一条不是文档里的口号，而是本包的 API 契约：
// [Manifest.Add] 对 Restorable=false 的条目**直接返回错误**。
// 也就是说"写一个不可还原的操作进去"在代码层就做不到 ——
// 除非有人显式绕过这个包，而那会被 tests/run_restore_check.sh 抓到。
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SchemaVersion 是清单格式版本。读到更高版本必须拒绝，
// 因为新版本可能有旧代码不认识、因而无法还原的条目类型。
const SchemaVersion = 1

// Kind 是改动的类型。取值与提示词 10 部分的 InstalledManifest 定义一致。
type Kind string

const (
	KindMount    Kind = "mount"    // OverlayFS / bind mount
	KindCert     Kind = "cert"     // 系统 CA 证书
	KindHosts    Kind = "hosts"    // hosts 注入
	KindModule   Kind = "module"   // 模块目录
	KindAPK      Kind = "apk"      // 内置软件安装
	KindDir      Kind = "dir"      // 创建的目录
	KindFile     Kind = "file"     // 创建/修改的文件
	KindIptables Kind = "iptables" // 防火墙规则
	KindProp     Kind = "prop"     // 系统属性
	KindSELinux  Kind = "selinux"  // SELinux 上下文
)

// Valid 检查 Kind 是否是已知类型。
// 未知类型一律拒绝 —— 见 [Manifest.Add] 的说明。
func (k Kind) Valid() bool {
	switch k {
	case KindMount, KindCert, KindHosts, KindModule, KindAPK,
		KindDir, KindFile, KindIptables, KindProp, KindSELinux:
		return true
	}
	return false
}

// Entry 是一条改动记录。
type Entry struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// Target 是被改动的对象（路径、挂载点、规则标识）。
	Target string `json:"target"`
	// Source 是来源（模块内路径 / 生成方式 / 上游 URL）。用于"从哪来"。
	Source string `json:"source"`
	// Backup 是备份路径。还原时优先从这里恢复。
	Backup string `json:"backup,omitempty"`
	// Restorable 表示是否可自动还原。
	// 本包拒绝 Restorable=false 的条目 —— 不可还原的操作不允许实现。
	Restorable bool `json:"restorable"`
	// CreatedAt / UpdatedAt 是 RFC3339 时间戳。
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	// Note 是给用户看的说明（例如"证书有效期至 2036-01-01"）。
	Note string `json:"note,omitempty"`
}

// Manifest 是完整清单。
type Manifest struct {
	SchemaVersion int     `json:"schemaVersion"`
	ModuleID      string  `json:"moduleId"`
	ModuleVersion string  `json:"moduleVersion"`
	InstalledAt   string  `json:"installedAt"`
	UpdatedAt     string  `json:"updatedAt"`
	Entries       []Entry `json:"entries"`
}

// 错误值。调用方用 errors.Is 判断。
var (
	ErrNotRestorable = errors.New("manifest: 不可自动还原的操作不允许登记（还原性是硬约束）")
	ErrUnknownKind   = errors.New("manifest: 未知的改动类型")
	ErrSchemaTooNew  = errors.New("manifest: 清单版本高于本程序支持的版本")
	ErrDuplicateID   = errors.New("manifest: 条目 ID 重复")
	ErrNotFound      = errors.New("manifest: 条目不存在")
)

// New 创建一个空清单。
func New(moduleID, moduleVersion string) *Manifest {
	now := time.Now().UTC().Format(time.RFC3339)
	return &Manifest{
		SchemaVersion: SchemaVersion,
		ModuleID:      moduleID,
		ModuleVersion: moduleVersion,
		InstalledAt:   now,
		UpdatedAt:     now,
	}
}

// Load 从文件读取清单。文件不存在时返回一个空清单而不是错误 ——
// 首次安装时还没有清单，这是正常状态。
func Load(path, moduleID, moduleVersion string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(moduleID, moduleVersion), nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		// 清单损坏时**不静默重建**：那会让已有改动失去还原依据。
		// 返回错误让调用方决定（通常是拒绝继续，而不是丢掉还原信息）。
		return nil, fmt.Errorf("清单损坏，拒绝重建（以免丢失还原依据）: %w", err)
	}
	if m.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("%w（清单 %d > 本程序 %d）", ErrSchemaTooNew, m.SchemaVersion, SchemaVersion)
	}
	if m.Entries == nil {
		m.Entries = []Entry{}
	}
	return &m, nil
}

// Save 原子写入清单。
//
// 用"临时文件 + rename"而不是直接覆盖：卸载过程中若掉电，
// 半写的清单会让下次卸载无法还原 —— 那是不可接受的。
func (m *Manifest) Save(path string) error {
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Add 登记一条改动。
//
// **这是本包的硬约束执行点**：Restorable=false 或未知 Kind 一律拒绝。
// 调用方无法通过"先登记再想办法"绕过 —— 登记不进去。
func (m *Manifest) Add(e Entry) error {
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: %q", ErrUnknownKind, e.Kind)
	}
	if !e.Restorable {
		return fmt.Errorf("%w: kind=%s target=%s", ErrNotRestorable, e.Kind, e.Target)
	}
	if strings.TrimSpace(e.Target) == "" {
		return fmt.Errorf("manifest: Target 不能为空（否则无法定位要还原什么）")
	}
	for _, x := range m.Entries {
		if x.ID == e.ID {
			return fmt.Errorf("%w: %s", ErrDuplicateID, e.ID)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if e.CreatedAt == "" {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	m.Entries = append(m.Entries, e)
	return nil
}

// Remove 按 ID 删除条目（还原成功后调用）。
func (m *Manifest) Remove(id string) error {
	for i, e := range m.Entries {
		if e.ID == id {
			m.Entries = append(m.Entries[:i], m.Entries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Get 按 ID 取条目。
func (m *Manifest) Get(id string) (Entry, bool) {
	for _, e := range m.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// ByKind 按类型筛选，顺序稳定（按 ID 排序）。
func (m *Manifest) ByKind(k Kind) []Entry {
	var out []Entry
	for _, e := range m.Entries {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Problem 描述一条校验发现的问题。
type Problem struct {
	EntryID string `json:"entryId"`
	Reason  string `json:"reason"`
}

// Verify 校验清单自身的可还原性。
//
// 检查项：
//  1. 所有条目的 Kind 已知、Restorable 为真（防手工编辑过的清单混入不可还原项）；
//  2. Target 非空；
//  3. 声明了 Backup 的条目，备份文件必须真实存在 ——
//     否则"可还原"是假的：还原时才发现备份不在，那就晚了。
//
// 返回的问题列表为空表示清单可以安全执行还原。
func (m *Manifest) Verify() []Problem {
	var problems []Problem
	seen := map[string]bool{}
	for _, e := range m.Entries {
		if seen[e.ID] {
			problems = append(problems, Problem{e.ID, "ID 重复"})
		}
		seen[e.ID] = true
		if !e.Kind.Valid() {
			problems = append(problems, Problem{e.ID, fmt.Sprintf("未知类型 %q", e.Kind)})
		}
		if !e.Restorable {
			problems = append(problems, Problem{e.ID, "标记为不可还原"})
		}
		if strings.TrimSpace(e.Target) == "" {
			problems = append(problems, Problem{e.ID, "Target 为空"})
		}
		if e.Backup != "" {
			if _, err := os.Stat(e.Backup); err != nil {
				problems = append(problems, Problem{e.ID, fmt.Sprintf("备份不存在: %s", e.Backup)})
			}
		}
	}
	return problems
}

// RestorePlan 是还原计划：按"后进先出"排列的操作序列。
//
// 为什么是逆序：挂载要后于目录创建被卸载，证书要后于 hosts 被移除。
// 正序还原会在依赖关系上出错，所以这里固定为逆序，
// 避免每个调用方各自决定顺序（那一定会有人搞错）。
func (m *Manifest) RestorePlan() []Entry {
	out := make([]Entry, len(m.Entries))
	copy(out, m.Entries)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Summary 返回按类型统计的条目数，用于 App 的"还原清单检查"页面。
func (m *Manifest) Summary() map[string]int {
	out := map[string]int{}
	for _, e := range m.Entries {
		out[string(e.Kind)]++
	}
	return out
}
