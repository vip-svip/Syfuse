# module/bin —— 守护进程二进制（构建时注入）

本目录在**源码树中为空**。`package.sh` 打包时会把交叉编译产物放进来：

```
bin/syfused-arm64     # arm64-v8a
bin/syfused-arm       # armeabi-v7a
bin/syfused-amd64     # x86_64
```

来源：`src/` 目录的 Go 代码，`GOOS=android GOARCH=... go build ./cmd/syfused`。
`customize.sh` 按设备 ABI 选择对应文件并复制为 `bin/syfused`。

**当前状态：未交付。** M1 只交付守护进程骨架的**源码**，
二进制随包在 M3（内核打通）时接入。模块包里没有 `bin/syfused-*` 时，
`customize.sh` 会明确打印「未随包提供守护进程二进制」，`action.sh` 的
启停菜单会走 `not_implemented` 分支 —— 不会假装启动成功。
