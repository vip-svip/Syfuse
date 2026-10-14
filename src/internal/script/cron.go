// cron.go 实现 cron 类型脚本的调度（规范 5.4"定时任务（cron）"）。
//
// 表达式为标准 5 段：分 时 日 月 周（周 0=周日），支持：
//   - 任意值
//     */n      步进
//     a-b      区间
//     a,b,c    列表（可与区间/步进混用：1-5,10,*/2）
//
// Next 计算下一次触发时刻；Scheduler 用宿主注入的时钟逐分钟扫描，
// 到点的脚本用引擎执行并带回日志/错误（调试控制台可见）。
package script

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CronExpr 是解析后的 cron 表达式。
type CronExpr struct {
	minute map[int]bool
	hour   map[int]bool
	dom    map[int]bool
	month  map[int]bool
	dow    map[int]bool
	hasDom bool // dom/dow 有一方受限时另一方按 OR 语义（标准 cron 行为）
	hasDow bool
	raw    string
}

// ParseCron 解析 5 段表达式。空段数、超范围值都明确报错。
func ParseCron(expr string) (*CronExpr, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: 表达式必须 5 段（分 时 日 月 周），得到 %d 段: %q",
			len(fields), expr)
	}
	c := &CronExpr{
		raw:    expr,
		dom:    map[int]bool{},
		dow:    map[int]bool{},
		month:  map[int]bool{},
		hour:   map[int]bool{},
		minute: map[int]bool{},
	}
	var err error
	if c.minute, err = parseField(fields[0], 0, 59); err != nil {
		return nil, fmt.Errorf("cron: 分钟段: %w", err)
	}
	if c.hour, err = parseField(fields[1], 0, 23); err != nil {
		return nil, fmt.Errorf("cron: 小时段: %w", err)
	}
	if c.dom, err = parseField(fields[2], 1, 31); err != nil {
		return nil, fmt.Errorf("cron: 日段: %w", err)
	}
	if c.month, err = parseField(fields[3], 1, 12); err != nil {
		return nil, fmt.Errorf("cron: 月段: %w", err)
	}
	if c.dow, err = parseField(fields[4], 0, 6); err != nil {
		return nil, fmt.Errorf("cron: 周段: %w", err)
	}
	c.hasDom = fields[2] != "*"
	c.hasDow = fields[4] != "*"
	return c, nil
}

// parseField 解析单段：逗号分隔的多个部分，每部分是 *、a、a-b 或 x/n。
func parseField(f string, min, max int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(f, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("空片段")
		}
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("步进 %q 非法", part[i+1:])
			}
			step = n
			part = part[:i]
		}
		lo, hi := min, max
		switch {
		case part == "*":
			// lo/hi 保持满区间
		case strings.Contains(part, "-"):
			bounds := strings.SplitN(part, "-", 2)
			a, err1 := strconv.Atoi(bounds[0])
			b, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("区间 %q 非法", part)
			}
			lo, hi = a, b
		default:
			a, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("值 %q 非法", part)
			}
			lo, hi = a, a
		}
		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("范围 [%d,%d] 越界（合法 %d-%d）", lo, hi, min, max)
		}
		for v := lo; v <= hi; v += step {
			out[v] = true
		}
	}
	return out, nil
}

// Matches 判断 t 这一分钟是否触发。
func (c *CronExpr) Matches(t time.Time) bool {
	if !c.minute[t.Minute()] || !c.hour[t.Hour()] || !c.month[int(t.Month())] {
		return false
	}
	domOK := c.dom[t.Day()]
	// Go 的 Weekday() 周日=0，与表达式约定一致。
	dowOK := c.dow[int(t.Weekday())]
	// 标准 cron：dom 与 dow 都受限时取 OR；只限其一时取受限者。
	switch {
	case c.hasDom && c.hasDow:
		return domOK || dowOK
	case c.hasDom:
		return domOK
	case c.hasDow:
		return dowOK
	default:
		return true
	}
}

// Next 返回 t 之后（不含 t）的下一个触发时刻，精确到分钟。
// 找不到（表达式永不触发，如 2 月 30 日）返回零值 + false。
func (c *CronExpr) Next(after time.Time) (time.Time, bool) {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(0, 0, 366*5) // 5 年兜底，防死循环
	for t.Before(limit) {
		// 逐级剪枝：月不对直接跳到下月 1 日 0 分。
		if !c.month[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !c.hour[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if c.Matches(t) {
			return t, true
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}

// String 返回原始表达式。
func (c *CronExpr) String() string { return c.raw }

// CronRun 记录一次 cron 脚本执行的结果（调度日志可见）。
type CronRun struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	At      time.Time     `json:"at"`
	Err     string        `json:"err,omitempty"`
	Logs    []string      `json:"logs,omitempty"`
	Elapsed time.Duration `json:"elapsed"`
}

// Scheduler 按 cron 表达式跑存储里的 cron 脚本。
// 时钟可注入（测试确定性）；宿主负责定期调用 RunDue。
type Scheduler struct {
	store  *DiskStore
	notify Notifier
	now    func() time.Time

	mu       sync.Mutex
	lastRuns map[string]time.Time // id -> 上次执行时刻（防同一分钟重复跑）
	runs     []CronRun            // 最近执行记录（环形，调试控制台可见）
}

// MaxRunHistory 是保留的执行记录条数。
const MaxRunHistory = 100

// NewScheduler 构造调度器。
func NewScheduler(store *DiskStore, notify Notifier) *Scheduler {
	return &Scheduler{
		store:    store,
		notify:   notify,
		now:      time.Now,
		lastRuns: map[string]time.Time{},
	}
}

// SetClock 仅供测试注入。
func (s *Scheduler) SetClock(now func() time.Time) { s.now = now }

// RunDue 执行所有"自上次扫描以来到点且本分钟未跑过"的 cron 脚本。
// 返回本轮执行的记录。单个脚本失败不影响其它脚本。
func (s *Scheduler) RunDue() ([]CronRun, error) {
	entries, err := s.store.List()
	if err != nil {
		return nil, err
	}
	now := s.now()
	var out []CronRun
	for _, e := range entries {
		if !e.Enabled || e.Type != TypeCron || e.Cron == "" {
			continue
		}
		expr, err := ParseCron(e.Cron)
		if err != nil {
			out = append(out, CronRun{ID: e.ID, Name: e.Name, At: now, Err: err.Error()})
			continue
		}
		s.mu.Lock()
		last, ran := s.lastRuns[e.ID]
		due := expr.Matches(now) && (!ran || !sameMinute(last, now))
		if due {
			s.lastRuns[e.ID] = now
		}
		s.mu.Unlock()
		if !due {
			continue
		}
		cr := s.runOne(e, now)
		out = append(out, cr)
	}
	return out, nil
}

func (s *Scheduler) runOne(e *Entry, at time.Time) CronRun {
	start := time.Now()
	_, logs, err := Run(e.Source, Options{
		Type:   TypeCron,
		Script: e.Name,
		Notify: s.notify,
	})
	cr := CronRun{
		ID: e.ID, Name: e.Name, At: at,
		Elapsed: time.Since(start), Logs: logs,
	}
	if err != nil {
		cr.Err = err.Error()
	}
	s.mu.Lock()
	s.runs = append(s.runs, cr)
	if len(s.runs) > MaxRunHistory {
		s.runs = s.runs[len(s.runs)-MaxRunHistory:]
	}
	s.mu.Unlock()
	_ = s.store.IncHits(e.ID, 1)
	return cr
}

// Recent 返回最近执行记录（拷贝）。
func (s *Scheduler) Recent() []CronRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CronRun, len(s.runs))
	copy(out, s.runs)
	return out
}

func sameMinute(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() &&
		a.Day() == b.Day() && a.Hour() == b.Hour() && a.Minute() == b.Minute()
}
