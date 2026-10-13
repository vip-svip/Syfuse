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
| **M3 内核打通** | 进行中 | ① **规则语义移植到 Go**（`src/syfuse/rule/`，与 `core:rule` 共用 `tests/corpus` + `tests/expect.tsv` 作跨语言预言机）；② **`rules.test/syntax/stats/build` 四个控制方法真实现**（不再占位）；③ **`UdsDataPlane` 替换 `NotLinkedDataPlane`**，界面走真实 UDS 通道；④ **协议层拆成纯 JVM 的 `daemon-protocol`**，用本机真实 `syfused` 进程跑通「客户端 → UDS → 守护进程」往返（13 条）；⑤ 内核宿主仍缺：`proxies.start` 如实返回"尚未实现（M3）"，**未接入内核宿主、未做 VpnService TUN** | `src/syfuse/rule/`（Go `go test` **127 用例 / 0 失败**）、`daemon-protocol/src/test/.../DaemonWireRoundTripTest.kt`（13 条真实往返）、`engine/.../UdsDataPlane.kt`、`tests/run_module_checks.sh` R2/R2b 段 | 见 `TASKS` 段的 **T1**（内核宿主 AAR 构建前置：gomobile + NDK） |
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
TASKS

> 提示词 2.11-A：单元 = 1～3 个文件 + 1 条验收命令，目标 30～60 分钟。
> 状态只能取：未开始 / 进行中 / 已完成 / 受阻。
> **接口冻结声明**：`DataPlane`（`domain/engine/EngineController.kt`）、
> `DaemonTransport` / `DaemonClient`（`daemon-protocol`）、
> `src/internal/ipc` 的请求/应答字段名 **已冻结**；以下单元的并行以这三个为前提。
> 同一文件不并行改 —— T1/T2 碰不到同一文件，T4/T5/T6 各自独占页面文件。

| 单元 | 范围(文件) | 依赖 | 验收命令 | 预计 | 状态 | 实测耗时 |
|---|---|---|---|---|---|---|
| **T0** 门禁脚本与提示词附录重同步（含附录 B 第⑥组 `$PRUNE` 勘误） | `tools/verify_deliverable.sh`、`tools/verify_deliverable.ps1` | 无 | `bash tools/verify_deliverable.sh README.md 300` 且第⑥组**真的列出了脚本**、`exit 0` | 30 min | 未开始 | — |
| **T1** 内核宿主 AAR 构建链路（gomobile + NDK 前置检查，产出 `kernel/out/*.aar`） | `src/tools/build_aar.sh`、`kernel/` | 无 | `bash src/tools/build_aar.sh` 产出 AAR 且 `unzip -l` 含 `libsyfuse.so` | 60 min | 受阻（无 gomobile + NDK） | — |
| **T2** `proxies.start/stop/reload/select/urlTest` 在 Go 侧真实现（先出最小可跑隧道） | `src/cmd/syfused/proxies.go`、`src/cmd/syfused/main.go` | T1 | `syfused proxies start --profile p1` 起进程并 `status` 报 running | 60 min | 未开始 | — |
| **T3** `UdsDataPlane` 接真实 `proxies.*`（去掉"未实现"路径）并补往返断言 | `engine/.../UdsDataPlane.kt`、`daemon-protocol/src/test/.../DaemonWireRoundTripTest.kt` | T2 | `:daemon-protocol:test` 全绿且新增隧道往返用例 | 45 min | 未开始 | — |
| **T4** M4：节点模型 + 分享链接 round-trip（ss/ssr/vmess/vless/trojan/hysteria2/http/socks5） | `domain/model/Models.kt`、`domain/node/ShareLink.kt`、`domain/src/test/.../ShareLinkTest.kt` | 无 | `:domain:test --tests '*ShareLink*'` 全绿（8 协议各 1 条往返） | 60 min | 未开始 | — |
| **T5** M4：订阅解析与 diff（`base64`/`clash yaml` 两种形态，diff 出新增/删除/改动） | `domain/node/Subscription.kt`、`domain/src/test/.../SubscriptionTest.kt` | T4 | `:domain:test --tests '*Subscription*'` 全绿 | 60 min | 未开始 | — |
| **T6** M5：`tests/corpus` 语料扩容（38 → ≥120 条期望，覆盖 `$denyallow`/`$ctag`/`$removeparam`/`$redirect`/正则） | `tests/corpus/*.txt`、`tests/expect.tsv` | 无 | `:core:rule:test` + `go test ./syfuse/rule/` 两侧同时绿且条数一致 | 60 min | 未开始 | — |

**并行批次**（2.11-B，常驻 3～6 个，同文件不并行）：
- 批次 1（无依赖，可并行 4 个）：**T0 / T1 / T4 / T6**
- 批次 2：**T2**（等 T1）→ **T3**（等 T2）→ **T5**（等 T4）

```
NEXT_ACTION: T0 —— 门禁脚本与提示词附录重同步。按 2.9 把 tools/verify_deliverable.sh / .ps1
             逐字节落盘为附录 A/B 的**当前版本**（附录 A 238 行、附录 B 228 行；本地分别是
             226 / 226 行），补上缺的两条需求行 `37 提速与并行执行规范`、`38 安全提速红线`
             与附录 A 里的 `Get-HeadBytes`（按 $PSVersionTable.PSVersion.Major 分支，
             5.1→-Encoding Byte / 7+→-AsByteStream，并包 try/catch）；
             `.ps1` 带 UTF-8 BOM、`.sh` 无 BOM + LF。
             然后按 2.3 对附录 B 第⑥组的缺陷提勘误并修掉：`$PRUNE` 变量未加引号，
             被 bash 通配展开成 58 个 `app/build/...` 路径，find 报
             "paths must precede expression" 后**静默返回空**，该组因此从未真正执行过
             （只报 WARN 跳过，退出码仍 0）。修法：pattern 写成 `-path './*/.git/*'` 形式
             或对每个 pattern 单独引号化；并把"找不到脚本可检查"从 WARN 改成 **FAIL**，
             避免再次静默失效。修完重跑两版门禁，把**原始输出整段**贴进 docs/EVIDENCE.md。
             完成后直接进批次 1 的 T1 / T4 / T6。
BLOCKERS: ① 无 Android 真机 → M3 的 VpnService TUN 与全部真机项待外部验证（PENDING-EXTERNAL.md）
          ② 无 gomobile + NDK → T1（内核宿主 AAR）受阻，`src/tools/build_aar.sh` 实测 exit 2
          ③ 仓库 private + 无 Release → 自更新链路（18.5）与 update.json 匿名可达性无法验证
          ④ 【本轮新发现，已停手待处理】tools/verify_deliverable.* 与提示词附录不一致 4 处，
             其中 3 处高危（详见 T0）。**里程碑级门禁在此之前不具备完整效力**：
             第⑤组看不到 37/38 两条需求、第⑥组是死检查。
ROUND_TIME: 第 4 轮 ≈ 3h10m（含 2 次 HTTP 429 中断重跑）；本段 Step 0 测量另计 3m31s
UNIT: 第 4 轮完成 M3 的 ①②③④ 四步；Step 0 已做完（测量 + 门禁比对），按指令在发现差异处停手
LAST_UPDATED: 2026-10-13T04:35Z (2026-10-13 12:35 +0800)
```

---

## 第 4 轮（2026-10-13）实际做了什么

按提示词 2.10 第 6 条，证据同步落在 `docs/EVIDENCE.md`。

1. **把 `core:rule` 的规则语义移植进 Go**（`src/syfuse/rule/`，约 7 个文件）。
   两侧共用同一份 `tests/corpus/*.txt` + `tests/expect.tsv` 作**跨语言预言机** ——
   不是"两边各写各的测试"，而是同一份期望表被两个语言各自消费一遍。
   移植过程**抓出 3 个 Kotlin 侧的真实缺陷**（见下），这正是建立这个预言机的目的。

2. **发现并修掉 3 个既有缺陷**（都是"断言存在但永不命中"这一类）：
   - `PatternCompiler.wildcardBody` 把结尾锚点写成 `"\\$"`（正则字面美元符），
     导致 `|https://x/y|` 这一整族规则**永远不可能命中**。两侧同步改为 `'$'`。
   - `#` 开头的注释行若含 `##`，会被误判为元素隐藏规则（语料第 70 行），
     元素规则数因此虚报为 7（实际 6）。两侧同步加 `startsWithMarker` 前置判定。
   - 行数统计与 `wc -l` 不一致：Kotlin `lineSequence()` 会为"以 `\n` 结尾的文件"
     多留一个空段（71 行语料 → 72）。以 `wc -l` 为基准，两侧统一为"恰好去掉一个尾部空段"。

3. **`rules.test` / `rules.syntax` / `rules.stats` / `rules.build` 四个控制方法真实现**，
   不再是返回"未实现（M5/M10）"的占位处理器。CLI 同步接上
   （`syfused rules stats|test|syntax|build|verify`）。

4. **`UdsDataPlane` 替换 `NotLinkedDataPlane`**：界面走真实 UDS 控制通道。
   关键设计：通道是通的（`available = true`），但 `proxies.start` 还没实现，
   于是它把守护进程回的"尚未实现（计划里程碑 M3）"**原样带上来**，
   **绝不**让界面显示"已连接"。`tests/run_module_checks.sh` 的 R2 段用两条断言守着
   （绑定必须是 `UdsDataPlane`；`proxies.start` 必须仍在待实现表里），缺一条都可能谎报。

5. **把协议层拆成纯 JVM 的 `daemon-protocol` 模块**（这是实测倒逼的，不是设计洁癖）。
   Android library 的单元测试源码集是对着 `android.jar` 编译的，而 `android.jar` 里
   **没有** `java.net.UnixDomainSocketAddress`（实测 android-35：无此类；
   `android/net/LocalSocket.class` 有）。后果是"App → UDS → 守护进程"这条往返
   在沙箱里**连编译都过不去**，只能等真机。拆开之后，测试对着真正的 JDK 编译，
   注入基于 AF_UNIX 的传输（`JvmUnixSocketTransport`，仅 test 源集），
   用**本机真实编译出的 `syfused` 进程**跑通 13 条真实往返
   （status / version / rules 决策与优先级 / 链路 / url 推导 host / 带行号诊断 /
   错误令牌 / 待实现方法 / 未知方法 / 非法资源类型）。
   真机专属的 `LocalSocketTransport` 留在 `:daemon`，两边各自被覆盖。

6. **跑通往返的过程里又抓到 3 个缺陷**（都是"没真跑过就不知道"这一类）：
   - `JvmUnixSocketTransport` **从未重新 register 兴趣集**：只注册了 `OP_CONNECT`，
     之后 select 永远不会报"可写"，第一次 write 就一路等到 deadline，
     报出"写入 超时" —— 一个看起来像"守护进程没应答"、实际是"我根本没在等写"的假象。
   - **Kotlin 用例名里不能出现双引号**：编译器用反引号里的名字生成 `.class` 文件名，
     而 Windows 文件名不允许 `" * ? < > | :`，于是抛
     `InvalidPathException: Illegal char <">` —— 一个完全看不出与"命名"有关的
     内部编译器错误。Linux/macOS 上完全正常，只在 Windows 上炸。
     已加门禁断言（`tests/run_module_checks.sh`）防复发。
   - `rules_test` 往返测试里内联规则集**定义了却没用**（变量 `inline` 未被引用），
     等于"用真实规则引擎裁决"这句注释是假的。已改为真的传入并断言具体决策。

7. **修掉 `tests/run_unit.sh` 的两处统计缺陷**：
   - 汇总 glob 只匹配 `root/*/build/test-results`，**漏掉 `core/rule`、`core/common`**
     （它们在 `core/` 下一层），于是"失败=0"是绿的但根本没统计到那两个模块。
     改为递归 glob 并打印模块清单。
   - **陈旧报告被计入**：`:daemon` 的契约测试迁到 `:daemon-protocol` 之后，
     旧 XML 仍躺在磁盘上，模块数虚报为 6、用例数虚报为 140（实际 5 / 126）。
     改为跑之前先清理全部 `*/build/test-results`。
   - 跳过项现在**点名**（只报一个数字会让"有 1 个跳过"变成没人追问的常态）。

8. **变异测试确认新断言有效**（区分"断言有效"与"断言空转"）：
   - 协议版本 1→2 → 13 条往返全部红；还原 → 全绿
   - Go 侧 `PriorityNormalBlock` 的名字改成 `normal-block-MUTANT` →
     **只有**语义一致性那一条红（精确命中）；还原 → 全绿

## 第 3 轮（2026-10-13）实际做了什么

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

7. **修掉 `tools/verify_remote.py` 的假失败**。推送后它报 `关键文件 22/23` + `[MISSING] tools/set_owner.sh`，
   但该文件确实在远端（API 返回 200，字节数与本地一致）。根因两条：① 对 23 个文件逐个发请求，易撞限流；
   ② **把任何非 200 都当成"文件缺失"**，于是 403/429/5xx/网络错误全被报成 MISSING。
   验证脚本报假失败比不报更糟——它会把真问题淹没掉。
   改为一次 `git/trees` 取全量路径（23 次请求 → 1 次），并严格区分
   **200=存在 / 404=确认缺失(FAIL) / 其他=无法确定(INCONCLUSIVE, exit 3)**；
   瞬时错误退避重试，且确认 API 不可用后停止逐条硬试（原实现 >120s 被超时杀掉，现 8.5s）。
   两条失败路径都做了变异测试。

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

bash tests/run_unit.sh            # JVM 单测（:core:rule / :domain / :daemon-protocol / :data / :ui / :engine 六模块）
bash tests/run_kernel.sh          # Go：gofmt / vet / build / 127 用例 / 零依赖断言
bash tests/perf_rule_engine.sh    # 规则引擎基准（1 轮预热丢弃 + 5 轮测量，约 8 分钟）
bash tests/run_module_checks.sh   # 模块静态校验
bash tests/run_restore_check.sh   # 沙箱内安装→卸载→残留比对
python tools/scan_secrets.py      # 密钥/令牌扫描
bash package.sh                   # 产出 dist/（APK × 3 ABI + 模块 ZIP）
bash tools/verify_deliverable.sh README.md 300   # 交付门禁，退出码必须为 0
```

需要设备/Release 的检查**无设备时 `exit 3 = SKIP`**，`SKIP` 不是通过（`tests/env.sh` 的硬约束）。

## 耗时基线（2.11-D / 2.11-I 第 9 条：先量再优化）

采集环境：Windows 10 / JDK 21.0.12.1+1 / Go 1.23.4 / Gradle 8.11.1 / Android SDK 35，
**Gradle daemon 与 build cache 已预热**（冷启动会显著更久）。

| 项目 | 实测 |
|---|---|
| 全量构建 `:app:assembleDebug` | 40 s |
| `tests/run_unit.sh`（5 模块 / 126 用例 / 0 失败 / 1 跳过） | 98 s |
| `tests/run_kernel.sh`（Go，127 用例） | 19 s |
| `tests/run_module_checks.sh`（PASS=109 FAIL=0 PEND=1） | 41 s |
| `tools/scan_secrets.py` | 1 s |
| `tools/verify_deliverable.sh README.md 300` | 10 s |
| **门禁合计** | **≈169 s（2.8 min）** |

结论：门禁总耗时不构成瓶颈（2.8 min），**不需要为了速度换环境或上 WSL2**
（2.11-D 明确"先量一次再决定"）。真正的杠杆在 2.11-A/B 的**粒度与并行**。

> ⚠️ 上表是在 **T0 之前**采集的。当时门禁第⑤组看不到两条新需求、第⑥组是死检查，
> 所以这 169 s 里有约 10 s 是"跑了但没真检查"。T0 完成后需重采一次。

## 退出码约定

| 码 | 含义 |
|---|---|
| 0 | 通过 |
| 1 | 有失败项（门禁/测试） |
| 2 | 环境缺失（缺 JDK/Gradle/SDK）——**不得当作通过** |
| 3 | SKIP（缺设备等外部依赖）——**不得当作通过** |
