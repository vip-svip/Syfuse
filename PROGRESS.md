# PROGRESS.md — 跨轮续跑的**唯一进度真相**

> 本文件自包含：新会话只读它就能接着干，不依赖任何聊天记录（提示词 2.10）。
> 续跑入口固定为：**读 `PROGRESS.md`，从 `NEXT_ACTION` 继续。**

```
MILESTONES
```

| 里程碑 | 状态 | 本轮产出 | 证据位置 | 下一轮第一个动作 |
|---|---|---|---|---|
| **M0 方案** | 已完成 | 架构文档、模块/App 分工、数据模型、`syfuse.conf` 键表、覆盖度对照表初稿、内核选型与许可核实、风险清单 | `README.md`：`## 架构`、`### 内核选型与许可核实`、`## 数据模型`、`## syfuse.conf 键表`、`## 功能覆盖度对照表`、`## 风险清单（M0）`、`## 【待核实】项` | 无（M0 随 M1 交付，提示词 13 允许） |
| **M1 模块骨架** | 待外部验证 | 模块骨架（`module.prop`/`customize.sh`/`post-fs-data.sh`/`service.sh`/`action.sh`/`uninstall.sh`）、`action.sh` 19 项菜单、Go 守护进程骨架、UDS 控制通道、还原清单机制、卸载残留检查 | `tests/run_module_checks.sh` **PASS=101 FAIL=0**；`tests/run_restore_check.sh` **PASS=17 FAIL=0**；`dist/syfuse-0.1.0.zip`（23 项，含 `bin/syfused-{arm64,arm,amd64}` 与 `apk/syfuse.apk`） | 接真机后跑 `tests/run_restore_check.sh` 真机分支与 `tests/run_e2e.sh` |
| **M2 App 骨架与视觉** | 进行中 | 7 Tab 布局 B + 布局预设 A/B/C（含 4.1 六条硬标准的**可执行判据** `LayoutCriteria`）、OKLCH 设计令牌、受控玻璃（三档/降采样 4/层数上限 3/白名单断言/`FrameMonitor` 超标自动降档/API<31 伪玻璃）、7 个功能页 | `ui/src/main/kotlin/.../ui/glass/Glass.kt`、`ui/nav/LayoutPreset.kt`、`core/common/.../color/DesignTokens.kt`、`Oklch.kt`；`tests/ui_assets_check.sh` **PASS=8**；`tests/ui_parity_check.sh` **PASS=27 PEND=6** | 玻璃性能与 UI 运行时回归需真机；本轮已补 `ui/src/test/.../GlassPolicyTest.kt` 让策略层可在 JVM 验证 |
| **M3 内核打通** | 进行中 | Go 守护进程与 UDS 通信已可编译可测；内核宿主**未接入**（`Engine.Start()` 返回 `ErrTunnelNotImplemented`） | `src/`（`go test ./...` 全绿）、`domain/.../engine/EngineController.kt` | 接入内核宿主（sing-box 静态链接）+ `VpnService` TUN，打通最小代理 |
| **M4 节点与订阅** | 未开始 | — | — | 节点 CRUD / 分享链接 round-trip / 订阅 diff |
| **M5 规则与分流** | 未开始 | 规则引擎已可离线运行（28 用例 + 38 条语料全绿），**未接数据面** | `core/rule`、`tests/corpus/`、`tests/expect.tsv` | 把 `core:rule` 接到 `domain:engine` 数据面 |
| **M6 DNS** | 未开始 | — | — | 多上游 / DoH·DoT·DoQ / fake-ip |
| **M7 模块与脚本** | 未开始 | — | — | 模块系统 + QuickJS 脚本引擎 |
| **M8 MITM 与抓包** | 未开始 | — | — | CA 全流程 + 抓包查看器 |
| **M9 数据与统计** | 未开始 | — | — | 日志 / 连接列表 / 统计图表 |
| **M10 过滤内核** | 未开始 | — | — | AGH + uBO Lite + SY-Fusion 全语法接数据面 |
| **M11 Hook 与特权** | 未开始 | — | — | Hook 框架检测 + 系统证书 + hosts 注入 |
| **M12 防泄漏** | 未开始 | — | — | 全维度检测 + 一键自检/修复 |
| **M13 增强补全** | 未开始 | — | — | 开屏跳过 / 通知净化 / 网络封禁 / 浏览器面板 / CLI |
| **M14 打磨与发布** | 未开始 | — | — | 性能实测达标 + 18.8 逐项勾选 + Release |

```
NEXT_ACTION: M2 —— 在无真机的前提下把 M2 里"可在 JVM 验证"的部分做完并纳入门禁：确认 :ui:testDebugUnitTest 已进 tests/run_unit.sh，随后更新 README 的「功能可用性报告」与状态徽章到 M2 真实状态（消除"M1 / 视觉未按 4.11 重做"的陈旧声明）。
BLOCKERS: 无外部阻塞导致停摆。存在 3 项「待外部验证」，见 PENDING-EXTERNAL.md：
          ① 无 Android 真机（adb devices 为空）→ 13 个功能的最低可用路径无法执行
          ② 仓库为 private → update.json / module.prop 的 updateJson 与 Release 资源地址不可匿名访问
          ③ 无 GitHub Release → 自更新链路（18.5）无法端到端验证
LAST_UPDATED: 2026-10-13T02:14Z (2026-10-13 10:14 +0800)
```

---

## 本轮（2026-10-13 第 2 轮）实际做了什么

按提示词 2.10 第 6 条，证据同步落在 `docs/EVIDENCE.md`。

1. **落盘附录三个脚本**到 `tools/`，逐字节与附录一致（`.ps1` 带 UTF-8 BOM、`.sh` 无 BOM + LF），
   并对提示词文件本身跑门禁 → **退出码 0**。
2. **修 4 处真实缺陷**（见「自主决策登记」）：
   - 失效仓库 URL（owner 误用包名段 `vipsvip`）→ 已修，并给 `tools/set_owner.sh` 补上 URL 改写
   - `package.sh` 在 `zip` 缺失时**静默产出不完整产物且退出码 0** → 已修（便携 zip 回退 + 缺件即退出 1）
   - `update.json` 的 `zipUrl`/`changelog`/资产名全部对不上实际产物 → 已修
   - Go module path 指向不存在的仓库 → 已对齐真实仓库路径
3. **让模块 ZIP 真的能构建**：此前 `zip: command not found` 导致交付主体缺失，现在
   `dist/syfuse-0.1.0.zip`（23 项）可产出并通过 SHA-256 自校验。
4. **补 `GlassPolicyTest`**：`Glass.kt` 的文件注释声称该测试类存在，实际不存在（无法验证的声明）→ 补真实用例。
5. **补本文件与 `PENDING-EXTERNAL.md`、`docs/EVIDENCE.md`**：此前三者都缺失，续跑协议不成立。

## 怎么跑（不依赖设备）

```bash
# 工具链（已 bootstrap 到 ~/.workbuddy-ai/toolchain）
export JAVA_HOME="$HOME/.workbuddy-ai/toolchain/jdk-21.0.12.1+1"
export PATH="$JAVA_HOME/bin:$HOME/.workbuddy-ai/toolchain/go/bin:$PATH"
export ANDROID_HOME="$HOME/.workbuddy-ai/toolchain/android-sdk"
export GRADLE_USER_HOME="$HOME/.workbuddy-ai/toolchain/gradle-home"

bash tests/run_unit.sh            # JVM 单测（含 :ui:testDebugUnitTest）
bash tests/run_module_checks.sh   # 模块静态校验
bash tests/run_restore_check.sh   # 沙箱内安装→卸载→残留比对
python tools/scan_secrets.py      # 密钥/令牌扫描
bash package.sh                   # 产出 dist/（APK × 3 ABI + 模块 ZIP）
bash tools/verify_deliverable.sh README.md 300   # 交付门禁，退出码必须为 0
```

需要设备/Release 的检查**无设备时 `exit 3 = SKIP`**，`SKIP` 不是通过（`tests/env.sh` 的硬约束）。

## 退出码约定

| 码 | 含义 |
|---|---|
| 0 | 通过 |
| 1 | 有失败项（门禁/测试） |
| 2 | 环境缺失（缺 JDK/Gradle/SDK）——**不得当作通过** |
| 3 | SKIP（缺设备等外部依赖）——**不得当作通过** |
