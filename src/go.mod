module github.com/vip-svip/syfuse/src

go 1.23

// 刻意**不引入任何第三方依赖**。
//
// 理由（供应链 + 可验证性）：
//  1. 本目录同时产出两个交付物：`syfused` 守护进程（交叉编译到 3 个 ABI）
//     与 gomobile bind 的 AAR（给无 root 时的 App 内嵌使用）。依赖越少，
//     交叉编译越不容易失败。
//  2. 代理内核是安全敏感组件，每多一个依赖就多一个需要审计的供应链面。
//  3. 纯 stdlib 让 `go test` 在任何装了 Go 的机器上都能跑，`GOPROXY=off` 也通过
//     —— 这一点直接决定了"守护逻辑可验证"是不是空话。
//     `tests/run_kernel.sh` 有断言守着（出现 require 或 go.sum 即失败）。
//
// 如果将来必须引入依赖（例如 QuickJS 脚本引擎、protobuf）：
// 请在 NOTICE 登记其许可证，确认与 GPL-3.0-or-later 兼容，并同步更新那条断言。
