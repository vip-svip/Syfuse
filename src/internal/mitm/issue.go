// issue.go 实现叶证书（服务器证书）的按需签发。
//
// MITM 引擎每遇到一个要解密的域名就现场签一张叶证书（规范 5.5）：
//   - Subject CN = 域名，SAN DNS = 域名（现代客户端只看 SAN）；
//   - EKU = ServerAuth（不能带 ClientAuth，避免证书被误用于双向 TLS）；
//   - 有效期短（默认 30 天）：叶证书随用随弃，短命把泄露影响压到最低；
//   - RSA 叶证书一律 SHA-256 签名（规范 5.5：RSA 2048 / ECC P-256）。
package mitm

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"net"
	"time"

	"encoding/pem"
)

// DefaultLeafValidity 是叶证书的缺省有效期。
const DefaultLeafValidity = 30 * 24 * time.Hour

// LeafCert 是一张已签发的叶证书及其私钥。
type LeafCert struct {
	Cert *x509.Certificate
	Key  crypto.Signer
}

// IssueLeaf 用根 CA 为 host 签发一张服务器证书。
// host 允许普通域名与通配符（*.example.com，会被原样写进 SAN）。
func (ca *CA) IssueLeaf(host string, validFor time.Duration) (*LeafCert, error) {
	if host == "" {
		return nil, errors.New("mitm: IssueLeaf 需要非空 host")
	}
	if validFor <= 0 {
		validFor = DefaultLeafValidity
	}

	// 叶私钥跟随根 CA 的算法：RSA 根签 RSA 叶，EC 根签 EC 叶 —— 避免
	// 签名算法与叶公钥类型组合出老客户端不认的混合形态。
	var key crypto.Signer
	var err error
	switch ca.Key.(type) {
	case *rsa.PrivateKey:
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	case *ecdsa.PrivateKey:
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	default:
		return nil, fmt.Errorf("mitm: 不支持的根私钥类型 %T", ca.Key)
	}
	if err != nil {
		return nil, fmt.Errorf("mitm: 生成叶私钥失败：%w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host, Organization: []string{"Syfuse"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	// SAN：纯 IP host 也支持（抓包工具常用 127.0.0.1）。
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, key.Public(), ca.Key)
	if err != nil {
		return nil, fmt.Errorf("mitm: 签发叶证书失败：%w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("mitm: 解析刚签发的叶证书失败：%w", err)
	}
	return &LeafCert{Cert: cert, Key: key}, nil
}

// CertPEM / KeyPEM 返回 PEM（PKCS#8），口径与根 CA 一致。
func (l *LeafCert) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: pemBlockCert, Bytes: l.Cert.Raw})
}

func (l *LeafCert) KeyPEM() []byte {
	der, err := x509.MarshalPKCS8PrivateKey(l.Key)
	if err != nil {
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{Type: pemBlockKeyPKCS8, Bytes: der})
}

// VerifyChain 校验叶证书链回根 CA（主机名、有效期、EKU 由 VerifyHostname
// 与库内部处理；这里做链级校验 + 主机名匹配）。
func (ca *CA) VerifyChain(leaf *x509.Certificate, host string) error {
	if host == "" {
		return errors.New("mitm: VerifyChain 需要非空 host")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: time.Now(),
		DNSName:     host,
	}); err != nil {
		return fmt.Errorf("mitm: 证书链校验失败：%w", err)
	}
	return nil
}
