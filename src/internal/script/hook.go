// hook.go 是 http 类型脚本的挂钩链：给定请求/响应，按存储顺序跑所有
// 启用且 Match 命中的脚本，把改写结果叠加传递。
//
// 语义（对齐 QX/Surge 重写）：
//   - 任一脚本返回 reject → 整条链立即拒绝，不再跑后续脚本；
//   - body 改写逐级叠加（后一个脚本看到的是前一个改完的）；
//   - headers 合并（后写覆盖同键）；
//   - 命中即 IncHits（调试控制台"命中次数"）。
//
// 当前调用方是 UDS 试跑与未来的 MITM 引擎（解密层拿到明文后调这里）。
package script

import (
	"fmt"
)

// HookChain 是脚本挂钩链。
type HookChain struct {
	store *DiskStore
}

// NewHookChain 构造。
func NewHookChain(store *DiskStore) *HookChain {
	return &HookChain{store: store}
}

// HookOutcome 是整条链跑完的总效果。
type HookOutcome struct {
	Rejected bool              `json:"rejected"`
	Status   int               `json:"status,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Body     string            `json:"body,omitempty"`
	// BodyModified 标记 body 是否被改过（未改时 Body 为原值）。
	BodyModified bool     `json:"body_modified"`
	Ran          []string `json:"ran"` // 命中并执行的脚本 ID
	Err          string   `json:"err,omitempty"`
}

// Apply 跑完整条链。req/resp 任一可为 nil（请求阶段/响应阶段）。
func (h *HookChain) Apply(req *Request, resp *Response, opts Options) (*HookOutcome, []string, error) {
	entries, err := h.store.List()
	if err != nil {
		return nil, nil, err
	}
	out := &HookOutcome{Ran: []string{}}
	var logs []string
	body := ""
	if resp != nil {
		body = resp.Body
	}

	for _, e := range entries {
		if !e.Enabled || e.Type != TypeHTTP {
			continue
		}
		if !e.Match.Matches(req, resp) {
			continue
		}
		// 把当前（可能已被前面脚本改过的）状态注入。
		curResp := resp
		if resp != nil {
			cp := *resp
			cp.Body = body
			curResp = &cp
		}
		_ = h.store.IncHits(e.ID, 1)
		res, scriptLogs, err := RunHTTP(e.Source, req, curResp, opts)
		logs = append(logs, scriptLogs...)
		out.Ran = append(out.Ran, e.ID)
		if err != nil {
			// 单脚本错误记为结果的一部分，不中断链（下一个脚本继续）。
			msg := fmt.Sprintf("脚本 %s: %v", e.Name, err)
			if out.Err == "" {
				out.Err = msg
			} else {
				out.Err += "; " + msg
			}
			continue
		}
		if res == nil {
			continue
		}
		if res.Reject {
			out.Rejected = true
			if res.Status != 0 {
				out.Status = res.Status
			}
			return out, logs, nil
		}
		if res.Body != nil {
			body = *res.Body
			out.BodyModified = true
		}
		if len(res.Headers) > 0 {
			if out.Headers == nil {
				out.Headers = map[string]string{}
			}
			for k, v := range res.Headers {
				out.Headers[k] = v
			}
		}
		if res.Status != 0 {
			out.Status = res.Status
		}
	}
	out.Body = body
	return out, logs, nil
}
