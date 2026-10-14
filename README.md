# Syfuse · 融合代理

> **Shadowrocket 超集，Android 原生实现。** 一个 KernelSU / Magisk / APatch 模块，
> 模块内内置 APK。模块提供系统级能力（透明代理 · DNS · MITM · 过滤 · 封禁 ·
> 脚本 · Hook · 证书与 hosts 注入），App 提供全部界面与配置。
> 两者各自可独立工作，合起来是一个**只能比 Shadowrocket 多、不能少**的客户端。

[![CI](https://github.com/vip-svip/Syfuse/actions/workflows/ci.yml/badge.svg)](https://github.com/vip-svip/Syfuse/actions/workflows/ci.yml)
[![License: GPL-3.0-or-later](https://img.shields.io/badge/License-GPL--3.0--or--later-blue.svg)](LICENSE)
[![Status](https://img.shields.io/badge/status-M0--M14%20%E5%AE%8C%E6%88%90%20%C2%B7%20%E5%BE%85%E7%9C%9F%E6%9C%BA%E9%AA%8C%E8%AF%81-yellow.svg)](#功能可用性报告)

> **当前状态：M0–M14 全部里程碑完成（引擎/守护进程/模块侧），353 项 Go 测试与
> Kotlin 契约测试全绿。** 剩余工作为 Android 真机接线验证——逐项状态见
> [功能可用性报告](#功能可用性报告)，不夸大任何能力。
> 跨轮进度真相见 [`PROGRESS.md`](PROGRESS.md)；门禁原始输出见 [`docs/EVIDENCE.md`](docs/EVIDENCE.md)。

## 特性一览

- **隧道与分流**：本地 SOCKS5 入站，规则驱动 proxy / direct / reject；
  跨语言规则语料 238 行两侧全绿
- **过滤引擎（AGH + uBO）**：域名 Trie + 正则 + 作用域修饰符 + 严格优先级链
  （重要放行 > 重要拦截 > 普通放行 > 普通拦截），热重载，1 万条规则编译 4ms
- **脚本系统**：内置 goja（ECMAScript）沙箱——超时看门狗、受控网络出口、
  QX / Surge / Loon 兼容 API 面；cron 定时任务；HTTP 改写挂钩
- **模块系统**：解析 / 行级开关 / 多模块合并与冲突检测，`modules.*` 全套管理
- **MITM 证书体系**：按需叶证书签发（30 天短效）、解密主机列表（通配/排除）、
  Apple `.mobileconfig` 一键导出
- **防泄漏**：DNS / IPv6 / STUN / QUIC / IP 回显 / 分流 / 系统层七维自检，
  `leak.fix` 一键生成修复计划并应用配置级修复
- **通知净化**：关键字 / 包名限定 / 正则；**网络封禁**：域名 + 字面 IP 连接层拦截
- **备份还原**：ZIP + SHA-256 清单，防半还原
- **热重载**：`config.reload` 原子换新过滤与通知规则，不中断连接
- **零第三方依赖内核**：goja 等全部 vendored 进 `internal/third_party`，
  `go.mod` 无 require、无 go.sum，依赖面完全可控

## 安装

1. 从 [Releases](https://github.com/vip-svip/Syfuse/releases) 下载模块 zip
2. 在 KernelSU / Magisk / APatch 管理器中刷入
3. 模块自带 APK 会被自动安装（`apk_install.sh`）；也可单独安装 `dist/` 下的分 ABI APK
4. 打开 App，或在终端运行模块的 `action.sh` 音量键菜单（11 项，免 root 界面依赖）

## 快速上手

- **启动代理**：App 首页大开关，或 `ksu/magisk --execute action.sh`（守护进程模式）
- **过滤规则**：规则文件放在 `<data>/filter_rules.txt`，App 内改完一键
  `config.reload` 热生效
- **脚本**：脚本编辑器支持草稿试跑（`scripts.run`），命中计数在调试页可见
- **防泄漏自检**：防护页一键自检 → 修复计划 → 配置级修复自动写入

## 文档

- [功能可用性报告](#功能可用性报告) —— 逐项状态与证据（**最重要**）
- [`docs/EVIDENCE.md`](docs/EVIDENCE.md) —— 门禁原始输出与性能数据
- [`docs/filter_gap.md`](docs/filter_gap.md) —— 过滤语法差距调研
- [`PROGRESS.md`](PROGRESS.md) —— 跨轮进度真相

---

## 功能可用性报告

**这是本文档最重要的一节。** 状态只有三种：

- `可用` = 有端到端证据（命令输出 / 截图 / 抓包）
- `实验性` = 能构建能运行但未端到端验证；UI 上必须显著标记
- `不可用` = 未实现；入口禁用并写明原因与计划里程碑

**当前里程碑：M0–M12 已完成，M13（增强补全）Go 侧完成，M14（打磨）进行中。**
沙箱内可验证的项全部有自动化证据；需要 Android 真机的项标注 `真机验证`。

| 功能 | 状态 | 证据 / 原因 |
|---|---|---|
| 模块安装/卸载/还原 | 实验性 | 沙箱端到端 PASS=17；真机验证 |
| 还原清单机制 | **可用** | 88 用例 + 沙箱端到端；manifest 拒绝不可还原条目 |
| `action.sh` 菜单 | 实验性 | 主菜单 11 项门禁断言；随里程碑接通 |
| `syfused` 守护进程 | 实验性 | 交叉编译产物在 `dist/`；真机运行 |
| UDS 控制通道 | 实验性 | socket 生命周期断言经变异测试；真机验证 |
| **代理上网（隧道闭环）** | **可用**（沙箱端到端） | 真隧道 + 最小 SOCKS5 客户端字节往返；M3 |
| 分流/规则生效 | **可用**（沙箱端到端） | core:rule 跨语言语料 238 行两侧全绿；M5 |
| DNS（DoH/DoT、hosts/dnsmasq 解析） | **可用**（解析器层） | 24 用例；DNS 服务真机接上游 |
| 模块系统 | **可用**（解析/开关/合并/冲突） | 19 用例 + modules.* UDS；M7a |
| 脚本系统（goja 沙箱） | **可用**（引擎/沙箱/调度/挂钩） | 22+14 用例；goja vendored 零依赖保持；M7 |
| MITM 证书体系 | **可用**（CA/叶签发/主机列表/描述文件） | 18 用例；MITM 引擎接线真机 |
| 连接跟踪与统计 | **可用**（端到端） | connections/traffic 接真实 Tracker；M9 |
| 过滤拦截（AGH/uBO P0+P1） | **可用**（引擎层） | 18 用例；1 万条编译 4ms；filter.stats/netblock 接线；M10 |
| Hook 框架检测 | **可用**（探测层） | 6 用例；hook.status；真机探测真实路径；M11 |
| 防泄漏自检与一键修复 | **可用**（引擎层） | 12+5 用例；leak.check/leak.fix；真机探测真实网络；M12 |
| 通知净化 | **可用**（引擎层） | 7 用例；notify.test；真机接通知监听；M13 |
| 网络封禁 | **可用**（连接层） | NetBlock 钩子端到端（域名+IP 双拦）；M13 |
| 备份还原 | **可用**（引擎层） | ZIP+SHA-256 防半还原，5 用例；backup.* UDS；M13 |
| cron 调度 | **可用**（调度器层） | 时钟注入确定性测试；daemon 循环接线；M7 |
| 开屏跳过 | **不可用** | 需真机 Hook/无障碍（规范 6.5 三级降级） | 
| UID 精细分流 | **不可用** | 需真机 iptables UID |
| 局域网面板 / 局域网投送 | **不可用** | M13 后段 |
| 内置 Hook 注入本体 | **不可用** | 需真机 + 注入框架 |
| MRS 二进制规则源 | **不可用** | 明确报错指向 P2 路线（docs/filter_gap.md） |

**死 UI 声明**：界面开关只允许出现在上表状态 ≥ `实验性` 的功能上；
`不可用` 项的入口禁用并写明里程碑。

---
## 详细设计文档

历史设计文档（架构 / 数据模型 / syfuse.conf 键表 / 规则语法全表 / API 与 CLI /
性能预算 / 权限 / 风险清单 / FAQ）已随本仓库归档，见：

- [`docs/EVIDENCE.md`](docs/EVIDENCE.md) —— 门禁原始输出与性能数据
- [`docs/filter_gap.md`](docs/filter_gap.md) —— 过滤语法差距调研与路线
- [`PROGRESS.md`](PROGRESS.md) —— 跨轮进度真相
- [`PENDING-EXTERNAL.md`](PENDING-EXTERNAL.md) —— 待外部验证项
- [`docs/` 目录](docs/) —— 其余设计文档


## 免责声明

本项目仅供学习与技术研究。使用本模块产生的任何后果由使用者自行承担。
请遵守所在地区法律法规。

## 致谢与许可

- [goja](https://github.com/dop251/goja)（MIT）—— ECMAScript 引擎
- [regexp2](https://github.com/dlclark/regexp2)（MIT）—— uBO 兼容正则
- [AdGuard Home](https://github.com/AdguardTeam/AdGuardHome) / [uBlock Origin](https://github.com/gorhill/uBlock) —— 过滤语法设计参考
- [SY](https://github.com/vip-svip/SY) / SY-Fusion —— 前身项目
- 本项目采用 [GPL-3.0-or-later](LICENSE) 许可
