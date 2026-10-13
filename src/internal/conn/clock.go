package conn

import "time"

// SystemClock 是默认时钟（墙钟）。
// 测试通过 TrackerOptions.Now 注入可控时钟，保证时间序断言确定性。
func SystemClock() time.Time { return time.Now() }
