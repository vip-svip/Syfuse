# PENDING-EXTERNAL.md — 待外部验证清单

> 提示词 2.10 第 5 条：外部环境缺失**不得让项目停摆**。受影响项标 `待外部验证`，
> 逐项写清「缺什么 / 怎么补 / 补齐后跑哪条命令 / 期望结果」，其余里程碑继续推进。
>
> **纪律**：`待外部验证` 不等于 `已完成`。无设备时相关脚本一律 `exit 3 = SKIP`，
> `SKIP` 不是通过（`tests/env.sh` 的硬约束）。

更新时间：2026-10-13T02:14Z

---

## 1. 缺 Android 真机（影响面最大）

**缺什么**：一台处于 `device` 状态的 Android 设备（Android 8 / 10 / 13 / 15 中至少一台；
arm64 优先），且已刷 KernelSU / Magisk / APatch 之一。

**当前证据**：

```
$ adb devices -l
List of devices attached
（空）
```

**怎么补**：USB 连接设备并开启 USB 调试；或 `adb connect <ip>:5555` 连接无线调试设备；
或启动模拟器（模拟器只能作补充，真机项不得用模拟器替代，见 12.2 第 2 条）。

**补齐后跑**：

```bash
bash tests/run_e2e.sh              # 端到端：真起隧道、真发请求
bash tests/run_feature_matrix.sh   # 13 个功能的最低可用路径
bash tests/run_leak_check.sh       # 7 个维度的实际泄漏检测
bash tests/run_hook_check.sh       # Hook 装载 / 命中 / 卸载
bash tests/run_restore_check.sh    # 真机安装 → 卸载 → 系统状态 diff
bash tests/ui_runtime_check.sh     # 真实执行 UI 逻辑（非静态检查）
bash tests/ui_screenshot.sh        # 截图 + 布局/裁剪断言
bash tests/ui_glass_perf.sh        # 三档玻璃强度的 P90 帧耗时 / 丢帧率
bash tests/run_kernel.sh           # 内核与隧道
```

**期望结果**：各脚本退出码 0（当前均为 3 = SKIP）。
`tests/run_feature_matrix.sh` 的 13 项从 `不可用` 变为 `可用`，README 的
「功能可用性报告」同步更新。

**影响哪些里程碑完成判定**：M1（真机刷入可启动与卸载）、M2（P90 ≤ 24ms 且自动降级生效）、
M3（两种模式真机上网、冷启动 < 800ms）、M4–M13 全部（都要求最低可用路径真机通过）。

---

## 2. 无 GitHub Release（自更新链路无法端到端验证）

**缺什么**：至少一个 Release，附模块 ZIP + 分 ABI APK + `update.json` + SHA-256。

**怎么补**：本轮已产出可发布产物（`dist/`，见 `docs/EVIDENCE.md`），
打 tag 后建 Release 并上传即可：

```bash
# 令牌不得落盘：用一次性注入，不要写进 remote URL 或 credential store
B64=$(printf 'x-access-token:%s' "$GH_TOKEN" | base64 -w0)
git -c http.extraheader="Authorization: Basic $B64" push origin main
# tag 与 Release 用 gh CLI（未安装时先装：见 README「构建环境」）
gh release create v0.1.0 dist/syfuse-0.1.0.zip dist/*.apk \
  --title "v0.1.0 骨架版本" --notes-file CHANGELOG.md
```

**补齐后跑**：

```bash
python tools/verify_remote.py      # 远程树与本地 HEAD 的 path/mode/blob 一致性
python tools/verify_files.py dist/MANIFEST.sha256
curl -sSL -o /dev/null -w '%{http_code}\n' \
  https://github.com/vip-svip/Syfuse/releases/latest/download/syfuse-0.1.0.zip   # 期望 200（需仓库公开）
```

**期望结果**：`update.json` 的 `zipUrl` 返回 200 且 SHA-256 与
`dist/MANIFEST.sha256` 一致；模块内更新器先验哈希再解压，失败丢弃并保留旧版。

**当前状态**：`update.json` 的 `zipUrl` / `changelog` 已按真实产物与真实 owner 生成
（`sha256=fe896710…`），但 Release 尚不存在，故链接当前为 404。

---

## 3. 仓库可见性（已于 2026-10-13 由所有者拍板改为 public —— 本条已关闭）

**缺什么**：仓库公开（`updateJson` 与 raw 地址必须可匿名访问）。

**为什么不自行放开**：提示词 2.6 与 18.6 第 3 条 —— **仓库一律先建 private，
任何情况都不自行改 public**，因为改可见性是不可逆动作。

**本次结论**：该动作已由**仓库所有者本人**明确指示执行（原话："把仓库公开"），
不属于"AI 自行放开"。2026-10-13 通过 GitHub API `PATCH /repos/vip-svip/Syfuse
{"private":false}` 完成，返回可见性 `public`。护栏（`tools/verify_remote.py`
默认值 + CI `remote-guard`）已同步翻转为 `--expect-public true` —— 护栏没有被取消，
方向反过来了：**此后若有人把仓库改回 private，CI 会失败**。

**补齐后跑**（现在可跑，应全为 200）：

```bash
curl -sS -o /dev/null -w '%{http_code}\n' \
  https://raw.githubusercontent.com/vip-svip/Syfuse/main/update.json            # 期望 200
curl -sS -o /dev/null -w '%{http_code}\n' \
  https://raw.githubusercontent.com/vip-svip/Syfuse/main/module.prop           # 期望 200
```

**期望结果**：两个地址均 200。`module/module.prop` 与 `update.json` 中的注释已随
本次变更改写为"公开分发"口径（原先写的是"private 期间该地址不可用是有意登记的已知前提"）。

**剩余未决**：本条只解决"地址可达"。`update.json` 的 `zipUrl` 指向 Release 资产，
而 **Release 仍不存在，故该 URL 依旧 404** —— 那属于下面第 2 条，与本条无关，仍待办。

---

## 4. 无 Zygisk / LSPosed 框架（Hook 能力只能降级验证）

**缺什么**：已刷 Zygisk / ReZygisk / Zygisk Next / LSPosed 之一的目标设备。

**怎么补**：在已 root 的设备上刷入 LSPosed（或 Zygisk Next）。

**补齐后跑**：`bash tests/run_hook_check.sh`

**期望结果**：`Hook 装载 / 命中 / 卸载` 三段均有真实输出，命中计数增长；
无框架时按 2.6 走 TPROXY/VPN 等价方案（能力降级而非功能缺失），该降级路径已实现，
但**降级路径本身也需要真机验证**。

---

## 5. 无 gomobile + NDK（内核宿主 AAR 无法构建）

**缺什么**：`gomobile` 工具链与 Android NDK（`ANDROID_NDK_HOME`）。

**为什么需要**：M3 的「内核宿主」要把 Go 侧的隧道实现编成 AAR 供 App 静态链接
（提示词 9.2 的内核策略：**不运行时下载内核**）。没有 gomobile + NDK，
`src/tools/build_aar.sh` 只能做前置检查后如实 `exit 2`（缺工具链），
于是 `proxies.start` 保持"尚未实现"、T1/T2/T3 三个单元全部受阻。

**怎么补**：

```bash
go install golang.org/x/mobile/cmd/gomobile@latest
go install golang.org/x/mobile/cmd/gobind@latest
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/<版本>"   # 或 sdkmanager "ndk;<版本>"
gomobile init
```

**补齐后跑**：`bash src/tools/build_aar.sh`

**期望结果**：`src/out/*.aar` 产出，且 `unzip -l` 里能看到
`jni/<abi>/libgojni.so`（arm64-v8a / armeabi-v7a / x86_64 三个 ABI）。

> **勘误（2026-10-13 实测）**：本条原写"能看到 `libsyfuse.so`"，**与实际不符**。
> `gomobile bind` 生成的 AAR 里原生库文件名固定为 `libgojni.so`（gomobile 的运行时
> 入口就叫 gojni），不是按 Go 包名命名。按原文字面去断言会永远失败，而失败原因是
> "断言写错了"而不是"构建没成功" —— 这会让一个真实成功的构建被误判为受阻。
> 已按实测改正。
>
> 2026-10-13 实测结论：**本项已完成**。三个 ABI 全部构建成功
> （arm64-v8a 2 002 746 B / armeabi-v7a 1 999 696 B / x86_64 2 155 723 B），
> 每个 AAR 内含 `jni/<abi>/libgojni.so` 与 `classes.jar`（生成的 Java 绑定：
> `syfuse/Engine.java`、`syfuse/Node.java`、`syfuse/Syfuse.java` 等）。
> 过程中真正需要的三项前置在原文里**没有写全**，见下方"实测补齐步骤"。
> 之后按 `PROGRESS.md` 的 `NEXT_ACTION` 接 T2（Go 侧 `proxies.*` 真实现）→ T3。

**实测补齐步骤（原文缺的关键一步）**：

1. `gomobile` 当前 latest 要求 **Go ≥ 1.26**，而已装的工具链是 1.23.4。
   用 `GOTOOLCHAIN=local` 会在 `install` 阶段直接被拒；
   不带 `GOTOOLCHAIN=local` 又会在下载 1.26 时撞上 `GOFLAGS`/sumdb 冲突。
   → 需先装 Go 1.26（本机落在 `~/.workbuddy-ai/toolchain/go1.26`）。
2. **`gomobile init` 必须显式跑一次**。跳过它时 `gomobile bind` 报
   `gobind was not found`；`init` 之后会在 `$GOPATH/bin` 落下 `gobind.exe`。
3. `javac` 必须在 PATH 里（gomobile 要编译生成的 Java 绑定）。
   只设 `JAVA_HOME` 不够 —— 实测报 `exec: "javac": executable file not found in %PATH%`。
   三者（go / javac / gomobile）都要在 PATH 首位，且 `go` 必须是 1.26。

**另外发现并修掉的一个真实缺陷**：`ParseNodesJSON` 与
`Engine.ParseSubscriptionJSON` 原本返回 `(string, string, error)`，
而 `gomobile bind` 只接受 `(零或一个值[, error])`，导致构建报
`too many result values`。已拆成"导出两返回值 + 内部 detailed 三返回值"两层，
既满足 gomobile 约束，又不丢"哪几行坏了"的信息（并有专项测试守住摘要不被丢弃）。

> 这一项与第 1 项不同：它是**主机工具链缺口**，补齐后不需要真机即可验证
> 「AAR 构建 + 静态链接 + 最小代理链路」；只有隧道真的跑流量时才需要设备。

---

## 6. 本轮未能执行的其余检查（同属「待外部验证」）

| 检查 | 阻塞原因 | 补齐条件 |
|---|---|---|
| `tests/run_feature_matrix.sh` 的 13 项最低可用路径 | 无设备 | 同第 1 项 |
| `tests/ui_runtime_check.sh` / `ui_screenshot.sh` | 无设备 | 同第 1 项 |
| `tests/ui_glass_perf.sh` 的三档帧耗时实测 | 无设备 | 同第 1 项 |
| `tests/run_kernel.sh`（隧道吞吐、冷启动） | 无设备 | 同第 1 项 |
| M4–M13 全部功能 | 代码未实现（不是外部阻塞） | 见 `PROGRESS.md` 的 `NEXT_ACTION` |

> 第 6 项中"M4–M13 代码未实现"**不是外部阻塞**，是本项目的正常进度，
> 已按里程碑登记在 `PROGRESS.md`，不在此处重复计入。
