# 贡献指南

## 开发环境要求

| 工具 | 版本 | 用途 |
|---|---|---|
| JDK | 17 | Kotlin 编译、Gradle |
| Android SDK | API 35 | 构建 APK |
| Go | 1.25+ | 构建 sing-box 内核 AAR（M2 起必需） |
| Python | 3.10+ | 验证脚本 |

> 注意：M1–M12 期间内核仅内置 sing-box，Go 工具链在 M2 起才需要。

## 快速开始

```bash
# 1. 克隆
git clone https://github.com/vip-svip/Syfuse.git
cd Syfuse

# 2. 生成 Gradle Wrapper（仓库首次提交时可能尚未包含）
gradle wrapper --gradle-version 8.11.1
chmod +x gradlew

# 3. 构建
./gradlew :app:assembleDebug
```

## 运行验证（提交前必做）

```bash
# 静态合规检查（无需 SDK/JDK，最快）
bash tests/run_module_checks.sh

# 单元测试
bash tests/run_unit.sh

# 密钥扫描
python tools/scan_secrets.py

# UI 资产基线比对
python tests/ui_assets_check.py
```

全部验证脚本的清单与用途见 README 的「构建与验证」一节。

**CI 会在推送后运行完整的 5 个 job。任一失败将阻断合并。**

## 提交规范

### 提交信息格式

```
<type>(<scope>): <subject>
```

**type 取值：**

| type | 含义 |
|---|---|
| `feat` | 新功能 |
| `fix` | 缺陷修复 |
| `perf` | 性能优化 |
| `refactor` | 重构（不改变外部行为） |
| `docs` | 文档 |
| `test` | 测试 |
| `build` | 构建配置 |
| `ci` | CI 配置 |
| `chore` | 杂项 |

**scope 取值：** 模块名，如 `ui`、`domain`、`engine`、`data`、`module`、`ci`、`docs`。

**语言：** 全库统一使用中文（与代码注释一致）。

**示例：**

```
feat(engine): 接入 sing-box AAR 并实现 TUN 建连

fix(ui): 修正节点页延迟徽标在深色主题下的对比度
```

### 分支策略

| 分支 | 用途 |
|---|---|
| `main` | 可发布分支，受保护，需 CI 全绿 |
| `feat/<name>` | 功能开发 |
| `fix/<name>` | 缺陷修复 |

### 提交粒度

- 一次提交只做一件事，便于 review 与回滚
- **禁止**把不相关的改动混在一个提交里
- 首次提交已拆分为多个有意义的提交（脚手架 / 内核集成 / UI / 规则引擎 / 测试 / 文档）

## 代码规范

### 注释

注释说明**为什么**，而非**是什么**：

```kotlin
// 好：解释决策原因
// 用不可见字符作分隔符：域名与正则里都可能出现逗号、竖线，
// 用常见字符会导致数据损坏
const val SEPARATOR = "\u0000"

// 差：复述代码
// 用 \u0000 作为分隔符
const val SEPARATOR = "\u0000"
```

### 测试要求

- 关键算法必须有单测
- 公共 API 必须有 KDoc（或等价文档注释）
- 修复缺陷时必须附带回归测试

### 文档要求

- **全部文档写在 README 一个文件里**，不拆成多个 md 文件
- 仅在 README 超过约 1000 行时才考虑拆分，且拆分后每个文件必须自包含

## 不可协商的红线

以下六条约束**不接受 PR 修改**。提交的代码若违反，CI 会直接失败：

| 编号 | 约束 |
|---|---|
| R1 | 主界面必须原生（Jetpack Compose），禁止 WebView / H5 / 混合框架承载 UI |
| R2 | 内核静态内置，不下载内核二进制（可选更新须带 SHA-256 + 签名校验） |
| R3 | 零遥测，默认不上报任何数据 |
| R4 | 不造假：不编造 API、常量、性能数字，不把推断写成实测 |
| R5 | 密钥不入库、不落明文 |
| R6 | 危险能力显式告知且可完整还原 |

## 新增依赖

新增任何第三方依赖时，必须：

1. 核实其许可，确认与本项目的 GPL-3.0-or-later 兼容
2. 同步更新 `NOTICE` 文件
3. 若为 GPL 组件，确认其不引入额外的品牌使用限制

## 安全问题

请勿通过公开 Issue 报告安全漏洞。见 `SECURITY.md`。

## 许可

提交代码即表示你同意以 GPL-3.0-or-later 授权你的贡献。
