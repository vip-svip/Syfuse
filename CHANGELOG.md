# 更新日志

本文件记录**已交付**的变更。计划中的能力不写在这里 —— 计划在 README 的
「当前完成度」表里，两者严格分开。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

---

## [未发布]

### 修复（交付链路与身份一致性）

这一轮没有加新功能，全部是"让已有的东西真的成立"。

- **`package.sh` 会在 `zip` 缺失时静默产出不完整产物并退出 0**。Windows Git Bash 默认没有
  `zip`，于是**模块 ZIP（提示词 3.5 的交付主体）根本没被生成**，脚本却报"打包完成"。
  修法：加 Python `zipfile` 便携回退（保留权限位），两条路径都失败则走缺件路径，
  且**缺件时退出码改为 1**。现在 `dist/syfuse-0.1.0.zip` 可产出并通过 SHA-256 自校验。
- **失效的仓库 URL**。`.github/ISSUE_TEMPLATE/*.yml` 与 `update.json` 里的
  `github.com/vipsvip/Syfuse` 是错的 —— `vipsvip` 是**包名段**（Java 包名不能有连字符），
  不是 GitHub 账号 `vip-svip`，链接全部 404。已修正，并给 `tools/set_owner.sh` 补上
  URL owner 改写（原脚本只改包名引用，这正是缺口长期存在的原因）。
- **`update.json` 与实际产物对不上**：`zipUrl` 的资产名是 `Syfuse-Module-v0.1.0.zip`，
  而实际产出是 `syfuse-0.1.0.zip`；`changelog` 指向错误的 owner。现在由 `package.sh`
  从 `git remote` 解析 owner 后写入，`sha256` 与构建产物一致。
- **Go module path 指向不存在的仓库**（`github.com/vipsvip/syfuse/src`）→ 对齐真实仓库路径
  `github.com/vip-svip/syfuse/src`，`go build` / `go test` 复验通过。
- **玻璃策略层的"超标两次提示用户"从未触发过**。`GlassPerformanceController.onFrame`
  在降档后调用 `monitor.reset()`，紧接着的帧样本不足；旧实现只看 `isOverBudget()`
  （样本不足时它也返回 `false`），于是每轮都把 `consecutiveViolations` 清零，
  `WARN_AFTER_VIOLATIONS = 2` **不可达**。修法：新增 `FrameMonitor.hasEnoughSamples()`，
  样本不足时不下结论也不清计数。已补回归用例钉住。
- **`tools/set_owner.sh` 在受限环境下超时**（每文件 spawn `grep`/`sed`/`rm`，183 个文件
  >180 s）。改为 `git grep -l` 先筛候选 + `sed -i` 就地改写，实测降到 **33 s**。
- 文档陈旧引用：`CHANGELOG` 里 `kernel/tools/build_aar.sh` 在目录重构后已不存在（实际为
  `src/tools/build_aar.sh`）；README 的状态徽章与「功能可用性报告」仍停留在 M1，
  与已落地的 M2 代码矛盾。

### 新增

- `ui/src/test/.../GlassPolicyTest.kt`：玻璃策略层 26 条 JVM 用例（强度档位、白名单、
  层数上限、P90 判定、超标降档状态机）。**补它的原因**：`Glass.kt` 的文件注释一直声称
  这个测试类存在，实际不存在 —— 那是一句无法验证的声明，提示词 2.9 不允许。
- `PROGRESS.md`、`PENDING-EXTERNAL.md`、`docs/EVIDENCE.md`：续跑协议与证据落盘（提示词 2.10 / 18.1 要求，此前缺失）。

### 计划中（M2）

- 代理内核（Go，`gomobile bind` → AAR），静态随包
- VpnService 隧道建立
- 订阅解析：ss / ssr / vmess / vless / trojan / hysteria
- 规则引擎接入数据面（当前规则引擎可离线运行，但无数据面调用它）
- 域名嗅探与 DNS 分流
- 界面文案抽取到 `strings.xml`（当前 372 处内联，见「已知缺口」）

---

## [v0.1.0] — 骨架版本

首个可编译、可测试的骨架。**不含代理内核**，因此不具备实际代理能力。

### 新增

**过滤规则引擎（`core/rule`，纯 JVM）**

- 三类输入语法识别：hosts / dnsmasq / AdGuard Home-ABP
- 优先级匹配模型：收集全部候选后取最高秩，**非短路**
  （重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中）
- `DomainTrie`：反标签后缀 Trie，标签级匹配（防止 `notexample.com` 误命中 `example.com`）
- `Ipv4CidrTrie`：二进制 Trie，最长前缀匹配
- `PatternCompiler`：IPv4 CIDR → `||domain^` → `/regex/` → 协议锚点 → 通配
- 全部 6.1 节修饰符：`$important` `$third-party`/`$3p` `$first-party`/`$1p`
  `$badfilter` `$domain=` `$script` `$image` `$stylesheet` `$xhr`(含 `xmlhttprequest` 别名)
  `$subdocument` `$document` `$font` `$media` `$websocket` `$other` `$popup` `$all`
- 未知修饰符产生 **WARNING 而非静默丢弃**
- `$badfilter` 编译期消解；签名从结构化字段重建，规避正则 `$` 锚点误拆
- `CosmeticRule`：6 种元素隐藏标记（`##^` `#@#` `#$#` `#?#` `#%#` `##`），按最长优先匹配

**领域层（`domain`，纯 JVM）**

- 设置目录：11 组，`SettingSpec` 带 title / subtitle / risk / default / expertOnly / requiresConfirm
- 设置搜索（范围包含被专家模式隐藏的项）
- 配置包编解码：未知字段保留（`rawExtras`），拒绝更高 `schemaVersion`
- `EngineController` 状态机：未连接 / 连接中 / 已连接 / 连接失败
- `TrafficMeter`：EMA 速率估算，含计数器回退（重连）检测

**数据层（`data`）**

- Room 持久化 + schema 导出（供迁移审计）
- `CredentialVault`：Android Keystore AES-256-GCM，存储格式 `v1:<b64 iv>:<b64 ct>`
- `AppSettingsDataStore`：DataStore 键值设置
- `ConfigRepository`：导入前自动备份，失败自动回滚

**内核抽象（`engine`）**

- `DataPlane` 接口 —— 内核实现的唯一替换点
- `NotLinkedDataPlane`：**如实**返回"内核未接入"，不伪造连接成功
- `EngineModule`：单点绑定，换内核不改 UI

**特权层（`privileged`）**

- `PrivilegeCommand`：sealed enum 命令白名单，**禁止拼接命令字符串**，参数正则校验
- libsu 6.0.0 集成与可用性探测

**LSPosed 桥接（`lsposed-bridge`）**

- `HookContract`：`HookSpec` / `HookStats` / `HookRegistry.guarded`（异常隔离）
- `xposed_init` 入口
- **注意：只有契约与注册保护，没有具体 Hook 逻辑**

**Go 内核（`kernel/`）**

- **零第三方依赖**（`go.mod` 无 `require`，无 `go.sum`）。理由：交叉编译成功率、
  供应链面、以及"`GOPROXY=off` 也能跑测试"——后者直接决定内核逻辑可验证性是不是空话
- 订阅 URI 解析：`ss`（SIP002 / 整体 base64 / 明文 userinfo 三种历史格式）、`ssr`、
  `vmess`（base64 JSON，port/aid 兼容字符串与数字）、`vless`（含 reality 判定）、
  `trojan`（默认 TLS，`security=none` 可关）、`hysteria2`（含 `hy2://` 别名）、
  `http(s)`、`socks5`
- IPv6 字面量支持（`[2001:db8::1]:8388`）；未加方括号时**明确拒绝**而不是猜端口
- 订阅"部分成功"语义：坏行不废掉整份订阅，错误带**行号**定位
- 节点去重：`Key()` 不含名称（同名不同参数不去重；同参数不同名去重）
- **凭据隔离用白名单投影**（`nodePublic`）而非黑名单清空。
  黑名单在新增敏感字段时会静默失效；白名单默认"不输出"
- 配置严格校验：拒绝更高 `schemaVersion`、路由模式白名单、日志级别白名单、
  `fakeIp` 必须带 `fakeIpRange`
- 引擎状态机：`not_linked` / `failed` / `stopped`；`Start()` 做完**真实**的配置校验与
  节点解析后返回 `ErrTunnelNotImplemented`，**不返回 nil 假装成功**
- 55 个 Go 用例，`gofmt` / `go vet` / `go build` / `go test` 全绿
- `kernel/tools/build_aar.sh`：gomobile bind 入口，含 ABI→gomobile target 映射；
  缺 gomobile 或 NDK 时 **exit 2** 并打印安装步骤，不静默产出空 AAR
- `tests/run_kernel.sh`：内核验证入口，含**零依赖断言**（出现 `require` 或 `go.sum` 即失败）

**界面（`ui`）**

- 四 Tab：首页 / 节点 / 规则 / 设置
- 四主题：跟随系统 / 浅色 / 深色 / AMOLED 纯黑（无 blur，滚动容器全虚拟化）
- 四状态组件集：加载 / 空 / 错误 / 内容
- `RiskBadge`：颜色 + 图标 + 文字三重编码（色觉障碍可用）
- 规则页含**可交互的规则测试**，真实运行引擎并展示完整匹配链路

**模块（`module/`，KernelSU / Magisk）**

- `module.prop`（id=syfuse）、`customize.sh`（使用 `$MODPATH`）、
  `post-fs-data.sh`、`service.sh`、`action.sh`、`uninstall.sh`
- `META-INF/com/google/android/update-binary`（shell 脚本）+ `updater-script`
- 安装只写模块目录与 `/data/adb/syfuse`，**不触碰系统分区**
- `uninstall.sh` 完整还原

**验证套件（`tests/`）**

- `env.sh`：工具链自动定位；找不到时 `exit 2`，不静默降级
- `run_unit.sh`：JVM 单测 + JUnit XML 计数汇总
- `run_e2e.sh`：安装 / 冷启动 / 崩溃与 ANR 检查 / UI 层级断言（需设备）
- `run_module_checks.sh`：**把六条硬红线变成可执行断言**
- `ui_assets_check.sh`：资源引用解析、自适应图标、XML 良构性
- `ui_runtime_check.sh`：真实渲染 + 交互断言（需设备）
- `ui_screenshot.sh`：各页面 × 各主题截图（需设备）
- `perf_rule_engine.sh` / `perf_apk_size.sh`：性能原始数据采集
- `package.sh`：APK + 模块 zip + SHA256 清单 + 自校验
- 语料对拍：`tests/corpus/01_syntax_mixed.txt` + `tests/expect.tsv`（38 条期望）

**工具（`tools/`）**

- `scan_secrets.py`：凭据扫描闸门（BLOCK / WARN 分级，命中即失败）
- `verify_files.py`：SHA256 清单校验
- `verify_remote.py`：远端仓库状态核验（私有性 + 许可证 + 关键文件）
- `set_owner.sh`：账号运行时解析 → 包名/署名统一改写

**仓库**

- `LICENSE`：GPL-3.0 逐字全文，SPDX `GPL-3.0-or-later`
- `NOTICE`：署名、第三方组件、参考来源（含 SY-Fusion 无许可证的边界说明）
- `README.md`：单一文档入口，含 Shadowrocket 功能覆盖表
- `DISCLAIMER.md` / `SECURITY.md` / `CONTRIBUTING.md`
- `.github/workflows/`：CI

### 已知缺口

以下能力**未交付**，代码中亦不假装可用：

| 缺口 | 现状 |
|---|---|
| 代理内核 | 未构建。`engine` 绑定 `NotLinkedDataPlane`，`start()` 如实返回"内核未接入" |
| VpnService 隧道 | 未实现。Manifest **未声明** `BIND_VPN_SERVICE`（声明了不实现属于虚假声明） |
| 订阅解析 | 未实现。`ProxyNode` 模型就位，解析器未写 |
| 数据面过滤 | 规则引擎可离线运行与测试，但**没有数据面在调用它**，因此不产生实际拦截 |
| 开屏广告跳过 / 通知清理 | 未实现 |
| LSPosed 具体 Hook | 未实现 |
| 本地 API | 只有接口约定，未实现 |
| 界面文案本地化 | **372 处**用户可见文案内联在 Compose / ViewModel 中，尚未抽取到 `strings.xml`。`tests/ui_assets_check.sh` 会统计并打印该数字（**以脚本输出为准**；本条此前写 117 处，是布局预设 `ui/nav/LayoutPreset.kt` 加入后的陈旧值 —— 该文件单独占 204 处，因为它承载布局预设的中文标签）。抽取时 ViewModel 侧需改为"携带资源 ID 的错误类型 + UI 层解析"，是一个独立重构任务 |
| 规则引擎线性路径索引化 | 单次匹配 p99 中位数 **0.6962~0.8033 ms**，预算 **< 1 ms** → **达标（余量 19.7%~30.4%）**，但**余量不厚**：10 个测量轮里有 2 轮越过 1ms（1.3077 / 1.2108），且 p50 已达 **0.60~0.68 ms** —— 说明线性扫描不是只影响尾部，而是压住了整个分布。根因：`FilterEngine.collectCandidates` 对**每个**请求（含域名 Trie 命中的）都把全部 10,000 条线性规则复制成新列表并逐条测试，是 O(线性规则数)。修法是给正则规则提取必需字面量子串并按字面量分桶（uBlock Origin 同类思路）。**未在本次交付中实施**：需要改动 `PatternCompiler` 与 `FilterEngine` 核心匹配路径，而现有 38 条语料期望只覆盖语义正确性，不足以证明索引化后语义不变。顺序是先补语料、再改索引。（本条此前写作"超出 0.5 ms 预算约 1.9 倍"，那个 0.5 ms 是自造阈值；后又写作"余量仅 3.5%"，那是单轮暖机数字。两次都已更正，见下方性能表后的说明） |
| 内核 AAR 构建 | `src/tools/build_aar.sh` 已就绪（含 ABI 映射、前置检查、缺失时 exit 2），但需要 gomobile + NDK，本次未执行。因此 `engine` 仍绑定 `NotLinkedDataPlane` |

### 性能实测（原始数据见 `tests/perf_results.txt`）

环境：Windows / JDK 21.0.12.1+1 / 100 万条规则
方法：**1 轮预热（丢弃）+ 5 轮测量 × 10 万次采样，取逐轮 p99 的中位数**（提示词第 11 部分要求"多轮取中位数"）

| 指标 | 预算 | 实测（中位数） | 判定 |
|---|---|---|---|
| 解析耗时 | < 1 s | 447~474 ms | ✅ |
| 编译耗时 | < 3 s | 436~470 ms（索引化 990,000 / 线性 10,000） | ✅ |
| 单次匹配 p50 | — | 0.60~0.68 ms | — |
| 单次匹配 p90 | — | 0.66~0.76 ms | — |
| 单次匹配 p99 | < 1 ms | **0.6962 / 0.8033 ms**（两次独立 5 轮运行） | ✅（余量 30.4% / 19.7%） |
| 单次匹配 max（单轮内） | — | 3.0~6.7 ms | — |
| 匹配吞吐 | — | 1,470~1,543 次/秒 | — |
| release 单 ABI APK | < 15 MB | 1.3 MB（debug 未压缩 18 MB） | ✅ |

**预算取自提示词第 11 部分原文：`单次规则匹配 p99 | < 1ms | 基准脚本（≥10 万次采样）`。**

### 这一行被改过三次，过程本身是教训

| 版本 | 写法 | 为什么错 |
|---|---|---|
| 最初 | `预算 < 0.5 ms`，判定 ❌ | 0.5ms 是**自造**的，比需求更严；据此声称"超出预算 1.9 倍"，把达标写成了未达标 |
| 上一次修正 | `预算 < 1 ms`，判定 ✅ 余量 3.5% | 预算改对了，但**用单轮结果下结论**：那个 0.965ms 是暖机阶段的数字 |
| 本次 | `预算 < 1 ms`，判定 ✅ 余量 19.7%~30.4% | 改为多轮取中位数 + 丢弃预热轮 |

根因是**方法论**而不是实现：单轮基准永远测在 JIT/GC 收敛过程中。证据：同一机器、同一份代码，
单轮 p99 得到 0.9651ms 与 1.3183ms（相差 36%）；改成多轮后第 1 轮 1.3077ms、稳态 0.69ms 左右。
现在 10 个测量轮里仍有 2 轮（1.3077 / 1.2108）越过 1ms —— **这一点如实保留**：
中位数达标，但单轮偶有越线，真机上需重测（ART/GC 行为与 HotSpot 不同）。
原始记录见 `tests/perf_results.txt`（逐轮可核对，含作废段落的留痕与作废原因）。

### 安全

- 应用仅申请 `INTERNET` + `ACCESS_NETWORK_STATE`
- `allowBackup=false`
- 未申请 `QUERY_ALL_PACKAGES` / `READ_PHONE_STATE` / `AD_ID` / 定位 / 通讯录 / 短信 / 录音
- 零遥测：无崩溃上报、无统计、无广告 SDK

---

## 版本号约定

`v0.x.y` 期间，**不保证**向后兼容：

- 配置包 `schemaVersion` 会在破坏性变更时递增；应用**拒绝**读取更高版本
- Room schema 变更必须提供迁移，`data/schemas/` 下保留历史 schema 供审计

首个 `v1.0.0` 会在满足以下条件时发布：

1. 代理内核静态随包且可用
2. VpnService 隧道建立且可断开
3. 规则引擎接入数据面并有可核对的拦截计数
4. 端到端验证在真实设备上跑通
