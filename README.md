# Syfuse · 融合代理

> **一个 KernelSU / Magisk / APatch 模块**，模块内**内置 APK**。模块提供系统级能力
> （透明代理 · DNS · MITM · 过滤 · 封禁 · Hook · 证书与 hosts 注入），
> App 提供全部界面与配置。两者各自可独立工作，合起来是一个
> **只能比 Shadowrocket 多、不能少**的客户端。

[![CI](https://github.com/vip-svip/Syfuse/actions/workflows/ci.yml/badge.svg)](https://github.com/vip-svip/Syfuse/actions/workflows/ci.yml)
[![License: GPL-3.0-or-later](https://img.shields.io/badge/License-GPL--3.0--or--later-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-M2%20%E7%95%8C%E9%9D%A2%E4%B8%8E%E8%AE%BE%E8%AE%A1%E7%B3%BB%E7%BB%9F-orange.svg)](#功能可用性报告)

> ⚠️ **当前状态：M2（App 骨架与视觉）进行中。代理内核未接入，代理上网不可用。**
> 逐项状态见[功能可用性报告](#功能可用性报告)。本文档不夸大任何能力。
> 跨轮进度真相见 [`PROGRESS.md`](PROGRESS.md)；待外部验证项见 [`PENDING-EXTERNAL.md`](PENDING-EXTERNAL.md)；
> 门禁原始输出与性能数据见 [`docs/EVIDENCE.md`](docs/EVIDENCE.md)。

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
- [性能预算与实测（第 11 部分）](#性能预算与实测第-11-部分)
- [权限说明](#权限说明)
- [风险清单](#风险清单)
- [【待核实】项](#待核实项)
- [常见问题](#常见问题)
- [免责声明](#免责声明)
- [致谢与许可](#致谢与许可)
- [进度与外部依赖](#进度与外部依赖)

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

**当前里程碑：M2（App 骨架与视觉）进行中。没有任何功能达到 `可用`** —— 因为真机验证未执行（本机无 Android 设备）。

| 功能 | 状态 | 证据 / 原因 | 计划 |
|---|---|---|---|
| 模块安装（沙箱模拟） | 实验性 | `tests/run_restore_check.sh` PASS=17（沙箱内真实执行 customize.sh） | 真机验证 |
| 模块卸载与完全还原 | 实验性 | 同上；沙箱内安装→卸载→diff 无残留 | 真机验证 |
| 还原清单机制 | **可用** | 88 个 Go 用例（86 通过 / 2 跳过 / 0 失败）+ 沙箱端到端；`manifest.Add()` 拒绝不可还原条目 | — |
| `action.sh` 菜单 | 实验性 | 主菜单 **11 项** + 2 个子菜单（守护进程控制 3 项、备份恢复 2 项）；门禁断言主菜单 ≥11 项、11 个处理函数齐备、未实现项走显式 `not_implemented` 分支（**不产生死菜单**） | 随各里程碑接通 |
| `syfused` 守护进程 | 实验性 | 源码可编译、Go 用例全绿；**二进制已随包**（`bin/syfused-{arm64,arm,amd64}`，交叉编译产物在 `dist/syfuse-0.1.0.zip` 内）；未在真机运行 | M3 |
| UDS 控制通道 | 实验性 | 协议与鉴权有测试；**socket 生命周期断言（创建 / 关闭零残留 / 崩溃残留清理）已在本机真实执行**，且经变异测试确认有效（移除清理逻辑即失败）；仅 2 条 POSIX 权限位断言因环境不实现 0600 而跳过（按能力探测跳过，非按 OS 名）。**仍未在真机验证** | M3 + 真机验证 |
| 规则引擎（Kotlin，离线） | 实验性 | 28 用例 + 38 条语料期望全绿；未接入数据面 | M5/M10 |
| 配置编解码 / 设置目录 | 实验性 | JVM 单测通过 | M2 |
| 设计系统（OKLCH 令牌 + 受控玻璃） | 实验性 | 7 Tab 布局 B + 布局预设 A/B/C（4.1 六条硬标准有可执行判据）；玻璃三档/降采样 4/层数上限 3/白名单断言/超标自动降档/API<31 伪玻璃**均已实现**；策略层 26 条 JVM 用例通过（`ui/src/test/.../GlassPolicyTest.kt`）。**未验证**：模糊是否真的生效、降采样是否真的降负载、降档是否真的在超标时触发 —— 需真机 | 真机跑 `tests/ui_glass_perf.sh` |
| Compose 界面（7 个功能页） | 实验性 | 7 页（首页/代理/规则/拦截/调试/监控/设置）可构建可启动；`ui-assets` PASS=8、`ui-parity` PASS=27 PEND=6；**未在真机做运行时与截图回归** | 真机跑 `ui_runtime_check.sh` / `ui_screenshot.sh` |
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

## 布局预设与可自建 Tab（4.1）

**默认使用布局 B「极客全显」7 Tab**：首页 / 代理 / 规则 / 拦截 / 调试 / 监控 / 设置。
首次启动即用，不做选择弹窗；用户可在「设置 → 布局」一键切到 A / C 或自建。
**7 Tab 是默认值，不是上限** —— 功能量确实大，宁可默认全显让用户自己收。

| 布局 | Tab 数 | 内容 |
|---|---|---|
| **B「极客全显」★默认★** | 7 | 首页 / 代理 / 规则 / 拦截 / 调试 / 监控 / 设置 |
| A「日常优先」 | 5 | 首页 / 代理 / 规则 / 监控 / 设置（最重的两组收进设置与监控） |
| C「极简」 | 3 | 首页（大开关 + 可定制九宫格）/ 控制台 / 设置 |

**为什么不是固定的 4 Tab**：4.1 复盘指出，把「功能域」直接当成 Tab 会导致单 Tab 超载 ——
原「配置」要同时装分流 + 过滤 + 模块 + 脚本四种东西，「设置」塞进 10 个子域，
「数据」这种名字又含义模糊。正确做法是**按用户任务切分 + 让布局本身可换**。

### 判定 Tab 是否合理的六条硬标准（4.1）

在 `ui/nav/LayoutPreset.kt` 的 `LayoutSpec.violationsOf()` 里做成了**可执行断言**：

1. 主 Tab 数量 3～7 个
2. 每个 Tab 名字是**用户会说的词**（「数据」「防护」「配置」「工具」在黑名单里）
3. 单个 Tab 承载的功能组不超过总数 1/3，且不少于 5%
4. 任意功能 ≤ 2 步可达（Tab → 页面/分段）
5. 每个 Tab 内的二级页 ≤ 7 个
6. **布局可替换、Tab 可自建**（任何一套固定 IA 都不许写死）

### 完全自定义

自建 Tab（任意命名/图标/配色/从功能库勾选/拖拽排序）、
改动内置布局（隐藏 Tab、调顺序、二级页一键提升为主 Tab）、
每 Tab 首页可编辑（卡片化，可增删/折叠/换尺寸）、
布局导入导出（`layout.json`，多套并存并按场景自动切换）、
Tab 栏形态可选（底部栏 / 4 固定 + 更多 / 可滚动 / 侧边抽屉 / 顶部分段）、
全局搜索始终能直达任意功能与设置项并显示所属路径。

| 项 | 状态 |
|---|---|
| 三套布局预设的数据模型与六条断言 | **可用**（`LayoutPreset.kt` + 编译通过） |
| 导航由预设驱动（默认 7 Tab） | **可用**（`SyfuseApp.kt`） |
| 布局切换 UI / 自建 Tab / layout.json 导入导出 | **不可用**（计划 M2 后续） |

---

## 主题系统 = 完整界面方案（皮肤）（4.12）

**主题不能只是"改配色"。** 一个主题包必须覆盖**六个维度**，六维齐全才叫一套界面方案：

| # | 维度 | 可调项 |
|---|---|---|
| ① 色彩 | 语义色板 20+ 角色、主色/强调色/状态色/图表 8 色、功能域色标、透明度梯度、明暗模式 |
| ② 形状与材质 | 圆角、描边、阴影层级、玻璃参数、材质（纯色/玻璃/瓷砖/拟物/扁平/新拟态） |
| ③ 组件样式 | 开关、复选框、滑块、按钮形态、卡片样式、列表样式、菜单弹层、输入框、加载动画、空状态插画 |
| ④ 布局与密度 | 布局预设、Tab 栏形态与位置、卡片列数、间距栅格、页边距、分组间距、二级页样式 |
| ⑤ 排版与图标 | 字体族、字号阶梯、字重、行高、字距、数字样式、图标包、图标描边 |
| ⑥ 动效与反馈 | 转场类型、时长与曲线、列表入场、加载动画、触感强度、音效集、启动页 |

### 内置「完整界面方案」预设（每套都打包全部六维）

**主流主题预设**：极光玻璃（默认）、Miuix 黑金瓷砖、Miuix 白瓷、
Material You / M3 Expressive、iOS 玻璃、Nothing OS 点阵；
**暗色经典 13 套**（Dracula / Nord / Tokyo Night / Catppuccin / Gruvbox / One Dark /
GitHub Dark / Rosé Pine / Everforest / Ayu / Night Owl / Monokai Pro / Solarized）；
**潮流视觉 5 套**（Synthwave '84 / Vaporwave / Cyberpunk Neon / Deep Ocean / Forest Mist）；
**极简与无障碍 8 套**（AMOLED 纯黑 / High Contrast / Paper White / Sepia / 色盲友好三套 / 弱视增强）。

每套都补齐配套的形状 / 组件 / 图标 / 动效，**不是只换色**。

### 自定义主题方案

1. **逐维度编辑**（①②③④⑤⑥ 每项可改，可只覆盖某一维度，其余继承预设）
2. **逐组件覆盖**（长按组件 → 应用到该组件 / 该页面 / 全局，支持继承链与单点重置）
3. **从素材生成**（壁纸或图片取色生成 ①，并按风格推荐 ②③⑤）
4. **主题包 `.sftheme`**（JSON 打包六维 + 可选壁纸/字体/图标集引用 + 作者与版本；导出/导入/分享码/二维码/兼容性校验）
5. **主题 × 布局 = 界面方案**（如「调试专用」= 极客全显布局 + 暗色经典 + 去玻璃，按场景自动切换）
6. **实时预览 + 对比度与性能提示**（对比度不达标显著标注；玻璃超标自动降档并提示来源）
7. **每 Tab 独立主题**（可选，默认关）；跟随系统深浅 / 日出日落 / 网络场景 / 低电省电自动切换
8. **主题随附**（启动页、图标包、状态栏样式、图表配色、通知样式、音效集保持一致）

**硬性要求**：切换即时生效不重启（≤300ms）；六维配置全部持久化并与配置文件双向同步；
任何方案都必须满足 4.11 的对比度硬要求；深浅色两套都要健全；AMOLED 下玻璃改纯黑半透明。

| 项 | 状态 |
|---|---|
| ① 色彩（OKLCH token + 四主题） | **可用**（`DesignTokens` + 15 个断言） |
| ② 形状与材质（圆角/玻璃） | **部分**（圆角与玻璃已实现；描边/阴影层级/材质切换未做） |
| ③④⑤⑥ 组件/布局/排版/动效 | **未做**（计划 M2 后续） |
| 预设库（主流主题预设 30+ 套） | **未做** |
| 自定义主题方案 / `.sftheme` 主题包 | **未做** |

---

## 作者与开源信息**内置到 App 里**（4.10.1，必做）

**地址怎么来**：`https://github.com/` + 运行时解析出的账号（1.2）。
**禁止在源码、资源、`module.prop`、README、CI 里硬编码任何具体账号字面量** ——
一律由解析结果拼接；解析不到时显示「作者信息未解析」并提示运行 `tools/set_owner.sh`，
**不许写占位符交付**。

**必须出现的 8 处**：

| # | 位置 | 形式 | 状态 |
|---|---|---|---|
| 1 | 设置 → 关于与作者 | 头像 + 账号名 + 可点击地址 + 一键复制 + 二维码 | 未做 |
| 2 | 关于页内的开源信息 | 仓库地址、License、第三方组件（NOTICE 摘要）、致谢（SY / SY-Fusion / SY-Rules 按项目名）、版本号与构建时间、构建渠道 | 未做 |
| 3 | 模块 `module.prop` | `author=<解析出的账号>`；`description` 末尾附 `github.com/<账号>` | **部分**（author 已由 `set_owner.sh` 写入，description 未附地址） |
| 4 | `action.sh` 状态输出 | 状态菜单打印作者与仓库地址 | 未做 |
| 5 | 本地 API `/status` | 返回 `author`、`authorUrl`、`repoUrl`、`license` | 未做（API 本身未实现） |
| 6 | 浏览器面板页脚 | 作者 + 仓库链接 | 未做 |
| 7 | 首启引导页 / 关于卡片 | 欢迎页底部署名；首页「关于作者」可选卡片 | 未做 |
| 8 | README 与 LICENSE | 作者段（含地址）、版权行、致谢段 | **部分**（有作者段，未含地址） |

**交互要求**：点击用**系统浏览器**打开（不许 WebView，R1）；长按或按钮复制；
提供二维码；`syfusion://about` 深链直达；无障碍要有 `contentDescription`。

**隐私要求**：头像等网络资源**默认本地缓存 + 可关闭**（关掉后显示本地生成的字母头像）；
展示作者信息**不得引入任何遥测**（R3 不因它破例）；不请求除 GitHub 之外的任何地址。

**离线要求**：无网络时关于页必须完整可用 —— 账号名、地址文本、License、致谢全部本地内置，
只有头像退化为字母头像。

---

## 功能覆盖度对照表

三态：`复刻`（与 Shadowrocket 功能对齐）/ `超集增强`（Shadowrocket 没有）/ `不做`（附技术理由）。

### 5.0 去重裁决规则（**Shadowrocket 有就不加**）

本产品是**只能多不能少**的超集，但**超集 ≠ 重复造轮子**。每实现一个功能前先回答三个问题，
答案直接写进下表：

1. **Shadowrocket 有这个功能吗？** 有 → 按它的**行为与命名**复刻，**不发明第二套**；没有 → 才进超集增强层
2. **这件事在 Shadowrocket 里叫什么？** 就用那个名字，别改叫法让用户重新学
3. **我的新设计会不会让原功能多绕一步？** 会 → 砍掉或降级为可选

| 已覆盖能力 | 正确做法 | 禁止做法 |
|---|---|---|
| 分流规则与规则集 | 复刻其规则类型与「配置 → 规则」组织 | 再造一套命名的平行规则体系 |
| DNS 设置（多上游 / DoH / DoT / fake-ip） | 复刻其设置项与默认值 | 另做一个独立 DNS 模块入口 |
| HTTPS 解密与证书管理 | 复刻其主机列表与 CA 流程 | 另做一套证书中心 |
| URL / 头部 / 正文重写 / 本地映射 | 复刻其四类重写 | 合并成新概念或另建平行入口 |
| 脚本 | 复刻其脚本管理与 `$` API 面 | 另建"脚本中心"平行体系 |
| 模块 | 复刻「配置同语法 + `#!name`/`#!desc` 元数据」 | 自创另一套模块格式 |
| 节点 / 策略组 / 订阅 / 测速 | 全部按其行为复刻 | 自创分组或订阅模型 |
| 判断路由四档（配置 / 代理 / 直连 / 场景）+ 启用回退 | 沿用命名与语义 | 自创"模式"概念 |
| 数据页（日志 / 服务器节点 / 统计） | 复刻其小节划分 | 另做"连接审计"独立页 |
| 网络诊断工具集 | 复刻其工具与入口 | 另做一套诊断入口 |
| 快捷指令 / 小组件 / 剪贴板识别 | 复刻其交互 | 自创入口 |

只有确认 Shadowrocket **确实没有**的能力（过滤规则生态、IP 防泄漏、Hook、模块形态特权能力、
开屏跳过、通知净化等）才允许作为增强层新增，且**必须挂在已有页面的分区里**，不新开并列体系。

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
| 1 | 首页 | sr-01 | Tab 1 | 实验性（骨架已建，已接入 4.11 设计系统；未做真机运行时与截图回归） |
| 2 | 全局路由 | sr-02 | 首页 → 全局路由 | 未实现（M2） |
| 3 | 配置列表 | sr-04 | Tab 2 | 未实现（M2） |
| 4 | 配置详情（10 分区计数） | sr-05 | 配置 → 详情 | 未实现（M2） |
| 5 | 模块 | sr-03 | 配置 → 模块 | 未实现（M7） |
| 6 | 数据 | sr-06 | Tab 3 | 未实现（M2） |
| 7 | 设置（一） | sr-07 | Tab 4 | 实验性（骨架已建，已接入 4.11 设计系统） |
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

## 视觉设计系统（OKLCH + 透明玻璃实现规范）

视觉走 4.11 的现代设计系统：**深色优先、明亮强调色、层叠透明、大圆角、柔和光影**。
**不复刻**实拍的纯蓝 iOS 6 风导航栏。所有颜色以 **OKLCH** 定义并集中在
`ui/theme/Tokens.kt` 一个文件里。

| 语义 | 深色 | 浅色 |
|---|---|---|
| 背景底（含极光渐层） | `oklch(0.18 0.04 265)` ≈ `#0B1020` | `oklch(0.97 0.01 265)` ≈ `#F6F7FB` |
| 表面 / 玻璃底色 | `rgba(255,255,255,0.06~0.10)` | `rgba(255,255,255,0.55~0.70)` |
| 主色（品牌） | `oklch(0.65 0.22 285)` ≈ `#7C5CFF` | `oklch(0.55 0.22 285)` ≈ `#5B5BD6` |
| 强调色 | `oklch(0.82 0.14 200)` ≈ `#22D3EE` | `oklch(0.62 0.13 200)` ≈ `#0891B2` |
| 成功 / 警告 / 危险 | `#34D399` / `#FBBF24` / `#FB7185` | 同色相降明度 |
| 主渐层 | `linear-gradient(135deg, #7C5CFF, #22D3EE)` | 同左降饱和 |

对比度硬要求：正文 ≥ 4.5:1，大字 ≥ 3:1；玻璃表面上的文字必须**实测**通过。
状态色语义固定：连接中=强调色、已连接=成功、错误=危险、未连接=中性。

### 透明玻璃实现规范（受控实现，不是无边界的模糊）

| 项 | 规定 |
|---|---|
| 白名单位置 | 顶部导航栏、底部 Tab 栏、首页悬浮连接卡与状态条、BottomSheet、对话框、小组件、快捷设置磁贴、刷新加载占位 |
| 同时可见层数 | ≤ 3 层，**不允许全屏玻璃**（整页背景不得做实时模糊） |
| 模糊半径 | 16–24dp；三档强度 关 / 轻（12dp）/ 强（24dp），默认「轻」，用户可调 |
| 实现方式 | API 31+ 用 `RenderEffect.createBlurEffect` 对**静态背景层**模糊；必须**离屏降采样 → 模糊 → 放大**（降采样 ≥2，建议 4），**禁止对整屏原始分辨率实时模糊** |
| 低版本降级 | API < 31 自动降级为「半透明 + 渐变 + 描边」的**伪玻璃**（零模糊成本） |
| 滚动期策略 | 滚动/动画进行中自动降一档或临时关闭，停止后恢复（可配置） |
| 自动保护 | 内建帧率监测：P90 帧耗时 > 24ms 或丢帧率 > 3% 时**自动降一档并记录日志**，连续两次超标提示用户 |
| 可访问性 | 系统「减少动态效果 / 降低透明度」开启时自动关模糊并提高不透明度 |

**为什么要受控**：参考项目实测过全屏实时模糊的代价 —— P50 帧耗时从 **19ms 拉到 150ms**、
丢帧率从 **3.68% 拉到 100%**。玻璃是明确要求，**所以要实现**；但必须按白名单 + 降采样 + 预算 + 自动降级来做。

**玻璃专项性能预算**（进第 11 部分验收）：玻璃开启时 **P90 ≤ 24ms**、丢帧率 < 3%、
首屏渲染因玻璃增加 ≤ 80ms；超标即自动降档并记录。由 `tests/ui_glass_perf.sh` 断言。

---

## UI 深度定制与功能分区

### 分区原则

功能比 Shadowrocket 多，**绝不堆在一个列表里**：

1. **跟随用户心智**：Shadowrocket 已有的功能放在它原来的位置，不搬走
2. **新功能按「谁用、什么时候用」归位**：一次性配置 → 设置；日常操作 → 首页/数据；深度调试 → 诊断
3. **每个页面不超过两级深度**：主页 → 详情；需要第三级说明分组没做好
4. **全局搜索**：任何设置项/功能都能被搜索直达（结果显示所属路径）
5. **专家模式**：默认隐藏高风险与冷门项，一个开关统一揭示；每项标风险等级（安全/进阶/危险）
6. **配置即产品**：每个设置项都能在 `syfuse.conf` 找到对应键，UI 与文件双向同步

### 导航结构（超集）

底部 4 Tab 保持不变（**首页 / 配置 / 数据 / 设置**），新增内容全部落在已有 Tab 内，
**不新增第 5 个 Tab**（避免破坏 Shadowrocket 习惯）：

| Tab | 新增落点 |
|---|---|
| 首页 | 顶部状态条（速率/流量/拦截数/出口 IP，可关）；大开关下新增「泄漏防护」徽标（异常变红可点进自检） |
| 配置 | `恢复默认配置 / 导入... / Wi-Fi 上传 / 模块 / 测试规则` 之后新增 `过滤规则`（AGH/uBO）与 `Hook 配置`；配置详情页新增「模块引用」「过滤规则」「Hook 应用范围」三段 |
| 数据 | 新增小节：`连接`、`拦截`、`Hook`、`局域网投送` |
| 设置 | 新增小节：`模块与特权`、`广告与过滤`、`隐私与防泄漏`、`脚本与 Hook`、`备份与还原`、`浏览器面板` |

首页卡片可拖拽排序、可增删、可折叠；每张卡可选显示内容（状态条 / 快捷动作 / 统计 / 拦截数 / 出口 IP / 最近日志）。

### 全功能自定义化清单

每一项都必须满足：**UI 可达 + 可持久化 + 可导入导出 + 可被设置搜索命中 + 可恢复默认 + 可脚本化**。

主题（浅色/深色/跟随系统/AMOLED 纯黑）、动态取色、主色与强调色自定义、渐层方案（极光/日落/单色/自定义）、
**玻璃效果开关与强度三档 + 模糊半径微调 + 着色不透明度 + 高光描边开关**、导航栏样式、密度、字号缩放、
行高、圆角、卡片阴影、列表分隔线、图标包、动效开关与时长（含弹性强度）、起始 Tab、手势、Tab 顺序与显隐、
启动页、图表配色（色盲友好）、日期与单位格式、语言、浏览器面板入口显隐。

以及 18 项系统级自定义：原始配置编辑（原生语法高亮，**禁止 Web 编辑器**）、规则源自定义、DNS 全参数、
隧道全参数、TLS 指纹（全局与单节点两级）、链式代理、场景自动化、脚本中心、Hook 中心、过滤引擎、
MITM、防泄漏、统计与审计、开放 API 与 CLI、内核调试、多配置并行、模块订阅源、备份策略。

---

## 防泄漏（IP 防泄漏，必须做全）

| 维度 | 必须实现 |
|---|---|
| DNS 泄漏 | DNS 强制走隧道；覆盖/阻断系统 Private DNS；加密上游（DoH/DoT/DoQ）；禁止 fallback 直连；DNS 绑定隧道接口；检测并提示"解析结果来自本地运营商 DNS" |
| IPv6 泄漏 | IPv6 全阻断或全走隧道；检测 AAAA 直连；提示运营商 IPv6 直连风险 |
| WebRTC / STUN 泄漏 | 防火墙阻断 STUN/TURN 直连或用 Hook 阻断；内嵌浏览场景检测 |
| QUIC / UDP 泄漏 | QUIC 阻断或走隧道；UDP 全量转发；检测 UDP 直连 |
| 真实 IP 回显检测 | 连接后自动请求多个 IP 回显端点（可自定义列表），比对出口 IP 与节点预期地区，检测「未走代理 / 出口与节点不一致 / 混合出口」 |
| 分流泄漏 | 检测本机 IP、局域网 IP、网关、运营商 DNS 是否直连；敏感域名（银行/支付/邮箱）走直连时告警 |
| 系统级 | 私有 DNS 状态、Captive Portal、热点/蓝牙共享、NTP/SSDP/mDNS 泄漏 |
| 一键自检面板 | 逐项检测 + 结论 + 证据（原始请求/响应）+ **一键修复**（改成推荐配置）+ 修复前后对比 |
| 定时巡检 | 间隔可设（每 30 分钟 / 每次连接后 / 网络切换时），异常发通知，保留历史 |
| 报告导出 | 导出自检报告（含时间、网络、节点、各项结果） |

同时保留 SY-Fusion 的 `PROXY_LEAK` 语义（节点级防泄漏开关）。

---

## 过滤引擎（AdGuard Home + uBlock Origin Lite + SY-Fusion 模块功能全量并入）

三层能力全部并入，**离线全可用**：

| 来源 | 并入内容 |
|---|---|
| **AdGuard Home** | DNS 层拦截、上游 DNS 管理与竞速、查询日志、统计（查询数/拦截数/威胁数/成人内容数）、DNS rewrites（`address=/x/`、`local=/x/`）、客户端管理（按客户端/标签作用域 `$client=` `$ctag=`）、黑白名单、规则订阅与自动更新、拦截页可选、每客户端配置 |
| **uBlock Origin Lite** | 网络过滤 + 修饰过滤（元素隐藏、CSS 注入、scriptlet 注入、HTML 过滤）+ 资源类型与修饰符全集 + 优先级模型 |
| **SY-Fusion 模块功能全量并入** | 融合规则引擎（AGH 语义 × uBO 匹配与优先级，零外部依赖、全离线）、DNS 层内核拦截、MITM 透明代理、开屏广告拦截、网络拦截、网络代理（`PROXY_LEAK`）、脚本扩展中心、一体化面板信息架构、配置体系同名键、`abserver` CLI 子命令、规则引擎 CLI、备份恢复、自动更新、即插即用、安全机制、自检体系 |

工程要求：解析 → 校验（报错到行号）→ 编译为二进制（域名 Trie + IP Patricia Trie + 正则集合）→
热重载不中断连接；支持本地编辑、远程规则集、四种格式（`domain` / `ipcidr` / `classical` / `mrs`）、
定时更新、哈希校验、镜像加速。误拦截可 1 秒内恢复（临时放行）。

---

## Hook 系统（内置能力开箱可用）

**引擎检测与选择**：Zygisk / ReZygisk / Zygisk Next / LSPosed / APatch / LD_PRELOAD / ptrace。
UI 显示当前可用引擎并允许切换；全部缺失时自动降级到 TPROXY/VPN 等价方案（**能力降级而非功能缺失**）。

| 内置 Hook | 作用 |
|---|---|
| **证书固定绕过** | 让 MITM 对目标 App 生效（TrustManager / OkHttp CertificatePinner / Conscrypt / Cronet） |
| 用户 CA 信任注入 | 免改 APK 让目标 App 信任本项目 CA |
| 网络栈 Hook | OkHttp / HttpURLConnection / WebView / Cronet / Retrofit / Volley 的请求重定向与观测 |
| DNS Hook | `android_getaddrinfo`、`InetAddress`、`DnsResolver` 强制走本地 DNS |
| 代理重定向 | 按 App 强制走指定节点/策略组（含不支持代理的 App） |
| 开屏跳过 | 启动广告 Activity 拦截与自动跳过 |
| WebRTC / STUN 阻断 | 防真实 IP 泄漏 |
| 环境伪装（默认关） | 对指定 App 隐藏 root/VPN/代理状态（需明确告知风险） |

管理 UI：Hook 列表（名称、引擎、作用域包名、开关、命中次数、日志、冲突提示）、作用域选择器、
命中统计图表、错误日志、试跑与回滚。自定义 Hook 脚本暴露 Hook 点（方法签名、类名、参数）给 JS。

---

## SY / SY-Fusion 功能复现规格（6）

第 6 部分把参考项目的可复现规格写成了**可执行、可验收**的形式。
它与第 5 部分的区别：第 5 部分是"要对齐 Shadowrocket 的什么"，
第 6 部分是"要把参考项目的哪些能力原样复现出来、怎么验收"。

### 逐项复现映射（来源 → 本产品实现 → 验收）

| SY / SY-Fusion 能力 | 本产品实现位置 | 验收方式 | 状态 |
|---|---|---|---|
| 融合规则引擎（AGH 语义 × uBO 匹配与优先级，零外部依赖、全离线） | `core:rule`（Kotlin，已实现）→ M10 移植到 Go 供守护进程用 | 语料 + 期望表逐条对拍 | **部分**（Kotlin 侧 28 用例 + 38 条期望全绿；Go 侧未做） |
| DNS 层内核拦截（hosts 绑定挂载） | `module/post-fs-data.sh` + 守护进程 DNS 服务 | 真机：加规则 → 访问被拦 → 计数增加 | 未做（M6/M10） |
| MITM 透明代理（自动签发注入根证书） | 守护进程 MITM 引擎 | 真机：抓包看到明文 | 未做（M8） |
| 开屏广告拦截（内核级窗口监听） | Hook 内置能力 + 无障碍降级 | 真机：开屏被跳过 → 计数增加 | 未做（M11/M13） |
| 网络拦截（域名/IP/网段/端口） | 过滤引擎 + 连接层 | 真机：访问被阻断 → 日志可见 | 未做（M13） |
| 网络代理（`PROXY_LEAK` 节点级防泄漏） | 代理层 + 防泄漏 | 真机：出口 IP 与节点地区一致 | 未做（M3/M12） |
| 脚本扩展中心（油猴 / MITM / QX-Surge-Loon-SR 兼容） | 脚本系统 + 生态兼容层 | 导入脚本 → 命中 → 日志有输出 | 未做（M7） |
| 一体化面板（仪表盘/过滤器/设置/工具与备份，瓷砖瓦片） | App 的统计/过滤/工具页 + 可选局域网面板 | 面板可开关、令牌可用、关闭释放端口 | 未做（M13） |
| 配置体系同名键（`DNS_ENABLE` … `RULES_MIRROR`） | `module/config/syfuse.conf` | 键表逐项核对 | **可用**（36 键已落地，含类型/范围/风险/UI 位置） |
| 分项配置 10 个文件 | `module/config/*` | 文件存在 + 可解析 | **可用**（10 个模板已落地） |
| `abserver` CLI（`fetch`/`ca`/`proxy`/`serve`/`token`） | `syfused` 同名子命令 | 子命令存在 + 未实现时退出码 78 | **部分**（`serve`/`token` 可用，其余报未实现） |
| 规则引擎 CLI（`build`/`stats`/`syntax`/`verify`/`test`/`testfile`/`paths`/`clear`） | `syfused rules <sub>` | 同上 | **部分**（`paths`/`stats` 可用，其余报未实现） |
| 备份与恢复（config/rules/scripts） | 设置 → 备份与还原 + `action.sh` 第 8 项 | 导出 → 清空 → 导入 → 完整恢复 | **部分**（`action.sh` 备份可用；App 侧未做） |
| 自动更新（`update.json` + SHA-256） | `package.sh` 写入 sha256；模块内先验哈希再解压 | 哈希校验 + 失败保留旧版 | **部分**（写入已做；模块内校验未做） |
| 即插即用（OverlayFS/bind mount，不占系统分区） | `module/customize.sh` | 安装 → 系统分区未变 | **可用**（沙箱验证 PASS=17） |
| 安全机制（脚本沙箱、按应用隐藏、纯本地无日志、无系统分区改动） | 脚本沙箱 + UID 分流 + 第 15 部分禁止事项 | 逃逸测试 + 权限核对 | **部分**（无系统分区改动已保证；沙箱未做） |
| 自检体系（多轮自检、端到端、模块静态校验、UI 资产回验、真实 UI 执行、截图回归、规则语料、真机性能、打包 SHA-256、远程树一致性） | `tests/` 全部脚本 + `tools/` | 各脚本退出码 | **部分**（19 个脚本中 14 个有真实断言；5 个的设备部分未执行） |
| 面板 UI 性能教训（全屏模糊 P50 19ms→150ms、丢帧 3.68%→100%） | `ui/glass/Glass.kt` 的白名单 + 预算 + 自动降档 | `tests/ui_glass_perf.sh` | **部分**（策略层已实现并断言；真机未测） |

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

### 模块划分的两处偏离（已登记）

提示词 9.1 的模块清单是 `app / ui / domain / data / core / engine / daemon / privileged / hook`。
本仓库有**两个**额外的子工程，都是被"必须能在只有 JDK 的环境里独立单测"这条要求逼出来的：

| 额外模块 | 为什么不能塞进原模块 |
|---|---|
| `core:common` + `core:rule`（拆自 `core`） | 规则引擎是纯 JVM 逻辑，必须能在没装 Android SDK 的最小环境里验证语义。和 Android 相关的 `common` 放同一个工程会让这个能力丧失。 |
| `daemon-protocol`（拆自 `daemon`） | **这条是实测倒逼的，不是洁癖。** 协议模型/客户端/传输接口本身一行 Android 代码都没有，但住在 Android library 里时，其**单元测试**源码集是对着 `android.jar` 编译的，而 `android.jar` 里**没有** `java.net.UnixDomainSocketAddress`（实测 android-35：无此类；`android/net/LocalSocket.class` 有）。后果是"App → UDS → 守护进程"这条往返在沙箱里**连编译都过不去**，只能等真机。拆成纯 JVM 模块后，测试对着真正的 JDK 编译，可以注入 AF_UNIX 传输，用**本机真实编译出的 `syfused` 进程**跑通往返。真机专属的 `android.net.LocalSocket` 实现留在 `:daemon`，两边各自被覆盖。 |

代价与边界（诚实说明）：多两个 Gradle 子工程，构建图更复杂一点；
并且**仍然不能**证明 `android.net.LocalSocket` 在真机上可用 ——
那一步必须真机验证，记在 `PENDING-EXTERNAL.md`。
这次拆分买到的是"协议是否正确"与"平台 API 是否可用"两件事的**解耦**，
让前者可以在沙箱里被证明，而不是把两者捆在一起一起不可验证。

`tests/run_module_checks.sh` 的 **R2b** 段逐条守着这个结构（模块是纯 JVM、
无 `android.*` 依赖、JDK-only 实现只在 test 源集、真机实现仍在 `:daemon`）。

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

> **默认值不是随便定的。** 它们逐项对齐提示词 **6.4「必须沿用的具体参数」** ——
> 6.1 规定「参数同值」是"复现"的四维判定之一，默认值漂移就等于复现失败。
> 注意其中几条与"安全默认值"直觉相反：**DNS / 开屏 / 通知 / 面板 / 黑名单默认开**，
> **MITM 与代理默认关** —— 这是 6.4 的硬规定（复现原项目行为）。
> `tests/run_module_checks.sh` 有 20 条断言逐项守着这些默认值。
>
> 另：`DNS_UPSTREAM_ENC` 是 **URL 形态**（加密方式由 scheme 决定），不是枚举；
> 三个更新周期的单位是**小时**不是分钟。这两条我最初都写错了，已按 6.4 改正。

| 键 | 默认 | 类型 | 范围 | 风险 | UI 位置 |
|---|---|---|---|---|---|
| `DNS_ENABLE` | `1` | bool | 0/1 | 安全 | 设置 → 广告与过滤 |
| `MITM_ENABLE` | `0` | bool | 0/1 | 危险 | 设置 → 广告与过滤 |
| `SPLASH_ENABLE` | `1` | bool | 0/1 | 危险 | 设置 → 广告与过滤 |
| `NOTIFY_ENABLE` | `1` | bool | 0/1 | 进阶 | 设置 → 广告与过滤 |
| `PANEL_ENABLE` | `1` | bool | 0/1 | 危险 | 设置 → 浏览器面板 |
| `DNS_UPSTREAM` | `1.1.1.1` | string | IPv4/IPv6/URL | 进阶 | 设置 → 隐私与防泄漏 |
| `DNS_UPSTREAM_ENC` | `https://dns.alicdn.com/dns-query` | string(URL) | scheme 决定 DoH/DoT/DoQ | 进阶 | 同上 |
| `DNS_AUTO_UPDATE` | `0` | bool | 0/1 | 安全 | 同上 |
| `DNS_UPDATE_INTERVAL` | `24` | int(**小时**) | 1..720 | 安全 | 同上 |
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
| `PROXY_LEAK` | `0` | bool | 0/1 | 进阶 | 设置 → 隐私与防泄漏 |
| `PROXY_SUB_INTERVAL` | `24` | int(**小时**) | 1..720 | 安全 | 配置 → 订阅 |
| `BLACKLIST_ENABLE` | `1` | bool | 0/1 | 安全 | 设置 → 广告与过滤 |
| `ENGINE_HEURISTIC` | `0` | bool | 0/1 | 进阶 | 同上（可能误拦） |
| `ENGINE_STRICT_SCOPE` | `1` | bool | 0/1 | 进阶 | 同上 |
| `ENGINE_CLIENT` / `ENGINE_CTAG` | 空 | string | — | 进阶 | 同上（对应 `$client=` / `$ctag=`） |
| `HOSTS_EXTRA` | 空 | string | — | 进阶 | 配置 → Hosts |
| `RULES_REPO` / `RULES_BRANCH` / `RULES_MIRROR` | **空**（由 `set_owner.sh` 运行时填入）/ `main` / 空 | string | — | 安全 | 设置 → 更新 |
| `SCRIPT_UPDATE_INTERVAL` | `24` | int(**小时**) | 1..720 | 安全 | 设置 → 脚本与 Hook |
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
`rules paths` `rules stats` `rules syntax|build|verify` `rules test` `version` `help`

**已注册但未实现**（返回**退出码 78** + 计划里程碑，区别于普通错误 1）：
`fetch`(M7/M10) `ca`(M8) `proxy`(M8) `hook`(M11) `leak`(M12)
`rules testfile|clear`(M5/M10)

```bash
syfused status                    # 运行状态 + 还原清单摘要 + 逐条校验问题
syfused token                     # 显示访问令牌
syfused token --rotate            # 重新生成（旧令牌立即失效）
syfused restore                   # 打印还原计划（dry-run，默认）
syfused restore --apply           # 执行还原
syfused rules paths               # 打印全部规则相关路径及其存在性
syfused rules stats               # 真实编译一次规则集后报数
syfused rules test block-a.test   # 对单个请求跑一次匹配（含候选链路）
syfused rules verify              # 规则体检（带行号的逐条诊断，有 ERROR 则退出码 1）
```

### UDS 控制协议

socket `/data/adb/syfuse/control.sock`（0600），换行分隔 JSON，每次连接须带 token。

**已实现方法**（`DaemonProtocol.IMPLEMENTED_METHODS`）：
`status` `version` `manifest.summary` `token.rotate` `log.tail`
`rules.test` `rules.syntax` `rules.stats` `rules.build`

**已注册未实现**（返回明确错误 + 里程碑，**不是** `unknown_method`）：
`proxies` `proxies.start` `proxies.stop` `proxies.reload` `proxies.select` `proxies.urlTest`
`connections` `traffic` `filter.stats` `hook.status` `leak.report` `leak.fix` `config.reload`

> 为什么要**注册**而不是留空：留空会让 App 拿到 `unknown_method`，
> 而它会被翻译成"模块不认识这个请求，可能是版本不匹配"——把用户引向完全错误的方向。
> 注册之后 App 拿到的是"方法 `proxies.start` 尚未实现（计划里程碑 M3）"，
> 这是一句用户能看懂、也能据此判断"该等还是该升级"的话。

请求/应答格式见 `src/internal/ipc/ipc.go` 与
`daemon-protocol/src/main/kotlin/.../DaemonProtocol.kt`
（两侧是同一份协议，**必须同步修改**；`DaemonProtocolContractTest` 把 Go 源码当唯一事实来源逐项核对）。

### HTTP API / 浏览器面板 / Intent / URL Scheme

**全部未实现**，计划 M13。设计约束已定：默认关闭、需 token、**禁止 WebView 内嵌面板**、
关闭面板立即释放端口并清空会话。

---

## 构建与自检

```bash
# 全量自检（不需要设备）
tests/run_module_checks.sh     # 红队静态校验（R1–R6 + 18.1 结构 + M1 判定）
tests/run_kernel.sh            # Go：gofmt / vet / build / 88 用例 / 零依赖断言
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

## 性能预算与实测（第 11 部分）

**预算取自提示词第 11 部分原文，不是本项目自拟的数字。** 达不到就写不达标 + 差距 + 原因；
测不了的写"未测"并说明卡在哪 —— 一律不写成达标。

| # | 指标 | 预算（提示词原文） | 实测 | 判定 |
|---|---|---|---|---|
| 1 | 冷启动到首页可交互 | < 800ms（中端机） | 未测 | ⏸ 需真机 Macrobenchmark |
| 2 | 连接建立（点开关到可上网） | < 1.5s | 未测 | ⏸ 内核未接入（M3） |
| 3 | APK 单 ABI 体积 | < 30MB | **1.30MB**（release arm64-v8a，实测 1,362,138 字节） | ✅ |
| 4 | 模块 ZIP 体积 | < 45MB | **4.34MB**（实测 4,555,719 字节，23 项） | ✅ |
| 5 | 常驻内存（守护 + App 空闲） | < 120MB | 未测 | ⏸ 需真机 `dumpsys meminfo` |
| 6 | 待机耗电 | < 3%/h | 未测 | ⏸ 需真机 8 小时实测 |
| 7 | 100 万条规则编译 | < 3s | **436~470ms** | ✅ |
| 8 | 单次规则匹配 p99 | < 1ms（≥10 万次采样） | **0.6962 / 0.8033ms**（两次独立 5 轮运行的中位数） | ✅ 余量 19.7%~30.4% |
| 9 | MITM 解密吞吐 | ≥ 直连 60% | 未测 | ⏸ 未实现（M8） |
| 10 | Hook 命中附加延迟 | < 5ms/请求 | 未测 | ⏸ 未实现（M11） |
| 11 | 列表滚动丢帧率 | < 1% @120Hz（玻璃关）/ < 3%（玻璃开） | 未测 | ⏸ 需真机 Macrobenchmark |
| 12 | 玻璃开启时帧耗时 | P90 ≤ 24ms，首屏增量 ≤ 80ms | 未测（策略层已断言，真机未测） | ⏸ 需真机 |
| 13 | 玻璃降级正确性 | 超标自动降档 + 写日志；API<31 走伪玻璃 | 策略层 26 条 JVM 用例通过 | 🟡 逻辑已验证，真机未测 |
| 14 | 隧道吞吐 | ≥ 直连 80%（千兆局域网） | 未测 | ⏸ 内核未接入（M3） |
| 15 | 崩溃率 | 0 | 未测 | ⏸ 需真机 |

### 规则引擎 p99：这一行被改过三次

| 版本 | 写法 | 为什么错 |
|---|---|---|
| 最初 | `预算 < 0.5ms`，判定 ❌ | **0.5ms 是自造的**，比需求更严。据此声称"超出预算 1.9 倍"，把达标写成了未达标 |
| 上一次 | `预算 < 1ms`，判定 ✅ 余量 3.5% | 预算改对了，但**拿单轮结果下结论** |
| 本次 | `预算 < 1ms`，判定 ✅ 余量 19.7%~30.4% | 改为「1 轮预热丢弃 + 5 轮测量取中位数」 |

错的是**方法学**，不是实现。单轮基准**永远**测在 JIT/GC 收敛过程中：同一台机器、同一份代码，
单轮 p99 得到 0.9651ms 与 1.3183ms（相差 36%）；改成多轮后，第 1 轮 1.3077ms、稳态稳定在 0.69ms 左右。
提示词第 11 部分要求"多轮取中位数"，正是为了避免这种误判。

**仍未解决的、如实保留的部分**：10 个测量轮里有 2 轮（1.3077 / 1.2108ms）越过 1ms 预算；
且 p50 已达 **0.60~0.68ms** —— 说明线性扫描压住的是**整个分布**，不只是尾部。
根因在 `FilterEngine.collectCandidates`：它对**每个**请求（含域名 Trie 命中的）都把全部线性规则
复制成新列表并逐条测试。修法（正则必需字面量分桶）排在 M10，且按"先补语料、再改索引"的顺序做。

原始逐轮记录见 [`tests/perf_results.txt`](tests/perf_results.txt)（含作废段落的留痕与作废原因）。
复现：`tests/perf_rule_engine.sh [规则数] [测量轮数]`。

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
| 7 | 规则引擎 p99 | 100 万条实测中位数 **0.6962~0.8033ms**，预算 **<1ms** → **达标（余量 19.7%~30.4%）**；但 10 个测量轮里 2 轮越线，p50 已 0.60~0.68ms | M10 做正则字面量分桶索引化；先补语料再改索引。详见[性能预算与实测](#性能预算与实测第-11-部分) |
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

## 浏览器面板（App 内一键开关）

设置 → 新增小节 `浏览器面板`：

- **`启用浏览器面板` 开关** —— 默认**关**；打开时提示局域网可见性与风险
- **`打开面板` 按钮** —— 用**系统浏览器**打开（**禁止用 WebView 内嵌**，见 15.1）
- `端口`（默认 8848，可改）、`仅本机访问 / 允许局域网`、`访问令牌`（显示 / 复制 / 重新生成）
- `二维码`（供同网段设备扫码打开）、`复制地址`、`局域网地址自动探测`（列出本机各网卡 IP 供选择）
- `面板功能范围`（可勾选：仪表盘 / 过滤器 / 模块与脚本 / 连接与日志 / 设置 / 工具与备份）
- `空闲自动关闭`（可设分钟数）

首页与设置的模块状态里显示面板运行状态（运行中 / 端口 / 在线连接数）。

面板本身已并入 SY-Fusion 面板的信息架构（仪表盘 / 过滤器 / 设置 / 工具与备份，瓷砖瓦片统计），
**用第 4.11 的现代设计系统重做视觉**，不是照抄它的旧样式。

**关闭面板时立即释放端口并清空会话。**

| 项 | 状态 |
|---|---|
| 面板服务端与前端 | 未实现（M13） |
| App 内开关与一键打开 | 未实现（M13） |
| 设计约束（默认关 / 需 token / 禁止 WebView 内嵌 / 关闭即释放端口） | 已定，写在本节 |

---

## 工程纪律与交付门禁

本节记录本项目**自己遵守**的规则。它们不是口号，都有对应的可执行断言。

| 纪律 | 落地方式 |
|---|---|
| **完全体授权** | 技术栈、架构、Hook 方案、UI 细节、命名、功能增补都由实现者定；唯一边界是合法合规、许可兼容、可完整还原、不造假 |
| **反僵化条款** | 提示词是能力下限不是思考上限。发现提示词写错（如 mihomo 引用错误）**直接指出并给修正建议**，不机械执行。已据此出具 2 处勘误 |
| **账号名运行时解析** | 按 `GITHUB_USER` → `GITHUB_ACTOR` → `GITHUB_REPOSITORY_OWNER` → `gh api user` → `git config github.user` → `git remote` 顺序解析，**不硬编码**，也不写 AI 工具名 |
| **令牌已授权使用** | 建仓 / 推送 / 打 tag / 发 Release / 读取仓库信息 / 更新描述与主题均获授权；但**绝不落盘**（不写 `git remote set-url`、不用 `credential.helper store`） |
| **功能可用性门禁** | 每个功能必须有最低可用路径并进 `tests/run_feature_matrix.sh`；状态只有 `可用/实验性/不可用` 三种，**禁止死 UI** |
| **单文件 · 自包含** | 文档全部写在 README 内，不拆多个 md、不做跳跃引用；超过约 1000 行才拆，拆后每个文件自包含 |
| **主界面非 Web 端** | 硬约束：**禁止 WebView / H5 / 混合框架承载主界面**（含设置页、规则页、编辑器）。可选局域网面板是唯一例外且默认关闭 |
| **交付完整性纪律** | 读文件必须核对返回行数 vs 真实总行数；预览/截断**不算读过**；禁用无法证伪的说法（"已重读全文"等）。本项目曾**把没读完说成读过了** —— 这是比技术错误更严重的一类失败，已记入风险清单并作为强制检查项 |
| **交付门禁** | 每次交付必须运行 `tools/verify_deliverable.sh`（CI 等价版）或 `tools/verify_deliverable.ps1`（Windows），**并把整段原始输出贴进报告**；退出码非 0 即禁止交付 |
| **提速与并行执行规范** | 提速靠**粒度与并行**，不靠跳步。动手前先量三个数（全量构建 / 单测 / 门禁耗时）写进 `PROGRESS.md`；把剩余里程碑拆成 30–60 分钟、1–3 个文件的任务单元并登记验收命令；无依赖单元用子代理并行，**但必须先冻结接口与目录约定、同一文件绝不两个单元同时改**；子代理产出由主代理统一验证后合入，**不接受子代理自证**；同模式工作（多个页面、设置页、协议解析）用统一模板或生成器一次做完。详见 [任务拆分与耗时基线](#任务拆分与耗时基线) |
| **安全提速红线（提速不得降质）** | 提速**只省时间，不省正确性**。不跳门禁、不降证据标准、不删测试、不放宽验收口径、不在接口冻结前并行、不让子代理自证、不隐瞒失败（失败单元标「受阻」）、不为速度牺牲还原性与安全、不做无度量提速。验证**只降频率不降标准**：单元级最低证据 = 编译输出 + 单测结果 + 变更文件清单；每 3 个单元一次集成冒烟；全量门禁与功能可用性矩阵在里程碑级跑（退出码非 0 即该里程碑未交付）；性能/截图/卸载残留只在里程碑末与发布前跑。真机相关做不了的项标「待外部验证」写进 `PENDING-EXTERNAL.md` 后**继续做不依赖它的单元，不停摆** |

### 交付门禁的 7 组检查

`tools/verify_deliverable.sh` 与 `tools/verify_deliverable.ps1` 检查项一致：

1. **体积与行数** —— 字节数、行数下限、首末行非空（确认没被截断）
2. **结构完整性** —— `##` / `###` 数量、代码围栏成对、重复小节标题
3. **占位符与未完成标记** —— `TODO` / `FIXME` / `<your-name>` / 待补写 / `[略]` / 截断省略号 / `XXX`
4. **密钥与令牌残留** —— `ghp_` / `github_pat_` / `AKIA` / 私钥块 / Slack token
5. **需求逐条覆盖** —— **38 条**需求关键词必须全部命中。这一组只对**交付文档**（即本 README）有意义：
   它是全项目唯一的文档入口（18.7），38 条需求必须在这里都能被检索到。
   把它指向 `CHANGELOG.md` 之类的文件会大量 FAIL —— 那是**预期行为**，不是缺陷
6. **脚本自身编码** —— `.ps1` 必须带 UTF-8 BOM（否则 PS 5.1 按 ANSI 解码中文会报语法错误）；`.sh` 必须**无** BOM（否则破坏 shebang）且不得含 CRLF
7. **附录与磁盘一致性** —— 附录 A/B/C 的 base64 解码结果必须与 `tools/` 里三个脚本逐字节一致
   （依赖随母版分发的 `tools/sync_appendix.ps1 -Mode Check`）

```bash
# 本仓库的交付文档（这是门禁的**正确用法**；退出码必须为 0）
bash tools/verify_deliverable.sh README.md 300
# 提示词文件本身
bash tools/verify_deliverable.sh "F:/Ai/Syfuse-完美提示词.md" 1000
```

> 当前状态：`README.md` → **全部通过（WARN 2 项）**，退出码 0。原始输出见
> [`docs/EVIDENCE.md`](docs/EVIDENCE.md) 第 2 节。

#### 本仓库对门禁的五处勘误（2.3）

门禁脚本由提示词附录 A/B 交付。本项目在 M3 轮做过一次逐字节重同步，并对**附录自身的缺陷**
出具 5 条勘误（E1–E5）。全部登记在 `docs/errata/`，是 unified diff，可逐字核对。

1. **E1 · 附录 B 第 6 组是死检查（已修）**：`$PRUNE` 变量未加引号，被 bash 通配展开成几十条
   `app/build/...` 路径，`find` 报 `paths must precede expression` 后**静默返回空**，
   而 `2>/dev/null` 吞掉了 stderr、`FOUND=0` 只报 WARN、退出码仍为 0 ——
   也就是说这一组**从未真正执行过**（修前报"未找到脚本"，修后列出 28 个脚本）。
   修法：`read -ra` 切成数组，pattern 原样交给 `find`。
2. **E2 / E3 · "找不到脚本可检查"由 WARN 改成 FAIL（已修）**：`.sh` 与 `.ps1` 两版都改。
   「静默跳过」正是 E1 能长期潜伏的原因 —— 一个检查从"0 个对象"变成"28 个对象"却没人发现。
3. **E4 · 头部注释与实际组数不符（已修）**：附录新增了第 7 组，但两版头部仍写「共 6 组」。
4. **E5 · 第 6 组的判定逻辑在 Windows 上极慢（已修，见 T7）**：
   取 BOM 用 `head -c 3 | od | tr | sed` 一条 4 进程流水线，CRLF 判定每个文件一次 `grep`，
   文件名用 `$(basename)`。Git Bash 创建进程 ≈155 ms/次，于是 28 个文件要 ≈39 s。
   改成 bash 内建读取（`read -N 3` / `read` 逐行扫 / `${s##*/}`）后降到 **13.3 s**，
   而**检查的对象与断言条数完全不变**（仍是 26 个 `.sh` × 2 条 + 2 个 `.ps1` × 1 条 = 54 条）。
   替换前先做了 **12 组等价性矩阵**（BOM × CRLF × 长度）逐组核对新旧判定 ——
   这个矩阵当场抓出了第一版的错误（Git Bash 的 `$(<file)` 会把末尾 `\r\n` 一起吃掉，
   导致单行 CRLF 文件被误判为无 CRLF）。详见 `docs/EVIDENCE.md` §14.11。
5. **第 7 组在本仓库不生效（已知局限，未修）**：它依赖 `tools/sync_appendix.ps1`，
   而该脚本是随母版分发的工具、不在附录 A/B/C 三件套里。缺失时第 7 组只报 WARN 跳过，
   因此**附录与磁盘的漂移不会被自动拦住**。补偿手段：`tests/run_appendix_sync.sh`
   把"与附录的偏差必须逐字等于已登记勘误"写成断言（含 2 个变异测试），已挂进 CI。

> 另有一处**本仓库自身**的缺陷（不属于附录）：`.gitattributes` 曾把 `.ps1` 定成 `eol=crlf`，
> 入库 blob 是 LF、但每次 checkout 都写出 CRLF，会让"与附录逐字节相同"这条断言
> 在 clone / CI 上必红而本地全绿。已改为 `eol=lf`，详见 `docs/EVIDENCE.md` §14.10。

### 任务拆分与耗时基线

2.11 要求「先量再动」。本仓库的实测基线（Windows 10 / JDK 21 / Go 1.23.4 /
Gradle 8.11.1 / Android SDK 35，**Gradle daemon 与 build cache 已预热**）：

| 项目 | 实测 |
|---|---|
| 全量构建 `:app:assembleDebug` | 40 s |
| `tests/run_unit.sh`（5 模块 / 126 用例） | 98 s |
| `tests/run_kernel.sh`（Go，127 用例） | 19 s |
| `tests/run_module_checks.sh` | 41 s |
| `tools/scan_secrets.py` | 1 s |
| `tools/verify_deliverable.sh README.md 300` | **13.3 s** |
| `tools/verify_deliverable.ps1 -Path README.md -MinLines 300` | **0.96 s** |
| `tests/run_appendix_sync.sh` | 14 s |
| **门禁合计** | **≈227 s（3.8 min）** |

结论：门禁总耗时不到 4 分钟，**不构成瓶颈**，因此本项目不为此更换环境或上 WSL2；
提速的杠杆在任务粒度与并行度，不在工具链。任务单元（单元 / 范围 / 依赖 / 验收命令 /
预计 / 状态 / 实测耗时）登记在仓库根目录的 `PROGRESS.md`。

`.sh` 与 `.ps1` 两版门禁的耗时差 14 倍，不是算法差异而是**进程创建开销**：
`.sh` 版过去每个文件要起 4 个子进程，`.ps1` 版用的是内建 `Get-Content`。
按 2.11「先量再动」量清之后改成了 bash 内建读取（勘误 E5），并附 12 组等价性矩阵。

---

## 常见问题

**Q：为什么还没有代理功能？**
A：当前是 M2（App 骨架与视觉）进行中，代理内核在 M3 接入。本文档不会把"计划做"写成"已做"。

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

无需要用户粘贴执行的命令。账号与令牌可用，建仓 / 推送 / 核验均已完成。

> 仓库已于 **2026-10-13 由所有者指示转为 public**（原先按提示词 2.6 / 18.6 先建 private）。
> 转为公开后 `module.prop` 的 `updateJson` 与 `update.json` 中的 raw 地址即可匿名访问，
> 无需改动文件内容即生效。相应地，`tools/verify_remote.py` 的默认期望值与 CI 的
> `remote-guard` 已同步翻转为 `--expect-public true`：护栏未被取消，而是改为
> **在仓库被改回 private 时失败**。
>
> 注意这与"自更新链路可用"不是一件事：`update.json` 的 `zipUrl` 指向 Release 资产，
> 而 Release 尚未创建，该 URL 目前仍是 404（见 [`PENDING-EXTERNAL.md`](PENDING-EXTERNAL.md) 第 2 条）。

---

## 进度与外部依赖

跨轮续跑的唯一进度真相在 [`PROGRESS.md`](PROGRESS.md)（格式见提示词 2.10）：
里程碑状态表 + `NEXT_ACTION` + `BLOCKERS`。新会话只读它就能接着干。

需要真实设备或 GitHub Release 才能完成的检查集中在 [`PENDING-EXTERNAL.md`](PENDING-EXTERNAL.md)，
逐项写明「缺什么 / 怎么补 / 补齐后跑哪条命令 / 期望结果」。

门禁原始输出、功能可用性报告与性能数据落在 [`docs/EVIDENCE.md`](docs/EVIDENCE.md)。

**当前外部依赖（不影响其余里程碑推进）**：

| 依赖 | 影响 | 状态 |
|---|---|---|
| Android 真机（已 root） | 13 个功能的最低可用路径、玻璃帧耗时、隧道吞吐 | 缺（`adb devices` 为空） |
| GitHub Release | 自更新链路（18.5）端到端验证 | 缺（产物已可产出，未发布） |
| 仓库 public | `updateJson` 与 raw 地址可匿名访问 | 缺（按规则不自行放开） |
| Zygisk / LSPosed 框架 | Hook 命中验证 | 缺（降级路径已实现，同样待真机） |

---

## 作者

`vip-svip`（运行时解析出的 GitHub 账号，非硬编码、非 AI 工具名）。
改账号请运行 `tools/set_owner.sh --apply`。
