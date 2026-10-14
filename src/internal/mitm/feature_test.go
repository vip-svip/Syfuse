package mitm

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

// 叶证书：CN/SAN/EKU/IsCA/有效期；RSA 根签 RSA 叶，EC 根签 EC 叶。
func TestIssueLeaf_Basics(t *testing.T) {
	ca := genCA(t, "Leaf Root", AlgRSA2048)
	leaf, err := ca.IssueLeaf("api.example.com", 0)
	if err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}
	if leaf.Cert.Subject.CommonName != "api.example.com" {
		t.Fatalf("CN = %q", leaf.Cert.Subject.CommonName)
	}
	if len(leaf.Cert.DNSNames) != 1 || leaf.Cert.DNSNames[0] != "api.example.com" {
		t.Fatalf("SAN = %v", leaf.Cert.DNSNames)
	}
	if leaf.Cert.IsCA {
		t.Fatal("叶证书不能是 CA")
	}
	hasServerAuth := false
	for _, eku := range leaf.Cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
	}
	if !hasServerAuth {
		t.Fatal("叶证书必须带 ServerAuth EKU")
	}
	if _, ok := leaf.Key.(*rsa.PrivateKey); !ok {
		t.Fatalf("RSA 根应签 RSA 叶，得到 %T", leaf.Key)
	}
	if d := leaf.Cert.NotAfter.Sub(leaf.Cert.NotBefore); d != DefaultLeafValidity+time.Hour {
		t.Fatalf("缺省叶有效期 = %v", d)
	}

	// EC 根 → EC 叶。
	ecCA := genCA(t, "EC Root", AlgECDSAP256)
	ecLeaf, err := ecCA.IssueLeaf("a.example.com", 0)
	if err != nil {
		t.Fatalf("EC IssueLeaf: %v", err)
	}
	if _, ok := ecLeaf.Key.(*ecdsa.PrivateKey); !ok {
		t.Fatalf("EC 根应签 EC 叶，得到 %T", ecLeaf.Key)
	}
}

// 通配符与 IP host 进 SAN。
func TestIssueLeaf_SpecialHosts(t *testing.T) {
	ca := genCA(t, "W", AlgRSA2048)
	wild, err := ca.IssueLeaf("*.example.com", 0)
	if err != nil {
		t.Fatalf("通配符: %v", err)
	}
	if len(wild.Cert.DNSNames) != 1 || wild.Cert.DNSNames[0] != "*.example.com" {
		t.Fatalf("通配符 SAN = %v", wild.Cert.DNSNames)
	}
	ip, err := ca.IssueLeaf("127.0.0.1", 0)
	if err != nil {
		t.Fatalf("IP host: %v", err)
	}
	if len(ip.Cert.IPAddresses) != 1 || ip.Cert.IPAddresses[0].String() != "127.0.0.1" {
		t.Fatalf("IP SAN = %v", ip.Cert.IPAddresses)
	}
	if _, err := ca.IssueLeaf("", 0); err == nil {
		t.Fatal("空 host 应报错")
	}
}

// 链校验：签发的证书能验回根并匹配主机名；换主机名不匹配。
func TestIssueLeaf_VerifyChain(t *testing.T) {
	ca := genCA(t, "Verify", AlgRSA2048)
	leaf, _ := ca.IssueLeaf("a.example.com", 0)
	if err := ca.VerifyChain(leaf.Cert, "a.example.com"); err != nil {
		t.Fatalf("合法链应通过: %v", err)
	}
	if err := ca.VerifyChain(leaf.Cert, "b.example.com"); err == nil {
		t.Fatal("主机名不匹配应失败")
	}
	// 叶 PEM 能被标准库解析（导出可用性）。
	block, rest := pem.Decode(leaf.CertPEM())
	if block == nil || block.Type != "CERTIFICATE" || len(rest) > 0 {
		t.Fatal("叶 CertPEM 应是单个 CERTIFICATE 块")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		t.Fatalf("叶 PEM 解析失败: %v", err)
	}
}

// 两张叶证书序列号不同、私钥独立。
func TestIssueLeaf_Unique(t *testing.T) {
	ca := genCA(t, "U", AlgRSA2048)
	a, _ := ca.IssueLeaf("u.example.com", 0)
	b, _ := ca.IssueLeaf("u.example.com", 0)
	if a.Cert.SerialNumber.Cmp(b.Cert.SerialNumber) == 0 {
		t.Fatal("序列号不应相同")
	}
	if bytes.Equal(a.KeyPEM(), b.KeyPEM()) {
		t.Fatal("两次签发的私钥不应相同")
	}
}

// 主机列表：精确 / 一层通配 / 宽松点前缀 / 排除优先 / 空列表全不匹配。
func TestHostList_Match(t *testing.T) {
	hl, err := ParseHostList(`
# 注释
example.com
*.cdn.net
.open.com
!ads.example.com
`)
	if err != nil {
		t.Fatalf("ParseHostList: %v", err)
	}
	cases := []struct {
		host string
		want bool
	}{
		{"example.com", true},
		{"EXAMPLE.com", true},    // 大小写不敏感
		{"a.example.com", false}, // 精确项不匹配子域
		{"a.cdn.net", true},
		{"a.b.cdn.net", false},       // *. 只匹配一层
		{"cdn.net", false},           // *. 不匹配裸域
		{"open.com", true},           // . 前缀含裸域
		{"x.y.open.com", true},       // . 前缀任意深度
		{"ads.example.com", false},   // 排除优先于 example.com 精确? ads 不是精确项——被 ! 排除
		{"b.ads.example.com", false}, // 子域同样被排除
		{"other.org", false},         // 未列出
		{"", false},
	}
	for _, tc := range cases {
		if got := hl.ShouldDecrypt(tc.host); got != tc.want {
			t.Errorf("ShouldDecrypt(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
	if len(hl.Patterns()) != 4 {
		t.Fatalf("Patterns = %v", hl.Patterns())
	}
	if hl.Patterns()[3] != "!ads.example.com" {
		t.Fatalf("排除项应带 ! 前缀: %v", hl.Patterns())
	}

	// 空列表全不匹配（默认全关）。
	empty := &HostList{}
	if empty.ShouldDecrypt("example.com") {
		t.Fatal("空列表应全不匹配")
	}
}

// 非法 pattern：多星、星不在首、空、!空。
func TestHostList_Invalid(t *testing.T) {
	for _, bad := range []string{"a*b.com", "x*.com", "", "!", "  "} {
		hl := &HostList{}
		if err := hl.Add(bad); err == nil {
			t.Fatalf("pattern %q 应报错", bad)
		}
	}
	if _, err := ParseHostList("good.com\nbad*bad.com\n"); err == nil {
		t.Fatal("坏行应让整个解析失败")
	}
}

// mobileconfig：结构合法、证书 base64 可解码回 DER、元数据就位、XML 转义。
func TestMobileConfig(t *testing.T) {
	ca := genCA(t, "MC", AlgRSA2048)
	mc, err := ca.MobileConfig(MobileConfigOptions{
		DisplayName:        "Syfuse <测试>",
		PayloadDescription: "根 & 证书",
	})
	if err != nil {
		t.Fatalf("MobileConfig: %v", err)
	}
	s := string(mc)
	for _, want := range []string{
		"PayloadType", "com.apple.security.root", "Configuration",
		"Syfuse &lt;测试&gt;", "根 &amp; 证书", "PayloadUUID",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("描述文件缺 %q", want)
		}
	}
	// base64 内文应解码为合法 DER 证书。
	start := strings.Index(s, "<data>") + 6
	end := strings.Index(s, "</data>")
	if start < 6 || end < 0 {
		t.Fatal("缺 <data> 块")
	}
	der, err := base64.StdEncoding.DecodeString(s[start:end])
	if err != nil {
		t.Fatalf("证书 base64 解码失败: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("DER 解析失败: %v", err)
	}
	if cert.Subject.CommonName != "MC" {
		t.Fatalf("描述文件里的证书 CN = %q", cert.Subject.CommonName)
	}
	// 两次生成 UUID 不同。
	mc2, _ := ca.MobileConfig(MobileConfigOptions{})
	if string(mc) == string(mc2) {
		t.Fatal("两次生成 UUID 应不同")
	}
}
