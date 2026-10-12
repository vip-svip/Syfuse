# Syfuse

> **融合代理** — Shadowrocket 级 Android 代理客户端，以 Jetpack Compose 原生实现，
> 核心内核静态内置，可选 root / LSPosed 特权增强。

[![CI](https://github.com/vip-svip/Syfuse/actions/workflows/ci.yml/badge.svg)](https://github.com/vip-svip/Syfuse/actions/workflows/ci.yml)
[![License: GPL-3.0-or-later](https://img.shields.io/badge/License-GPL--3.0--or--later-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-M1%20skeleton-orange.svg)](#项目状态)

> ⚠️ **当前状态：M1（工程骨架）。内核尚未接入，代理功能不可用。**
> 详见[项目状态](#项目状态)与[路线图](#路线图)。本文档不夸大当前能力。

---

## 目录

- [定位](#定位)
- [项目状态](#项目状态)
- [功能特性](#功能特性)
- [功能覆盖度对照表](#功能覆盖度对照表)
- [架构](#架构)
- [安装](#安装)
- [快速开始](#快速开始)
- [权限说明](#权限说明)
- [过滤规则语法](#过滤规则语法)
- [构建与验证](#构建与验证)
- [账号与包名映射](#账号与包名映射)
- [常见问题](#常见问题)
- [路线图](#路线图)
- [免责声明](#免责声明)
- [致谢与许可](#致谢与许可)

---

## 定位

Syfuse 是一款 Android 网络代理客户端。它的设计目标是：

1. **以 iOS Shadowrocket 的功能与界面为唯一主干做功能等价复刻**
   —— 同样的操作产生同样的结果，同样的概念使用同样的名字。
2. **只把 Shadowrocket 没有的能力作为差异增强层补上**
   —— 广告过滤内核、网络层封禁、开屏跳过、通知净化、root/LSPosed 特权层等。
3. **核心内核静态内置** —— 冷启动与首次连接都不下载内核二进制，断网可用。
4. **界面走「合理化」路线** —— Material 3、原生 Compose、四态完备、可访问性达标。

**复刻的边界：** 复刻的是**信息架构与行为语义**，**不是**视觉资产。
本项目不提取、不使用任何商业软件的图标、字体、截图或本地化文案。

---

## 项目状态

项目按 13 个里程碑推进（M0–M12）。当前完成情况：

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M0 | 方案（架构、模块划分、数据模型、内核选型、覆盖度对照表） | ✅ 完成 |
| M1 | 多模块工程骨架、四 Tab 导航、设置页框架、CI 与验证脚手架 | ✅ 完成 |
| M2 | 内核打通（sing-box AAR、VPN 模式 TUN、最小可用代理） | ⬜ 未开始 |
| M3 | 节点与订阅 | ⬜ 未开始 |
| M4 | 规则与分流 | ⬜ 未开始 |
| M5 | DNS | ⬜ 未开始 |
| M6 | MITM 与脚本 | ⬜ 未开始 |
| M7 | 连接、日志、统计 | ⬜ 未开始 |
| M8 | 工具与系统集成 | ⬜ 未开始 |
| M9 | 差异增强层一（过滤内核、网络层封禁） | ⬜ 未开始 |
| M10 | 差异增强层二（开屏跳过、通知净化、配置即产品） | ⬜ 未开始 |
| M11 | 特权层（root、TPROXY、系统证书、LSPosed） | ⬜ 未开始 |
| M12 | 打磨与发布 | ⬜ 未开始 |

### M1 的诚实说明

M1 交付的是**可编译的工程骨架**，不是可用产品：

- **界面结构完整**，但数据来自空状态，因为没有内核与数据源
- **`ProxyEngine` 接口已定义**，但 sing-box 实现目前返回「功能尚未实现」
  —— 这是刻意的：不伪装成可用
- **设置页只有「通用」分组有内容**，其余 10 个分组的框架已就位
- **尚无 Release**，`update.json` 中 `sha256` 为 `PENDING-FIRST-RELEASE`

---

## 功能特性

### 主干（复刻 Shadowrocket）

| 分类 | 能力 |
|---|---|
| 首页 | 连接四态大开关、全局路由三态（配置/代理/直连）、配置与节点卡片、实时速率、快捷动作 |
| 节点 | 20 种协议、8 种传输、uTLS 指纹、策略组（select/url-test/fallback/load-balance/relay）、延迟测速 |
| 订阅 | 多档案、URL/文件/二维码/剪贴板导入、格式嗅探、定时更新与差异预览 |
| 规则 | 20 种规则类型、逻辑组合、规则集、规则体检、规则测试 |
| DNS | 多上游、DoH/DoT/DoQ、fake-ip、嗅探、缓存、防泄漏 |
| MITM | CA 生成/安装/卸载、解密白名单、URL 重写、JS 脚本与沙箱 |
| 观测 | 连接列表、分级日志、统计排行、通知 |
| 工具 | Ping/TCPing/Traceroute/DNS 查询/端口检测/HTTP HEAD、一键体检 |
| 集成 | 快捷磁贴、桌面小组件、URL Scheme、开机自启 |

### 差异增强（Shadowrocket 没有的）

| 分类 | 能力 |
|---|---|
| 过滤内核 | AdGuard Home × uBlock Origin 全语法，离线可用，优先级模型完整 |
| 网络层封禁 | 域名/IP/网段/端口，DNS 层 + 连接层双保险 |
| 开屏跳过 | root / LSPosed / 无障碍三策略，规则化、可统计 |
| 通知净化 | 按关键字/包名/正则折叠或拦截 |
| 特权层 | TPROXY 透明代理、系统 CA 注入、hosts 注入、开机静默自启 |
| LSPosed | 免改 APK 注入 CA 信任、按应用 DNS/代理覆盖 |
| 开放接口 | 本地 HTTP API、内核 CLI 子命令、配置热重载 |
| 配置即产品 | `syfuse.conf` 与 UI 双向同步、可复用预设片段 |

---

## 功能覆盖度对照表

**状态定义：**
- `复刻` — Shadowrocket 已有，按其行为与命名实现
- `增强` — Shadowrocket 没有，属差异增强层
- `不做` — 不实现（附技术或法律理由）

### 首页与状态

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 连接大开关 | 顶部大开关，错误态可点进查看原因 | 同左，四态（未连接/连接中/已连接/错误） | 复刻 | — | UI Test |
| 全局路由三态 | 配置 / 代理 / 直连 | 同左，命名保留 | 复刻 | — | 单测 |
| 当前配置与节点卡片 | 点按即切换 | 同左 | 复刻 | — | UI Test |
| 实时速率与累计流量 | 首页展示 | 同左 | 复刻 | — | 单测 |
| 快捷动作区 | 测速 / 更新订阅 / 清 DNS 缓存 / 查看连接 | 同左 | 复刻 | — | UI Test |
| 过滤拦截计数 | 无对应位置 | 首页底部轻量统计 | 增强 | 本项为增强层功能的展示位 | UI Test |

### 节点与策略组

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 协议覆盖 | SS/SSR/VMess/VLESS(Reality,XTLS-Vision)/Trojan/Trojan-Go/Hysteria/Hysteria2/TUIC v5/WireGuard/Snell/HTTP(S)/SOCKS5/SSH/AnyTLS/ShadowTLS/Juicity/Mieru | 同左（sing-box 原生支持） | 复刻 | — | 分享链接 round-trip |
| 插件 | obfs、v2ray-plugin | 同左 | 复刻 | — | 单测 |
| 传输层 | TCP/WS/HTTP-Upgrade/gRPC/HTTP2/QUIC/mKCP/HTTP-Obfs | 同左 | 复刻 | — | 单测 |
| TLS 参数 | SNI/ALPN/uTLS 指纹/skip-cert-verify/证书固定 | 同左 + 自定义 ClientHello | 复刻 | — | 单测 |
| 节点管理 | 增删改查/分组/备注/排序/置顶/收藏/批量/导入导出 | 同左 | 复刻 | — | 单测 |
| 分享链接 | ss/vmess/vless/trojan/hysteria2/tuic/snell + Clash YAML + sing-box JSON | 同左 | 复刻 | — | **字段级等值** round-trip |
| 策略组类型 | select/url-test/fallback/load-balance/relay | 同左，含嵌套与 dialer-proxy | 复刻 | — | 每种组类型单测 |
| 负载均衡子模式 | round-robin/consistent-hash/sticky-sessions | 同左 | 复刻 | — | 单测 |
| 节点测速 | TCP/TLS/HTTP HEAD/ICMP，并发分批 | 同左，<200ms 绿 / <500ms 黄 / 其余红 | 复刻 | — | 单测 + 真机 |
| 链式代理可视化 | relay 类型，无图形化链路 | 多级链 + UI 链路图 | 增强 | Shadowrocket 无图形化链路视图 | UI Test |

### 配置与订阅

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 多配置档案 | 并存、快速切换、每档案独立规则/DNS/脚本 | 同左 | 复刻 | — | 单测 |
| 导入方式 | URL 订阅 / 本地文件 / 二维码 / 剪贴板 | 同左 | 复刻 | — | 单测 |
| 格式嗅探 | Base64 / Clash YAML / sing-box JSON / V2Ray / SS 列表 | 同左 | 复刻 | — | 单测 |
| 订阅信息 | 流量、到期时间 | 同左 | 复刻 | — | 单测 |
| 定时更新 | 间隔可配、可限仅 Wi-Fi | 同左 + 更新前后**差异预览** | 复刻 | — | 单测 |
| 导入导出 | 导出配置包、导入 schema 校验 + 版本迁移 + 备份 | 同左 | 复刻 | — | 单测 |
| 配置即产品 | 无对应能力 | `syfuse.conf` 与 UI 双向同步 | 增强 | Shadowrocket 无等价能力 | 单测 |
| 预设片段 | 无对应能力 | DNS/规则/脚本保存为可复用片段 | 增强 | Shadowrocket 无等价能力 | 单测 |

### 规则与分流

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 规则页组织 | 配置级规则 / 本地规则 / 远程规则集 / 更新状态 | 同左 | 复刻 | — | UI Test |
| 规则类型 | DOMAIN/DOMAIN-SUFFIX/DOMAIN-KEYWORD/DOMAIN-REGEX/IP-CIDR/IP-CIDR6/IP-ASN/GEOIP/GEOSITE/SRC-IP/SRC-PORT/DST-PORT/PROCESS-NAME/PACKAGE-NAME/UID/NETWORK/PROTOCOL/DSCP/RULE-SET/MATCH | 同左全类型 | 复刻 | — | 一致性语料 |
| 逻辑组合 | AND/OR/NOT/SUB-RULE/FALLBACK | 同左 + `no-resolve` 修饰 | 复刻 | — | 单测 |
| 规则集更新 | 哈希校验、缓存、失败重试 | GEOIP/GEOSITE 离线内置并可更新 | 复刻 | — | 单测 |
| 规则体检 | 语法错误定位、冲突检测 | 同左（行号 + 原因） | 复刻 | — | 单测 |
| 规则测试 | 输入 → 命中规则 / 经过组 / 最终动作 / 匹配链路 | 同左 | 复刻 | — | 单测 |
| 过滤规则引擎 | 仅 URL 重写能挡一部分，无完整规则生态 | AdGuard × uBO 全语法，融入规则页第五分段 | **增强** | Shadowrocket 无完整过滤生态 | 过滤语料全绿 |
| 网络层封禁 | 无独立封禁能力 | 域名/IP/网段/端口，DNS + 连接层双保险 | **增强** | Shadowrocket 无此能力 | 单测 + e2e |

### DNS

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 上游协议 | UDP/TCP/DoH/DoH3/DoT/DoQ，多上游并发竞速 | 同左 | 复刻 | — | 单测 |
| 按域名指定上游 | nameserver-policy、direct-nameserver | 同左 | 复刻 | — | 单测 |
| 模式 | normal / fake-ip / hosts / redir-host | 同左 | 复刻 | — | 单测 |
| 回退 | fallback、fallback-filter | 同左 | 复刻 | — | 单测 |
| 防泄漏 | DNS 走隧道、respect-rules、域名嗅探 | 同左 | 复刻 | — | 泄漏检测 |
| 缓存 | LRU + 持久化 + 命中统计 + 手动清理 + 单域名查询 | 同左 | 复刻 | — | 单测 |

### HTTPS 解密、重写、脚本

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| CA 管理 | 生成（RSA 2048 / ECC P-256）、指纹、导出、安装、卸载 | 同左 | 复刻 | — | 还原测试 |
| MITM | 解密白名单、TLS 1.3、HTTP/2、QUIC 直通、上游端口、证书固定 | 同左 | 复刻 | — | 真机 |
| URL 重写 | reject/reject-200/reject-dict/reject-array/302/307/请求头/响应头/请求体正则/响应体正则 | 同左全动作 | 复刻 | — | 单测 |
| JS 脚本 | QuickJS，Hook: http-request/http-response/rule/cron | 同左 + dns hook | 复刻 | — | 逃逸测试 |
| 脚本 API | `$request`/`$response`/`$done`/`$notify`/`$httpClient`/`$persistentStore`/`$argument` | 同左 + 文档与示例库 | 复刻 | — | 单测 |
| 脚本沙箱 | 无文件系统、无任意网络、无反射逃逸、超时 5s | 同左 | 复刻 | — | 逃逸测试全拒 |

### 连接、日志、统计

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 连接列表 | 源 App/目标/命中规则/链路/上下行/时长/状态，可关闭单条与全部 | 同左 | 复刻 | — | UI Test |
| 日志 | 分级、按模块过滤、关键字高亮、实时滚动、导出 | 同左（虚拟列表） | 复刻 | — | 10 万条不掉帧 |
| 统计 | 实时速率曲线、会话上下行、按 App/域名/规则/策略组排行 | 同左 | 复刻 | — | 误差 < 1% |
| 通知 | 连接状态、订阅更新、规则集失败、流量阈值 | 同左 | 复刻 | — | 真机 |

### 工具与系统集成

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 诊断工具 | Ping/TCPing/Traceroute/DNS 查询/端口连通/HTTP HEAD | 同左 + 一键体检 | 复刻 | — | 真机 |
| 网络环境信息 | SSID/网络类型/本机 IP/网关/DNS/MTU | 同左 | 复刻 | — | 真机 |
| 快捷设置磁贴 | 开关 + 当前配置名 | 同左 | 复刻 | — | 真机 |
| 桌面小组件 | 2×1 / 2×2 / 4×2 | 同左（Glance 原生绘制） | 复刻 | — | 真机 |
| URL Scheme 与 Intent | 导入订阅/切换配置/开关连接/跳转规则 | 同左 | 复刻 | — | 真机 |
| 开机自启 / Always-on VPN | 有 | 同左（默认关闭） | 复刻 | — | 真机 |

### 设置

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 设置分组 | 通用/网络与DNS/隧道/解密/脚本/订阅/规则/统计/关于 | 同左 + 特权与增强 / 实验室 | 复刻 | 增强层需独立分组 | UI Test |
| 设置搜索 | 有 | 同左 | 复刻 | — | 单测 |
| 专家模式 | 高级项默认隐藏 | 同左统一揭示 | 复刻 | — | UI Test |
| 风险等级标记 | 无明确标记 | 三色标记（安全/进阶/危险） | 增强 | 本项目安全要求（R6） | UI Test |
| 恢复推荐值 | 无 | 每个专家项可恢复默认 | 增强 | 本项目安全要求 | 单测 |

### 特权与生态（全部为增强）

| 功能项 | Shadowrocket 行为 | Syfuse 实现 | 状态 | 差异理由 | 验收方式 |
|---|---|---|---|---|---|
| 开屏广告跳过 | 无 | root / LSPosed / 无障碍三策略，独立开关 | 增强 | Shadowrocket 无此能力 | 真机 |
| 通知净化 | 无 | 按关键字/包名/正则折叠或拦截 | 增强 | Shadowrocket 无此能力 | 真机 |
| root 特权层 | 无 | 能力探测、TPROXY 透明代理、系统 CA、hosts 注入 | 增强 | Shadowrocket 无此能力 | 还原测试 |
| LSPosed 增强 | 无 | 免改 APK 注入 CA、按应用 DNS/代理覆盖 | 增强 | Shadowrocket 无此能力 | 真机 |
| 内核 CLI | 无 | `fetch`/`ca`/`proxy`/`serve`/`token` | 增强 | Shadowrocket 无此能力 | CLI 测试 |
| 本地 HTTP API | 无 | RESTful + WS，默认关闭需 token | 增强 | Shadowrocket 无此能力 | 接口测试 |
| 局域网面板 | 无 | 仅桌面浏览器查看管理，默认关闭 | 增强 | Shadowrocket 无此能力 | 真机 |
| 脚本生态兼容 | 无 | 油猴/QuantumultX/Surge/Loon/Stash 语法适配层 | 增强 | Shadowrocket 无此能力 | 单测 |
| 场景自动化 | 无 | 按 SSID/网络类型/时间/前台 App 自动切换 | 增强 | Shadowrocket 无此能力 | 真机 |

### 不做

| 功能项 | 状态 | 技术/法律理由 | 验收方式 |
|---|---|---|---|
| iOS 专属能力（iOS 小组件、TestFlight 分发等） | **不做** | 目标平台为 Android，无等价系统 API。`targetSdk 35` 下的能力集合与 iOS 无交集 | 不适用 |
| Shadowrocket 视觉资产与本地化文案（图标、字体、截图、文案） | **不做** | 受版权保护。本项目复刻信息架构与行为语义，不搬运视觉资产 | `tools/scan_secrets.py` 与 CI 资源检查 |
| WebView / H5 承载主界面 | **不做** | 违反红线 R1（性能与安全考量：WebView 承载主 UI 会引入渲染开销与攻击面） | `tests/run_module_checks.sh` 的 R1 检查 |
| 运行时下载内核二进制 | **不做** | 违反红线 R2（断网可用性与供应链安全） | `tests/run_module_checks.sh` 的 R2 检查 |

---

## 架构

### 分层与进程模型

```
┌──────────────── UI 进程 (app) ────────────────┐
│ Compose UI · ViewModel · DataStore/Room       │
│   ↓ IPC (protobuf over UDS)                   │
├──────── 内核进程 (engine, :core) ─────────────┤
│ ProxyEngine · 规则匹配 · DNS · MITM · 过滤    │
│   ↓ (root 模式) nftables/TPROXY · /dev/tun    │
├──────── 特权层 (privileged) ──────────────────┤
│ libsu · 模块检测 · 证书安装 · hosts 注入       │
└───────────────────────────────────────────────┘
        ↑ (可选) LSPosed 模块进程内 Hook
```

### 模块划分

| 模块 | 职责 | 关键约束 |
|---|---|---|
| `app` | 入口 Activity、DI 装配、清单 | 无 WebView（R1） |
| `ui` | Compose 组件、主题、四 Tab 页面 | 全虚拟化列表；禁大面积 blur |
| `domain` | 用例、领域模型、`ProxyEngine` 接口 | 纯逻辑，UI 零依赖内核类型 |
| `data` | Room、DataStore、导入导出、订阅 | schema 带版本，迁移前备份 |
| `core` | 常量、错误模型、结果类型 | 无业务逻辑 |
| `engine` | 内核封装、配置编译、IPC | **唯一**允许依赖内核的模块 |
| `privileged` | root 探测、TPROXY、证书、hosts 注入 | 全程可还原（R6） |
| `lsposed-bridge` | LSPosed Hook 入口 | 独立开关、异常兜底 |
| `src/`（Go） | sing-box fork 裁剪 + RPC | 产出 AAR |

### 内核策略

**默认内核：sing-box**（协议覆盖最全，直接满足 20 种协议的要求）。

`ProxyEngine` 抽象接口从 M1 起即按**双内核可切换**设计：

```kotlin
interface ProxyEngine {
    val kind: EngineKind
    suspend fun start(config: EngineConfig): SyfuseResult<Unit>
    suspend fun stop(): SyfuseResult<Unit>
    suspend fun reload(config: EngineConfig): SyfuseResult<Unit>
    fun stats(): Flow<TrafficStats>
    fun connections(): Flow<List<ConnSnapshot>>
    suspend fun select(group: String, member: String): SyfuseResult<Unit>
    suspend fun urlTest(target: String): SysfuseResult<List<LatencyResult>>
}
```

UI 层**零依赖**具体内核类型。将来接入 mihomo 只需新增一个实现类，UI 不动。

**为何不同包内置两个内核：** 两内核均为几十 MB 级产物，同包会使单 ABI APK
超过 25MB 体积预算；且 sing-box 用 JSON、mihomo 用 Clash YAML，需维护两套
配置编译与 IPC 适配。故 M1–M12 只内置 sing-box，`MihomoEngine` 排入 v2。

**许可说明：** 静态链接 sing-box（GPL-3.0-or-later）使本项目必须为
GPL-3.0-or-later。mihomo 为 MIT，与 GPL 兼容，不产生冲突。详见 [NOTICE](NOTICE)。

---

## 安装

> **当前无可用安装包。** M1 为工程骨架，未经真机验证，**不建议安装**。
> 以下为计划中的安装方式，M12 完成后可用。

### 方式一：APK（推荐，免 root）

1. 从 [Releases](https://github.com/vip-svip/Syfuse/releases) 下载对应 ABI 的 APK
2. 安装后首次启动会请求 VPN 权限 —— 这是代理功能的必需权限（见[权限说明](#权限说明)）
3. 导入节点或订阅，打开连接开关

### 方式二：KernelSU / Magisk 模块（可选，获取特权能力）

1. 从 Releases 下载 `syfuse-<version>.zip`
2. 在 KernelSU 或 Magisk 管理器中刷入
3. 重启设备
4. 模块提供 TPROXY 透明代理（免 VPN 弹窗）、系统 CA 注入、hosts 注入

**卸载：** 在模块管理器中卸载即可，`uninstall.sh` 会还原所有改动。

---

## 快速开始

> M12 完成后本节才有效。当前无可用构建。

1. 打开应用 → 「节点」页 → 导入订阅 URL
2. 「首页」→ 选择配置与节点
3. 「首页」→ 打开连接开关
4. 首次会请求 VPN 权限，授权后即可上网

---

## 权限说明

本项目遵循**最小权限**与**显式告知**原则。每一权限的用途如下：

| 权限 | 用途 | 何时请求 |
|---|---|---|
| `INTERNET` | 拉取订阅、下载规则集、代理流量本身 | 安装时授予；用途仅限用户显式配置的出网 |
| `ACCESS_NETWORK_STATE` | 判断 Wi-Fi / 蜂窝，用于按网络类型切换配置 | 安装时授予 |
| `FOREGROUND_SERVICE` | 隧道必须常驻 | 首次连接时 |
| `POST_NOTIFICATIONS` | 显示连接状态通知（Android 13+ 需显式请求） | 首次连接时 |
| `RECEIVE_BOOT_COMPLETED` | 开机自启（**默认关闭**，需用户显式开启） | 用户开启自启时 |
| `BIND_VPN_SERVICE` | 建立 TUN 隧道 | 首次连接时，系统弹窗确认 |
| 无障碍服务 | 开屏广告跳过（**默认关闭**） | 用户在系统设置中手动开启，附风险说明 |
| 通知监听 | 通知净化（**默认关闭**） | 同上 |
| root | TPROXY 透明代理、系统 CA、hosts 注入（**可选**） | 用户显式授权，每次提权写审计日志 |

**零遥测承诺：** 本项目不上报任何数据。所有出网请求均来自用户显式配置的操作。
CI 会检查是否引入遥测 SDK（红线 R3）。

---

## 过滤规则语法

> 本功能属差异增强层，M9 实现。以下为实现目标。

支持 AdGuard Home 规则语义与 uBlock Origin 匹配模型的融合，**完全离线可用**。

### 网络规则

```
||ads.example.com^            域名锚定拦截（含子域）
@@||good.example.com^         例外放行
https://cdn.example.com/js/   精确 URL 锚定
ads?banner\d+/                正则规则
^                             通配符与分隔符
```

### 修饰符

`$important`、`$badfilter`、`$match-case`、`$denyallow=`、`$client=`、`$ctag=`、
`$domain=`、`$third-party`（`$3p`）、`$first-party`（`$1p`）、`$dnsrewrite=`、
`$csp=`、`$removeparam=`、`$redirect=`、`$cookie=`、`$header=`、`$method=`、
`$permissions=`

### 资源类型

`$document`、`$script`、`$image`、`$stylesheet`、`$xmlhttprequest`、`$subdocument`、
`$websocket`、`$webrtc`、`$ping`、`$object`、`$media`、`$font`、`$other`、`$popup`、
`$all`、`$empty`、`$mp4`、`$inline-script`、`$inline-font`、`$generichide`、
`$specifichide`、`$elemhide`、`$genericblock`

### 元素与修饰规则

```
example.com##.ad-banner              元素隐藏
example.com#@#.ad-banner             取消隐藏
example.com#$#.ad { display:none }   CSS 注入
example.com#?#div:has(> .sponsor)    扩展 CSS
example.com#%#//scriptlet('...')     脚本注入
example.com##^script:has-text(ads)   HTML 过滤
```

### hosts / dnsmasq / 纯域名

```
0.0.0.0 ads.example.com        hosts 拦截
127.0.0.1 tracker.example.com
1.2.3.4   rewrite.example.com  hosts 解析重写
address=/ads.example.com/      dnsmasq 拦截
local=/ads.example.com/        dnsmasq 本地应答
example.com                    纯域名清单
```

注释符 `!`、`#`、`;` 与 `[Adblock Plus 2.0]` 头均兼容。

### 优先级模型

```
重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中（默认放行）
```

- `$badfilter` 精确关闭同形规则
- `$denyallow` 限定例外规则的适用范围
- 例外规则（`@@`）自身带 `$important` 时提升为最高优先级

---

## 构建与验证

### 构建

```bash
# 环境：JDK 17 + Android SDK (API 35)
gradle wrapper --gradle-version 8.11.1
./gradlew :app:assembleDebug
```

### 验证脚本

| 脚本 | 内容 | 当前可用性 |
|---|---|---|
| `tests/run_module_checks.sh` | 静态合规：R1/R2/R3/R5、标识、ABI、i18n | ✅ 可用 |
| `tools/scan_secrets.py` | 密钥、签名文件、绝对路径、占位符扫描 | ✅ 可用 |
| `tests/ui_assets_check.py` | UI 资产基线比对（防入口静默丢失） | ✅ 可用 |
| `tests/perf_baseline.py --static` | 静态代码规模统计 | ✅ 可用 |
| `tests/run_unit.sh` | 单元测试（需 JDK） | ⚠️ 需 JDK |
| `tests/run_e2e.sh` | 端到端（需内核，M2 实现） | ⬜ 未实现 |
| `tests/ui_runtime_check.sh` | UI 运行时逻辑（需 SDK） | ⬜ 未实现 |
| `tests/ui_screenshot.sh` | 截图与可视化回归（需 SDK） | ⬜ 未实现 |
| `tests/run_corpus.sh` | 规则一致性语料（M9 实现） | ⬜ 未实现 |
| `package.sh` | 打包 → ZIP 校验 → SHA-256 | ⚠️ 需 SDK |
| `tools/verify_remote.py` | 远程树与本地 HEAD 一致性 | ✅ 可用 |
| `tools/verify_files.py` | 关键文件远程/本地比对 | ✅ 可用 |

**未实现的脚本以退出码 78（EX_CONFIG）失败**，而非返回虚假的「通过」。

### CI

推送后 CI 运行 5 个 job，任一失败阻断合并：

1. `static-checks` — 静态合规检查
2. `unit-tests` — 单元测试
3. `build` — APK 构建 + 体积检查（25MB 单 ABI 红线）
4. `module-package` — 模块骨架完整性与脚本语法
5. `i18n` — 中英资源一致性

---

## 账号与包名映射

| 项 | 值 |
|---|---|
| GitHub 账号 | `vip-svip` |
| 包名 | `io.github.vipsvip.syfuse` |
| 规范化规则 | 账号名删除连字符 → `vipsvip` |
| 模块 id | `syfuse` |
| 应用显示名 | Syfuse · 融合代理 |

规范化依据：GitHub 用户名允许连字符，Java/Kotlin 包名不允许，
故需转换为合法 Java 标识符（仅 ASCII 小写字母、数字、下划线，不以数字开头）。

---

## 常见问题

**Q: 为什么不直接内置两个内核（sing-box + mihomo）？**

A: 两者均为几十 MB 级的 Go 静态链接产物，同包会使单 ABI APK 从约 23MB
升至 45MB 以上，突破 25MB 体积预算；且两者的配置模型不同（JSON vs Clash YAML），
需维护两套配置编译与 IPC 适配。因此 M1–M12 只内置 sing-box，
但 `ProxyEngine` 抽象层已按可切换设计，mihomo 实现排入 v2。

**Q: 为什么不选 MIT 许可？**

A: 不是偏好问题，而是许可约束。本项目静态链接 sing-box（GPL-3.0-or-later），
GPL 的传染性要求衍生作品同样以 GPL 分发。若未来仅使用 MIT 许可的组件，
可重新评估更宽松的许可。

**Q: 和 SY-Fusion 是什么关系？**

A: 本项目独立实现。在**过滤规则语义**（AdGuard/uBO 规范的融合与优先级模型）
与**工程验证纪律**（多层自检体系）两方面参考了开源项目 SY-Fusion。
不复制其任何源代码，不继承其模块 id，不使用其 WebView 面板方案。详见 [NOTICE](NOTICE)。

**Q: 为什么需要 VPN 权限？**

A: 免 root 模式下，Android 只能通过 `VpnService` 建立 TUN 隧道来接管流量。
这是系统提供的唯一免 root 全局代理机制。若使用 root 模式，则通过
TPROXY 实现透明代理，不占用 VPN 通道。

**Q: 会不会上传我的数据？**

A: 不会。本项目默认不上报任何数据（红线 R3），CI 会检查是否引入遥测 SDK。
所有出网请求均来自你显式配置的操作（拉订阅、下规则集、代理流量）。

---

## 路线图

见[项目状态](#项目状态)的里程碑表。当前 M1 完成，下一里程碑为 M2（内核打通）。

**M2 的目标：** 真机通过代理上网，冷启动 < 800ms。

---

## 免责声明

网络代理、HTTPS 解密与系统级网络拦截属于**双刃性质**的技术能力。

**合法使用范围：** 仅可在目标地区法律法规与目标应用服务条款允许的范围内使用；
仅可对**自有设备**与**你拥有授权的流量**执行 MITM。

**明确禁止：** 规避合法监管、窃取他人数据、侵犯他人隐私、对未授权设备或流量执行 MITM。

完整声明见 [DISCLAIMER.md](DISCLAIMER.md)。安全问题报告见 [SECURITY.md](SECURITY.md)。

---

## 致谢与许可

### 第三方组件

- **sing-box** — GPL-3.0-or-later。本项目的代理内核。
  静态链接其代码，本项目因此以 GPL-3.0-or-later 分发。
  其许可含「不得暗示关联」条款，本项目已相应避免任何关联性表述。
- **mihomo** — MIT。v2 计划接入的内核备选。
- AndroidX、Jetpack Compose、Kotlin、Hilt、OkHttp 等，见 [NOTICE](NOTICE)。

### 参考项目

- **SY-Fusion** — 本项目在**过滤规则语义**（AdGuard Home 规则语法与
  uBlock Origin 匹配模型的融合、优先级模型）与**工程验证纪律**
  （多层自检体系、性能红线教训）两方面参考了该项目。
  本项目不复制其源代码；规则语义源自公开的 AdGuard / uBlock Origin 规范。

### 许可

本项目以 **GPL-3.0-or-later** 分发，完整文本见 [LICENSE](LICENSE)。

```
Syfuse — Shadowrocket 级 Android 代理客户端与融合过滤引擎
Copyright (C) 2026 vip-svip
```

**许可推导依据：** 静态链接 sing-box（GPL-3.0-or-later）→ 受 GPL 传染 →
本项目必须为 GPL-3.0-or-later。

第三方组件与许可的完整清单见 [NOTICE](NOTICE)。变更记录见 [CHANGELOG.md](CHANGELOG.md)。
贡献指南见 [CONTRIBUTING.md](CONTRIBUTING.md)。
