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
| **T0** 门禁脚本与提示词附录重同步（含附录 B 第⑥组 `$PRUNE` 勘误） | `tools/verify_deliverable.sh`、`tools/verify_deliverable.ps1`、`tools/extract_appendix.py`、`docs/errata/`、`tests/run_appendix_sync.sh` | 无 | `bash tools/verify_deliverable.sh README.md 300` 且第⑥组**真的列出了脚本**、`exit 0` | 30 min | **已完成** | **≈95 min（超预计 3.2 倍，见「第 5 轮」第 6 条）** |
| **T7** 门禁第⑥组去子进程化（Windows 上 620 ms/文件 → 目标 <20 ms/文件） | `tools/verify_deliverable.sh` | 无 | ① 新旧 BOM/CRLF 判定**等价性矩阵**（BOM×CRLF×文件长度 12 组）全通过；② `bash tools/verify_deliverable.sh README.md 300` 退出码 0 且耗时 < 20 s；③ 更新 `docs/errata/` 清单 | 30 min | **已完成** | **≈35 min**（39 s → **13.3 s**，检查对象与断言条数不变） |
| **T1** 内核宿主 AAR 构建链路（gomobile + NDK 前置检查，产出 `kernel/out/*.aar`） | `src/tools/build_aar.sh`、`kernel/` | 无 | `bash src/tools/build_aar.sh` 产出 AAR 且 `unzip -l` 含 `libsyfuse.so` | 60 min | 受阻（无 gomobile + NDK） | — |
| **T2** `proxies.start/stop/reload/select/urlTest` 在 Go 侧真实现（先出最小可跑隧道） | `src/cmd/syfused/proxies.go`、`src/cmd/syfused/main.go` | T1 | `syfused proxies start --profile p1` 起进程并 `status` 报 running | 60 min | 未开始 | — |
| **T3** `UdsDataPlane` 接真实 `proxies.*`（去掉"未实现"路径）并补往返断言 | `engine/.../UdsDataPlane.kt`、`daemon-protocol/src/test/.../DaemonWireRoundTripTest.kt` | T2 | `:daemon-protocol:test` 全绿且新增隧道往返用例 | 45 min | 未开始 | — |
| **T4** M4：节点模型 + 分享链接 round-trip（ss/ssr/vmess/vless/trojan/hysteria2/http/socks5） | `domain/node/ShareLink.kt`、`domain/src/test/.../node/ShareLinkTest.kt` | 无 | `source tests/env.sh && syfuse_gradle :domain:test --tests '*ShareLink*'` 全绿（8 协议各 1 条往返） | 60 min | **未开始**（子代理尝试失败，见下） | — |
| **T4a** 分享链接：`ss` / `ssr` / `vmess` 三协议 + 往返 | 同上（同一文件，故 T4a→T4b→T4c **串行**） | 无 | `:domain:test --tests '*ShareLink*'` 中这三个协议的用例全绿 | 30 min | 未开始 | — |
| **T4b** 分享链接：`vless` / `trojan` / `hysteria2` 三协议 + 往返 | 同上 | T4a | 同上 | 30 min | 未开始 | — |
| **T4c** 分享链接：`http`(`https`) / `socks5` + 全 8 协议往返矩阵 + 与 Go 侧行为对齐核对 | 同上 | T4b | 同上，且 8 协议往返矩阵全绿 | 30 min | 未开始 | — |
| **T5** M4：订阅解析与 diff（`base64`/`clash yaml` 两种形态，diff 出新增/删除/改动） | `domain/node/Subscription.kt`、`domain/src/test/.../SubscriptionTest.kt` | T4 | `:domain:test --tests '*Subscription*'` 全绿 | 60 min | 未开始 | — |
| **T6** M5：`tests/corpus` 语料扩容（38 → ≥120 条期望，覆盖 `$denyallow`/`$ctag`/`$removeparam`/`$redirect`/正则） | `tests/corpus/*.txt`、`tests/expect.tsv` | 无 | `:core:rule:test` + `go test ./syfuse/rule/` 两侧同时绿且条数一致 | 60 min | 未开始 | — |

**并行批次**（2.11-B，常驻 3～6 个，同文件不并行）：
- 批次 1（无依赖）：**T0 ✅ / T7 ✅**；**T4a→T4b→T4c**（串行，同一文件）；**T6**；**T1** 受阻（缺 gomobile + NDK）
- 批次 2：**T2**（等 T1）→ **T3**（等 T2）；**T5**（等 T4c）
- 文件边界（已冻结）：T4a/b/c 只动 `domain/node/ShareLink.kt` 与其测试；
  T6 只动 `tests/corpus/*` + `tests/expect.tsv`；两者**无交集**，可与 T4a 并行。
- **并行纪律**：禁止并行跑 `tests/run_unit.sh`（它会先删 `*/build/test-results`，会互相破坏），
  改用定向的 `syfuse_gradle :<module>:test --tests '...'`；集成级 `run_unit.sh` 由主代理串行跑一次收口。
- **本轮的教训（见「第 5 轮」第 11 条）**：子代理**没有交付任何文件**。
  下一轮改策略：由主代理自己做，并把 T4 拆成 T4a/b/c 三个 30 分钟的单元。

```
NEXT_ACTION: T0 / T7 已完成并推送（`636983e`）。下一轮**由主代理自己做**批次 1 剩余单元，
             **不再用子代理做代码单元**（本轮实测子代理零产出，见「第 5 轮」第 11 条）。
             顺序（同文件，必须串行）：**T4a → T4b → T4c**，其间可并行做 **T6**（不同文件）。
             - **T4a**：新建 `domain/src/main/kotlin/.../domain/node/ShareLink.kt`
               （先只做 `ss` / `ssr` / `vmess`）+ `domain/src/test/.../domain/node/ShareLinkTest.kt`。
               **语义真相是 Go 侧 `src/syfuse/node.go`**（已有 8 个 parse 函数 + 28 个用例），
               Kotlin 实现必须与它行为一致（SIP002 base64 userinfo、legacy 全 base64、
               percent-encoding、vmess 端口是字符串或数字、URL-safe base64、默认节点名等）。
               验收：`source tests/env.sh && syfuse_gradle :domain:test --tests '*ShareLink*'`。
             - **T4b**：追加 `vless`（含 REALITY）/ `trojan`（含 security=none）/ `hysteria2`（含 `hy2` 别名）。
             - **T4c**：追加 `http`(`https`) / `socks5`，补全 8 协议往返矩阵，并与 Go 侧行为逐条核对。
             - **T6**：`tests/corpus/*` + `tests/expect.tsv` 从 38 条期望扩到 ≥120 条。
               验收：`bash tests/run_kernel.sh` + `syfuse_gradle :core:rule:test` 两侧同时绿且条数一致。
             收口：主代理串行跑 `bash tests/run_unit.sh` 取集成证据 → 全量门禁 → 提交 → 推送。
             之后进批次 2 的 **T5**（订阅解析与 diff，依赖 T4c）；T2/T3 仍等 T1（缺 gomobile + NDK）。
BLOCKERS: ① 无 Android 真机 → M3 的 VpnService TUN 与全部真机项待外部验证（PENDING-EXTERNAL.md）
          ② 无 gomobile + NDK → T1（内核宿主 AAR）受阻，`src/tools/build_aar.sh` 实测 exit 2
             （已在 PENDING-EXTERNAL.md 第 5 节写清"缺什么 / 怎么补 / 补齐后跑哪条命令 / 期望结果"）
          ③ 仓库 private + 无 Release → 自更新链路（18.5）与 update.json 匿名可达性无法验证
          ④ 【T0 已解决】门禁脚本与提示词附录不一致 —— 已逐字节重同步并修掉附录自身 4 处缺陷
             （E1–E4），两版门禁 exit 0、需求 38/38。**遗留一条已知局限**：门禁第⑦组
             依赖随母版分发的 tools/sync_appendix.ps1，本仓库没有它 → 那一组只 WARN 跳过。
             已用 tests/run_appendix_sync.sh（含 3 次变异/护栏自证）做补偿控制并挂进 CI。
          ⑤ 【T7 已解决】第⑥组在 Windows 上 54 s → 改成 bash 内建读取后 **13.3 s**，
             检查对象与断言条数不变（26 个 `.sh` × 2 条 + 2 个 `.ps1` × 1 条 = 54 条）。前置的 12 组等价性矩阵
             当场抓出第一版的错误（$(<file) 会吃掉末尾 CRLF），详见 EVIDENCE §14.11。
          ⑥ 【已解决】`git push` 曾因 GitHub 443 不可达失败三次；连通性恢复后已推送成功
             （`626befd..32f9fa5`，origin/main 与本地同步）。令牌用 `-c http.extraheader`
             逐命令注入，**未落盘**（未写 remote、未设 credential.helper）。
ROUND_TIME: 第 5 轮 ≈3h40m（T0 ≈95 min 超预计 3.2 倍；T7 ≈35 min 达标；
             子代理尝试 T4/T6 约 30 min 后确认零产出并终止）
UNIT: 第 5 轮交付 T0（重同步 + 勘误 E1–E4 + 补偿控制）与 T7（勘误 E5 + 12 组等价性矩阵）；
      **T4/T6 未交付**（子代理零产出，已拆成 T4a/b/c 并改为主代理自做）
LAST_UPDATED: 2026-10-13T06:20Z (2026-10-13 14:20 +0800)
```

---

## 第 5 轮（2026-10-13）实际做了什么

本轮做两件事：**T0 —— 把交付门禁拉回与提示词附录一致，并修掉附录自身的缺陷**；
以及**T7 —— 把门禁第⑥组从"死检查"修好之后暴露出来的性能问题量清并解决**。
起因是上一轮 Step 0 的比对发现门禁与附录不一致（详见 `docs/EVIDENCE.md` §14）。

1. **先提交上一轮的 M3 改动集**（`8928de6`）：提交前把四项门禁全跑一遍取原始输出，
   确认全绿才提交 —— 5 模块 / 126 用例 / 0 失败、Go 127 用例 / 0 失败、
   module-checks PASS=109 FAIL=0 PEND=1、secrets PASS。

2. **确认提示词文件又变了**：`146185 B / 2098 行` → `157194 B / 2205 行` →
   **`170504 B / 1985 行`**。第三次变化把附录从纯文本代码块改成 **base64 + 自带声明字节数**。
   教训写在 §14.1：**附录是活的，每次动手前都要重新比对**，不能沿用上一轮的差量结论。

3. **按 base64 解码逐字节落盘**，不用编辑器（编辑器会顺手改 BOM / 行尾 / 尾随换行，
   而这些正是第⑥组要检查的东西）。三份全部 `cmp` IDENTICAL，
   解码字节数与附录声明值逐个相符（12238 / 10211 / 8187）。

4. **修掉附录自身的 4 处缺陷（勘误 E1–E4）**，登记在 `docs/errata/`：
   其中 **E1 是真正的高危** —— 第⑥组的 `$PRUNE` 未加引号，被 bash 通配展开后
   `find` 报 `paths must precede expression` 并**静默返回空**，而 `2>/dev/null` 吞掉 stderr、
   `FOUND=0` 只报 WARN、退出码仍是 0。**这一组从未真正执行过**：
   修之前它说"未找到脚本可检查"，修之后它列出 28 个脚本、共 54 条断言全 PASS。

5. **给"第⑦组在本仓库失效"补上补偿控制**：第⑦组要 `tools/sync_appendix.ps1`，
   而它随母版分发、不在附录三件套里，本仓库没有 → 只 WARN 跳过。
   新增 `tests/run_appendix_sync.sh`：从附录还原 → 与 `tools/` 逐字节比对 →
   偏差必须**逐字等于** `docs/errata/` 的登记清单；配 2 个变异测试证明它不是空断言；
   已挂进 CI 的 `static` job。

6. **单元超时如实上报**：T0 预计 30 min，实际 **≈95 min（3.2 倍）**。
   超时的原因不是拖沓，而是"不一致"的**规模在动手后才暴露**：上一轮看到的是 4 处行级差异，
   实际是附录整体换代（多出整个第⑦组 + 两个版本各自的修复），
   且新增的补偿控制本身也要写、要变异测试。
   按 2.11「超过预计 2 倍就停下换策略」：**本轮就此收口**，不再顺手把性能问题一起改，
   而是把新量到的第⑥组 54 s 拆成独立单元 **T7**（有度量、有明确修法、有等价性矩阵作为前置），
   下一轮与 T4/T6 并行做。

7. **提交后才抓到一个隐形缺陷**：`git commit` 打出
   `warning: LF will be replaced by CRLF`（针对 `.ps1`）。根因是 `.gitattributes` 里
   `*.ps1 text eol=crlf` —— 入库 blob 是 LF（所以 `git diff` 干净、本机跑绿），
   但**每次 checkout 都写出 CRLF**。后果是：本机绿、任何人 clone / CI checkout 之后
   刚建立的逐字节断言必红，而门禁第⑥组**不会报**（它只查 `.ps1` 的 BOM，不查行尾）。
   已改为 `*.ps1 text eol=lf`，并用「删掉工作副本 → `git checkout` → 量字节」实测确认
   （8187 / 12300 字节，CR=0）。教训记在 `docs/EVIDENCE.md` §14.10：
   **逐字节类断言不能用"自己写出来的工作副本"当验证环境。**

8. **同步更新的文档**：`README.md`（门禁 6 组 → 7 组、勘误登记区、任务拆分与耗时基线两节，
   并把两处过时措辞对齐到提示词当前写法）、`docs/EVIDENCE.md`（§0 汇总重采、
   §2 标注为过期并指向 §14.6、§3 说明 WARN 25→32 全是自引用、**§14 整体重写**含 §14.10/§14.11）、
   `CHANGELOG.md`、`PENDING-EXTERNAL.md`（新增 gomobile + NDK 一节）、`.gitattributes`、
   `.github/workflows/ci.yml`。

9. **T7：把 E1 修好之后暴露的性能问题量清并解决**。E1 让第⑥组真的跑起来了，
   代价是 `.sh` 门禁 10 s → 39 s。先量：`find` 只要 668 ms（不是遍历的锅），
   每次迭代 ≈620 ms，成本全在 `head -c 3 | od | tr | sed` 这条 **4 进程流水线**上
   （Git Bash 创建进程 ≈155 ms/次）；而 `.ps1` 版用内建 `Get-Content` 只要 0.96 s，
   正好反证慢在进程创建。
   **替换前先做 12 组等价性矩阵**（BOM × CRLF × 长度）逐组核对新旧判定 ——
   这个矩阵**当场抓出了第一版的错误**：我原本用 `raw=$(<"$s")` 判 CRLF，
   而 Git Bash 的 `$(<file)` 会把末尾的 `\r\n` 一起吃掉（`a\r\n` → `a`），
   于是"整文件只有一行且以 CRLF 结尾"的脚本会被误判成没有 CRLF ——
   而这恰恰是最典型的坏文件。**只拿真实仓库跑一遍永远不会暴露它**（真实脚本都是多行的）。
   改成 `read` 逐行扫（只剥行尾 `\n`、保留 `\r`，bash 3.2 就有，不依赖 `mapfile`）后 12/12 一致。
   最终 39 s → **13.3 s**，而**检查对象与断言条数完全不变**（26 个 `.sh` × 2 条 + 2 个 `.ps1` × 1 条 = 54 条）。
   改完立刻被 `tests/run_appendix_sync.sh` 的登记清单拦住（`PASS=7 FAIL=1`），
   更新清单后回到 `PASS=8 FAIL=0` —— 这是该护栏第三次自证不是空断言。

10. **T4 / T6 派子代理并行执行 —— 结果：零产出，策略失败，已换策略**。
    并行前做了该做的准备：冻结文件边界（T4 只新建 `domain/node/ShareLink.kt` + 测试，
    T6 只动 `tests/corpus/*` + `tests/expect.tsv`，与 T7 无交集）、明确禁止子代理运行
    `tests/run_unit.sh`（它会先删 `*/build/test-results`，并行会互相破坏）、
    要求交回"文件哈希 + 验收命令原始输出 + 改动清单"。
    **但两个子代理最终没有交付任何文件**：`git status` 干净、`domain/node/` 不存在、
    `tests/corpus/` 仍是 1 个文件 2002 字节、`expect.tsv` 仍是 47 行、
    全仓库找不到任何 `*ShareLink*`。（只有 `.gradle/` 缓存被动过，说明它们确实跑过 Gradle。）
    按 2.11-I 红线，**如实标记为未交付，不计入任何进度**。
    换策略（2.11「单元超过预计 2 倍就停下换策略」）：
    ① 下一轮**由主代理自己做**，不再把代码单元外包；
    ② 把 T4 从 60 min 拆成 **T4a / T4b / T4c** 三个 30 min 单元（按协议分三批，同一文件故串行）；
    ③ T6 保持独立，可与 T4a 并行（不同文件）。
    这一条同时也是对"常驻 3～6 个子代理"这条提速手段的**有度量的反例**：
    在需要严格遵循仓库既有约定、且产出必须逐条对照另一个语言实现的单元上，
    子代理的返工/零产出风险高于收益。记下来，下次别再踩。

11. **本轮收口**：提交 4 个 commit 并全部推送成功
    （`8928de6` M3 改动集 / `a002e8c` T0 重同步 / `32f9fa5` .gitattributes / `636983e` T7），
    `origin/main` 与本地同步。

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
bash tests/run_appendix_sync.sh   # 附录 A/B/C ↔ tools/ 逐字节一致性（门禁第⑦组的补偿控制）
python tools/scan_secrets.py      # 密钥/令牌扫描
bash package.sh                   # 产出 dist/（APK × 3 ABI + 模块 ZIP）
bash tools/verify_deliverable.sh README.md 300   # 交付门禁，退出码必须为 0
```

`tests/run_appendix_sync.sh` 需要提示词文件：默认读 `F:/Ai/Syfuse-完美提示词.md`，
可用 `SYFUSE_PROMPT=<路径>` 覆盖；文件不存在时 `exit 3`（SKIP）并打印"缺什么 / 怎么补"。

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
| `tools/verify_deliverable.sh README.md 300`（**T0 重同步后、T7 优化后**） | **13.3 s** |
| `tools/verify_deliverable.ps1 -Path README.md -MinLines 300`（同上） | **0.96 s** |
| `tests/run_appendix_sync.sh`（新增） | 14 s |
| **门禁合计** | **≈186 s（3.1 min）** |

结论：门禁总耗时不构成瓶颈（3.1 min），**不需要为了速度换环境或上 WSL2**
（2.11-D 明确"先量一次再决定"）。真正的杠杆在 2.11-A/B 的**粒度与并行**。

**T0 前后的对比（这一栏必须看，否则会误读成"变慢了"）**：

| | T0 之前 | T0 之后 | T7 之后 |
|---|---|---|---|
| `.sh` 门禁耗时 | 10 s | 39 s | **13.3 s** |
| 第⑥组实际检查的脚本数 | **0**（`$PRUNE` 未引号 → `find` 报错 → 静默空） | **28** | **28**（不变） |
| 第⑥组断言条数 | 0 | 56 | **56**（不变） |
| 第⑤组需求条数 | 36（看不到 37/38） | **38** | **38**（不变） |

也就是说：T0 多出来的 29 s **不是新开销，是把一个从来没跑过的检查真正跑起来**；
T7 再把其中 26 s 的进程创建开销砍掉，**检查能力一点没减**（对照行"28 / 56 / 38"三行都不变）。
39 s → 13.3 s 的来源：BOM 判定去掉 4 个子进程、CRLF 判定去掉 1 个、文件名去掉 2 个
（`$(basename)` → `${s##*/}`）。`.ps1` 版一直是 0.96 s，因为它用的是内建 `Get-Content` ——
这正好反证了慢在**进程创建**（Git Bash ≈155 ms/次）而不是算法。
替换前先做了 **12 组等价性矩阵**（BOM × CRLF × 长度），它当场抓出了第一版的错误：
Git Bash 的 `$(<file)` 会把末尾 `\r\n` 一起吃掉，于是单行 CRLF 文件会被误判为无 CRLF。

## 退出码约定

| 码 | 含义 |
|---|---|
| 0 | 通过 |
| 1 | 有失败项（门禁/测试） |
| 2 | 环境缺失（缺 JDK/Gradle/SDK）——**不得当作通过** |
| 3 | SKIP（缺设备等外部依赖）——**不得当作通过** |
