# Syfuse · 融合代理

原生 Android 代理客户端。主干对齐 iOS Shadowrocket 的功能与界面结构，
差异层只做 Shadowrocket **没有**的能力：内容过滤引擎、开屏广告跳过、
通知清理、网络层拦截、root/LSPosed 特权层、配置即产品。

本仓库处于 **M1（骨架）阶段**。请先读「当前完成度」一节，
它如实列出哪些能力已可运行、哪些尚未交付。

---

## 目录

- [当前完成度](#当前完成度)
- [构建环境](#构建环境)
- [构建与验证](#构建与验证)
- [仓库结构](#仓库结构)
- [Shadowrocket 功能覆盖表](#shadowrocket-功能覆盖表)
- [账号 → 包名映射](#账号--包名映射)
- [安装](#安装)
- [权限](#权限)
- [规则语法](#规则语法)
- [脚本 API](#脚本-api)
- [本地 API](#本地-api)
- [性能预算与实测](#性能预算与实测)
- [安全模型与红线](#安全模型与红线)
- [免责声明](#免责声明)
- [致谢与许可](#致谢与许可)

---

## 当前完成度

**已可运行且已验证**（有原始命令输出为证，见 [构建与验证](#构建与验证)）：

| 能力 | 位置 | 状态 |
|---|---|---|
| 过滤规则解析（hosts / dnsmasq / ABP 三类语法） | `core/rule` | 已实现，corpus 38 条期望全绿 |
| 优先级匹配模型（重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中） | `core/rule/FilterEngine.kt` | 已实现，非短路、取最高秩 |
| 域名后缀 Trie（标签级，防 `notexample.com` 误命中 `example.com`） | `core/rule/DomainTrie.kt` | 已实现 |
| IPv4 CIDR 二叉 Trie（最长前缀匹配） | `core/rule/Ipv4CidrTrie.kt` | 已实现 |
| `$badfilter` 编译期消解、签名重建防正则 `$` 误拆 | `core/rule/FilterEngine.kt` | 已实现 |
| 设置目录（11 组，含风险标注/专家模式/搜索/恢复推荐值） | `domain/settings` | 已实现 |
| 配置编解码（未知字段保留、拒绝高版本 schema） | `domain/config` | 已实现 |
| 引擎状态机 + 流量计量（EMA，含计数器回退检测） | `domain/engine` | 已实现 |
| 凭据保险库（Android Keystore AES-256-GCM） | `data/security` | 已实现 |
| Room 持久化 + schema 导出 | `data/db` | 已实现 |
| Compose 四 Tab 骨架 + 四主题（含 AMOLED 纯黑） | `ui` | 已实现 |
| 特权层命令白名单（sealed enum，禁止拼字符串） | `privileged` | 已实现 |
| LSPosed 桥接契约（HookSpec / HookStats / 受保护注册） | `lsposed-bridge` | 已实现 |
| KernelSU/Magisk 模块骨架 + 卸载还原 | `module/` | 已实现 |
| 订阅 URI 解析（8 种协议，Go 实现） | `kernel/src/node.go` | 已实现，55 个 Go 用例全绿 |
| 内核配置校验（schema 版本拒绝、FakeIP 前置条件） | `kernel/src/config.go` | 已实现，20 个 Go 用例 |
| 内核凭据隔离（白名单投影，UI 用的 JSON 不含密码） | `kernel/src/node.go` | 已实现，泄漏专项用例 |

**尚未交付**（不在本版本内，代码中亦不假装可用）：

| 能力 | 现状 |
|---|---|
| **代理内核数据面**（TCP/UDP 转发、TLS 握手、协议编解码） | **未实现**。`Engine.Start()` 做完配置校验与节点解析后**如实返回** `ErrTunnelNotImplemented`，不返回 nil 假装成功。有专门测试守着这条行为 |
| VpnService 隧道 | 未实现。Manifest 未声明 `BIND_VPN_SERVICE`，因为声明了却没有实现属于虚假能力声明 |
| 内核 AAR 构建 | `kernel/tools/build_aar.sh` 已就绪，但需要 gomobile + NDK，本次未执行。因此 `engine` 仍绑定 `NotLinkedDataPlane` |
| 域名嗅探 / DNS 分流 / 透明代理 | 未实现（内核侧只有配置字段） |
| 广告过滤在数据面的实际生效 | **未生效**。规则引擎可离线运行与测试，但没有任何数据面在调用它。UI 规则页的"规则测试"是真实运行引擎的 |
| 开屏广告跳过 / 通知清理 | 未实现 |
| LSPosed 具体 Hook 逻辑 | 未实现。只有契约与注册保护 |
| 规则引擎线性路径索引化 | 未做。实测 p99 = 0.965 ms，**超出 0.5 ms 预算**（见「性能预算与实测」） |
| 界面文案本地化 | 117 处用户可见文案内联在 Compose / ViewModel 中，尚未抽取到 `strings.xml` |
| 远端发布（GitHub Release / 模块仓库） | **未执行**。见 [待执行的一步](#待执行的一步) |

> 之所以把这些明确列出来：本工程的第一条纪律是「不编造」。
> 一个把未完成能力说成已完成的项目，比一个明确说"还没做"的项目危险得多。

---

## 构建环境

本项目不假设你的机器上已经装好 Android 工具链。全部依赖可落到一个隔离目录里：

| 组件 | 版本 | 获取方式 |
|---|---|---|
| JDK | Temurin 21.0.12.1+1 | `https://api.adoptium.net/v3/binary/latest/21/ga/windows/x64/jdk/hotspot/normal/eclipse` |
| Gradle | 8.11.1 | `https://services.gradle.org/distributions/gradle-8.11.1-bin.zip` |
| Android SDK | platform 35 + build-tools 35.0.0 + platform-tools | `https://dl.google.com/android/repository/commandlinetools-win-*.zip` 后接 `sdkmanager` |
| Go（仅构建内核时需要） | 1.23.4 | `https://dl.google.com/go/go1.23.4.windows-amd64.zip` |

`tests/env.sh` 会按以下顺序自动定位它们，**无需手工配置**：

1. 显式环境变量（`JAVA_HOME` / `ANDROID_HOME` / `SYFUSE_GRADLE`）
2. `$SYFUSE_TOOLCHAIN`（默认 `$HOME/.workbuddy-ai/toolchain`）
3. 系统 PATH

定位失败时脚本 `exit 2` 并把缺什么打到 stderr —— 不会静默降级成"通过"。

### 为什么固定这些版本

- **Gradle 8.11.1 + AGP 8.7.3**：这是 AGP 8.7 支持的 Gradle 下限组合，再高会引入未验证的
  Kotlin/Compose 编译器组合。
- **Kotlin 2.1.0 + KSP 2.1.0-1.0.29**：KSP 版本必须与 Kotlin 版本严格配对，否则 Hilt 处理器
  会静默不生成代码，表现为运行时 `ClassNotFoundException` 而非编译错误。
- **compileSdk 35 / minSdk 26**：26 是 `VpnService` 的 `setUnderlyingNetworks` 与
  `NetworkStatsManager` 可用的下限；再低需要大量兼容分支。

---

## 构建与验证

```bash
# 单元测试（不需要设备，不需要 Android SDK 之外的任何东西）
tests/run_unit.sh

# Go 内核验证：gofmt + vet + build + 55 个用例 + 零依赖校验
tests/run_kernel.sh

# 红队静态检查：把六条硬红线变成可执行断言
tests/run_module_checks.sh

# UI 资源完整性（不需要设备）
tests/ui_assets_check.sh

# 凭据扫描
python tools/scan_secrets.py

# 构建 debug APK
gradle :app:assembleDebug

# 以下三项需要设备
tests/run_e2e.sh          # 装得上、起得来、不崩
tests/ui_runtime_check.sh # 界面元素在、点击生效
tests/ui_screenshot.sh    # 产出各页面 × 各主题截图

# 性能
tests/perf_rule_engine.sh        # 规则引擎编译/匹配基准
tests/perf_apk_size.sh           # 分 ABI 体积

# 打包（APK + 模块 zip + SHA256 清单）
tests/package.sh

# 内核 AAR（需要 gomobile + NDK；缺前置时 exit 2，不会静默产出空包）
kernel/tools/build_aar.sh

# 远端状态核验（需要 GITHUB_TOKEN）
python tools/verify_remote.py --repo <owner>/Syfuse --expect-public false
```

### 退出码约定

所有验证脚本遵守同一套约定，目的是**让"没跑"无法伪装成"跑过"**：

| 退出码 | 含义 |
|---|---|
| `0` | 真的通过了 |
| `1` | 真的失败了 |
| `2` | 环境缺失（找不到 JDK / Gradle / SDK） |
| `3` | 前置条件不满足而跳过（例如没有设备）——**不是通过** |

CI 里对 `3` 的处理是"标记为 skipped 并在 PR 上提示"，绝不是"绿色"。

---

## 仓库结构

```
Syfuse/
├── app/                  应用壳：Application / MainActivity / Manifest / 资源 / ABI splits
├── ui/                   Compose 设计系统与全部界面（四 Tab、四主题、四状态）
├── domain/               领域模型与用例契约（纯 JVM，可在无 Android 环境单测）
│   ├── model/            Profile / ProxyNode / RuleEntry / RoutingMode ...
│   ├── settings/         设置目录（11 组）与 SettingSpec
│   ├── config/           配置包编解码（未知字段保留）
│   └── engine/           EngineController / DataPlane / EngineState / TrafficMeter
├── core/
│   ├── common/           SyfuseResult / 错误类型
│   └── rule/             过滤规则引擎（纯 JVM）
│       ├── DomainTrie.kt         反标签后缀 Trie
│       ├── Ipv4CidrTrie.kt       二进制 Trie，最长前缀
│       ├── PatternCompiler.kt    ABP 模式 → 匹配器
│       ├── ModifiersParser.kt    $third-party 等全部修饰符
│       ├── CosmeticRule.kt       元素隐藏规则（6 种标记）
│       ├── RuleParser.kt         hosts / dnsmasq / ABP 三类语法识别
│       └── FilterEngine.kt       编译 + $badfilter + 优先级匹配
├── data/                 Room / DataStore / Keystore 凭据库 / 仓储
├── engine/               内核抽象与 IPC 契约（DataPlane 实现唯一替换点）
├── kernel/               Go 代理内核
│   ├── go.mod            零第三方依赖
│   ├── src/              节点解析 / 配置校验 / 引擎 API（55 个 Go 用例）
│   ├── tools/build_aar.sh gomobile bind 入口
│   └── Makefile
├── privileged/           root 特权层（libsu），命令白名单
├── lsposed-bridge/       LSPosed 桥接契约
├── module/               KernelSU / Magisk 模块
│   ├── module.prop
│   ├── customize.sh      安装钩子（$MODPATH）
│   ├── post-fs-data.sh   早期启动
│   ├── service.sh        延迟启动
│   ├── action.sh         模块管理器里的"动作"按钮
│   ├── uninstall.sh      卸载还原
│   └── META-INF/         update-binary / updater-script
├── tests/                验证套件（见上方「构建与验证」）
│   ├── corpus/           规则语料
│   ├── expect.tsv        语料期望表（唯一事实来源）
│   └── *.sh
├── tools/                scan_secrets / verify_files / verify_remote / set_owner
├── .github/workflows/    CI
├── update.json           模块更新清单
├── LICENSE               GPL-3.0-or-later
└── NOTICE                第三方组件与参考来源署名
```

### 模块边界的一条硬规矩

**UI 层不感知任何内核具体类型。** `ui` 依赖 `domain.engine.EngineController`（接口），
内核实现只通过 `engine` 模块的 `EngineModule` 注入。换内核（NotLinked → Go AAR → 其它）
只改一个文件的绑定，UI 与规则引擎零改动。

---

## Shadowrocket 功能覆盖表

三态标记：

- **复刻** —— 与 Shadowrocket 功能等价、概念同名、操作路径同构、默认值合理
- **增强** —— Shadowrocket 没有、本工程额外提供的差异能力
- **不做** —— 有意不做，并说明原因

### 主功能（复刻）

| 功能 | 状态 | 说明 |
|---|---|---|
| 首页大开关（连接/断开） | 复刻 | 四态：未连接 / 连接中 / 已连接 / 连接失败。**当前内核未接入，停在"未连接"并显示原因** |
| 实时上下行速率 | 复刻 | 1 秒采样；内核未接入时显示 0 而非伪造数字 |
| 累计流量 | 复刻 | 由 `TrafficMeter` 计量，含计数器回退（重连）检测 |
| 节点列表 | 复刻 | 名称/服务器/协议，支持搜索 |
| 节点测速 | 复刻 | 批量 URL 测试；无内核时明确报"未接入" |
| 路由模式（全局/规则/直连） | 复刻 | 三模式，概念与操作路径对齐 |
| 规则页 | 复刻 | 规则源管理 + **可交互的规则测试**（真实运行引擎，展示完整匹配链路） |
| 设置页 | 复刻 | 11 组，含搜索、专家模式、危险项二次确认 |
| 配置文件导入/导出 | 复刻 | 导入前自动备份，失败自动回滚；未知字段保留 |
| 主题 | 复刻 | 跟随系统 / 浅色 / 深色 / AMOLED 纯黑 |
| 分应用代理 | 不做 | 需要 `QUERY_ALL_PACKAGES` 与包可见性声明，权限代价高于收益；且与 R3 的"最小权限"冲突 |
| 流量统计图表 | 不做 | Shadowrocket 该功能本身较弱，复刻价值低 |

### 差异能力（增强）

| 能力 | 状态 | 说明 |
|---|---|---|
| 内容过滤引擎 | **部分** | 规则语义与匹配模型已实现且可测（AdGuard Home 规则语义 × uBlock Origin 优先级模型）；**数据面尚未接入**，因此目前不产生实际拦截 |
| 优先级模型（非短路） | 复刻增强 | 收集全部候选后取最高秩，而不是首个命中即返回。这让"重要放行"能压过"普通拦截"，语义更可预期 |
| 开屏广告跳过 | 不做 | 需要 LSPosed 具体 Hook，本版本只有契约 |
| 通知清理 | 不做 | 同上 |
| 网络层拦截 | 不做 | 依赖内核与 VpnService |
| root / LSPosed 特权层 | **骨架** | 命令白名单与模块骨架已实现；具体能力未接入 |
| 配置即产品 | 复刻增强 | 配置包编解码支持未知字段保留与版本拒绝，便于跨版本演进 |

**元素隐藏规则（cosmetic rules）的诚实声明**：`##` / `#@#` 等规则需要**渲染上下文**
（DOM 或视图树）才能生效。它们只能在两条路径上工作：MITM 时重写 HTML，或在局域网面板里注入。
**在纯 DNS/连接层上它们无法生效**，本项目不会把它们计入"拦截数"。
`CosmeticControl` 只负责解析与开关，不做效果承诺。

---

## 账号 → 包名映射

包名在运行时从账号推导，**不硬编码**：

```
GitHub 账号  →  规范化（转小写 + 去连字符/下划线/点）  →  包名
vip-svip     →  vipsvip                              →  io.github.vipsvip.syfuse
Some_User    →  someuser                             →  io.github.someuser.syfuse
```

解析顺序（`tools/set_owner.sh`）：

1. `$SYFUSE_OWNER`
2. `$GITHUB_REPOSITORY_OWNER` / `$GITHUB_ACTOR`（GitHub Actions 自动注入）
3. `gh api user --jq .login`
4. `git remote get-url origin` 中的 owner
5. 保守默认值 `vip-svip`（**会显式打印"这是默认值而非解析结果"**）

改账号：

```bash
tools/set_owner.sh --print              # 先看会改成什么
tools/set_owner.sh --apply              # 改写包名 + 目录 + 署名
tools/set_owner.sh --apply --owner xxx  # 显式指定
```

**署名不写 AI 工具名**，写解析出的账号。

---

## 安装

### 应用

```bash
# 从 Release 下载对应 ABI 的 APK
adb install -r Syfuse-v0.1.0-arm64-v8a.apk
```

支持的 ABI：`arm64-v8a` / `armeabi-v7a` / `x86_64`。分 ABI 打包是为了控制单包体积。

### 特权层模块（KernelSU / Magisk）

```bash
# 从 Release 下载 Syfuse-Module-v0.1.0.zip，在模块管理器里"从本地安装"
# 或
adb push Syfuse-Module-v0.1.0.zip /sdcard/
# 然后在 KernelSU/Magisk 应用中安装该 zip
```

安装器会把模块落到 `/data/adb/modules/syfuse`，并创建 `/data/adb/syfuse`（0700）作为数据目录。

**卸载即还原**：`module/uninstall.sh` 会删除模块目录、`/data/adb/syfuse`、以及
`service.sh` 注册的任何运行时文件。不触碰系统分区。也可在模块管理器里直接移除。

> ⚠️ 本版本模块包**不含内核二进制**（M2 未完成）。安装它只会得到特权层骨架，
> 不具备实际代理能力。

---

## 权限

应用只申请两个权限：

| 权限 | 用途 |
|---|---|
| `INTERNET` | 代理本身 |
| `ACCESS_NETWORK_STATE` | 判断网络可用性，用于连接状态机 |

**未申请**（刻意）：`QUERY_ALL_PACKAGES`、`READ_PHONE_STATE`、`AD_ID`、
定位、通讯录、短信、录音。`allowBackup=false`，凭据不随系统备份外流。

未来实现 VpnService 时会新增 `BIND_VPN_SERVICE`（`android:permission` 属性，非用户授权项）
与用户可见的 VPN 授权弹窗 —— 到那时才会加，现在不加是因为**声明了不实现等于虚假声明**。

---

## 规则语法

支持的三种输入形态（可混在同一个规则源里，解析器逐行识别）：

### 1. hosts

```
0.0.0.0 ads.example.com
127.0.0.1 tracker.example.net
```

### 2. dnsmasq

```
address=/ads.example.com/0.0.0.0
server=/example.org/1.1.1.1
```

### 3. AdGuard Home / ABP 语义

```
||ads.example.com^             域名及其子域
||ads.example.com^$important   重要规则（优先级最高）
@@||safe.example.com^          例外放行（白名单）
/ads\/banner\d+/               正则
10.0.0.0/8                     网段（最长前缀匹配）
example.com##.ad-banner        元素隐藏（需渲染上下文，见上方说明）
example.com#@#.ad-banner       元素隐藏例外
||a.com^$third-party,script    带修饰符
||a.com^$badfilter             编译期消解该条
```

### 优先级模型

不是"首个命中即返回"，而是**收集全部候选后取最高秩**：

```
重要放行 (important + 例外)   rank 4
重要拦截 (important)          rank 3
普通放行 (例外)               rank 2
普通拦截                      rank 1
未命中                        rank 0
```

这样 `@@||x^` 与 `||x^$important` 同时存在时结果可预期，不受规则书写顺序影响。

### 修饰符

支持 `$important` `$third-party` / `$3p` `$first-party` / `$1p` `$badfilter`
`$domain=` `$script` `$image` `$stylesheet` `$xhr`（含 `xmlhttprequest` 别名）
`$subdocument` `$document` `$font` `$media` `$websocket` `$other` `$popup`
`$all` 等。未知修饰符产生 **WARNING 而非静默丢弃** —— 静默丢弃会让规则集
悄悄失效，是最难排查的一类问题。

### 语料与期望表

`tests/corpus/01_syntax_mixed.txt` 是规则语料，`tests/expect.tsv` 是期望表
（唯一事实来源）。`CorpusConsistencyTest` 会逐条比对，任何一条不符即失败。

---

## 脚本 API

模块内的 shell 脚本约定：

| 脚本 | 何时被调用 | 职责 |
|---|---|---|
| `customize.sh` | 安装时被安装器 `source` | 按架构落位内核、建数据目录、收敛权限。**必须用 `$MODPATH`**（`$0` 指向安装器本身） |
| `post-fs-data.sh` | 早期启动（`/data` 挂载后） | 只做不能延后的事（如创建运行时目录）。此时网络与大部分服务尚不可用 |
| `service.sh` | 启动后延迟执行 | 拉起常驻进程。必须自带存活检查，不能假设只跑一次 |
| `action.sh` | 用户在模块管理器点"动作" | 交互入口（如打印状态、重载配置） |
| `uninstall.sh` | 卸载时 | **完整还原**：删模块目录、删 `/data/adb/syfuse`、清运行时文件 |

约定：脚本只写模块目录与 `/data/adb/syfuse`，**不触碰系统分区**（侵入原则）。

---

## 本地 API

规划中的本地面板/API（用于局域网查看状态、批量导入配置）：

- 默认**关闭**。开启需要显式操作，且默认只监听回环。
- 需要 token 鉴权；token 由应用生成，不写入仓库。
- 只读接口（状态/规则统计）与写接口（导入配置）分离授权。

**本版本未实现**。上面写的是接口约定，不是已有能力 —— 之所以先写下来，
是为了让后续实现不会退化成"默认开启、无鉴权"。

---

## 性能预算与实测

预算（第 11 部分目标）与实测是两回事，这里分开记。

| 指标 | 预算 | 实测（2026-10-12，Windows / JDK 21.0.12.1 / 1M 规则） | 判定 |
|---|---|---|---|
| 100 万条规则解析 | < 1 s | **468 ms** | ✅ |
| 100 万条规则编译 | < 3 s | **439 ms**（索引化 990,000 / 线性 10,000） | ✅ |
| 单次匹配 p99 | < 0.5 ms | **0.965 ms** | ❌ **超预算约 1.9 倍** |
| 单次匹配 p50 | — | 0.701 ms | — |
| 匹配吞吐 | — | 1,503 次/秒 | — |
| release 单 ABI APK | < 15 MB | 见 `tests/perf_apk_size.txt` | — |
| 冷启动到首帧 | < 800 ms | 需要设备，见 `tests/run_e2e.sh` 输出 | — |

原始数据在 `tests/perf_results.txt`，由 `tests/perf_rule_engine.sh` 生成。
**不把数字写进正文** —— 正文里的数字会随机器变化而失真，落文件才能核对。

### 关于 p99 超预算

**这是一个真实的、未解决的性能缺口，不是测量误差。** 根因是清楚的：

基准里 1% 的规则是线性正则（10,000 条），而采样中有一半是**未命中域名 Trie** 的请求。
未命中时引擎必须逐条测试全部 10,000 条线性规则 —— 这是 O(n) 的，
p99 因此被这一条路径主导（p50 与 p99 只差 0.26 ms，说明分布很窄，瓶颈就是"线性遍历"本身）。

**这不是"再调调参数"能解决的问题**，需要真正的索引化改造：给正则规则提取必需的
字面量子串（例如 `/tracker\d+/` 的字面量是 `tracker`），按字面量分桶，
未命中 Trie 时只测同桶的规则。uBlock Origin 用的是同类思路。

**我没有在这次交付里做这个改造。** 理由：它需要改动 `PatternCompiler` 与 `FilterEngine`
的核心匹配路径，而当前 `tests/expect.tsv` 的 38 条期望只覆盖语义正确性，
不足以证明索引化改造后语义不变。先补语料、再改索引，顺序不能反。

这条已记入 [`CHANGELOG.md`](CHANGELOG.md) 的「已知缺口」。

> 顺带一提：对 DNS 层过滤的实际场景（每秒几十次查询量级），0.7–1.0 ms 的单次匹配
> 完全够用。所以这不是"能不能上线"的问题，而是"宣称的预算没达到"的问题。
> 把没达到的预算说成达到了，才是真正的问题。


---

## 安全模型与红线

### 六条硬红线

| 编号 | 红线 | 执行方式 |
|---|---|---|
| R1 | 主界面必须原生 Compose，**无 WebView** | `run_module_checks.sh` 静态检索 `android.webkit` / `WebView` / `loadUrl`；同时反向验证确实用了 `setContent` |
| R2 | 内核**静态随包**，不在运行时下载二进制 | 静态检索 `DownloadManager` / `Runtime.exec("curl`；校验存在 AAR / jniLibs 接入点 |
| R3 | **零遥测** | 版本目录中禁止出现 firebase / crashlytics / sentry / umeng / bugly / appsflyer / adjust 等坐标；Manifest 权限白名单校验 |
| R4 | **不编造** | 禁止 `NotImplementedError` / `TODO(implement)`；版本号三处必须一致；UI 不得硬编码成功态文案 |
| R5 | 密钥不进仓库、不落明文 | `tools/scan_secrets.py` 作为提交前闸门；`allowBackup=false`；`local.properties` 与 `*.jks` 在 `.gitignore` |
| R6 | 危险能力**显式披露** + **一键还原** | 本文件与 `DISCLAIMER.md` 必须存在且含还原说明；`uninstall.sh` 必须真做清理 |

### 凭据处理

- 订阅链接、密码等敏感值存在 Android Keystore 保护的 AES-256-GCM 保险库中，
  存储格式 `v1:<b64 iv>:<b64 ct>`。
- 仓库内**不存在**任何真实凭据。`scan_secrets.py` 会对 git 跟踪的全部文本文件做正则扫描，
  命中即失败。

### 危险能力的披露

特权层（root / LSPosed）属于高风险能力：

- 它能让应用修改**其它应用**的行为（广告过滤、通知清理）。
- 它要求设备已解锁并安装 KernelSU/Magisk。
- 误用可能导致目标应用崩溃或数据异常。

因此：所有危险项在设置页带 `RiskBadge`（三重编码：颜色 + 图标 + 文字，
不只靠颜色，保证色觉障碍可用），且带 `requiresConfirm` 的项一律二次确认。
所有专家级改动都可一键恢复推荐值。

---

## 免责声明

见 [`DISCLAIMER.md`](DISCLAIMER.md)。摘要：本项目仅供学习与研究网络协议使用。
使用者需自行确认在当地法律法规下使用代理工具的合法性，并对自己的行为负责。
作者不对任何因使用本软件导致的损失承担责任。

---

## 致谢与许可

- 本项目以 **GPL-3.0-or-later** 发布，见 [`LICENSE`](LICENSE)。
  许可证**不会**改成 MIT —— 内核侧的 GPL 传染性决定了整体必须留在 GPL 族。
- 参考了 [`vip-svip/SY-Fusion`](https://github.com/vip-svip/SY-Fusion) 的
  **规则语义设计与工程验证纪律**。该仓库当前**未声明许可证**，因此本项目
  **未复制其任何代码**，只在设计层面借鉴并在 [`NOTICE`](NOTICE) 中具名致谢。
- 第三方组件与许可证清单见 [`NOTICE`](NOTICE)。

---

## 待执行的一步

以下动作需要 GitHub 凭据，本地无法完成。**这是一条命令，不是一个问题**：

```bash
# 1) 登录（若无 gh）
gh auth login

# 2) 建私有仓库并推送（默认私有，不得自动改为公开）
cd "$(git rev-parse --show-toplevel)"
gh repo create "$(gh api user --jq .login)/Syfuse" --private --source=. --remote=origin --push

# 3) 统一包名与署名到你的账号
tools/set_owner.sh --apply && git commit -am "chore: set owner" && git push

# 4) 核验远端状态（必须报 private 且 license 为 GPL-3.0 族）
python tools/verify_remote.py --repo "$(gh api user --jq .login)/Syfuse" --expect-public false
```

发布首个 Release（可选，需先完成 M2 内核构建）：

```bash
tests/package.sh
gh release create v0.1.0 --title "Syfuse v0.1.0" \
  --notes-file CHANGELOG.md dist/Syfuse-v0.1.0-*.apk dist/Syfuse-Module-v0.1.0.zip
```
