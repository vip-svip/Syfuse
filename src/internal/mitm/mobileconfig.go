// mobileconfig.go 生成 iOS/macOS 的描述文件（.mobileconfig）。
//
// 规范 5.5：CA 要能导出 .crt/.pem/.mobileconfig。描述文件本质是
// Apple 的 XML plist，包裹一段 PKCS#7/CMS 签名的 PEM 载荷。
// 这里生成"根证书安装"描述文件：PayloadType = com.apple.security.root，
// 即装进系统信任库的根证书（对应 root 模式的系统证书注入语义）。
package mitm

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"time"
)

// MobileConfigOptions 控制描述文件的元数据。
type MobileConfigOptions struct {
	// DisplayName 在"设置 → 已下载的描述文件"里显示。
	DisplayName string
	// PayloadIdentifier 是描述文件的唯一标识（反域名习惯写法）。
	PayloadIdentifier string
	// PayloadDescription 展示给用户的说明。
	PayloadDescription string
}

// MobileConfig 生成安装根 CA 用的 .mobileconfig 内容。
// 结构参照 Apple Configuration Profile Reference 的最小合法形态：
// 顶层 dict + 一个 root-cert Payload。
func (ca *CA) MobileConfig(opts MobileConfigOptions) ([]byte, error) {
	if opts.DisplayName == "" {
		opts.DisplayName = "Syfuse Root CA"
	}
	if opts.PayloadIdentifier == "" {
		opts.PayloadIdentifier = "io.github.vipsvip.syfuse.root-ca"
	}
	if opts.PayloadDescription == "" {
		opts.PayloadDescription = "安装 Syfuse 的 MITM 根证书"
	}

	uuid, err := randomUUID()
	if err != nil {
		return nil, err
	}
	// PayloadCertificatePKCS1 用证书 PEM 的 base64（苹果接受 PEM 内文的 base64，
	// 即去掉 BEGIN/END 行后的内容）。
	certB64 := pemInnerBase64(ca.CertPEM())

	var b bytes.Buffer
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>PayloadCertificateFileName</key>
			<string>syfuse-root-ca.cer</string>
			<key>PayloadContent</key>
			<data>%s</data>
			<key>PayloadDescription</key>
			<string>%s</string>
			<key>PayloadDisplayName</key>
			<string>%s</string>
			<key>PayloadIdentifier</key>
			<string>%s.certificate.%s</string>
			<key>PayloadOrganization</key>
			<string>Syfuse</string>
			<key>PayloadRemovalDisallowed</key>
			<false/>
			<key>PayloadType</key>
			<string>com.apple.security.root</string>
			<key>PayloadUUID</key>
			<string>%s</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>%s</string>
	<key>PayloadIdentifier</key>
	<string>%s</string>
	<key>PayloadOrganization</key>
	<string>Syfuse</string>
	<key>PayloadRemovalDisallowed</key>
	<false/>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>%s</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>
`,
		certB64,
		xmlEscape(opts.PayloadDescription),
		xmlEscape(opts.DisplayName),
		xmlEscape(opts.PayloadIdentifier), uuid,
		uuid,
		xmlEscape(opts.DisplayName),
		xmlEscape(opts.PayloadIdentifier),
		uuid,
	)
	return b.Bytes(), nil
}

// pemInnerBase64 取 PEM 第一段的 base64 内文（去 BEGIN/END 行与换行）。
func pemInnerBase64(pemBytes []byte) string {
	var b bytes.Buffer
	for _, line := range bytes.Split(pemBytes, []byte("\n")) {
		l := string(line)
		if l == "" || l[0] == '-' {
			continue
		}
		b.Write(line)
	}
	return b.String()
}

// randomUUID 生成 v4 UUID（无第三方依赖，crypto/rand + sha1 混熵足够——
// UUID 在这里只做去重标识，不承担安全语义）。
func randomUUID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var seed uint32
	binary.BigEndian.PutUint32(buf, seed)
	_ = seed
	h := sha1.New()
	if _, err := h.Write(buf); err != nil {
		return "", err
	}
	if _, err := h.Write([]byte(time.Now().Format(time.RFC3339Nano))); err != nil {
		return "", err
	}
	sum := h.Sum(nil)
	randBytes := make([]byte, 16)
	if _, err := rand.Read(randBytes); err != nil {
		return "", err
	}
	for i := range randBytes {
		randBytes[i] ^= sum[i%len(sum)]
	}
	randBytes[6] = (randBytes[6] & 0x0f) | 0x40 // version 4
	randBytes[8] = (randBytes[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		randBytes[0:4], randBytes[4:6], randBytes[6:8], randBytes[8:10], randBytes[10:16]), nil
}

func xmlEscape(s string) string {
	r := bytes.NewBuffer(nil)
	for _, c := range s {
		switch c {
		case '&':
			r.WriteString("&amp;")
		case '<':
			r.WriteString("&lt;")
		case '>':
			r.WriteString("&gt;")
		case '"':
			r.WriteString("&quot;")
		case '\'':
			r.WriteString("&apos;")
		default:
			r.WriteRune(c)
		}
	}
	return r.String()
}
