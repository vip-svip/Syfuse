# Changelog

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

每个正式版本都会附上模块 ZIP 的 SHA-256，供下载后校验完整性。

---

## [Unreleased]

### 计划中

- M2 内核打通：sing-box AAR 接入、VPN 模式 TUN 起停、最小可用代理
- M3 节点与订阅：节点 CRUD、测速、分享链接、订阅导入与更新差异
- M4 规则与分流：规则页、规则集、体检、测试
- M5 DNS：多上游、DoH/DoT/DoQ、fake-ip、嗅探、缓存、防泄漏
- M6 MITM 与脚本：CA 生成安装卸载、解密白名单、URL 重写、JS 沙箱
- M7 连接日志统计
- M8 工具与系统集成
- M9 差异增强层一：广告过滤内核、网络层封禁
- M10 差异增强层二：开屏跳过、通知净化、配置即产品
- M11 特权层：root 能力探测、TPROXY、系统证书、hosts 注入、LSPosed 桥接
- M12 打磨与发布

---

## [1.0.0] — 待发布

**状态：M1 里程碑完成，尚未创建 Release。**

### 新增（M1：多模块工程骨架）

- 多模块 Gradle 工程：`app` / `ui` / `domain` / `data` / `core` / `engine` /
  `privileged` / `lsposed-bridge`
- 底部四 Tab 导航（首页 / 节点 / 规则 / 设置），与 Shadowrocket 四段式结构对齐
- Material 3 主题，支持浅色 / 深色 / 跟随系统 / AMOLED 纯黑四种模式
- 首页完整骨架：连接四态开关、全局路由三态（配置/代理/直连）、配置与节点卡片、
  实时速率、快捷动作、过滤拦截计数
- 节点页骨架：策略组横向标签、虚拟列表、延迟着色（<200 绿 / <500 黄 / 其余红）
- 规则页骨架：五分段（分流规则 / 规则集 / 规则体检 / 规则测试 / 过滤规则）
- 设置页：11 个分组结构、设置搜索、专家模式、三段式设置项与三色风险标记
- 中英双语字符串资源（89 个键，键集合完全一致）
- `ProxyEngine` 内核抽象接口，UI 层零依赖具体内核类型
- sing-box 配置编译器（含 tag 唯一性、空入站校验、rawExtras 透传）
- Room 数据库骨架（13 张表）+ DataStore 偏好存储
- KernelSU / Magisk 模块骨架（`module.prop` id=`syfuse` 及全部必需脚本）
- 完整验证体系脚手架：静态校验、单测、资产基线、密钥扫描、性能基准、打包校验
- GitHub Actions CI：5 个 job + 合并门禁
- GPL-3.0-or-later 许可，附第三方组件与许可清单

### 验证证据

M1 的验证结果在对应里程碑交付报告中给出，包含命令与原始输出。

**当前已通过的检查（可复现）：**

- `tests/run_module_checks.sh`：R1（无 WebView）/ R2（无运行时内核下载）/
  R3（无遥测 SDK）/ R5（无密钥）/ 标识一致性 / ABI 配置 / 中英资源键一致性
- `tools/scan_secrets.py`：未发现密钥、签名文件、绝对路径或未替换占位符
- `tests/ui_assets_check.py`：32 项 UI 资产与基线一致（4 个导航目标 + 28 个 Composable）
- `tests/perf_baseline.py --static`：60 个源文件、3059 行有效代码

**当前无法在本机执行的检查（需 CI 的 Linux runner 或真机）：**

- `tests/run_unit.sh`：本机无 JDK，无法运行 Gradle
- `tests/run_e2e.sh` / `tests/ui_runtime_check.sh` / `tests/ui_screenshot.sh` /
  `tests/run_corpus.sh`：依赖内核或 Android 运行时，且功能本身尚未实现

### 已知限制

- 内核尚未接入（M2）；`ProxyEngine` 的 sing-box 实现目前返回
  「功能尚未实现」而非伪装成功
- 设置页仅「通用」分组有内容，其余 10 个分组的框架已就位，内容按里程碑填充
- GEO 数据来源与许可待核实（见 NOTICE），须在 M4 前解决
- 尚无 Release；`update.json` 的 `sha256` 字段为 `PENDING-FIRST-RELEASE`

---

<!--
版本条目模板：

## [X.Y.Z] — YYYY-MM-DD

### 新增
### 变更
### 修复
### 移除
### 安全

**产物校验：**
- 模块 ZIP SHA-256：`<hash>`
- 提交：`<commit>`
-->
