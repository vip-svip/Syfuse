# Syfuse · 融合代理

> **一个 KernelSU / Magisk / APatch 模块**，模块内**内置 APK**。模块提供系统级能力
> （透明代理 · DNS · MITM · 过滤 · 封禁 · Hook · 证书与 hosts 注入），
> App 提供全部界面与配置。两者各自可独立工作，合起来是一个
> **只能比 Shadowrocket 多、不能少**的客户端。

[![CI](https://github.com/vip-svip/Syfuse/actions/workflows/ci.yml/badge.svg)](https://github.com/vip-svip/Syfuse/actions/workflows/ci.yml)
[![License: GPL-3.0-or-later](https://img.shields.io/badge/License-GPL--3.0--or--later-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-M1%20模块骨架-orange.svg)](#功能可用性报告)

> ⚠️ **当前状态：M1（模块骨架）。代理内核未接入，代理上网不可用。**
> 逐项状态见[功能可用性报告](#功能可用性报告)。本文档不夸大任何能力。

---

## 目录

- [定位与产品形态](#定位与产品形态)
- [功能可用性报告](#功能可用性报告)
- [功能覆盖度对照表](#功能覆盖度对照表)
- [逐屏界面清单](#逐屏界面清单)
- [架构](#架构)
- [数据模型](#数据模型)
- [syfuse.conf 键表](#syfuseconf-键表)
- [规则语法全表](#规则语法全表)
- [API 与 CLI](#api-与-cli)
- [构建与自检](#构建与自检)
- [权限说明](#权限说明)
- [风险清单](#风险清单)
- [【待核实】项](#待核实项)
- [常见问题](#常见问题)
- [免责声明](#免责声明)
- [致谢与许可](#致谢与许可)

---

## 定位与产品形态

```
┌──────────────── 模块层（systemless，交付主体）────────────────┐
│ KernelSU / Magisk / APatch 刷入；OverlayFS + bind mount        │
│ 守护进程 syfused · 代理内核宿主 · DNS 服务 · MITM 引擎         │
│ 过滤引擎 · Hook 框架 · 证书注入 · hosts 注入 · 网络封禁         │
│ 脚本引擎 · 本地控制 API(UDS) · 日志与统计                       │
│ 内含 sidecar APK：apk/syfuse.apk                               │
└───────────────────────────────────────────────────────────────┘
                    ↑↓ UDS + token（编码见下方说明）
┌──────────────── App 层（内置软件 / 控制台助手）───────────────┐
│ Compose UI · 首页/配置/数据/设置 · 模块与脚本管理              │
│ MITM 抓包查看 · Hook 管理 · 防泄漏自检 · 统计图表 · 诊断        │
└───────────────────────────────────────────────────────────────┘
```

**互相增强**：

- 模块给 App：免 VPN 弹窗的透明代理、系统 CA 注入、hosts 注入、UID 级分流、Hook 能力、开机自启、无 SELinux 干扰
- App 给模块：全部配置界面、订阅与节点管理、模块/脚本/MITM/Hook 编辑、抓包可视化、统计图表、自检与一键修复、模块自身的安装与更新
- **模块可独立工作**：`action.sh` 11 项音量键菜单 + CLI + 可选局域网面板
- **App 可独立工作**：无 root 时用 `VpnService` 模式，功能不缺失、仅能力降级（无系统证书、无 UID 精确分流、无 Hook）

---

## 功能可用性报告

**这是本文档最重要的一节。** 状态只有三种：

- `可用` = 有端到端证据（命令输出 / 截图 / 抓包）
- `实验性` = 能构建能运行但未端到端验证；UI 上必须显著标记
- `不可用` = 未实现；入口禁用并写明原因与计划里程碑

**当前里程碑：M1（模块骨架）。没有任何功能达到 `可用`** —— 因为真机验证未执行（本机无 Android 设备）。

| 功能 | 状态 | 证据 / 原因 | 计划 |
|---|---|---|---|
| 模块安装（沙箱模拟） | 实验性 | `tests/run_restore_check.sh` PASS=17（沙箱内真实执行 customize.sh） | 真机验证 |
| 模块卸载与完全还原 | 实验性 | 同上；沙箱内安装→卸载→diff 无残留 | 真机验证 |
| 还原清单机制 | **可用** | 84 个 Go 用例 + 沙箱端到端；`manifest.Add()` 拒绝不可还原条目 | — |
| `action.sh` 11 项菜单 | 实验性 | 菜单结构与 11 个处理函数已断言；4 项接真实功能，7 项明确报未实现 | 随各里程碑接通 |
| `syfused` 守护进程 | 实验性 | 源码可编译、84 用例通过；**二进制未随包** | M3 |
| UDS 控制通道 | 实验性 | 协议与鉴权有测试；Windows 上跳过 UDS 断言，**未在 Linux/真机验证** | M3 + 跑通 CI |
| 规则引擎（Kotlin，离线） | 实验性 | 28 用例 + 38 条语料期望全绿；未接入数据面 | M5/M10 |
| 配置编解码 / 设置目录 | 实验性 | JVM 单测通过 | M2 |
| Compose 界面 | 实验性 | 可构建可启动；**视觉未按 4.11 重做，Tab 命名仍是旧的** | M2 |
| **代理上网** | **不可用** | 内核未接入；`Engine.Start()` 返回 `ErrTunnelNotImplemented` | M3 |
| 分流 / 规则生效 | **不可用** | 规则引擎未接数据面 | M5 |
| DNS（DoH/DoT/DoQ、fake-ip） | **不可用** | 未实现 | M6 |
| 模块系统 / 脚本系统 | **不可用** | 未实现 | M7 |
| MITM / 抓包 | **不可用** | 未实现 | M8 |
| 统计 / 连接列表 | **不可用** | 未实现 | M9 |
| 过滤拦截生效 | **不可用** | 未实现 | M10 |
| Hook / 系统证书 / hosts 注入 | **不可用** | 未实现 | M11 |
| 防泄漏自检与修复 | **不可用** | 未实现 | M12 |
| 开屏跳过 / 通知净化 / 网络封禁 | **不可用** | 未实现 | M13 |
| 浏览器面板 | **不可用** | 未实现 | M13 |
| 局域网投送 | **不可用** | 未实现 | M9 |

**死 UI 声明**：当前 App 界面上**没有任何**看起来可用但实际未接通的功能开关 ——
因为界面仍是最小骨架，尚未接入上述任何能力。M2 做界面时必须保持这一点。

---

## 功能覆盖度对照表

三态：`复刻`（与 Shadowrocket 功能对齐）/ `超集增强`（Shadowrocket 没有）/ `不做`（附技术理由）。

### 5.1 代理与节点

| 项 | 状态 | 说明 |
|---|---|---|
| 协议：SS / SSR / VMess / VLESS / Trojan / Hysteria / Hysteria2 / HTTP(S) / SOCKS5 | 复刻 | URI 解析已实现并测试（`src/syfuse/node.go`，8 种协议）；**代理本身未实现** |
| 协议：TUIC v5 / WireGuard / Snell v3-v4 / SSH / AnyTLS / ShadowTLS / Juicity / Mieru | 复刻 | 计划 M4；随 sing-box 内核获得 |
| Trojan-Go、插件 obfs / v2ray-plugin | 复刻 | 计划 M4 |
| 传输：TCP / WS / HTTP-Upgrade / gRPC / HTTP2 / QUIC / mKCP / HTTP-Obfs | 复刻 | 计划 M4；随内核 |
| TLS 全参数（SNI/ALPN/uTLS 指纹/skip-cert-verify/证书固定/ECH） | 复刻 | 计划 M4 |
| 节点 CRUD / 分组 / 备注 / 排序 / 置顶 / 收藏 / 批量 | 复刻 | 计划 M4 |
| 导入导出 / 分享链接 / 二维码 / 剪贴板识别 | 复刻 | URI 解析已就绪；二维码计划 M4 |
| Clash YAML / sing-box JSON 互转 | 复刻 | 计划 M4 |
| 订阅：URL / 二维码 / 剪贴板 / 文件；覆盖现有 / 自动重命名 / 定时更新 / 差异预览 / 流量与到期解析 / 失败保留旧配置 | 复刻 | 计划 M4 |
| 测速：TCP / TLS / HTTP HEAD / ICMP，并发与分批，结果着色 | 复刻 | 计划 M4 |
| 策略组：select / url-test / fallback / load-balance / relay，嵌套与健康检查 | 复刻 | 计划 M4 |

### 5.2 分流与规则

| 项 | 状态 | 说明 |
|---|---|---|
| 规则类型：DOMAIN / DOMAIN-SUFFIX / DOMAIN-KEYWORD / DOMAIN-REGEX / IP-CIDR / IP-CIDR6 / IP-ASN / GEOIP / GEOSITE / SRC-IP / SRC-PORT / DST-PORT / PROCESS-NAME / PACKAGE-NAME / UID / NETWORK / PROTOCOL / DSCP / RULE-SET / MATCH | 复刻 | 计划 M5；域名与 CIDR 两类已在过滤引擎实现（`core:rule`） |
| AND / OR / NOT / SUB-RULE / FALLBACK / `no-resolve` | 复刻 | 计划 M5 |
| 规则集：本地 + 远程 URL、定时更新、哈希校验、离线 GEOIP/GEOSITE、镜像加速 | 复刻 | 计划 M5 |
| 规则体检（语法错误到行号、冲突、永不命中、重复） | 复刻 | 语法错误定位**已实现**（`RuleDiagnostic` 带行号） |
| 测试规则（命中规则、经过的组、最终动作、完整链路） | 复刻 | 引擎侧已支持 `collectChain`；UI 规则测试页已实现 |

### 5.3–5.7 模块 / 脚本 / MITM / 重写 / Hook

| 项 | 状态 | 说明 |
|---|---|---|
| 模块系统（同配置语法 + `#!name` `#!desc` 元数据、分段开关、订阅更新、冲突检测） | 复刻 | 计划 M7 |
| 脚本系统（QuickJS、沙箱、HTTP/规则/cron/DNS、兼容 SR/Surge/QX/Loon API） | 复刻 | 计划 M7 |
| MITM（CA 生成/轮换/安装/还原、主机列表、按应用、入站代理） | 复刻 | 计划 M8 |
| **抓包查看器**（请求响应列表、HAR/JSON 导出、时间线） | **超集增强** | 计划 M8；Shadowrocket 无独立抓包查看器 |
| 重写系统（URL / 头部 / 正文 / 本地映射） | 复刻 | 计划 M8 |
| Hook 框架检测与引擎切换（Zygisk / LSPosed / APatch / LD_PRELOAD / ptrace） | **超集增强** | 计划 M11；契约层已建（`hook/`） |
| 内置 Hook（pinning 绕过、用户 CA 信任注入、网络栈 Hook、DNS Hook、代理重定向、WebRTC 阻断） | **超集增强** | 计划 M11 |
| 自定义 Hook 脚本（暴露 Hook 点给 JS） | **超集增强** | 计划 M11 |
| 环境伪装（对指定 App 隐藏 root/VPN 状态） | **超集增强** | 计划 M11；**默认关**，需明确告知风险 |

### 5.8–5.11 防泄漏 / 统计 / 隧道 / 系统集成

| 项 | 状态 | 说明 |
|---|---|---|
| DNS / IPv6 / WebRTC / QUIC 泄漏检测与防护 | **超集增强** | 计划 M12 |
| 真实 IP 回显检测（多端点比对出口 IP 与节点地区） | **超集增强** | 计划 M12 |
| 分流泄漏检测（本机/局域网 IP、网关、运营商 DNS 直连） | **超集增强** | 计划 M12 |
| 一键自检 + 一键修复 + 前后对比 + 报告导出 + 定时巡检 | **超集增强** | 计划 M12 |
| 日志（自动删除 / 代理 / DNS） | 复刻 | 计划 M9；守护侧日志轮转**已实现**（`internal/logx`） |
| 服务器节点（覆盖现有 / 自动重命名 / 导入 / 导出 / 删除所有） | 复刻 | 计划 M9 |
| 统计（速率曲线、按 App/域名/规则/组/节点排行、导出 CSV/JSON） | 复刻 | 计划 M9 |
| **连接列表**（源 App、目标、命中规则、链路、上下行、时长、单条断开） | **超集增强** | 计划 M9 |
| **拦截日志**（按规则来源/类型筛选） | **超集增强** | 计划 M10 |
| 局域网投送（对应 Apple TV 小节语义） | 复刻（替代） | 计划 M9。原功能依赖 Apple 生态，改为向同网段浏览器推送 |
| 隧道参数（MTU / stack / auto-route / strict-route / TUN 实现） | 复刻 | 计划 M3/M6 |
| 代理参数（拥塞算法 / TFO / Mux / UDP 中继 / 缓冲区） | 复刻 | 计划 M3 |
| 按需求连接（SSID / 网络类型 / 时间 / 前台 App） | 复刻 | 计划 M13 |
| 权限与集成（位置 / 通知 / 剪贴板 / 无障碍 / 小组件 / 捷径 / 磁贴 / 触感 / 语言 / 外观） | 复刻 | 计划 M2（外观）/ M13（其余） |
| **浏览器面板**（App 内一键开关 + 系统浏览器打开 + 二维码 + 功能范围勾选） | 复刻 | 计划 M13；**禁止 WebView 内嵌** |
| 小组件（连接开关/速率/节点/延迟，多尺寸） | 复刻 | 计划 M13 |
| 捷径 / URL Scheme `syfusion://` | 复刻 | 计划 M13 |

### 明确不做（附技术理由）

| 项 | 理由 |
|---|---|
| 复刻 Shadowrocket 的视觉资产（图标、字体、截图、本地化文案） | 15.4 明令禁止提取商业 App 受版权资产。**复刻的是信息架构与行为语义，不是视觉搬运** |
| WebView / H5 承载主界面与编辑器 | 15.1。原生 Compose 是硬要求；**仅可选局域网面板例外且默认关闭** |
| 运行时下载内核二进制并执行 | 15.2。内核静态随包；仅允许带 SHA-256 校验的可选更新 |
| 从零手写加密协议栈或自创算法 | 15.5。用成熟实现（sing-box / 标准库） |
| 整页背景实时模糊 / 原始分辨率实时模糊 / 动态噪点冒充玻璃 | 15.3 + 7.5。参考项目实测：全屏模糊把 P50 帧耗时从 19ms 拉到 150ms、丢帧率 3.68%→100% |
| 默认开启遥测 / 崩溃上报 | 15.6 |
| `保存到 iCloud` | 平台不可用；等价实现为「保存到本地 / 云备份」（语义等价） |
| `Apple TV` 投送 | 依赖 Apple 生态；等价实现为「局域网投送」 |

---

## 逐屏界面清单

**实拍图说明**：提示词 4 部分引用 8 张 Shadowrocket 实拍作为**功能与内容参考**。
这 8 张图**不在本次执行提供的上下文中**，我不能凭空生成、也不能去抓取商业 App 截图
（15.4）。因此 `screenshots/` 目录为空并附说明；
`tests/ui_screenshot.sh` 目前只产出本应用自己的截图，**不做实拍比对**。
把 8 张图放进 `screenshots/` 后比对会自动启用。

**允许重新设计**：提示词 4 部分明确"实拍仅供参考，可重新设计"——
功能必须对齐，信息架构与视觉可以换新，视觉走 4.11 设计系统。

| # | 页面 | 实拍参考 | 落点 | 状态 |
|---|---|---|---|---|
| 1 | 首页 | sr-01 | Tab 1 | 实验性（骨架已建，视觉未重做） |
| 2 | 全局路由 | sr-02 | 首页 → 全局路由 | 未实现（M2） |
| 3 | 配置列表 | sr-04 | Tab 2 | 未实现（M2） |
| 4 | 配置详情（10 分区计数） | sr-05 | 配置 → 详情 | 未实现（M2） |
| 5 | 模块 | sr-03 | 配置 → 模块 | 未实现（M7） |
| 6 | 数据 | sr-06 | Tab 3 | 未实现（M2） |
| 7 | 设置（一） | sr-07 | Tab 4 | 实验性（骨架已建） |
| 8 | 设置（二） | sr-08 | Tab 4 | 未实现（M2） |
| 9 | 节点编辑（协议/TLS/传输/插件全参数） | 未拍到 | 节点 → 编辑 | 未实现（M4） |
| 10 | 策略组编辑 | 未拍到 | 节点 → 策略组 | 未实现（M4） |
| 11 | 规则编辑 / 规则体检 / 测试规则 | 未拍到 | 配置 → 规则 | 未实现（M5） |
| 12 | Hosts 编辑 | 未拍到 | 配置 → Hosts | 未实现（M5） |
| 13 | URL / 头部 / 正文重写编辑 | 未拍到 | 配置 → 重写 | 未实现（M8） |
| 14 | 本地映射编辑 | 未拍到 | 配置 → 本地映射 | 未实现（M8） |
| 15 | HTTPS 解密（主机列表 + 证书管理） | 未拍到 | 配置 → HTTPS 解密 | 未实现（M8） |
| 16 | 脚本编辑 / 脚本规则 / 调试控制台 | 未拍到 | 配置 → 脚本 | 未实现（M7） |
| 17 | 代理分组编辑 | 未拍到 | 配置 → 代理分组 | 未实现（M4） |
| 18 | 规则集 URL 管理 / 脚本 URL 管理 | 未拍到 | 配置 → 详情 → 第二卡片 | 未实现（M5/M7） |
| 19 | 日志 → 代理 / 日志 → DNS | 未拍到 | 数据 → 日志 | 未实现（M9） |
| 20 | 统计详情 | 未拍到 | 数据 → 统计 | 未实现（M9） |
| 21 | 连接列表 / 拦截日志 / Hook 命中日志 | 超集新增 | 数据 → 连接/拦截/Hook | 未实现（M9/M10/M11） |
| 22 | 诊断 / 外观 / 隧道 / 代理 / TCP / UDP / 按需求连接 | 未拍到 | 设置 → 高级/隧道 | 未实现（M2/M13） |
| 23 | 捷径 / 小组件 / 场景 / 分组 | 未拍到 | 设置 | 未实现（M13） |
| 24 | 配置 → 通用（那 8 项） | 未拍到 | 配置 → 详情 → 通用 | 未实现（M5） |
| 25 | 关于 / 更多 | 未拍到 | 设置 | 未实现（M2） |
| 26 | **浏览器面板** | 超集新增 | 设置 → 浏览器面板 | 未实现（M13） |
| 27 | 模块与特权 / 广告与过滤 / 隐私与防泄漏 / 脚本与 Hook / 备份与还原 | 超集新增 | 设置新增小节 | 未实现（M11/M10/M12/M7/M13） |

---

## 架构

### 三层进程模型

```
┌─ App 进程 ─┐   Compose UI · ViewModel · Room/DataStore · 无 root 时内嵌内核
│            │   ↕ UDS + token
├─ 守护进程 ─┤   syfused（Go）：内核宿主 · DNS · MITM · 过滤 · 封禁 · 开屏
│  (root)    │   ↕ nftables/TPROXY · /dev/tun · 证书 · hosts · Hook 装载
├─ Hook 层 ──┤   Zygisk / LSPosed 模块 + 自定义 Hook 脚本
└────────────┘
```

### 数据通路

| 模式 | 通路 |
|---|---|
| 无 root | `VpnService` + TUN（gvisor/system/mixed）+ `addDisallowedApplication` |
| 有 root | `nftables` TPROXY + `ip rule fwmark` + UID 精确分流；TUN 与 TPROXY 双通路择优 |

### 内核选型与许可核实（**实证，非采信**）

| 候选 | 实测结果 | 结论 |
|---|---|---|
| **sing-box**（`SagerNet/sing-box`） | LICENSE 原文：*"GNU General Public License … either version 3 of the License, or (at your option) any later version"* → **GPL-3.0-or-later**。Go 5.63MB，★38.8k。**默认分支是 `testing`，不是 `main`** | ✅ **选它**。协议最全，许可与要求一致 |
| mihomo（`MetaCubeX/mihomo`） | **不是代理内核**。该仓库今天是 *"A simple Python Pydantic model for Honkai: Star Rail parsed data from the Mihomo API"*，MIT，仅 7 个根条目 | ❌ **提示词 9.2/18.3 的引用有误**，见下 |

> **提示词勘误（按 2.3 提出）**
>
> 提示词 9.2 写「mihomo（规则生态成熟）」、18.3 写「mihomo（GPL-3.0）」。
> 实测：`MetaCubeX/mihomo` 当前是崩坏星穹铁道的**游戏数据解析器**（MIT，Python），
> 与代理内核无关。提示词给的仓库指向已经不对了。
>
> **修正建议**：把 9.2 与 18.3 的 mihomo 引用改为
> 「mihomo（Clash.Meta 系代理内核，仓库地址需以实际为准）」，
> 或直接删掉该候选 —— 本项目选 sing-box 已足够覆盖协议需求。
>
> **许可推导结论不变**：sing-box 是 GPL-3.0-or-later → 本项目**必须** GPL-3.0-or-later。
> 不会因为"MIT 更流行"改成 MIT（18.3）。

### 通信协议的一处偏离（已登记）

提示词 3.4 写「UDS + **protobuf** + token」。当前实现用**换行分隔 JSON**：

- 理由：protobuf 需引入 `google.golang.org/protobuf`，与"Go 侧零第三方依赖"冲突。
  零依赖的价值是**交叉编译成功率**与**可验证性**（`GOPROXY=off` 也能跑测试，有断言守着）。
- 协议已按「版本号 + 方法名 + 结构化参数」设计，换编码只需替换 Encode/Decode 两个函数，方法语义不变。
- 若 M3 证明 JSON 是瓶颈（如 `/traffic` 高频推送），会重新评估并在此处更新说明。

---

## 数据模型

提示词 10 部分列出 21 个模型。**当前已实现 6 个**（下表标 ✅），其余为计划。

| 模型 | 状态 | 位置 |
|---|---|---|
| `ProxyNode` | ✅ 部分 | `src/syfuse/node.go`（Go，8 协议解析）+ `domain/model` |
| `RuleEntry` | ✅ | `core/rule` + `domain/model` |
| `InstalledManifest` | ✅ | `src/internal/manifest/manifest.go`（10 种 kind，含可还原性校验） |
| `AppSettings` | ✅ 部分 | `domain/settings/Settings.kt`（11 组） |
| `FilterConfig` | ✅ 部分 | `domain/EnhanceModels.kt` |
| `Profile` | ✅ 部分 | `domain/model/Models.kt` |
| `ProxyGroup` | 计划 M4 | — |
| `RuleSet` | 计划 M5 | — |
| `HostEntry` / `RewriteEntry` | 计划 M5/M8 | — |
| `MitmConfig` | 计划 M8 | — |
| `ModuleEntry` / `ScriptEntry` | 计划 M7 | — |
| `BanRule` / `SplashSkipRule` / `NotifyRule` | 计划 M13 | — |
| `HookRule` | 计划 M11 | 契约已建（`hook/HookContract.kt`） |
| `LeakPolicy` | 计划 M12 | — |
| `PrivilegeState` | ✅ 部分 | `privileged/` |
| `TrafficStat` / `ConnSnapshot` | 计划 M9 | — |

兼容性约定（已落地）：schema 带 `version`；**未知字段保留不丢弃**（`rawExtras`）；
拒绝更高 `schemaVersion`；`syfuse.conf` 键名与 SY-Fusion 保持同名兼容。

---

## syfuse.conf 键表

完整文件见 [`module/config/syfuse.conf`](module/config/syfuse.conf)。每个键都有类型 / 取值范围 / 风险等级 / UI 位置。

| 键 | 默认 | 类型 | 范围 | 风险 | UI 位置 |
|---|---|---|---|---|---|
| `DNS_ENABLE` | `0` | bool | 0/1 | 安全 | 设置 → 广告与过滤 |
| `MITM_ENABLE` | `0` | bool | 0/1 | 危险 | 设置 → 广告与过滤 |
| `SPLASH_ENABLE` | `0` | bool | 0/1 | 危险 | 设置 → 广告与过滤 |
| `NOTIFY_ENABLE` | `0` | bool | 0/1 | 进阶 | 设置 → 广告与过滤 |
| `PANEL_ENABLE` | `0` | bool | 0/1 | 危险 | 设置 → 浏览器面板 |
| `DNS_UPSTREAM` | `1.1.1.1` | string | IPv4/IPv6/URL | 进阶 | 设置 → 隐私与防泄漏 |
| `DNS_UPSTREAM_ENC` | `plain` | enum | plain/doh/dot/doq | 进阶 | 同上 |
| `DNS_AUTO_UPDATE` | `0` | bool | 0/1 | 安全 | 同上 |
| `DNS_UPDATE_INTERVAL` | `1440` | int(分) | 5..10080 | 安全 | 同上 |
| `DNSSRV_ENABLE` | `0` | bool | 0/1 | **危险** | 同上（接管 53 端口影响其他应用） |
| `MITM_PORT` | `8443` | int | 1024..65535 | 危险 | 设置 → 广告与过滤 |
| `MITM_HTTP_PORT` | `8080` | int | 1024..65535 | 危险 | 同上 |
| `MITM_APPS` | 空 | string | 逗号分隔包名 | 危险 | 同上（**空=不解密任何应用**） |
| `MITM_QUIC_POLICY` | `pass` | enum | pass/block/mitm | 进阶 | 同上 |
| `PANEL_PORT` | `8848` | int | 1024..65535 | 进阶 | 设置 → 浏览器面板 |
| `API_ENABLE` | `0` | bool | 0/1 | **危险** | 同上（开放控制通道） |
| `PROXY_ENABLE` | `0` | bool | 0/1 | 安全 | 首页大开关 |
| `PROXY_PORT` | `2080` | int | 1024..65535 | 安全 | 设置 → 隧道 → 代理 |
| `PROXY_CORE` | 空 | string | 内核标识 | 进阶 | 设置 → 模块与特权 |
| `PROXY_LEAK` | `1` | bool | 0/1 | 进阶 | 设置 → 隐私与防泄漏 |
| `PROXY_SUB_INTERVAL` | `1440` | int(分) | 5..10080 | 安全 | 配置 → 订阅 |
| `BLACKLIST_ENABLE` | `0` | bool | 0/1 | 安全 | 设置 → 广告与过滤 |
| `ENGINE_HEURISTIC` | `0` | bool | 0/1 | 进阶 | 同上（可能误拦） |
| `ENGINE_STRICT_SCOPE` | `1` | bool | 0/1 | 进阶 | 同上 |
| `ENGINE_CLIENT` / `ENGINE_CTAG` | 空 | string | — | 进阶 | 同上（对应 `$client=` / `$ctag=`） |
| `HOSTS_EXTRA` | 空 | string | — | 进阶 | 配置 → Hosts |
| `RULES_REPO` / `RULES_BRANCH` / `RULES_MIRROR` | 空 / `main` / 空 | string | — | 安全 | 设置 → 更新 |
| `SCRIPT_UPDATE_INTERVAL` | `1440` | int(分) | 5..10080 | 安全 | 设置 → 脚本与 Hook |
| `HOOK_ENABLE` | `0` | bool | 0/1 | **危险** | 设置 → 脚本与 Hook |
| `HOOK_ENGINE` | `auto` | enum | auto/zygisk/lsposed/apatch/ldpreload/ptrace | 危险 | 同上 |
| `HOOK_APPS` | 空 | string | 逗号分隔包名 | 危险 | 同上（**空=不 Hook 任何应用**） |
| `LEAK_GUARD` | `1` | bool | 0/1 | 安全 | 设置 → 隐私与防泄漏 |
| `LEAK_INSPECT_INTERVAL` | `30` | int(分) | 5..1440 | 安全 | 同上 |
| `LEAK_AUTO_FIX` | `0` | bool | 0/1 | 进阶 | 同上 |
| `SPLASH_ENGINE` | `hook` | enum | hook/a11y | **危险** | 设置 → 广告与过滤（无障碍会读取屏幕内容） |
| `NOTIFY_SCOPE` | 空 | string | — | 进阶 | 同上 |
| `RESTORE_MANIFEST` | `/data/adb/syfuse/manifest/installed.json` | string | — | 安全 | 设置 → 备份与还原 |

分项文件（App 内可视编辑，与文件双向同步）：`blacklist.conf`、`whitelist.conf`、
`lists.conf`、`sources.conf`、`mitm_rules.conf`、`notify_keywords.conf`、
`splash_rules.conf`、`user_rules.txt`、`hook_rules.conf`、`leak_policy.conf`。

---

## 规则语法全表

### 已实现（`core:rule`，28 用例 + 38 条语料期望全绿）

**三类输入形态**（可混排）：

```
0.0.0.0 ads.example.com             hosts
127.0.0.1 tracker.example.net
1.2.3.4   rewrite.example.com       hosts 解析重写
address=/ads.example.com/           dnsmasq 拦截
local=/ads.example.com/             dnsmasq 本地应答
server=/example.org/1.1.1.1         dnsmasq 上游
```

**ABP / AGH 语义**：

```
||ads.example.com^              域名锚定拦截（含子域）
||ads.example.com^$important    重要规则（优先级最高）
@@||good.example.com^           例外放行
@@||pay.example.com^$important  带 important 的例外 → 最高优先级
https://cdn.example.com/js/     精确 URL 锚定
/ads\/banner\d+/                正则
10.0.0.0/8                      网段（最长前缀匹配）
```

**修饰符**（全部实现）：`$important` `$badfilter` `$match-case` `$domain=`
`$third-party`(`$3p`) `$first-party`(`$1p`)

**资源类型**：`$document` `$script` `$image` `$stylesheet` `$xmlhttprequest`(`$xhr`)
`$subdocument` `$font` `$media` `$websocket` `$other` `$popup` `$all`

**修饰规则（cosmetic）**：6 种标记按最长优先匹配 ——
`##^`（HTML 过滤）`#@#`（取消隐藏）`#$#`（CSS 注入）`#?#`（扩展 CSS）`#%#`（脚本注入）`##`（元素隐藏）

**注释符**：`!` `#` `;` 与 `[Adblock Plus 2.0]` 头

**优先级模型**（严格实现，非短路 —— 收集全部候选后取最高秩）：

```
重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中（默认放行）
```

`$badfilter` 在**编译期**精确关闭同形规则。

### 计划实现（M10）

| 项 | 说明 |
|---|---|
| `$denyallow=` `$client=` `$ctag=` `$dnsrewrite=` `$csp=` `$removeparam=` `$redirect=` `$cookie=` `$header=` `$method=` `$permissions=` | M10 |
| 资源类型 `$webrtc` `$ping` `$object` `$empty` `$mp4` `$inline-script` `$inline-font` `$generichide` `$specifichide` `$elemhide` `$genericblock` | M10 |
| 四种规则集格式 `domain` / `ipcidr` / `classical` / `mrs` + 远程订阅 + 哈希校验 + 镜像加速 | M5/M10 |
| 误拦截 1 秒内恢复（临时放行） | M10 |

**cosmetic 规则的诚实声明**：`##` / `#@#` 等需要**渲染上下文**（DOM 或视图树）才能生效，
只能在 MITM HTML 重写或局域网面板注入两条路径上工作。**在纯 DNS/连接层上无法生效**，
本项目不会把它们计入"拦截数"。

---

## API 与 CLI

### 模块 CLI（`syfused`）

**已实现**：`daemon` `serve` `status` `token [--rotate]` `restore [--dry-run|--apply]`
`rules paths` `rules stats` `version` `help`

**已注册但未实现**（返回**退出码 78** + 计划里程碑，区别于普通错误 1）：
`fetch`(M7/M10) `ca`(M8) `proxy`(M8) `hook`(M11) `leak`(M12)
`rules build|syntax|verify|test|testfile|clear`(M5/M10)

```bash
syfused status                    # 运行状态 + 还原清单摘要 + 逐条校验问题
syfused token                     # 显示访问令牌
syfused token --rotate            # 重新生成（旧令牌立即失效）
syfused restore                   # 打印还原计划（dry-run，默认）
syfused restore --apply           # 执行还原
syfused rules paths               # 打印全部规则相关路径及其存在性
```

### UDS 控制协议

socket `/data/adb/syfuse/control.sock`（0600），换行分隔 JSON，每次连接须带 token。

**已实现方法**：`status` `version` `manifest.summary` `token.rotate` `log.tail`

**已注册未实现**（返回明确错误 + 里程碑）：`proxies` `connections` `traffic`
`rules.test` `filter.stats` `hook.status` `leak.report` `leak.fix` `config.reload` `proxies.select`

请求/应答格式见 `src/internal/ipc/ipc.go` 与 `daemon/src/main/kotlin/.../DaemonProtocol.kt`
（两侧是同一份协议，**必须同步修改**）。

### HTTP API / 浏览器面板 / Intent / URL Scheme

**全部未实现**，计划 M13。设计约束已定：默认关闭、需 token、**禁止 WebView 内嵌面板**、
关闭面板立即释放端口并清空会话。

---

## 构建与自检

```bash
# 全量自检（不需要设备）
tests/run_module_checks.sh     # 红队静态校验（R1–R6 + 18.1 结构 + M1 判定）
tests/run_kernel.sh            # Go：gofmt / vet / build / 84 用例 / 零依赖断言
tests/run_restore_check.sh     # 卸载残留检查（沙箱内真跑 安装→卸载→diff）
tests/run_unit.sh              # Kotlin JVM 单测
tests/ui_assets_check.sh       # UI 资源完整性 + 内联文案统计
python tools/scan_secrets.py   # 凭据扫描（豁免需行内显式标记）

# 构建
./gradlew :app:assembleDebug   # App
./gradlew :app:assembleRelease # 分 ABI release
package.sh                     # 模块 ZIP + 分 ABI APK + SHA-256 + update.json

# 交叉编译守护进程（由 package.sh 自动执行）
cd src && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath ./cmd/syfused

# 需要设备的检查（无设备时 exit 3 = SKIP，不是通过）
tests/run_e2e.sh
tests/ui_runtime_check.sh
tests/ui_screenshot.sh
tests/perf_rule_engine.sh
tests/perf_apk_size.sh
```

### 退出码约定

| 码 | 含义 |
|---|---|
| `0` | 真的通过了 |
| `1` | 真的失败了 |
| `2` | 环境缺失 |
| `3` | 前置不满足而跳过 —— **不是通过** |
| `78` | `syfused` 子命令尚未实现（区别于普通错误 1） |

### 环境

| 组件 | 版本 | 说明 |
|---|---|---|
| JDK | Temurin 21.0.12.1+1 | |
| Gradle | 8.11.1 | wrapper 已入库 |
| Android SDK | platform 35 / build-tools 35.0.0 | |
| Go | 1.23.4 | 零第三方依赖，`GOPROXY=off` 可测 |
| Kotlin / KSP / AGP | 2.1.0 / 2.1.0-1.0.29 / 8.7.3 | |

`tests/env.sh` 按 `显式环境变量 → $SYFUSE_TOOLCHAIN → PATH` 顺序自动定位，无需手工配置。

---

## 权限说明

| 权限 | 用途 | 何时申请 |
|---|---|---|
| `INTERNET` | 代理本身 | 已申请 |
| `ACCESS_NETWORK_STATE` | 判断网络可用性，驱动连接状态机 | 已申请 |
| `BIND_VPN_SERVICE` | 无 root 模式的 TUN 隧道 | **未申请** —— VpnService 未实现，声明了不实现属于虚假声明（15.8）。M3 实现时同步声明 |
| root（KernelSU/Magisk/APatch） | 系统级能力：TPROXY、证书、hosts、Hook | 可选，由用户在模块管理器授予 |
| 无障碍 | 无 root 时的开屏跳过 | **默认关**（危险能力，会读取屏幕内容） |
| 通知 | 常驻通知与告警 | 默认开 |
| 剪贴板 | 识别节点分享链接 | 默认开 |
| 位置 | 按 SSID 触发规则（Android 对读取 SSID 的限制） | **默认关** |
| `QUERY_ALL_PACKAGES` | 按应用分流 / Hook 作用域选择 | **未申请**，M11 评估后按需申请 |

**未申请**：`READ_PHONE_STATE`、`AD_ID`、`READ_SMS`、`READ_CONTACTS`、`RECORD_AUDIO`、
`ACCESS_FINE_LOCATION`、`ACCESS_BACKGROUND_LOCATION`。`allowBackup=false`。

---

## 风险清单（M0）

| # | 风险 | 影响 | 缓解 |
|---|---|---|---|
| 1 | **内核未接入** | 代理上网不可用，产品核心价值为 0 | M3 优先；当前如实报 `ErrTunnelNotImplemented`，不伪造 |
| 2 | **真机验证缺失** | 所有"可用"判定都只是"沙箱可用" | 下一步先做真机 `GOOS=linux` 二进制可用性验证（代价最低、证伪收益最大） |
| 3 | `GOOS=linux` 静态二进制在 Android/bionic 上能否运行 | 若不成立，交叉编译方案要重做（改 `GOOS=android` + NDK cgo） | 未验证，标【待核实】；M3 第一件事实测 |
| 4 | UDS + SELinux 域限制 | 模块进程与 App 不同 SELinux 域时 `LocalSocket` 可能被拒 | 未验证；M3 实测，备选方案是 abstract socket + 域标签 |
| 5 | 玻璃性能 | 参考项目实测全屏模糊 P50 19ms→150ms、丢帧 3.68%→100% | 4.11 白名单 + 离屏降采样(≥2) + 三档 + 超标自动降档；`tests/ui_glass_perf` 断言 |
| 6 | sing-box 静态链接 | GPL-3.0-or-later 传染 → 项目必须同许可（已满足）；内核体积大 | APK 预算 30MB / ZIP 45MB；M3 实测 |
| 7 | 规则引擎 p99 | 100 万条实测 **0.965ms**，预算 **<1ms** → **达标但余量小**（仅 3.5%） | M10 做正则字面量分桶索引化；先补语料再改索引 |
| 8 | 21 个数据模型只实现 6 个 | 后续里程碑工作量大 | 按里程碑分批；每批带 Room 迁移 |
| 9 | `screenshots/` 8 张实拍缺失 | 无法做实拍复刻比对 | 标【待核实】；不生成、不抓取（15.4） |
| 10 | 提示词 mihomo 引用错误 | 若照抄会选错内核 | 已实证并勘误（见「内核选型」） |
| 11 | 权限/无障碍被用户拒绝 | 开屏跳过等降级 | 按 3.6 降级矩阵；功能不缺失、仅能力降级 |
| 12 | 脚本沙箱逃逸 | 恶意脚本执行任意代码 | M7 做 QuickJS 沙箱 + 逃逸测试（14 部分安全维度） |

---

## 【待核实】项

| # | 项 | 核实方法 | 当前采用的默认实现 |
|---|---|---|---|
| 1 | `screenshots/` 8 张实拍是否存在 | 确认是否有归档位置；有则放入该目录 | 按 4 部分**文字规格**实现；`ui_screenshot.sh` 不做比对 |
| 2 | `GOOS=linux` 静态二进制在 Android 可运行性 | 真机 `adb push` + 执行 | 假定可用（纯 stdlib 无 cgo）；M3 第一件事实测 |
| 3 | UDS 在模块进程 ↔ App 之间的 SELinux 可达性 | 真机实测 | 假定可达；备选 abstract socket |
| 4 | `MetaCubeX/mihomo` 是否曾是代理内核、现迁往何处 | 查项目官网/文档 | **不采用该候选**，选 sing-box |
| 5 | sing-box 的 `gomobile bind` 可行性 | M3 实测 | 假定可行；若不可行改为子进程模式（模块内直接跑 sing-box 二进制） |

---

## 常见问题

**Q：为什么还没有代理功能？**
A：当前是 M1（模块骨架）。代理内核在 M3 接入。本文档不会把"计划做"写成"已做"。

**Q：为什么 `syfused` 子命令有的报"未实现"还返回 78？**
A：退出码 78 专门表示"该功能尚未实现"，让脚本能把它与真实错误（1）区分开。
这是刻意设计——比"静默什么都不做"诚实。

**Q：为什么不用 WebView 做界面？**
A：15.1 硬要求。原生 Compose 的性能与可访问性不可替代；局域网面板是唯一例外且默认关闭。

**Q：为什么玻璃效果这么麻烦？**
A：参考项目实测过全屏实时模糊的代价：P50 帧耗时 19ms→150ms、丢帧率 3.68%→100%。
玻璃是明确要求，所以要实现；但必须白名单 + 降采样 + 三档 + 自动降级，否则是失控。

**Q：为什么许可证是 GPL-3.0-or-later 而不是 MIT？**
A：内核 sing-box 是 GPL-3.0-or-later（已实证 LICENSE 原文），静态链接会传染。
换成 MIT 会造成许可冲突——这是法律问题，不是风格偏好。

**Q：为什么 `action.sh` 有 11 项但 7 项点了报"未实现"？**
A：菜单结构按 3.5 固定，但每项都真的接了功能或明确报错。**"按了没反应"才是违规**。

---

## 免责声明

见 [`DISCLAIMER.md`](DISCLAIMER.md)。摘要：本项目仅供学习与研究网络协议使用。
网络代理、HTTPS 解密、应用 Hook 与系统级网络拦截属于双刃性质的技术能力——
请在目标地区法律法规与目标应用服务条款允许的范围内使用；**仅对自有设备与你拥有授权的流量**
执行 MITM 与 Hook；不得用于规避合法监管、破解他人服务、窃取他人数据或侵犯他人隐私。
作者不对任何因使用本软件导致的损失承担责任。

---

## 致谢与许可

- 本项目以 **GPL-3.0-or-later** 发布，见 [`LICENSE`](LICENSE)。
  许可证**不会**改成 MIT —— 理由见「内核选型与许可核实」。
- 过滤规则语义与工程验证纪律参考开源项目 **SY-Fusion**
  （AdGuard Home 规则语义 × uBlock Origin 匹配模型的融合过滤模块，KernelSU / Magisk 形态）。
  本项目**未复制其任何代码**，只在设计层面借鉴。
- 内核候选 **sing-box**（`SagerNet/sing-box`，GPL-3.0-or-later）。
  第三方组件与许可证清单见 [`NOTICE`](NOTICE)。

---

## 待执行的一步

无。账号与令牌可用，建仓 / 推送 / 核验均已完成。

---

## 作者

`vip-svip`（运行时解析出的 GitHub 账号，非硬编码、非 AI 工具名）。
改账号请运行 `tools/set_owner.sh --apply`。
