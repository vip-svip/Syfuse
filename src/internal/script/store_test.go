package script

import (
	"path/filepath"
	"testing"
)

func openDisk(t *testing.T) *DiskStore {
	t.Helper()
	s, err := OpenDiskStore(filepath.Join(t.TempDir(), "scripts"))
	if err != nil {
		t.Fatalf("OpenDiskStore: %v", err)
	}
	return s
}

// Save → Get；同名 = 更新保留 enabled/order/hits。
func TestDiskStore_SaveGet(t *testing.T) {
	s := openDisk(t)
	e, err := s.Save("我的脚本", TypeHTTP, "$done(1);", Match{URLRegex: "a"}, "")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if e.ID != "script" { // 全非 ASCII → 全替换后 Trim 空 → 兜底名
		t.Fatalf("slug = %q", e.ID)
	}
	if _, err := s.SetEnabled(e.ID, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	e2, err := s.Save("我的脚本", TypeHTTP, "$done(2);", Match{}, "")
	if err != nil {
		t.Fatalf("再保存: %v", err)
	}
	if e2.Source != "$done(2);" || e2.Enabled {
		t.Fatalf("更新语义破坏: %+v", e2)
	}
}

// 缺名/缺类型/缺内容拒绝。
func TestDiskStore_Invalid(t *testing.T) {
	s := openDisk(t)
	if _, err := s.Save("  ", TypeHTTP, "x;", Match{}, ""); err == nil {
		t.Fatal("空名应报错")
	}
	if _, err := s.Save("a", Type("nope"), "x;", Match{}, ""); err == nil {
		t.Fatal("坏类型应报错")
	}
	if _, err := s.Save("a", TypeHTTP, "  ", Match{}, ""); err == nil {
		t.Fatal("空内容应报错")
	}
}

// List 排序 / SetOrder / Delete / IncHits。
func TestDiskStore_ListOrderDelete(t *testing.T) {
	s := openDisk(t)
	s.Save("Alpha", TypeGeneral, "$done(1);", Match{}, "")
	s.Save("Beta", TypeCron, "$done(2);", Match{}, "*/5 * * * *")
	list, err := s.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("List: %v %d", err, len(list))
	}
	if err := s.SetOrder([]string{"Beta", "Alpha"}); err != nil {
		t.Fatalf("SetOrder: %v", err)
	}
	list, _ = s.List()
	if list[0].ID != "Beta" {
		t.Fatalf("排序: %v", list[0].ID)
	}
	if list[0].Cron != "*/5 * * * *" {
		t.Fatalf("cron 字段丢失: %+v", list[0])
	}
	if err := s.IncHits("Beta", 3); err != nil {
		t.Fatalf("IncHits: %v", err)
	}
	e, _ := s.Get("Beta")
	if e.Hits != 3 {
		t.Fatalf("Hits = %d", e.Hits)
	}
	if err := s.Delete("Nope"); err == nil {
		t.Fatal("删不存在应报错")
	}
	if err := s.Delete("Alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := s.Get("Alpha"); got != nil {
		t.Fatal("删除后应 nil")
	}
}

// 空目录 List 返回空。
func TestDiskStore_Empty(t *testing.T) {
	s := openDisk(t)
	list, err := s.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("空存储: %v %d", err, len(list))
	}
}
