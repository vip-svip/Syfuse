package backup

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openZip 校验 zip 可读（测试辅助）。
func openZip(path string) (*zip.ReadCloser, error) { return zip.OpenReader(path) }

// rewriteZip 把 zip 里指定文件的内容替换后写出新包（清单原样保留 ——
// 制造"内容与 SHA 不符"的篡改包）。
func rewriteZip(src, dst, target, newContent string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	for _, zf := range zr.File {
		w, err := zw.Create(zf.Name)
		if err != nil {
			return err
		}
		if zf.Name == target {
			if _, err := io.Copy(w, bytes.NewReader([]byte(newContent))); err != nil {
				return err
			}
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, rc); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return zw.Close()
}

func setupData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte(content), 0o600)
	}
	write("config/syfuse.conf", "DNS_ENABLE=1\n")
	write("config/extra.conf", "X=1\n")
	write("scripts/a.js", "$done(1);")
	write("modules/m.json", `{"id":"m"}`)
	write("rules/rules.txt", "||ads.com^")
	return dir
}

// Create → 文件清单齐全；Restore 到新目录内容一致。
func TestBackupRoundTrip(t *testing.T) {
	data := setupData(t)
	zipPath, mf, err := Create(data, []string{"config", "scripts", "modules", "rules"}, "test-1.0")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(zipPath), "backup-") ||
		filepath.Dir(zipPath) != filepath.Join(data, "backups") {
		t.Fatalf("包路径异常: %s", zipPath)
	}
	if len(mf.Files) != 5 {
		t.Fatalf("应 4 个文件，得到 %d: %+v", len(mf.Files), mf.Files)
	}
	// 还原到新目录。
	restore := t.TempDir()
	back, err := Restore(restore, zipPath)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if back.Version != "test-1.0" || len(back.Files) != 5 {
		t.Fatalf("清单不符: %+v", back)
	}
	for _, want := range []struct{ rel, content string }{
		{"config/syfuse.conf", "DNS_ENABLE=1\n"},
		{"config/extra.conf", "X=1\n"},
		{"scripts/a.js", "$done(1);"},
		{"modules/m.json", `{"id":"m"}`},
		{"rules/rules.txt", "||ads.com^"},
	} {
		b, err := os.ReadFile(filepath.Join(restore, filepath.FromSlash(want.rel)))
		if err != nil || string(b) != want.content {
			t.Errorf("%s 还原不符: %v %q", want.rel, err, string(b))
		}
	}
}

// 篡改包内文件 → SHA-256 校验失败，不落盘。
func TestRestoreTamperDetection(t *testing.T) {
	data := setupData(t)
	zipPath, _, err := Create(data, []string{"config", "scripts"}, "v")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// zip 重写：改一个文件内容，保留旧清单 —— 制造"内容与 SHA 不符"。
	badPath := filepath.Join(t.TempDir(), "bad.zip")
	if err := rewriteZip(zipPath, badPath, "config/syfuse.conf", "DNS_ENABLE=0\nHACKED\n"); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	restore := t.TempDir()
	if _, err := Restore(restore, badPath); err == nil {
		t.Fatal("篡改后还原应报错")
	}
	// 半还原防护：报错后不应有任何被还原文件。
	if _, err := os.Stat(filepath.Join(restore, "config", "syfuse.conf")); !os.IsNotExist(err) {
		t.Fatal("校验失败后不应落盘")
	}
}

// 非法路径拒绝（绝对路径 / ..）。
func TestCreateRejectsEscape(t *testing.T) {
	data := t.TempDir()
	if _, _, err := Create(data, []string{"../escape"}, "v"); err == nil {
		t.Fatal(".. 路径应拒绝")
	}
	if _, _, err := Create(data, []string{"/etc"}, "v"); err == nil {
		t.Fatal("绝对路径应拒绝")
	}
	if _, _, err := Create(data, nil, "v"); err == nil {
		t.Fatal("空 dirs 应拒绝")
	}
}

// 缺 manifest 的 zip 不是合法备份包。
func TestRestoreNoManifest(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "fake.zip")
	if err := os.WriteFile(fake, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.TempDir(), fake); err == nil {
		t.Fatal("坏 zip 应报错")
	}
}

// 不存在的子目录跳过（不报错）。
func TestCreateSkipsMissingDirs(t *testing.T) {
	data := setupData(t)
	zipPath, mf, err := Create(data, []string{"config", "no-such-dir"}, "v")
	if err != nil {
		t.Fatalf("缺失目录应跳过: %v", err)
	}
	if len(mf.Files) != 2 {
		t.Fatalf("应只有 config 的 2 个文件: %+v", mf.Files)
	}
	_ = zipPath
}
