package mitm

import (
	"crypto/ecdsa"
	"crypto/x509"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 生成一把测试 CA 的快捷方式。
func genCA(t *testing.T, commonName string, alg KeyAlgorithm) *CA {
	t.Helper()
	ca, err := GenerateCA(commonName, alg, 0)
	if err != nil {
		t.Fatalf("GenerateCA(%s): %v", commonName, err)
	}
	return ca
}

// 两种算法都能生成：IsCA、CertSign KeyUsage、CN、Organization 就位。
func TestCA_GenerateRSA(t *testing.T) {
	ca := genCA(t, "Syfuse Root RSA", AlgRSA2048)
	if !ca.Cert.IsCA {
		t.Fatal("根 CA 必须 IsCA=true")
	}
	if ca.Cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatal("缺 CertSign KeyUsage")
	}
	if ca.Cert.Subject.CommonName != "Syfuse Root RSA" {
		t.Fatalf("CN = %q", ca.Cert.Subject.CommonName)
	}
	if len(ca.Cert.Subject.Organization) != 1 || ca.Cert.Subject.Organization[0] != "Syfuse" {
		t.Fatalf("Organization = %v", ca.Cert.Subject.Organization)
	}
}

func TestCA_GenerateECDSA(t *testing.T) {
	ca := genCA(t, "Syfuse Root EC", AlgECDSAP256)
	if _, ok := ca.Key.Public().(*ecdsa.PublicKey); !ok {
		t.Fatal("EC 算法应产生 EC 公钥")
	}
}

// 非法输入：空 CN、未知算法。
func TestCA_GenerateInvalid(t *testing.T) {
	if _, err := GenerateCA("", AlgRSA2048, 0); err == nil {
		t.Fatal("空 CN 应报错")
	}
	if _, err := GenerateCA("x", KeyAlgorithm(99), 0); err == nil {
		t.Fatal("未知算法应报错")
	}
}

// 有效期：缺省 10 年；自定义生效（含 -1h 时钟回拨容忍）。
func TestCA_Validity(t *testing.T) {
	ca := genCA(t, "V", AlgRSA2048)
	dur := ca.Cert.NotAfter.Sub(ca.Cert.NotBefore)
	// NotBefore 回拨 1h，所以跨度是 10 年 + 1h。
	if dur < DefaultCAValidity || dur > DefaultCAValidity+time.Hour {
		t.Fatalf("缺省有效期 = %v, want [%v, %v]", dur, DefaultCAValidity, DefaultCAValidity+time.Hour)
	}
	ca2, err := GenerateCA("V2", AlgRSA2048, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	if d := ca2.Cert.NotAfter.Sub(ca2.Cert.NotBefore); d != 365*24*time.Hour+time.Hour {
		t.Fatalf("自定义有效期 = %v, want %v", d, 365*24*time.Hour+time.Hour)
	}
	// NotBefore 容忍 1 小时回拨。
	if time.Until(ca2.Cert.NotBefore) > time.Hour {
		t.Fatalf("NotBefore = %v，回拨窗口异常", ca2.Cert.NotBefore)
	}
}

// 序列号随机：两枚 CA 序列号不同。
func TestCA_SerialUnique(t *testing.T) {
	a := genCA(t, "S1", AlgRSA2048)
	b := genCA(t, "S2", AlgRSA2048)
	if a.Cert.SerialNumber.Cmp(b.Cert.SerialNumber) == 0 {
		t.Fatal("两次生成的序列号不应相同")
	}
}

// 指纹：同一 CA 重复调用稳定；不同 CA 不同；Display 带前缀。
func TestCA_Fingerprint(t *testing.T) {
	ca := genCA(t, "F", AlgRSA2048)
	if ca.Fingerprint() != ca.Fingerprint() {
		t.Fatal("指纹应稳定")
	}
	if !strings.HasPrefix(ca.FingerprintDisplay(), "SHA256:") {
		t.Fatalf("Display = %q", ca.FingerprintDisplay())
	}
	if strings.TrimPrefix(ca.FingerprintDisplay(), "SHA256:") != ca.Fingerprint() {
		t.Fatal("Display 应为前缀 + Fingerprint")
	}
	other := genCA(t, "F", AlgRSA2048)
	if ca.Fingerprint() == other.Fingerprint() {
		t.Fatal("不同 CA 的 SPKI 指纹不应相同")
	}
}

// PEM 往返：CertPEM/KeyPEM → LoadCA 得回等价 CA（指纹一致、CN 一致）。
func TestCA_LoadCARoundTrip(t *testing.T) {
	for _, alg := range []KeyAlgorithm{AlgRSA2048, AlgECDSAP256} {
		ca := genCA(t, "RT-"+alg.String(), alg)
		back, err := LoadCA(ca.CertPEM(), ca.KeyPEM())
		if err != nil {
			t.Fatalf("%s: LoadCA: %v", alg, err)
		}
		if back.Fingerprint() != ca.Fingerprint() {
			t.Fatalf("%s: 指纹不一致", alg)
		}
		if back.Cert.Subject.CommonName != ca.Cert.Subject.CommonName {
			t.Fatalf("%s: CN 不一致", alg)
		}
	}
}

// 坏输入：坏 PEM、错块类型、非 CA 证书、key/cert 不配对、被换的证书。
func TestCA_LoadCAInvalid(t *testing.T) {
	ca := genCA(t, "Bad", AlgRSA2048)
	other := genCA(t, "Bad2", AlgRSA2048)

	cases := []struct {
		name       string
		cert, key  []byte
		wantSubstr string
	}{
		{"证书非PEM", []byte("garbage"), ca.KeyPEM(), "PEM"},
		{"证书块类型错", ca.KeyPEM(), ca.KeyPEM(), "块类型"},
		{"私钥非PEM", ca.CertPEM(), []byte("junk"), "PEM"},
		{"key证书不配对", ca.CertPEM(), other.KeyPEM(), "不匹配"},
		{"证书被调包", other.CertPEM(), ca.KeyPEM(), "不匹配"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadCA(tc.cert, tc.key)
			if err == nil {
				t.Fatalf("%s: 应报错", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("%s: 错误文案缺 %q: %v", tc.name, tc.wantSubstr, err)
			}
		})
	}
}

// Save → Load 往返；文件都在；Windows 之外校验私钥 0600。
func TestCA_SaveLoad(t *testing.T) {
	ca := genCA(t, "Disk", AlgRSA2048)
	dir := t.TempDir()
	certPath, keyPath, err := ca.Save(dir, "root")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !strings.HasSuffix(certPath, "root.pem") ||
		!strings.HasSuffix(keyPath, "root-key.pem") {
		t.Fatalf("路径错误: %s %s", certPath, keyPath)
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(keyPath); err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("私钥权限 = %v, want 0600", st.Mode().Perm())
		}
	}
	back, err := Load(dir, "root")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if back.Fingerprint() != ca.Fingerprint() {
		t.Fatal("落盘往返指纹不一致")
	}
	// Save 缺参。
	if _, _, err := ca.Save("", "x"); err == nil {
		t.Fatal("空目录应报错")
	}
	if _, _, err := ca.Save(dir, ""); err == nil {
		t.Fatal("空文件名应报错")
	}
	// Load 不存在。
	if _, err := Load(t.TempDir(), "nope"); err == nil {
		t.Fatal("Load 不存在应报错")
	}
}

// PEM 返回值是拷贝：调用方改坏不影响 CA 内部状态。
func TestCA_PEMDefensiveCopy(t *testing.T) {
	ca := genCA(t, "Copy", AlgRSA2048)
	pem1 := ca.CertPEM()
	pem1[0] = 'X'
	if ca.CertPEM()[0] != '-' {
		t.Fatal("CertPEM 应返回独立拷贝")
	}
	k := ca.KeyPEM()
	k[0] = 'X'
	if ca.KeyPEM()[0] != '-' {
		t.Fatal("KeyPEM 应返回独立拷贝")
	}
}

// 算法字符串。
func TestCA_KeyAlgorithmString(t *testing.T) {
	if AlgRSA2048.String() != "rsa2048" || AlgECDSAP256.String() != "ecdsa-p256" {
		t.Fatal("算法字符串错误")
	}
	if KeyAlgorithm(7).String() != "KeyAlgorithm(7)" {
		t.Fatal("未知算法应输出数值形式")
	}
}
