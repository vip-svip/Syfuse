package outbound

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
)

// 本文件放出站侧的协议小工具。
//
// 与 internal/socks5 的同名函数**有意重复**：socks5 是服务端
// （读方法、读请求、写应答），本包是客户端（写方法、写请求、读应答）。
// 两侧的读写方向相反，硬凑一份共享代码会让"改服务端"无意中改坏客户端。
// 重复的代价是多二十行；不重复的代价是两条链路互相牵制。选前者。

// hexSha224 返回明文的 hex(SHA224)。
//
// trojan 协议明确要求 SHA224：输出 56 个十六进制字符。
// 写成函数而不是内联，是为了让"用错哈希算法"这件事有一处可改、
// 且测试能直接锁住它（SHA256 输出 64 字符，类型上完全看不出差别）。
func hexSha224(text string) string {
	sum := sha256.Sum224([]byte(text))
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, 56)
	for _, b := range sum {
		out = append(out, hexDigits[b>>4], hexDigits[b&0x0f])
	}
	return string(out)
}

// basicAuth 生成 Proxy-Authorization 的 Basic 值。
func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// errBadAddrType 上游应答里的地址类型不在 SOCKS5 的三种之内。
//
// 单独定义而不是复用 socks5 的错误值：错误消息要能看出是**客户端侧**
// 解析上游应答时出的问题，与服务端收到畸形请求是两件事。
var errBadAddrType = errors.New("outbound: 非法的 SOCKS5 地址类型")

// readFull 读满 buf。
func readFull(c net.Conn, buf []byte) (int, error) {
	return io.ReadFull(c, buf)
}

// skipAddr 按 SOCKS5 地址格式跳过一个地址（读走但不使用）。
//
// bound 地址即使不关心也必须读走：残留字节会被当成业务数据，
// 症状是"第一次请求莫名失败，之后又正常"这种极难查的错。
func skipAddr(r io.Reader, atyp byte) error {
	switch atyp {
	case 0x01:
		_, err := readNReader(r, 4+2)
		return err
	case 0x04:
		_, err := readNReader(r, 16+2)
		return err
	case 0x03:
		n := make([]byte, 1)
		if _, err := io.ReadFull(r, n); err != nil {
			return err
		}
		_, err := readNReader(r, int(n[0])+2)
		return err
	default:
		return errBadAddrType
	}
}

func readNReader(r io.Reader, n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	buf := make([]byte, n)
	return io.ReadFull(r, buf)
}

// readLine 读一行（不含 CRLF），用于 HTTP 应答。
func readLine(c net.Conn) (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(c, buf); err != nil {
			return "", err
		}
		if buf[0] == '\n' {
			break
		}
		if buf[0] != '\r' {
			line = append(line, buf[0])
		}
	}
	return string(line), nil
}

// atoi 解析端口，非法值返回 0。
//
// 端口在上游应答里出现时不该让整个连接崩掉；返回 0 比返回错误更合适 ——
// 端口只用于记账与日志，不影响已建立的数据通道。
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 65535 {
		return 0
	}
	return n
}

// itoa 与 atoi 配对，保证端口格式化只有一处实现。
func itoa(n int) string {
	return strconv.Itoa(n)
}

// portBytes 把端口写成大端两字节。
func portBytes(port int) []byte {
	out := make([]byte, 2)
	binary.BigEndian.PutUint16(out, uint16(port))
	return out
}
