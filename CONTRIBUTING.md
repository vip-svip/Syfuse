# 贡献指南

先读 [`README.md`](README.md) 的「当前完成度」一节 —— 它决定了你的 PR 应该做什么。

---

## 最重要的三条

### 1. 不编造

**这是本项目的第一纪律。** 具体含义：

- 不要写"已完成"但实际没跑通的代码。跑不通就如实说跑不通。
- 不要在 README / 注释 / commit message 里写未经验证的性能数字。
- 不要声明一个未实现的能力（例如在 Manifest 里加 `BIND_VPN_SERVICE` 但没实现 VpnService）。
- 不要 `throw NotImplementedError()` 占位。没实现就**不要提供那个入口**，
  或者提供一个**如实报错**的实现（参考 `engine/NotLinkedDataPlane.kt`）。

`tests/run_module_checks.sh` 里有对应的静态断言。破坏了会让 CI 变红。

### 2. 证据优先

提交 PR 时，**贴原始命令输出**，不要只贴结论。

```bash
# 好
tests/run_unit.sh
# 输出：
#   [UNIT SUMMARY] 套件文件=3 用例=28 失败=0 错误=0 跳过=0
#   [UNIT RESULT] PASS

# 不好
# "单测都过了"
```

验证脚本的退出码约定见 README。**`3`（跳过）不等于通过**，不要在 PR 里把跳过写成通过。

### 3. 语义改动必须带语料

如果你改了规则解析或匹配语义，**必须**在 `tests/expect.tsv` 里补上对应的期望行，
并在 `tests/corpus/` 里补上触发的语料。理由：规则语义的 bug 极其难在真机上定位
（表现是"某个广告没被拦"），只能靠对拍守住。

---

## 开发流程

```bash
# 1. 确保本地工具链就绪（README「构建环境」）
tests/run_unit.sh              # 必须全绿

# 2. 改代码
# 3. 提交前自检（这三条都要过）
tests/run_unit.sh
tests/run_module_checks.sh
tests/ui_assets_check.sh
python tools/scan_secrets.py

# 4. 涉及界面的改动，额外跑（需要设备）
tests/ui_runtime_check.sh
tests/ui_screenshot.sh         # 截图贴到 PR 里
```

### 不要并发跑两个 Gradle 构建

在同一个项目目录上同时启动两个 Gradle 构建会争抢 `app/build/outputs/mapping/`
下的文件，表现为 `shrinkReleaseRes` 报「拒绝访问」。

这个失败**看起来像代码问题，实际是操作问题**，很容易浪费排查时间。
`tests/` 下的脚本都是串行执行 Gradle 的；如果你自己起了并行任务，请等前一个结束。

---

## 代码约定

### 模块边界

**UI 不感知内核具体类型。** 这是硬规矩：

```
ui  ──依赖──▶  domain.engine.EngineController（接口）
                      ▲
                      │ 实现
                engine.EngineModule（唯一替换点）
```

换内核（NotLinked → Go AAR → 其它）只改 `EngineModule` 一处绑定。
如果你的 PR 让 `ui` 直接 import 了某个内核实现类，这个 PR 会被要求改。

### 纯逻辑下沉

能被 JVM 单测覆盖的逻辑，**不要**放进 Android 模块。

- 规则引擎 → `core/rule`（纯 JVM，无 Android 依赖）
- 设置目录、配置编解码、状态机、流量计量 → `domain`（纯 JVM）
- 只有真正需要 Android API 的才放 `data` / `engine` / `ui`

这样 CI 的最小容器（只有 JDK）就能验证核心语义。

### 注释

注释写**为什么**，不写**是什么**。

```kotlin
// 好：解释了为什么这么写
// 用固定间隔而非事件驱动，因为内核上报本身是轮询式的。
delay(1000)

// 不好：重复了代码本身
// 延迟 1000 毫秒
delay(1000)
```

如果某个决定看起来"反直觉"，**一定要写清楚理由**。否则下一个维护者会"修好"它。

### 界面

- **禁止**大面积 blur / 毛玻璃 / 动态噪点（性能）。滚动容器全部虚拟化。
- 危险项必须有 `RiskBadge`，且**不能只靠颜色**区分（色觉障碍可用性）——
  当前实现是颜色 + 图标 + 文字三重编码。
- **新增**的用户可见文案请走 `strings.xml`，不要再往 Compose 里写中文字面量。

> **已知技术债**：骨架阶段已有 **372 处**界面文案内联在 Compose / ViewModel 中，
> 尚未抽取到资源文件。`tests/ui_assets_check.sh` 会统计并打印这个数字（PEND，不判失败）。
> 数字以该脚本的输出为准 —— 它会随界面推进而变化，本文件里的数字只是快照。
> 抽取时注意：ViewModel 里的提示文案不能直接用 `stringResource`（没有 Composable 上下文），
> 正确做法是让 VM 抛出携带资源 ID 的错误类型，由 UI 层解析。这是一个独立的重构任务。

---

## 提交信息

用 Conventional Commits：

```
feat(rule): 支持 $removeparam 修饰符
fix(engine): 修正流量计数器回退时的速率计算
docs(readme): 补充 M2 内核缺口说明
test(corpus): 补 6 条 cosmetic 规则期望
chore(ci): 固定 Gradle 版本到 8.11.1
```

scope 用模块名：`rule` / `domain` / `data` / `engine` / `ui` / `app` / `module` / `ci` / `docs`。

---

## 不要做的事

- ❌ 把许可证改成 MIT / Apache-2.0。本项目必须留在 GPL 族（见 NOTICE 第 1 节）
- ❌ 在代码或配置里写入真实凭据、订阅链接、密钥
- ❌ 引入遥测 / 崩溃上报 / 广告 SDK（R3）
- ❌ 让应用在运行时下载可执行内容（R2）
- ❌ 用 WebView 实现主界面（R1）
- ❌ 提交 `local.properties`、`*.jks`、`*.keystore`
- ❌ 自动把仓库从私有改成公开

---

## 新增依赖

引入新依赖前请确认：

1. 许可证与 GPL-3.0-or-later 兼容（Apache-2.0 / MIT / BSD 都可以；
   专有许可、SSPL、以及"仅限非商业"类不行）
2. 版本写进 `gradle/libs.versions.toml`，不要散落在模块里
3. 同步更新 [`NOTICE`](NOTICE) 的第三方组件表
4. 说明为什么不能用已有依赖或自己写

---

## 报告问题

- **功能缺陷 / 建议**：开 issue，附上 `tests/run_unit.sh` 的输出与复现步骤
- **安全问题**：走 [`SECURITY.md`](SECURITY.md) 的私密渠道，**不要**开公开 issue

## 许可

提交 PR 即表示你同意以 GPL-3.0-or-later 授权你的贡献。
