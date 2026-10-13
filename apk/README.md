# apk/ —— 内置软件（sidecar APK）产物占位

本目录在**源码树中为空**（构建产物不入库）。`package.sh` 会：
把 release APK 归集到 `dist/`（分 ABI），并复制一份为模块 ZIP 内的 `apk/syfuse.apk`，
由 `customize.sh` / `action.sh` 第 1 项安装。

为什么要有这个目录：提示词 3.1 的产品形态是
**模块内置 APK**（模块给系统级能力，App 给界面与配置）。
所以 APK 是模块的**内置组件**，不是独立分发的应用。

**当前状态：已注入。** v0.1.0 的 `dist/syfuse-0.1.0.zip` 内已含 `apk/syfuse.apk`
（取自 `arm64-v8a` release 构建，1.3 MB），`dist/` 下另有三个分 ABI APK。

**但仍属`实验性`**：APK 可构建、可安装，但 App 内部功能尚未接通代理内核（M3），
真机可用性未验证。`package.sh` 在拿不到 APK 时会走缺件路径并**退出码 1**，
不会产出"看起来完整"的包。
