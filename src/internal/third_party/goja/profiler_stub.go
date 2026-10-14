// profiler_stub.go：profiler 的禁用存根。
//
// 上游 profiler.go 依赖 github.com/google/pprof（我们 vendoring 时剔除的
// 重依赖），本文件以最小类型集合替换它：保持 vm.go 引用的符号全部存在，
// 但 StartProfile 永远返回错误 —— profiling 功能在本移植中不可用，
// 正常执行路径（enabled==0）零开销。
package goja

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// profiler 请求常量（与 vm.go runWithProfiler 的分支对应；实际不会触发）。
const (
	profReqDoSample    = 1
	profReqStop        = 2
	profReqSampleReady = 3
)

// profTracker 是单个 VM 的采样句柄；禁用形态下字段仅保持类型完整。
type profTracker struct {
	finished  int32
	req       int32
	stop      time.Time
	numFrames int
	frames    []StackFrame
}

// profiler 在禁用形态下不注册任何 VM。
type profiler struct{}

func (p *profiler) registerVm() *profTracker { return nil }

// globalProfiler 保持 enabled==0：vm.run 的快速路径永远不进 runWithProfiler。
var globalProfiler = struct {
	sync.Mutex
	enabled int32
	p       *profiler
}{}

// StartProfile 在本移植中不可用（pprof 依赖被剔除）。
func StartProfile(w interface{}) error {
	return errors.New("goja: profiling disabled in this build (pprof dependency stripped)")
}

// StopProfile 对应禁用形态：永远无操作。
func StopProfile() {}

// 保底：万一 enabled 被置 1（不可能路径），registerVm 返回 nil 会让
// runWithProfiler 把 nil 当作"需要注册"，防呆一层。
func init() {
	atomic.StoreInt32(&globalProfiler.enabled, 0)
}
