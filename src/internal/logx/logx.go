// Package logx 是守护进程的分级日志与轮转。
//
// 为什么自己写而不引入 logrus/zap：
//   - 本包的需求只有"分级 + 轮转 + 结构化字段"，用 stdlib 的 log/slog 足够；
//   - 守护进程跑在 root 下，第三方日志库的供应链面不值得为一个 log 引入；
//   - 轮转策略必须与模块的"零残留"承诺对齐（日志目录可被 uninstall 完整清理），
//     自己写才能保证这一点。
//
// 设计取舍：日志**默认只写文件**，不写 stdout。
// 理由：守护进程由 service.sh 在后台启动，stdout 会进 logcat 并被系统限流；
// 写自己的文件才能保证日志完整、可轮转、可被 App 读取。
package logx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level 是日志级别。
type Level int

const (
	LevelSilent Level = iota
	LevelError
	LevelWarn
	LevelInfo
	LevelDebug
)

// ParseLevel 解析级别名。未知值返回 LevelWarn 而不是报错 ——
// 配置文件里写错一个级别不该让守护进程起不来。
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "silent", "off", "none":
		return LevelSilent
	case "error", "err":
		return LevelError
	case "warn", "warning":
		return LevelWarn
	case "info":
		return LevelInfo
	case "debug", "trace", "verbose":
		return LevelDebug
	default:
		return LevelWarn
	}
}

func (l Level) String() string {
	switch l {
	case LevelSilent:
		return "silent"
	case LevelError:
		return "error"
	case LevelWarn:
		return "warn"
	case LevelInfo:
		return "info"
	case LevelDebug:
		return "debug"
	}
	return "unknown"
}

// Options 是 Logger 的构造参数。
type Options struct {
	// Dir 是日志目录。为空则只输出到 Writer。
	Dir string
	// FileName 是当前日志文件名，默认 syfused.log。
	FileName string
	// Level 是输出级别。
	Level Level
	// MaxBytes 是单文件上限，超过即轮转。<=0 用默认 2 MiB。
	MaxBytes int64
	// MaxFiles 是保留的历史文件数（不含当前文件）。<=0 用默认 5。
	MaxFiles int
	// Writer 是额外的输出目标（例如测试用 buffer）。可为 nil。
	Writer io.Writer
	// AlsoStderr 为 true 时同时写 stderr。守护进程默认 false（见包注释）。
	AlsoStderr bool
}

// Logger 是并发安全的日志器。
type Logger struct {
	mu       sync.Mutex
	dir      string
	file     string
	level    Level
	maxBytes int64
	maxFiles int
	w        io.Writer
	alsoErr  bool
	now      func() time.Time // 可注入，便于测试轮转
}

// New 创建 Logger。
//
// 目录创建失败时**不返回错误**，而是降级为"仅写 Writer"。
// 理由：日志不可用不该让守护进程拒绝启动 —— 那会导致"因为写不了日志所以整个代理挂了"，
// 用户完全无法理解。降级后 Error 会被打到 stderr 让上层能看见。
func New(opts Options) *Logger {
	l := &Logger{
		dir:      opts.Dir,
		file:     opts.FileName,
		level:    opts.Level,
		maxBytes: opts.MaxBytes,
		maxFiles: opts.MaxFiles,
		w:        opts.Writer,
		alsoErr:  opts.AlsoStderr,
		now:      time.Now,
	}
	if l.file == "" {
		l.file = "syfused.log"
	}
	if l.maxBytes <= 0 {
		l.maxBytes = 2 << 20 // 2 MiB
	}
	if l.maxFiles <= 0 {
		l.maxFiles = 5
	}
	if l.dir != "" {
		if err := os.MkdirAll(l.dir, 0o700); err != nil {
			// 降级：丢掉目录，只保留 Writer
			fmt.Fprintf(os.Stderr, "logx: 无法创建日志目录 %s: %v（降级为仅 Writer）\n", l.dir, err)
			l.dir = ""
		}
	}
	return l
}

// SetNow 注入时钟，仅用于测试轮转逻辑。
func (l *Logger) SetNow(f func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = f
}

// Path 返回当前日志文件的绝对路径。未配置文件目录时返回空串。
func (l *Logger) Path() string {
	if l.dir == "" {
		return ""
	}
	return filepath.Join(l.dir, l.file)
}

func (l *Logger) log(level Level, format string, args ...any) {
	if l == nil || level > l.level || l.level == LevelSilent {
		return
	}
	ts := l.now().Format("2006-01-02T15:04:05.000Z07:00")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %-5s %s\n", ts, strings.ToUpper(level.String()), msg)

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.w != nil {
		_, _ = io.WriteString(l.w, line)
	}
	if l.alsoErr {
		_, _ = io.WriteString(os.Stderr, line)
	}
	if l.dir == "" {
		return
	}
	l.rotateIfNeeded(len(line))
	f, err := os.OpenFile(l.Path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logx: 打开日志失败: %v\n", err)
		return
	}
	_, _ = io.WriteString(f, line)
	_ = f.Close()
}

// rotateIfNeeded 在写入前检查当前文件大小。sizeHint 是即将写入的字节数。
func (l *Logger) rotateIfNeeded(sizeHint int) {
	st, err := os.Stat(l.Path())
	if err != nil {
		return // 文件不存在，无需轮转
	}
	if st.Size()+int64(sizeHint) <= l.maxBytes {
		return
	}
	// 轮转：syfused.log -> syfused.log.1 -> ... -> syfused.log.N
	// 从最大编号开始倒着搬，避免覆盖。
	for i := l.maxFiles - 1; i >= 1; i-- {
		old := fmt.Sprintf("%s.%d", l.Path(), i)
		newer := fmt.Sprintf("%s.%d", l.Path(), i+1)
		if _, err := os.Stat(old); err == nil {
			_ = os.Rename(old, newer)
		}
	}
	_ = os.Rename(l.Path(), l.Path()+".1")
	l.pruneLocked()
}

// pruneLocked 删除超出 MaxFiles 的历史文件。
func (l *Logger) pruneLocked() {
	matches, err := filepath.Glob(l.Path() + ".*")
	if err != nil {
		return
	}
	type ent struct {
		path string
		n    int
	}
	var ents []ent
	for _, m := range matches {
		var n int
		if _, err := fmt.Sscanf(strings.TrimPrefix(m, l.Path()+"."), "%d", &n); err != nil {
			continue
		}
		ents = append(ents, ent{m, n})
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].n > ents[j].n })
	for i, e := range ents {
		if i < l.maxFiles {
			continue
		}
		_ = os.Remove(e.path)
	}
}

// Error 记录错误级日志。
func (l *Logger) Error(format string, args ...any) { l.log(LevelError, format, args...) }

// Warn 记录警告级日志。
func (l *Logger) Warn(format string, args ...any) { l.log(LevelWarn, format, args...) }

// Info 记录信息级日志。
func (l *Logger) Info(format string, args ...any) { l.log(LevelInfo, format, args...) }

// Debug 记录调试级日志。
func (l *Logger) Debug(format string, args ...any) { l.log(LevelDebug, format, args...) }

// Level 返回当前级别。
func (l *Logger) Level() Level { return l.level }

// SetLevel 运行时改级别（配置热重载用）。
func (l *Logger) SetLevel(v Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = v
}
