// Package backup 实现 M13"备份还原"（规范 6.3：一键导出/恢复
// config、rules、scripts、modules）。
//
// 格式：标准 ZIP（Go stdlib archive/zip，零第三方依赖），内含：
//
//	backup-manifest.json   元数据（版本/时间/文件清单+SHA-256）
//	<相对路径>              数据目录内的实际文件
//
// 还原时先校验 manifest 再逐文件写入，任何文件校验失败即中止
// （不留下半还原状态 —— 先收集全部校验结果再落盘）。
package backup

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Manifest 是备份包元数据。
type Manifest struct {
	Version   string      `json:"version"` // 产生备份的 syfused 版本
	CreatedAt string      `json:"created_at"`
	Dirs      []string    `json:"dirs"` // 备份了哪些子目录
	Files     []FileEntry `json:"files"`
}

// FileEntry 是单个文件的清单项。
type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// ManifestName 是包内元数据文件名（还原时用于识别合法备份包）。
const ManifestName = "backup-manifest.json"

// Create 把 dataDir 下 dirs 列出的子目录打包成 zip（写到 dest 目录，
// 文件名 backup-<时间戳>.zip），返回包路径与清单。
// dirs 里的相对路径必须不能逃出 dataDir（拒绝 ".." 与绝对路径）。
func Create(dataDir string, dirs []string, version string) (string, *Manifest, error) {
	if dataDir == "" {
		return "", nil, fmt.Errorf("backup: 数据目录为空")
	}
	if len(dirs) == 0 {
		return "", nil, fmt.Errorf("backup: 至少选择一个要备份的目录")
	}
	m := &Manifest{
		Version:   version,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Dirs:      dirs,
	}
	for _, d := range dirs {
		if err := checkRelative(d); err != nil {
			return "", nil, err
		}
		root := filepath.Join(dataDir, filepath.FromSlash(d))
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil // 目录不存在 = 没有可备份内容，跳过
				}
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dataDir, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			sum, size, err := hashFile(path)
			if err != nil {
				return err
			}
			m.Files = append(m.Files, FileEntry{Path: rel, Size: size, SHA256: sum})
			return nil
		})
		if err != nil {
			return "", nil, fmt.Errorf("backup: 遍历 %s: %w", d, err)
		}
	}
	if len(m.Files) == 0 {
		return "", nil, fmt.Errorf("backup: 选定目录没有任何文件可备份")
	}

	// 备份包固定写到 <dataDir>/backups/（与被备份数据同盘，天然自包含）。
	outDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", nil, err
	}
	out := filepath.Join(outDir, "backup-"+time.Now().UTC().Format("20060102-150405")+".zip")
	f, err := os.Create(out)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	// manifest 先写（放包首）。
	mj, _ := json.MarshalIndent(m, "", "  ")
	if w, err := zw.Create(ManifestName); err == nil {
		w.Write(mj)
	}
	for _, fe := range m.Files {
		w, err := zw.Create(fe.Path)
		if err != nil {
			return "", nil, err
		}
		src, err := os.Open(filepath.Join(dataDir, filepath.FromSlash(fe.Path)))
		if err != nil {
			return "", nil, err
		}
		_, err = io.Copy(w, src)
		src.Close()
		if err != nil {
			return "", nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return "", nil, err
	}
	return out, m, nil
}

// Restore 从备份包还原到 dataDir。逐文件先校验 SHA-256，
// 全部校验通过后才写盘（防半还原）。
func Restore(dataDir, zipPath string) (*Manifest, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("backup: 打开备份包: %w", err)
	}
	defer zr.Close()

	var mf *Manifest
	contents := map[string][]byte{}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if zf.Name == ManifestName {
			if err := json.Unmarshal(data, &mf); err != nil {
				return nil, fmt.Errorf("backup: 清单损坏: %w", err)
			}
			continue
		}
		contents[filepath.ToSlash(zf.Name)] = data
	}
	if mf == nil {
		return nil, fmt.Errorf("backup: 不是合法备份包（缺 %s）", ManifestName)
	}
	// 全量校验。
	for _, fe := range mf.Files {
		data, ok := contents[fe.Path]
		if !ok {
			return nil, fmt.Errorf("backup: 包内缺文件 %s", fe.Path)
		}
		if sum := hashBytes(data); sum != fe.SHA256 {
			return nil, fmt.Errorf("backup: %s 校验失败（SHA-256 不符）", fe.Path)
		}
	}
	// 校验通过：写盘。
	for _, fe := range mf.Files {
		if err := checkRelative(fe.Path); err != nil {
			return nil, err
		}
		dst := filepath.Join(dataDir, filepath.FromSlash(fe.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, contents[fe.Path], 0o600); err != nil {
			return nil, err
		}
	}
	return mf, nil
}

// --- helpers ---

func checkRelative(rel string) error {
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") ||
		filepath.IsAbs(filepath.FromSlash(rel)) {
		return fmt.Errorf("backup: 非法路径 %q（不允许绝对路径或 ..）", rel)
	}
	return nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
