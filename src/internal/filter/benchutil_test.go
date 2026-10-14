// bench 时钟辅助（避免测试文件直接 import time 命名冲突的小封装）。
package filter

import "time"

func now() time.Time                    { return time.Now() }
func since(t time.Time) time.Duration   { return time.Since(t) }
func sec(d time.Duration) time.Duration { return d }

// itoa 免 fmt 依赖的小整数转字符串。
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
