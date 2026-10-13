package module

import (
	"path/filepath"
	"strings"
	"testing"
)

const modA = `#!name=Alpha
#!desc=模块A
[Rule]
DOMAIN-SUFFIX,a.com,PROXY
`

const modB = `#!name=Alpha
#!desc=更新后的A
[Rule]
DOMAIN-SUFFIX,a2.com,PROXY
`

const modC = `#!name=Beta 模块
#!desc=中文 slug 测试
[Host]
b.com = 1.2.3.4
`

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "modules"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return s
}

// 导入 → Get → 字段正确；同名再导入 = 更新（保留 enabled/order）。
func TestStore_ImportAndGet(t *testing.T) {
	s := openStore(t)
	e, err := s.Import(modA)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if e.ID != "Alpha" || e.Name != "Alpha" || !e.Enabled {
		t.Fatalf("条目错误: %+v", e)
	}
	got, err := s.Get("Alpha")
	if err != nil || got == nil {
		t.Fatalf("Get: %v %v", got, err)
	}
	if got.Text != modA {
		t.Fatal("原文应无损保存")
	}

	// 更新语义。
	if _, err := s.SetEnabled("Alpha", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	e2, err := s.Import(modB)
	if err != nil {
		t.Fatalf("再导入: %v", err)
	}
	if e2.Text != modB {
		t.Fatal("更新应替换原文")
	}
	if e2.Enabled {
		t.Fatal("更新应保留停用状态")
	}
}

// 中文与空格名 → 安全 slug。
func TestStore_Slug(t *testing.T) {
	s := openStore(t)
	e, err := s.Import(modC)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if e.ID != "Beta" { // "Beta 模块" → 非 ASCII 全部替换成 '-' 再 Trim
		t.Fatalf("slug = %q", e.ID)
	}
	if !strings.HasPrefix(e.ID, "Beta") || strings.ContainsAny(e.ID, " /\\") {
		t.Fatalf("slug 不安全: %q", e.ID)
	}
}

// List 按 Order 排序；SetOrder 重排；Delete 删除；不存在报错。
func TestStore_ListOrderDelete(t *testing.T) {
	s := openStore(t)
	s.Import(modA) // Alpha, order 0
	s.Import(`#!name=Gamma
#!desc=g
`)
	s.Import(modC) // Beta
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("应 3 条，得到 %d", len(list))
	}
	// 同 order=0，按 ID 排：Alpha, Beta, Gamma。
	if list[0].ID != "Alpha" || list[1].ID != "Beta" || list[2].ID != "Gamma" {
		t.Fatalf("排序错误: %v", ids(list))
	}
	if err := s.SetOrder([]string{"Gamma", "Alpha", "Beta"}); err != nil {
		t.Fatalf("SetOrder: %v", err)
	}
	list, _ = s.List()
	if list[0].ID != "Gamma" || list[2].ID != "Beta" {
		t.Fatalf("重排失败: %v", ids(list))
	}
	if err := s.Delete("Nope"); err == nil {
		t.Fatal("删除不存在的模块应报错")
	}
	if err := s.Delete("Alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := s.Get("Alpha"); got != nil {
		t.Fatal("删除后 Get 应为 nil")
	}
}

// MergeEnabled：只合并启用模块，顺序跟随 Store。
func TestStore_MergeEnabled(t *testing.T) {
	s := openStore(t)
	s.Import(`#!name=First
#!desc=1
[Rule]
DOMAIN-SUFFIX,first.com,PROXY
`)
	s.Import(`#!name=Second
#!desc=2
[Rule]
DOMAIN-SUFFIX,second.com,DIRECT
`)
	res, err := s.MergeEnabled()
	if err != nil {
		t.Fatalf("MergeEnabled: %v", err)
	}
	if len(res.Conflicts) != 0 || !strings.Contains(res.Text, "first.com") {
		t.Fatalf("合并错误:\n%s", res.Text)
	}
	// 停用 First 后合并结果只剩 Second。
	if _, err := s.SetEnabled("First", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	res, _ = s.MergeEnabled()
	if strings.Contains(res.Text, "first.com") || !strings.Contains(res.Text, "second.com") {
		t.Fatalf("停用模块不应参与合并:\n%s", res.Text)
	}
}

// 缺 name 的文本拒绝导入。
func TestStore_ImportNoName(t *testing.T) {
	s := openStore(t)
	if _, err := s.Import("[Rule]\nfoo=bar\n"); err == nil {
		t.Fatal("缺 #!name= 应报错")
	}
}

// 空目录 List 返回空而非错误。
func TestStore_EmptyList(t *testing.T) {
	s := openStore(t)
	list, err := s.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("空存储: %v %d", err, len(list))
	}
}

func ids(list []*Entry) []string {
	var out []string
	for _, e := range list {
		out = append(out, e.ID)
	}
	return out
}
