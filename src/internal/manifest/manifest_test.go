package manifest

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func tmpManifest(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "manifest", "installed.json")
}

func okEntry(id string) Entry {
	return Entry{ID: id, Kind: KindFile, Target: "/data/adb/syfuse/" + id, Source: "module/x", Restorable: true}
}

// ---------------------------------------------------------------------------
// 硬约束：不可还原的操作登记不进来
// ---------------------------------------------------------------------------

func TestAdd_RejectsNonRestorable(t *testing.T) {
	m := New("syfuse", "0.1.0")
	e := okEntry("x")
	e.Restorable = false

	err := m.Add(e)
	if err == nil {
		t.Fatal("不可还原的条目必须被拒绝 —— 这是提示词 3.5 的硬约束")
	}
	if !errors.Is(err, ErrNotRestorable) {
		t.Errorf("错误类型应为 ErrNotRestorable，实际 %v", err)
	}
	if len(m.Entries) != 0 {
		t.Errorf("拒绝后不应留下条目，实际 %d 条", len(m.Entries))
	}
}

func TestAdd_RejectsUnknownKind(t *testing.T) {
	m := New("syfuse", "0.1.0")
	e := okEntry("x")
	e.Kind = Kind("teleport")

	if err := m.Add(e); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("未知类型必须被拒绝，实际 %v", err)
	}
}

func TestAdd_RejectsEmptyTarget(t *testing.T) {
	m := New("syfuse", "0.1.0")
	e := okEntry("x")
	e.Target = "   "

	// Target 为空就无法定位要还原什么，必须拒绝
	if err := m.Add(e); err == nil {
		t.Error("空 Target 必须被拒绝")
	}
}

func TestAdd_RejectsDuplicateID(t *testing.T) {
	m := New("syfuse", "0.1.0")
	if err := m.Add(okEntry("dup")); err != nil {
		t.Fatalf("首次登记失败: %v", err)
	}
	if err := m.Add(okEntry("dup")); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("重复 ID 必须被拒绝，实际 %v", err)
	}
}

func TestAdd_AcceptsAllKnownKinds(t *testing.T) {
	kinds := []Kind{KindMount, KindCert, KindHosts, KindModule, KindAPK,
		KindDir, KindFile, KindIptables, KindProp, KindSELinux}
	m := New("syfuse", "0.1.0")
	for i, k := range kinds {
		e := okEntry(string(rune('a' + i)))
		e.Kind = k
		if err := m.Add(e); err != nil {
			t.Errorf("已知类型 %s 应被接受，实际 %v", k, err)
		}
	}
	if len(m.Entries) != len(kinds) {
		t.Errorf("条目数 = %d, 期望 %d", len(m.Entries), len(kinds))
	}
}

// ---------------------------------------------------------------------------
// 持久化
// ---------------------------------------------------------------------------

func TestLoad_MissingFileReturnsEmpty(t *testing.T) {
	m, err := Load(tmpManifest(t), "syfuse", "0.1.0")
	if err != nil {
		t.Fatalf("文件不存在应返回空清单而不是错误: %v", err)
	}
	if len(m.Entries) != 0 || m.ModuleID != "syfuse" {
		t.Errorf("空清单内容不对: %+v", m)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := tmpManifest(t)
	m := New("syfuse", "0.1.0")
	if err := m.Add(okEntry("a")); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(okEntry("b")); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(path); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	back, err := Load(path, "syfuse", "0.1.0")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(back.Entries) != 2 {
		t.Fatalf("条目数 = %d, 期望 2", len(back.Entries))
	}
	if back.Entries[0].ID != "a" || back.Entries[1].ID != "b" {
		t.Errorf("顺序应保持: %v", []string{back.Entries[0].ID, back.Entries[1].ID})
	}
	if back.Entries[0].Target != m.Entries[0].Target {
		t.Errorf("Target 不一致: %q vs %q", back.Entries[0].Target, m.Entries[0].Target)
	}
}

func TestSave_AtomicNoTempLeftover(t *testing.T) {
	path := tmpManifest(t)
	m := New("syfuse", "0.1.0")
	_ = m.Add(okEntry("a"))
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("保存后不应留下 .tmp 文件（原子写必须 rename 掉）")
	}
}

func TestLoad_CorruptFileIsNotSilentlyRebuilt(t *testing.T) {
	path := tmpManifest(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{这不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 关键行为：损坏的清单**不能**被静默重建为空的 ——
	// 那会让已有改动失去还原依据，等于悄悄丢掉了还原能力。
	if _, err := Load(path, "syfuse", "0.1.0"); err == nil {
		t.Fatal("损坏的清单必须报错，不得静默重建")
	}
}

func TestLoad_RejectsHigherSchemaVersion(t *testing.T) {
	path := tmpManifest(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// 手写一份高版本清单
	doc := map[string]any{
		"schemaVersion": SchemaVersion + 1,
		"moduleId":      "syfuse",
		"entries":       []any{},
	}
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, "syfuse", "0.1.0"); !errors.Is(err, ErrSchemaTooNew) {
		t.Errorf("更高 schemaVersion 必须被拒绝，实际 %v", err)
	}
}

// ---------------------------------------------------------------------------
// 校验与还原计划
// ---------------------------------------------------------------------------

func TestVerify_CatchesMissingBackup(t *testing.T) {
	m := New("syfuse", "0.1.0")
	e := okEntry("withbackup")
	e.Backup = "/nonexistent/definitely/not/here"
	if err := m.Add(e); err != nil {
		t.Fatal(err)
	}

	probs := m.Verify()
	if len(probs) != 1 {
		t.Fatalf("应报告 1 个问题（备份不存在），实际 %d: %+v", len(probs), probs)
	}
	// "可还原"若备份不在就是假的 —— 必须在执行还原**之前**发现
	if probs[0].EntryID != "withbackup" {
		t.Errorf("问题条目 ID = %q", probs[0].EntryID)
	}
}

func TestVerify_CatchesHandEditedNonRestorable(t *testing.T) {
	// 模拟有人手工把 Restorable 改成 false 写进文件
	m := New("syfuse", "0.1.0")
	m.Entries = append(m.Entries, Entry{
		ID: "hand", Kind: KindFile, Target: "/x", Source: "s", Restorable: false,
	})
	probs := m.Verify()
	if len(probs) == 0 {
		t.Fatal("手工注入的不可还原条目必须被 Verify 抓到")
	}
}

func TestVerify_CatchesUnknownKindAndEmptyTarget(t *testing.T) {
	m := New("syfuse", "0.1.0")
	m.Entries = append(m.Entries,
		Entry{ID: "k", Kind: Kind("bogus"), Target: "/x", Restorable: true},
		Entry{ID: "t", Kind: KindFile, Target: "", Restorable: true},
		Entry{ID: "k", Kind: KindFile, Target: "/y", Restorable: true}, // 重复 ID
	)
	probs := m.Verify()
	if len(probs) != 3 {
		t.Errorf("应报告 3 个问题，实际 %d: %+v", len(probs), probs)
	}
}

func TestVerify_CleanManifestHasNoProblems(t *testing.T) {
	m := New("syfuse", "0.1.0")
	for _, id := range []string{"a", "b", "c"} {
		if err := m.Add(okEntry(id)); err != nil {
			t.Fatal(err)
		}
	}
	if probs := m.Verify(); len(probs) != 0 {
		t.Errorf("干净清单不应有问题: %+v", probs)
	}
}

func TestRestorePlan_IsReverseOrder(t *testing.T) {
	m := New("syfuse", "0.1.0")
	for _, id := range []string{"first", "second", "third"} {
		if err := m.Add(okEntry(id)); err != nil {
			t.Fatal(err)
		}
	}
	plan := m.RestorePlan()
	got := []string{plan[0].ID, plan[1].ID, plan[2].ID}
	want := []string{"third", "second", "first"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("还原顺序应为逆序（后进先出），实际 %v", got)
		}
	}
	// 原清单不能被改动
	if m.Entries[0].ID != "first" {
		t.Error("RestorePlan 不应修改原清单顺序")
	}
}

func TestSummary_CountsByKind(t *testing.T) {
	m := New("syfuse", "0.1.0")
	a := okEntry("a")
	a.Kind = KindCert
	b := okEntry("b")
	b.Kind = KindCert
	c := okEntry("c")
	c.Kind = KindHosts
	for _, e := range []Entry{a, b, c} {
		if err := m.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	s := m.Summary()
	if s["cert"] != 2 || s["hosts"] != 1 {
		t.Errorf("统计不对: %+v", s)
	}
}

func TestRemoveAndGet(t *testing.T) {
	m := New("syfuse", "0.1.0")
	_ = m.Add(okEntry("a"))
	_ = m.Add(okEntry("b"))

	if e, ok := m.Get("a"); !ok || e.ID != "a" {
		t.Errorf("Get 失败")
	}
	if _, ok := m.Get("zzz"); ok {
		t.Errorf("不存在的 ID 应返回 false")
	}
	if err := m.Remove("a"); err != nil {
		t.Errorf("Remove 失败: %v", err)
	}
	if len(m.Entries) != 1 || m.Entries[0].ID != "b" {
		t.Errorf("Remove 后内容不对: %+v", m.Entries)
	}
	if err := m.Remove("a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复 Remove 应返回 ErrNotFound，实际 %v", err)
	}
}

func TestByKind_StableOrder(t *testing.T) {
	m := New("syfuse", "0.1.0")
	for _, id := range []string{"z", "a", "m"} {
		e := okEntry(id)
		e.Kind = KindCert
		if err := m.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	got := m.ByKind(KindCert)
	ids := []string{got[0].ID, got[1].ID, got[2].ID}
	if ids[0] != "a" || ids[1] != "m" || ids[2] != "z" {
		t.Errorf("ByKind 应按 ID 排序，实际 %v", ids)
	}
}
