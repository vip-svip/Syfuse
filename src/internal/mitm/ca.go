// MITM 根 CA 的生成、加载与指纹。
//
// 本文件只依赖标准库（crypto/x509 等），与 src/ 的"零第三方依赖"硬约束一致。
// CA 全流程（签发 / 导出 / mobileconfig / 主机策略）见同包的 issue.go、hosts.go。
package mitm

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

// KeyAlgorithm 是 CA（及叶证书）可用的非对称算法。
//
// 产品规格 5.5 明确要求两种：RSA-2048（兼容面最广，老旧客户端/企业代理都认）
// 与 ECDSA P-256（同等安全强度下密钥小、签名快，但极少数老客户端不支持）。
type KeyAlgorithm int

const (
	// AlgRSA2048 RSA 2048 位，签名用 SHA-256。
	AlgRSA2048 KeyAlgorithm = iota
	// AlgECDSAP256 ECDSA P-256 曲线，签名用 SHA-256。
	AlgECDSAP256
)

func (a KeyAlgorithm) String() string {
	switch a {
	case AlgRSA2048:
		return "rsa2048"
	case AlgECDSAP256:
		return "ecdsa-p256"
	default:
		return fmt.Sprintf("KeyAlgorithm(%d)", int(a))
	}
}

func (a KeyAlgorithm) valid() bool {
	return a == AlgRSA2048 || a == AlgECDSAP256
}

// 默认 CA 有效期：10 年。
//
// 轮换是产品能力（规格 5.5），过期由轮换兜底；取 10 年只是让"装一次就
// 不用管"覆盖绝大多数用户生命周期，同时仍是有界的（永不过期的根 CA 在
// 泄露后无法自然退役）。
const DefaultCAValidity = 10 * 365 * 24 * time.Hour

// pemBlockCert / pemBlockKeyPKCS8 等是落盘与加载时识别的 PEM block 类型。
const (
	pemBlockCert     = "CERTIFICATE"
	pemBlockKeyPKCS8 = "PRIVATE KEY"     // PKCS#8（统一使用它，同时兼容 RSA/EC）
	pemBlockKeyPKCS1 = "RSA PRIVATE KEY" // 兼容外部工具（openssl genrsa）产物
	pemBlockKeySEC1  = "EC PRIVATE KEY"  // 兼容 openssl ecparam -genkey 产物
)

// CA 是一枚自签名根证书及其私钥，全部字段内存驻留。
//
// 为什么不缓存 PEM：调用方（daemon 的安装/导出路径）随时要 PEM 文本，
// 而 PEM 编码是确定性的，可以在构造时算一次缓存，避免每次导出重复做
// base64；证书 DER 同样缓存（移动配置要 base64(DER)）。
type CA struct {
	// Cert 是解析后的根证书（含公钥、序列号、SPKI 等）。
	Cert *x509.Certificate
	// Key 是根私钥，crypto.Signer 覆盖 RSA/ECDSA 两种算法。
	Key crypto.Signer

	certDER []byte // 证书 DER（= Cert.Raw）
	certPEM []byte // 证书 PEM 文本（公开，0644 落盘）
	keyPEM  []byte // 私钥 PEM 文本（PKCS#8，0600 落盘）
}

// GenerateCA 生成一枚新的自签名根 CA。
//
// commonName 会写进证书 Subject（用户在"证书信任设置"里看到的名字），
// 不允许为空——空 CN 的根 CA 在系统 UI 里显示为空白，用户无法分辨装的是
// 什么，属于明确的产品事故来源。validFor <= 0 时取 DefaultCAValidity。
func GenerateCA(commonName string, alg KeyAlgorithm, validFor time.Duration) (*CA, error) {
	if commonName == "" {
		return nil, errors.New("mitm: GenerateCA 需要非空 commonName（用户信任库里显示的名字）")
	}
	if !alg.valid() {
		return nil, fmt.Errorf("mitm: 不支持的密钥算法 %d（合法值：AlgRSA2048 / AlgECDSAP256）", int(alg))
	}
	if validFor <= 0 {
		validFor = DefaultCAValidity
	}

	key, err := generateKey(alg)
	if err != nil {
		return nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Syfuse"},
		},
		// 根 CA：可继续签子 CA / 叶证书，可签 CRL。
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		NotBefore:             now.Add(-time.Hour), // 容忍时钟回拨一小时
		NotAfter:              now.Add(validFor),
	}
	// 自签名：parent 传 tmpl 自身，CreateCertificate 会用 tmpl.Key 签名。
	// 对 RSA/ECDSA，CreateCertificate 从 key.(crypto.Signer) 取私钥。
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, fmt.Errorf("mitm: 签发根 CA 失败：%w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("mitm: 解析刚生成的根 CA 失败：%w", err)
	}
	return newCA(cert, key)
}

// LoadCA 从 PEM 文本加载 CA（证书 + 私钥），并做三项防御性校验：
//  1. PEM 结构合法（坏 PEM 明确报错，绝不静默返回半可用的 CA）；
//  2. 私钥与证书公钥匹配（防止"cert/key 装反串包"这类配置事故）；
//  3. 证书自签名有效（防止证书被换成了同Key的另一张非CA证书）。
//
// 私钥 PEM 兼容三种 block 类型：PKCS#8 / PKCS#1(RSA) / SEC1(EC)。
func LoadCA(certPEM, keyPEM []byte) (*CA, error) {
	certBlock, rest := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("mitm: 证书不是合法 PEM（缺少 -----BEGIN CERTIFICATE----- 之类的块头）")
	}
	if certBlock.Type != pemBlockCert {
		return nil, fmt.Errorf("mitm: 证书 PEM 块类型是 %q，期望 %q", certBlock.Type, pemBlockCert)
	}
	if len(rest) > 0 && len(bytesTrimSpace(rest)) > 0 {
		// 证书文件里混入第二段不报错，只忽略；真正致命的是第一段就解析不出。
		_ = rest
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mitm: 证书 DER 解析失败：%w", err)
	}
	if !cert.IsCA {
		return nil, errors.New("mitm: 证书 IsCA=false，不能用作 MITM 根 CA")
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, errors.New("mitm: 证书缺少 CertSign KeyUsage，不能用于签发")
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("mitm: 私钥不是合法 PEM（缺少私钥块头）")
	}
	key, err := parsePrivateKey(keyBlock)
	if err != nil {
		return nil, err
	}

	// 公钥匹配：类型不同（RSA cert 配 EC key）或参数不同都拒绝。
	if !reflect.DeepEqual(key.Public(), cert.PublicKey) {
		return nil, errors.New("mitm: 私钥与证书公钥不匹配（cert/key 装反了，或不是一对）")
	}
	// 自签名校验：证书必须由自己的公钥可验签，挡住"key 是对的、cert 被换过"。
	if err := cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature); err != nil {
		return nil, fmt.Errorf("mitm: 根 CA 自签名校验失败：%w", err)
	}
	return newCA(cert, key)
}

// newCA 由已校验的 cert+key 组装 CA，并预计算 PEM/DER 缓存。
func newCA(cert *x509.Certificate, key crypto.Signer) (*CA, error) {
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("mitm: 私钥 PKCS#8 编码失败：%w", err)
	}
	return &CA{
		Cert:    cert,
		Key:     key,
		certDER: cert.Raw,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: pemBlockCert, Bytes: cert.Raw}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: pemBlockKeyPKCS8, Bytes: keyDER}),
	}, nil
}

// generateKey 按算法生成私钥。
func generateKey(alg KeyAlgorithm) (crypto.Signer, error) {
	switch alg {
	case AlgRSA2048:
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("mitm: 生成 RSA-2048 私钥失败：%w", err)
		}
		return key, nil
	case AlgECDSAP256:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("mitm: 生成 ECDSA P-256 私钥失败：%w", err)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("mitm: 不支持的密钥算法 %d", int(alg))
	}
}

// parsePrivateKey 解析 PEM 私钥块，返回 crypto.Signer。
func parsePrivateKey(block *pem.Block) (crypto.Signer, error) {
	var (
		key any
		err error
	)
	switch block.Type {
	case pemBlockKeyPKCS8:
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case pemBlockKeyPKCS1:
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case pemBlockKeySEC1:
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("mitm: 私钥 PEM 块类型 %q 无法识别（期望 PRIVATE KEY / RSA PRIVATE KEY / EC PRIVATE KEY）", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("mitm: 私钥 DER 解析失败：%w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("mitm: 私钥类型 %T 不支持签名（期望 RSA 或 ECDSA 私钥）", key)
	}
	return signer, nil
}

// randomSerial 生成 128 位随机正序列号（RFC 5280 建议 ≥64 位熵，取 128 位）。
func randomSerial() (*big.Int, error) {
	// 上界 2^127，配合 >0 判定，保证整数且位数不超过 128。
	limit := new(big.Int).Lsh(big.NewInt(1), 127)
	for {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return nil, fmt.Errorf("mitm: 生成随机序列号失败：%w", err)
		}
		if n.Sign() > 0 { // 0 概率极低，但序列号为 0 语义上非法，重掷
			return n, nil
		}
	}
}

// Fingerprint 返回 CA 指纹的 base64 部分：SPKI（SubjectPublicKeyInfo）DER
// 的 SHA-256，base64 标准编码（带 = 补位）。
//
// 口径说明（为什么不是整证书摘要的 hex）：
//   - HPKP / Chrome SPKI 指纹的业界口径就是对"公钥结构"取 SHA-256 而不是对
//     整张证书，好处是**密钥不变则指纹不变**：续期、改 Subject、改有效期都
//     不影响用户核对的值，用户只需核对一次；
//   - 展示格式为 "SHA256:<base64>"（见 FingerprintDisplay），与主流浏览器
//     证书详情页一致；纯 base64 由 Fingerprint 返回，方便程序拼接。
//   - 若要对账"证书文件本身"（例如校验下载的 .crt 没被截断），应对整证书
//     DER 做 SHA-256，那是另一口径，不要混用。
func (ca *CA) Fingerprint() string {
	sum := sha256.Sum256(ca.Cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// FingerprintDisplay 返回带前缀的展示形式："SHA256:<base64>"。
func (ca *CA) FingerprintDisplay() string {
	return "SHA256:" + ca.Fingerprint()
}

// CertPEM / KeyPEM 返回 PEM 文本（导出与 IPC 传输用）。
func (ca *CA) CertPEM() []byte { return append([]byte(nil), ca.certPEM...) }
func (ca *CA) KeyPEM() []byte  { return append([]byte(nil), ca.keyPEM...) }

// Save 把 CA 写入 dir：name.pem（证书，0644）与 name-key.pem（私钥，0600）。
//
// 私钥 0600 是硬要求：根 CA 私钥一旦泄露，任何拿到它的人都能为任意域名
// 签证书并欺骗装了该 CA 的客户端。0644 的私钥等于把城堡钥匙挂在城门上。
func (ca *CA) Save(dir, name string) (certPath, keyPath string, err error) {
	if dir == "" {
		return "", "", errors.New("mitm: Save 需要非空目录")
	}
	if name == "" {
		return "", "", errors.New("mitm: Save 需要非空文件名（不含扩展名）")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mitm: 创建目录 %s 失败：%w", dir, err)
	}
	certPath = filepath.Join(dir, name+".pem")
	keyPath = filepath.Join(dir, name+"-key.pem")
	if err := os.WriteFile(certPath, ca.certPEM, 0o644); err != nil {
		return "", "", fmt.Errorf("mitm: 写证书 %s 失败：%w", certPath, err)
	}
	if err := os.WriteFile(keyPath, ca.keyPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("mitm: 写私钥 %s 失败：%w", keyPath, err)
	}
	return certPath, keyPath, nil
}

// Load 从 dir 读回 Save 写出的文件对（name 同 Save）。
func Load(dir, name string) (*CA, error) {
	certPath := filepath.Join(dir, name+".pem")
	keyPath := filepath.Join(dir, name+"-key.pem")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("mitm: 读证书 %s 失败：%w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("mitm: 读私钥 %s 失败：%w", keyPath, err)
	}
	return LoadCA(certPEM, keyPEM)
}

// bytesTrimSpace 判断剩余字节是否全是空白（用 strings 会引入 import，
// 这里手写 tiny helper 只为判断"PEM 尾部只剩换行"）。
func bytesTrimSpace(b []byte) []byte {
	i := 0
	for i < len(b) && (b[i] == '\n' || b[i] == '\r' || b[i] == ' ' || b[i] == '\t') {
		i++
	}
	return b[i:]
}
