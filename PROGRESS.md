# PROGRESS.md — 跨轮续跑的**唯一进度真相**

> 本文件自包含：新会话只读它就能接着干，不依赖任何聊天记录（提示词 2.10）。
> 续跑入口固定为：**读 `PROGRESS.md`，从 `NEXT_ACTION` 继续。**

```
MILESTONES
```

| 里程碑 | 状态 | 本轮产出 | 证据位置 | 下一轮第一个动作 |
|---|---|---|---|---|
| **M0 方案** | 已完成 | 架构文档、模块/App 分工、数据模型、`syfuse.conf` 键表、覆盖度对照表初稿、内核选型与许可核实、风险清单 | `README.md`：`## 架构`、`### 内核选型与许可核实`、`## 数据模型`、`## syfuse.conf 键表`、`## 功能覆盖度对照表`、`## 风险清单（M0）`、`## 【待核实】项` | 无（M0 随 M1 交付，提示词 13 允许） |
| **M1 模块骨架** | 待外部验证 | 模块骨架（`module.prop`/`customize.sh`/`post-fs-data.sh`/`service.sh`/`action.sh`/`uninstall.sh`）、`action.sh` **主菜单 11 项 + 2 个子菜单**（守护进程控制 3 项、备份恢复 2 项；含各项"0) 退出"共 19 行菜单项）、Go 守护进程骨架、UDS 控制通道、还原清单机制、卸载残留检查 | `tests/run_module_checks.sh` **PASS=101 FAIL=0 PEND=1**；`tests/run_restore_check.sh` **PASS=17 FAIL=0**；`dist/syfuse-0.1.0.zip`（23 项，含 `bin/syfused-{arm64,arm,amd64}` 与 `apk/syfuse.apk`） | 接真机后跑 `tests/run_restore_check.sh` 真机分支与 `tests/run_e2e.sh` |
| **M2 App 骨架与视觉** | JVM 可验证部分已完成；真机部分待外部验证 | 7 Tab 布局 B + 布局预设 A/B/C（含 4.1 六条硬标准的**可执行判据** `LayoutCriteria`）、OKLCH 设计令牌、受控玻璃（三档/降采样 4/层数上限 3/白名单断言/`FrameMonitor` 超标自动降档/API<31 伪玻璃）、7 个功能页；**策略层 26 条 JVM 用例**已进门禁 | `ui/src/main/kotlin/.../ui/glass/Glass.kt`、`ui/nav/LayoutPreset.kt`、`core/common/.../color/DesignTokens.kt`、`Oklch.kt`；`ui/src/test/.../GlassPolicyTest.kt`；`tests/ui_assets_check.sh` **PASS=8**；`tests/ui_parity_check.sh` **PASS=27 PEND=6** | 玻璃帧耗时与 UI 运行时回归需真机（`tests/ui_glass_perf.sh` / `ui_runtime_check.sh` / `ui_screenshot.sh`） |
| **M3 内核打通** | 进行中 | Go 守护进程与 UDS 通信已可编译可测；**跨语言协议契约测试已建立**（14 条，读 Go 源码核对 Kotlin 侧不漂移）；内核宿主**未接入**（`Engine.Start()` 返回 `ErrTunnelNotImplemented`） | `src/`（`go test ./...` **88 用例 / 86 PASS / 2 SKIP / 0 FAIL**）、`domain/.../engine/EngineController.kt`、`daemon/src/test/.../DaemonProtocolContractTest.kt` | 接入内核宿主（sing-box 静态链接）+ `VpnService` TUN，打通最小代理 |
| **M4 节点与订阅** | 未开始 | — | — | 节点 CRUD / 分享链接 round-trip / 订阅 diff |
| **M5 规则与分流** | 未开始 | 规则引擎已可离线运行（28 用例 + 38 条语料全绿），**未接数据面**；性能：100 万条编译 436~470ms、单次匹配 p99 中位数 0.6962~0.8033ms（预算 <1ms，达标） | `core/rule`、`tests/corpus/`、`tests/expect.tsv`、`tests/perf_results.txt` | 把 `core:rule` 接到 `domain:engine` 数据面；索引化前先补语料 |
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
NEXT_ACTION: M3 —— 接入内核宿主，把 DataPlane 从 NotLinkedDataPlane 换掉。顺序（每步都可在沙箱验证，不要跳到真机）：
             ① Go 侧：把 core:rule 的规则语义移植进 src/syfuse（或经 UDS 暴露 rules.test），让内核宿主能拿到真实规则集；
             ② Kotlin 侧：实现一个走 DaemonClient 的 UdsDataPlane，在 :engine 里替换绑定；
             ③ 用 JVM 单测 + 本机真实 syfused 进程（GOOS=windows 构建）跑通「App 调用 → UDS → 守护进程应答」的往返；
             ④ 只有 ③ 绿了才动 VpnService TUN（那一步必须真机）。
             另：M10 的索引化开工前必须先补 tests/corpus 语料（现有 38 条期望不足以证明索引化后语义不变）。
BLOCKERS: 无外部阻塞导致停摆。存在 4 项「待外部验证」，见 PENDING-EXTERNAL.md：
          ① 无 Android 真机（adb devices 为空）→ 13 个功能的最低可用路径、15 项性能预算中的 11 项无法执行
          ② 仓库为 private → update.json / module.prop 的 updateJson 与 Release 资源地址不可匿名访问
          ③ 无 GitHub Release → 自更新链路（18.5）无法端到端验证
          ④ 无 gomobile + NDK → 内核 AAR 无法构建（`src/tools/build_aar.sh` 实测 exit 2）
LAST_UPDATED: 2026-10-13T03:10Z (2026-10-13 11:10 +0800)
```

---

## 本轮（2026-10-13 第 3 轮）实际做了什么

按提示词 2.10 第 6 条，证据同步落在 `docs/EVIDENCE.md`。

1. **修掉一个错误的平台跳过**（`src/internal/ipc/ipc_test.go`）。原代码按 `runtime.GOOS == "windows"`
   跳过全部 UDS 断言，注释称"Windows 不支持 AF_UNIX，实测 bind 报 invalid argument"。
   **实测这句话不成立**：本机 AF_UNIX 的 listen / dial / 回环读写全部可用。
   改成**能力探测**（探测失败才跳过，并把探测到的具体错误写进跳过信息）。
   效果：Windows 上真实执行的用例从 12 条涨到 14 条，只有 2 条"环境不实现 POSIX 0600"的断言才跳过。

2. **发现原来的"遗留 socket 清理"测试是空转的**。它用 `first.ln.Close()` 制造残留，
   而 Go 的 `UnixListener.Close()` 本身就会 unlink 文件 —— 所以文件根本没留下，断言永远通过。
   改为 fork 一个只 bind、不 Close 就 `os.Exit` 的辅助进程，精确复现崩溃残留。
   变异测试确认有效：移除 `Listen()` 里的清理逻辑 → 该用例失败（报出预期的 bind 错误）；还原 → 通过。

3. **建立跨语言协议契约测试**（`daemon/src/test/.../DaemonProtocolContractTest.kt`，14 条）。
   `DaemonProtocol.kt` 与 `src/internal/ipc/ipc.go` 是同一份协议的两侧手工实现，
   文件头写着"必须同步修改"，但此前**没有任何东西在强制这件事** —— 漂移只有真机才暴露。
   现在把 Go 源码当唯一事实来源，逐项核对协议版本 / JSON 字段名（用 Kotlin 真实序列化器产出）/
   错误码集合 / 已实现与待实现方法表 / 路径常量。变异测试确认有效（注入 3 处漂移 → 精确命中 3 条断言）。
   测试本身也抓到过我自己的一个正则 bug（`M3/M4` 的第二段 M 漏匹配），已修并加了独立计数断言防漏抓。

4. **修掉性能基准的方法学缺陷**：提示词第 11 部分要求"多轮取中位数"，而基准只跑一轮。
   单轮测到的**永远是暖机阶段**：同一机器同一代码单轮 p99 = 0.9651 / 1.3183ms（相差 36%），
   据此先后写下过"达标余量 3.5%"与"超出 1.9 倍"两个互相矛盾的结论，**两个都错**。
   改为「1 轮预热丢弃 + 5 轮测量取中位数」，两次独立运行中位数 p99 = 0.6962 / 0.8033ms
   → 达标，余量 19.7%~30.4%。同时修正了 `perf_results.txt` 里把根因归给"未命中 Trie 的一半"的说法：
   p50 已达 0.60~0.68ms，说明线性扫描压住的是整个分布。

5. **修掉文档缺陷**：
   - `CHANGELOG.md` 自造 `0.5ms` 预算（比提示词原文更严），把达标写成未达标 → 按原文 `<1ms` 更正
   - `CHANGELOG.md` 引用 README「性能预算与实测」章节，**该章节不存在** → 已补真实章节（第 11 部分 15 项预算逐项对照）
   - `PROGRESS.md` 写 `action.sh` "19 项菜单"、README 写"11 项菜单"，两者看似矛盾 → 实际都不算错：
     门禁统计的是**全部菜单行**（主菜单 11 + 守护进程子菜单 3 + 备份子菜单 2 = 19），
     README 指的是**主菜单**。已把两处都改成明确写法，消除歧义（先误改成"11 项"，
     发现与门禁输出 19 冲突后回查门禁的统计方式，才定位到是子菜单的差异）
   - README「功能可用性报告」的 UDS 行仍写"Windows 上跳过 UDS 断言"（已不成立）→ 按实测更正
   - README 首页行与设置行仍写"视觉未重做"，但设计系统（269 行 OKLCH 令牌 + 469 行受控玻璃）
     与 7 个页面均已接入 → 更正为"已接入 4.11 设计系统，未做真机回归"

6. **核实 CHANGELOG 里两条"未交付"声明属实**（不许把没做的写成做了）：
   Manifest 确实未声明 `BIND_VPN_SERVICE`；`src/tools/build_aar.sh` 确实存在且缺 gomobile 时 `exit 2`。

## 第 2 轮（2026-10-13）实际做了什么

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

bash tests/run_unit.sh            # JVM 单测（:core:rule / :domain / :data / :ui / :daemon 五个模块）
bash tests/run_kernel.sh          # Go：gofmt / vet / build / 88 用例 / 零依赖断言
bash tests/perf_rule_engine.sh    # 规则引擎基准（1 轮预热丢弃 + 5 轮测量，约 8 分钟）
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
