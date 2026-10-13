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

head2 "1. 四 Tab 命名必须对齐实拍（4.1）"
NAV="ui/src/main/kotlin/io/github/vipsvip/syfuse/ui/nav/SyfuseApp.kt"
for tab in 首页 配置 数据 设置; do
  if grep -q "\"$tab\"" "$NAV" 2>/dev/null; then
    ok "Tab 存在：$tab"
  else
    no "Tab 缺失：$tab（4.1 要求 首页 | 配置 | 数据 | 设置）"
  fi
done
# 不得新增第 5 个 Tab（7.2）
n=$(grep -cE '^\s+[A-Z_]+\("[a-z]+", "[^\"]+", Icons\.' "$NAV" 2>/dev/null || echo 0)
if [ "$n" -eq 4 ]; then ok "Tab 数量为 4（7.2 要求不新增第 5 个）"; else no "Tab 数量为 $n，应为 4"; fi

head2 "2. 第 4 部分的页面是否都有屏幕文件或明确标注未实现"
for page in 首页 Home 配置 Config 数据 Data 设置 Settings; do :; done
SCREENS=$(ls -1 ui/src/main/kotlin/io/github/vipsvip/syfuse/ui/screen/*.kt 2>/dev/null | wc -l | tr -d ' ')
info "屏幕文件数：$SCREENS"
for s in HomeScreen ConfigScreen DataScreen SettingsScreen; do
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
