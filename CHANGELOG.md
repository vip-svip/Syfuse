# 更新日志

本文件记录**已交付**的变更。计划中的能力不写在这里 —— 计划在 README 的
「当前完成度」表里，两者严格分开。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

---

## [未发布]

### 计划中（M2）

- 代理内核（Go，`gomobile bind` → AAR），静态随包
- VpnService 隧道建立
- 订阅解析：ss / ssr / vmess / vless / trojan / hysteria
- 规则引擎接入数据面（当前规则引擎可离线运行，但无数据面调用它）
- 域名嗅探与 DNS 分流

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
| 界面文案本地化 | 117 处用户可见文案内联在 Compose / ViewModel 中，尚未抽取到 `strings.xml`。`tests/ui_assets_check.sh` 会统计并打印该数字。抽取时 ViewModel 侧需改为"携带资源 ID 的错误类型 + UI 层解析"，是一个独立重构任务 |
| 规则引擎线性路径索引化 | 实测单次匹配 p99 = **0.965 ms**，**超出 0.5 ms 预算约 1.9 倍**。根因：未命中域名 Trie 的请求需逐条测试全部 10,000 条线性正则规则，是 O(n) 路径。修法是给正则规则提取必需字面量子串并按字面量分桶（uBlock Origin 同类思路）。**未在本次交付中实施**：需要改动 `PatternCompiler` 与 `FilterEngine` 核心匹配路径，而现有 38 条语料期望只覆盖语义正确性，不足以证明索引化后语义不变。顺序是先补语料、再改索引 |
| 内核 AAR 构建 | `kernel/tools/build_aar.sh` 已就绪（含 ABI 映射、前置检查、缺失时 exit 2），但需要 gomobile + NDK，本次未执行。因此 `engine` 仍绑定 `NotLinkedDataPlane` |

### 性能实测（原始数据见 `tests/perf_results.txt`）

环境：Windows / JDK 21.0.12.1+1 / 100 万条规则

| 指标 | 预算 | 实测 | 判定 |
|---|---|---|---|
| 解析耗时 | < 1 s | 468 ms | ✅ |
| 编译耗时 | < 3 s | 439 ms（索引化 990,000 / 线性 10,000） | ✅ |
| 单次匹配 p50 | — | 0.701 ms | — |
| 单次匹配 p90 | — | 0.742 ms | — |
| 单次匹配 p99 | < 0.5 ms | **0.965 ms** | ❌ |
| 单次匹配 max | — | 9.191 ms | — |
| 匹配吞吐 | — | 1,503 次/秒 | — |

**不把未达标的预算写成达标。** p99 超预算这一条已列在上面的「已知缺口」里。

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
