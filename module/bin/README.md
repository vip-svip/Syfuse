# module/bin —— 守护进程二进制（构建时注入）

本目录在**源码树中为空**。`package.sh` 打包时会把交叉编译产物放进来：

```
bin/syfused-arm64     # arm64-v8a
bin/syfused-arm       # armeabi-v7a
bin/syfused-amd64     # x86_64
```

来源：`src/` 目录的 Go 代码，`GOOS=android GOARCH=... go build ./cmd/syfused`。
`customize.sh` 按设备 ABI 选择对应文件并复制为 `bin/syfused`。

**当前状态：已随包。** `package.sh` 会交叉编译并注入（v0.1.0 实测三个文件共约 7.9 MB，
已进入 `dist/syfuse-0.1.0.zip`）。源码树里本目录保持为空 —— 二进制是构建产物，不入库。

**仍未验证的是"能不能跑"**：二进制尚未在真机上启动过，因此 `action.sh` 的启停菜单
在真机上的行为仍属`实验性`。模块包里**没有** `bin/syfused-*` 时（例如只跑了
`customize.sh` 而没跑 `package.sh`），`customize.sh` 会明确打印
「未随包提供守护进程二进制」，`action.sh` 的启停菜单会走 `not_implemented` 分支
—— 不会假装启动成功。
