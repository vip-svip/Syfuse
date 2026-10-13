# Syfuse 内核（Go）

本目录是 Syfuse 的代理内核。它通过 `gomobile bind` 编译成 Android AAR，
由 Kotlin 侧 `io.github.vipsvip.syfuse.engine` 模块经 `DataPlane` 接口调用。

---

## 当前实现范围

诚实声明，不夸大。`tests/run_kernel.sh` 会把这些断言变成可执行的检查。

### 已实现且已验证

| 能力 | 位置 | 验证方式 |
|---|---|---|
| 订阅解析：`ss` / `ssr` / `vmess` / `vless` / `trojan` / `hysteria2` / `http(s)` / `socks5` | `src/node.go` | 55 个 Go 用例 |
| ss 的三种历史格式（SIP002 / 整体 base64 / 明文 userinfo） | `parseSS` | 各有独立用例 |
| IPv6 字面量（`[::1]:443`）与未加括号时的拒绝 | `splitHostPort` | 2 个用例 |
| 订阅"部分成功"语义（坏行不废掉整份订阅，且错误带行号） | `ParseSubscription` | 专项用例 |
| 节点去重（同名不同参数不去重；同参数不同名去重） | `Node.Key` | 专项用例 |
| **凭据隔离**（UI 用的 JSON 不含密码/UUID/原始 URI） | `nodePublic` 白名单投影 | 泄漏专项用例 |
| 配置校验（schema 版本拒绝、路由模式、日志级别、FakeIP 前置条件） | `src/config.go` | 20 个用例 |
| 引擎状态机（`not_linked` / `failed` / `stopped`） | `src/core.go` | 专项用例 |

### 未实现

| 能力 | 现状 |
|---|---|
| **数据面隧道**（TCP/UDP 转发、TLS 握手、协议编解码） | **未实现**。`Engine.Start()` 做完配置校验与节点解析后返回 `ErrTunnelNotImplemented` |
| 规则匹配（数据面） | 未实现。规则语义在 Kotlin 的 `core:rule` 模块（已有 28 个 JVM 用例覆盖），内核侧只做传递 |
| DNS / FakeIP | 只有配置字段，无实现 |
| 流量统计 | 只有计数器结构与 JSON 输出，无真实数据源 |

> **`Start()` 为什么返回错误而不是 nil。**
> 返回 nil 会让 Kotlin 侧把状态置为"已连接"并显示给用户，而实际没有任何流量被代理。
> 一个返回成功却什么都没做的 `Start()`，比一个明确报错的 `Start()` 危险得多。
> 这条行为有专门的测试守着：`TestEngine_StartReportsTunnelNotImplemented`。

---

## 目录结构

```
kernel/
├── go.mod              模块定义。**零第三方依赖**（见下）
├── src/
│   ├── doc.go          包说明、版本常量、ErrTunnelNotImplemented
│   ├── node.go         节点模型 + 8 种协议的 URI 解析 + 凭据隔离投影
│   ├── node_test.go
│   ├── config.go       配置模型与严格校验
│   ├── config_test.go
│   ├── core.go         gomobile 导出 API（Engine / 包级函数）
│   └── engine_test.go
├── tools/build_aar.sh  gomobile bind 构建入口（缺前置时 exit 2，不静默跳过）
└── out/                AAR 产物（gitignore）
```

---

## 为什么零第三方依赖

`go.mod` 里没有任何 `require`，也没有 `go.sum`。这不是偷懒，是设计：

1. **交叉编译成功率**：内核要 `gomobile bind` 到 3–4 个 ABI。依赖越少，
   交叉编译踩坑的概率越低。
2. **供应链面**：代理内核是安全敏感组件。每个依赖都是一个需要审计的入口。
3. **可验证性**：纯 stdlib 意味着 `go test` 在任何装了 Go 的机器上都能跑，
   `GOPROXY=off` 也能通过。这一点直接决定了"内核逻辑可验证"是不是空话。

`tests/run_kernel.sh` 里有两条断言守着它：`go.mod` 出现 `require` 或存在 `go.sum` 都会失败。
另外脚本用 `GOPROXY=off` 运行——如果哪天有人加了外部依赖，这里会立刻暴露。

如果将来必须引入依赖：请在 `NOTICE` 登记其许可证，并确认与 GPL-3.0-or-later 兼容。

---

## 构建

```bash
# 1) 验证（不需要设备、不需要网络、不需要 NDK）
tests/run_kernel.sh

# 2) 只构建本机二进制（用于快速验证编译）
cd kernel && go build ./...

# 3) 构建 Android AAR（需要 gomobile + NDK）
kernel/tools/build_aar.sh                  # 全部 ABI
kernel/tools/build_aar.sh arm64-v8a        # 单个 ABI
```

### AAR 前置

```bash
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
sdkmanager --install "ndk;27.0.12077973"
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/27.0.12077973"
```

`build_aar.sh` 在缺少 gomobile 或 NDK 时 **exit 2** 并把安装步骤打出来。
它不会静默产出一个空 AAR —— 那会让下游以为内核已经就绪。

### ABI 命名映射

gomobile 的 `-target` 取值与 Android ABI 名称不同，脚本里做了映射：

| Android ABI | gomobile target |
|---|---|
| `arm64-v8a` | `arm64` |
| `armeabi-v7a` | `arm` |
| `x86_64` | `amd64` |
| `x86` | `386` |

---

## 接入 Kotlin 侧

AAR 产出后，把 `kernel/out/syfuse-core-<abi>.aar` 放进 `engine/libs/`，
然后在 `engine/build.gradle.kts` 里声明：

```kotlin
dependencies {
    implementation(files("libs/syfuse-core-arm64-v8a.aar"))
    // 或按 ABI 分别声明
}
```

并新增一个 `DataPlane` 实现（或替换现有的 `UdsDataPlane`）。
**替换点只有 `EngineModule` 一处** —— UI 与规则引擎不需要任何改动（这是 9.2 的设计目标）。

当前绑定的是 `UdsDataPlane`：控制通道是真的（走 UDS 连 `syfused`），
但 `proxies.start` 尚未实现，所以它会**如实失败**并把守护进程给的原因带上来。
内核接入后只需换掉 `EngineModule.provideDataPlane` 的返回实现即可。

---

## 导出 API

gomobile 对参数类型的支持有限，因此所有导出方法**只用 string / int / bool / error**，
结构化数据一律走 JSON 字符串。这不是偷懒，是为了让绑定层稳定。

```go
// 包级函数
func Version() string
func SupportedProtocols() []string
func ParseNodesJSON(content string) (nodesJSON, errSummary string, err error)
func ValidateConfigJSON(configJSON string) (normalized string, err error)

// 实例
func NewEngine() *Engine
func (e *Engine) Start(configJSON string) error
func (e *Engine) Stop() error
func (e *Engine) State() string          // not_linked / failed / stopped / running
func (e *Engine) StatsJSON() string
func (e *Engine) NodeCount() int
func (e *Engine) NodesJSON() (string, error)              // 不含凭据
func (e *Engine) ParseSubscriptionJSON(content string) (nodesJSON, errSummary string, err error)
```

所有导出方法都加锁。gomobile 的 Java 侧可能从任意线程调用，
不加锁会在真机上表现为偶发 panic —— 那种 bug 极难复现。

---

## 测试

```bash
cd kernel
go test ./...            # 全部用例
go test ./... -v         # 带用例名
go test ./src -run TestParseSS -v
```

当前：**55 个用例，0 失败**。

用例里的 URI 由编码器**程序化构造**（`b64url()` / `b64std()`），而不是手写 base64 字面量。
理由：手写 base64 很容易抄错一个字符，然后测试"通过"了但测的是错的东西。

### 曾经真的失败过的用例

留着这几条记录，因为它们解释了测试为什么这样写：

| 用例 | 曾经的问题 |
|---|---|
| `TestMarshalNodes_NoSecretsLeak` | `Password` 标了 `json:"-"`，导致**带凭据**的序列化也是空的。修法不是去掉 tag，而是改成 `nodePublic` 白名单投影 |
| `TestParseIPv6Literal` | 加了"未加括号的 IPv6 应拒绝"之后，方括号形式被误拒 —— 括号内本来就含 `:` |
| `TestConfig_StringHasNoSecretFields` | 测试自身写错：反引号原始字符串里的 `"+"` 被当成 JSON 字面量 |

---

## 代码约定

- **注释写为什么，不写是什么。** 尤其要写清"为什么不用那个看起来更简单的做法"。
- **不引入第三方依赖**（除非经过明确讨论并更新 NOTICE）。
- **导出 API 只用标量类型**，结构化数据走 JSON。
- **凭据默认不输出**：新增字段时默认**不**进 `nodePublic`，要输出必须先显式加。
- **未实现的能力要显式报错**，不要返回 nil 假装成功。
