# M10 过滤引擎语法差距调研（filter_gap.md）

> 目标：AGH + uBO Lite + SY-Fusion 全语法（规范 5.12）。本文档盘点全语法，
> 划分实现阶段，作为 filter 引擎逐轮推进的对照表。
> 验收硬指标（规范 6.3/6.4）：过滤语料全绿；100 万条编译 < 3s、匹配 p99 < 1ms；
> 优先级链 `重要放行 > 重要拦截 > 普通放行 > 普通拦截 > 未命中(放行)` 严格实现。

## 1. 语法总清单与分层

### P0 —— DNS 层核心（引擎立即可用，本轮实现）

| 语法 | 语义 | 状态 |
|---|---|---|
| `example.com` | 纯域名清单（精确匹配该域） | ✅ internal/filter |
| `\|\|ads.example.com^` | 域名锚定拦截（含子域） | ✅ |
| `@@\|\|good.example.com^` | 例外放行 | ✅ |
| `@@` + `$important` | 例外提升为最高优先级 | ✅ |
| `$important` | 拦截/放行提升一级 | ✅ |
| `$badfilter` | 精确关闭同形规则 | ✅ |
| `0.0.0.0 ads.example.com` | hosts 拦截 | ✅ |
| `127.0.0.1 tracker.example.com` | hosts 拦截（回环） | ✅ |
| `1.2.3.4 rewrite.example.com` | hosts 解析重写 | ✅（Rewrite map） |
| `address=/ads.example.com/` | dnsmasq 拦截 | ✅ |
| `local=/ads.example.com/` | dnsmasq 本地应答 | ✅ |
| `!` `#` `;` 注释 + `[Adblock Plus 2.0]` 头 | 注释兼容 | ✅ |
| 优先级链 | 重要放行>重要拦截>普通放行>普通拦截>未命中 | ✅（黄金用例守住） |

### P1 —— 网络规则增强（下阶段）

| 语法 | 语义 | 状态 |
|---|---|---|
| `/regex/` | 正则规则 | ⬜（引擎留正则集合位，解析未接） |
| `https://cdn.x/js/` | 精确 URL 锚定 | ⬜ |
| `^` 通配符/分隔符 | URL 模式 | ⬜ |
| `$domain=` `$client=` `$ctag=` | 作用域限定 | ⬜ |
| `$third-party`(`$3p`) `$first-party`(`$1p`) | 来源关系 | ⬜ |
| `$denyallow=` | 例外规则限定范围 | ⬜ |
| 资源类型全集（$script/$image/…） | 类型过滤 | ⬜（DNS 层暂不需要） |
| `$dnsrewrite=` | DNS 应答改写 | ⬜（hosts 重写已覆盖常见场景） |

### P2 —— 修饰过滤（uBO 侧，需 MITM/浏览器内嵌场景）

| 语法 | 语义 | 状态 |
|---|---|---|
| `example.com##.ad` | 元素隐藏 | ⬜ |
| `example.com#@#.ad` | 取消隐藏 | ⬜ |
| `example.com#$#…` | CSS 注入 | ⬜ |
| `example.com#?#…` | 扩展 CSS（:has 等） | ⬜ |
| `example.com#%#//scriptlet(…)` | 脚本注入 | ⬜ |
| `example.com##^script:has-text(…)` | HTML 过滤 | ⬜ |
| `$csp=` `$removeparam=` `$redirect=` `$cookie=` `$header=` `$permissions=` | 响应修饰 | ⬜ |

## 2. 引擎形态（规范 6.3"Go 重写"）

```
解析（报错到行号）→ 校验 → 编译：
  域名 Trie        （||锚定 + 纯域名，含子域匹配）
  精确域名 map     （hosts/dnsmasq/rewrite）
  正则集合         （P1）
→ 热重载（原子换指针，不中断连接）
→ 匹配：四级决策 + $badfilter 去除
```

- 性能路径：Trie 节点 map[int]*node + 终结标志，DNS 查询是"按标签逆序走 Trie"，
  O(域名标签数)；无锁读（编译完换指针），写侧持锁。
- 语义对齐依据：SY-Fusion `scripts/engine.sh + *.awk` 的判定顺序用
  优先级链等价复现（规范 6.5"Go 重写 + 语料回归"）。
- 语料：`internal/filter` 内嵌黄金用例覆盖 P0 全行 + 优先级交叉组合；
  P1/P2 接入时逐条补语料。

## 3. 与现有组件的关系

- `internal/dns`（M6）：hosts 解析在 DNS 服务里已有一份；filter 的 hosts
  规则与它**并存不冲突**——filter 面向"规则集订阅形态"，dns 面向"本机
  hosts 文件形态"；后续由调用方决定合并顺序。
- `core:rule`（M5）：那是**分流**规则（走代理还是直连），filter 是**拦截**
  规则（阻断/放行），两套语义、两套语料，不共用引擎。
- 统计口径（规范 6.3）：查询数/拦截数/威胁数/成人内容数由调用方在
  Match 结果上累计，引擎只给 Decision。
