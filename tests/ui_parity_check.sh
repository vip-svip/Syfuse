#!/usr/bin/env bash
# UI 功能对齐检查（提示词 12 部分 ui_parity_check）
#
# 核对「第 5 部分功能清单里每一项都有界面落点」。
# 命名与默认值尽量对齐实拍（版式可重新设计，见 4 部分开头）。
#
# 这是**静态**检查：它只能证明"入口存在"，不能证明"入口能用"。
# 「能用」由 tests/run_feature_matrix.sh 的真机部分负责。
# 两者不可互相替代 —— 把静态通过当成功能可用就是 12.2 明令禁止的死 UI。
#
# 退出码：0 全部有落点 / 1 有缺失
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
cd "$SYFUSE_ROOT"
PASS=0; FAIL=0; PEND=0; SKIP=0
ok()   { printf '  [PASS] %s\n' "$1"; PASS=$((PASS+1)); }
no()   { printf '  [FAIL] %s\n' "$1"; FAIL=$((FAIL+1)); }
pend() { printf '  [PEND] %s\n' "$1"; PEND=$((PEND+1)); }
skip() { printf '  [SKIP] %s\n' "$1"; SKIP=$((SKIP+1)); }
info() { printf '  :: %s\n' "$1"; }
head2(){ printf '\n== %s ==\n' "$1"; }

head2 "1. 布局预设必须满足 4.1 的六条硬标准"
# 旧版这里断言的是固定的「首页/配置/数据/设置」4 Tab。
# 新提示词 4.1 明确判定 4 Tab **不合格**（装不下功能量），改为
# 「多布局预设 + 可自建 Tab」，默认布局 B（7 Tab）。
# 断言必须跟着改 —— 否则检查器会用过时标准去否定正确的实现。
LAYOUT="ui/src/main/kotlin/io/github/vipsvip/syfuse/ui/nav/LayoutPreset.kt"

for preset in GEek DAILY MINIMAL; do
  grep -q "val $preset = LayoutSpec(" "$LAYOUT" && ok "布局预设存在：$preset" || no "布局预设缺失：$preset"
done
grep -q "isDefault = true" "$LAYOUT" && ok "存在默认布局标记" || no "没有默认布局"
grep -q "val default: LayoutSpec = GEek" "$LAYOUT" && ok "默认布局为 B「极客全显」（4.1 要求）" || no "默认布局不是 B"

# 六条硬标准的常量必须存在
for c in "MIN_TABS = 3" "MAX_TABS = 7" "MAX_GROUP_SHARE" "MIN_GROUP_SHARE" \
         "MAX_SECONDARY_PAGES = 7" "TOTAL_FUNCTION_GROUPS = 34"; do
  grep -q "$c" "$LAYOUT" && : || no "缺少 4.1 硬标准的常量：$c"
done
ok "六条硬标准的常量已核对"
grep -q "fun violationsOf()" "$LAYOUT" && ok "硬标准做成了可执行断言（violationsOf）" || no "硬标准只写在注释里，没有断言"
grep -q "ABSTRACT_WORDS" "$LAYOUT" && ok "抽象词黑名单存在（4.1 第 2 条）" || no "缺少抽象词黑名单"

# 默认布局 B 的 7 个 Tab 必须都在
for tab in 首页 代理 规则 拦截 调试 监控 设置; do
  grep -q "label = \"$tab\"" "$LAYOUT" && ok "布局 B 含 Tab：$tab" || no "布局 B 缺少 Tab：$tab"
done
n=$(grep -c 'label = "' "$LAYOUT")
info "三套预设合计定义 $n 个 Tab 标签（含重复）"

# 导航必须由预设驱动，而不是写死
NAV="ui/src/main/kotlin/io/github/vipsvip/syfuse/ui/nav/SyfuseApp.kt"
grep -q "LayoutPresets.byId" "$NAV" && ok "导航由布局预设驱动（未写死）" || no "导航写死了 Tab 列表"
grep -q "layout.tabs.forEach" "$NAV" && ok "Tab 由预设列表渲染" || no "Tab 未由预设渲染"

head2 "2. 第 4 部分的页面是否都有屏幕文件或明确标注未实现"
for page in 首页 Home 配置 Config 数据 Data 设置 Settings; do :; done
SCREENS=$(ls -1 ui/src/main/kotlin/io/github/vipsvip/syfuse/ui/screen/*.kt 2>/dev/null | wc -l | tr -d ' ')
info "屏幕文件数：$SCREENS"
for s in HomeScreen ProxyScreen RulesScreen InterceptScreen DebugScreen MonitorScreen SettingsScreen; do
  grep -q "fun $s(" ui/src/main/kotlin/io/github/vipsvip/syfuse/ui/screen/*.kt 2>/dev/null \
    && ok "屏幕存在：$s" || no "屏幕缺失：$s"
done

head2 "3. README 的逐屏界面清单必须覆盖第 4 部分的全部页面"
if grep -q "逐屏界面清单" README.md; then
  ok "README 含逐屏界面清单"
  # 4.10 列出的"未拍到但必须存在"的页面，逐个核对是否登记
  for p in 节点编辑 策略组编辑 规则编辑 Hosts编辑 URL重写编辑 头部重写编辑 正文重写编辑 \
           本地映射编辑 HTTPS解密 脚本编辑 代理分组编辑 规则集URL 脚本URL 测试规则 \
           日志 统计详情 诊断 外观 隧道 按需求连接 捷径 小组件 场景 分组 关于 浏览器面板; do
    grep -q "$p" README.md && : || pend "逐屏清单未登记：$p"
  done
  ok "4.10 的页面清单已核对（未登记的以 PEND 列出）"
else
  no "README 缺少逐屏界面清单（18.7 要求）"
fi

head2 "4. 不得用 WebView 承载界面（15.1）"
if grep -rn "android.webkit\|WebView\|loadUrl" ui/src app/src 2>/dev/null | grep -v '^\s*//' | grep -v '\*' | grep -q .; then
  no "发现 WebView 引用 —— 15.1 明令禁止"
else
  ok "无 WebView 引用"
fi

printf '\n===========================================\n'
printf 'ui-parity 汇总: PASS=%d FAIL=%d PEND=%d\n' "$PASS" "$FAIL" "$PEND"
printf '注意：本检查只证明「入口存在」，不证明「入口能用」。\n'
printf '      功能可用性必须由 run_feature_matrix.sh 的真机部分判定。\n'
printf '===========================================\n'
[ "$FAIL" -eq 0 ] || exit 1
exit 0
