#!/usr/bin/env bash
# 红队静态检查：把提示词里的六条硬红线（R1–R6）变成**可执行的断言**。
#
# 三档结论，必须严格区分 —— 混为一谈会让这个脚本失去意义：
#   PASS    断言成立
#   FAIL    红线被**违反**（例如主界面里真的出现了 WebView）—— 退出码 1
#   PEND    里程碑**尚未完成**（例如内核还没构建）—— 如实标记，不导致失败
#
# 为什么必须区分：如果把"内核还没做"和"内核被运行时下载"都算 FAIL，
# 那么 M1 阶段这个脚本永远是红的，人就会开始忽略它 —— 那是更糟的结果。
# 反过来，如果把未完成当成 PASS，就是在编造。所以用第三档。
#
# 设计原则：只做能被机器判定的检查。判不了的（例如"性能是否达标"）不在这里假装检查。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

PASS=0; FAIL=0; PEND=0
pass() { printf '  [PASS] %s\n' "$1"; PASS=$((PASS+1)); }
fail() { printf '  [FAIL] %s\n' "$1"; FAIL=$((FAIL+1)); }
pend() { printf '  [PEND] %s\n' "$1"; PEND=$((PEND+1)); }
note() { printf '  [INFO] %s\n' "$1"; }
head2() { printf '\n== %s ==\n' "$1"; }

cd "$SYFUSE_ROOT"

SRC_DIRS="app/src ui/src core domain data engine daemon privileged hook"

# 在源码里检索，但**剔除纯注释行**。
# 没有这一步会误报：注释里解释"本模块不引用 WebView"也会被 grep 命中。
src_grep() {
  local pattern="$1"; shift
  grep -rn --include=*.kt --include=*.java -E "$pattern" "$@" 2>/dev/null \
    | grep -vE ':[0-9]+:[[:space:]]*(//|\*|/\*)' || true
}

# ---------------------------------------------------------------------------
head2 "R1 原生 Compose 主界面，无 WebView"
hits=$(src_grep 'android\.webkit\.|WebView|loadUrl|WebSettings' $SRC_DIRS)
if [ -z "$hits" ]; then
  pass "源码（剔除注释后）无 WebView / loadUrl 引用"
else
  fail "发现 WebView 相关引用："; echo "$hits" | sed 's/^/    /'
fi
# 反向验证：主界面确实是 Compose（否则"没有 WebView"可能是因为压根没界面）
if grep -q "setContent" app/src/main/kotlin/io/github/vipsvip/syfuse/MainActivity.kt 2>/dev/null; then
  pass "MainActivity 通过 setContent 承载 Compose"
else
  fail "MainActivity 未使用 setContent，无法确认是 Compose 主界面"
fi
if grep -rq "compose = true" app/build.gradle.kts ui/build.gradle.kts; then
  pass "Compose 构建开关已开启"
else
  fail "Compose 构建开关缺失"
fi
# 补一条：确认主界面确实由 NavHost + 多个页面组成，而不是一个空壳 Activity
nav_pages=$(src_grep 'composable\(' ui/src | wc -l | tr -d ' ')
if [ "$nav_pages" -ge 4 ]; then
  pass "Compose 导航含 $nav_pages 个页面"
else
  fail "Compose 导航页面不足（实际 $nav_pages，期望 ≥4）"
fi

# ---------------------------------------------------------------------------
head2 "R2 内核静态随包，不在运行时下载二进制"
dl=$(src_grep 'DownloadManager|Runtime\.exec\("(curl|wget)|URL\("https?://[^"]*\.(so|bin)' $SRC_DIRS)
if [ -z "$dl" ]; then
  pass "源码中无运行时下载内核二进制的行为"
else
  fail "疑似运行时下载二进制："; echo "$dl" | sed 's/^/    /'
fi

# 内核接入点：声明了 AAR / jniLibs 才算"随包"
kernel_declared=0
grep -rqE 'libs/.*\.aar|jniLibs' engine/build.gradle.kts app/build.gradle.kts 2>/dev/null && kernel_declared=1
kernel_built=0
if [ -d kernel/out ] && [ -n "$(ls -A kernel/out 2>/dev/null)" ]; then kernel_built=1; fi

if [ "$kernel_built" = "1" ] && [ "$kernel_declared" = "0" ]; then
  # 最危险的状态：内核产出了却没接进去，说明"构建了但没用上"
  fail "kernel/out 已有产物，但 engine/app 未声明 AAR 或 jniLibs 接入点"
elif [ "$kernel_declared" = "1" ]; then
  pass "已声明内核随包接入点（AAR / jniLibs）"
  if [ "$kernel_built" = "0" ]; then
    pend "接入点已声明，但 kernel/out 为空 —— 内核尚未构建"
  fi
else
  pend "内核尚未随包接入（计划里程碑 M3）。当前 engine 绑定 NotLinkedDataPlane，不得对外声称代理可用"
fi

# 内核未接入时，绑定必须确实是 NotLinked —— 防止"嘴上说没接入，代码里却声称已连接"
if grep -q "NotLinkedDataPlane" engine/src/main/kotlin/io/github/vipsvip/syfuse/engine/EngineModule.kt 2>/dev/null; then
  pass "EngineModule 绑定 NotLinkedDataPlane，与内核未接入状态一致"
else
  note "EngineModule 未绑定 NotLinkedDataPlane；若内核已接入请忽略本条"
fi

if src_grep 'https?://[^"]*(umeng|bugly|crashlytics|sentry|firebase|appsflyer|adjust\.com)' $SRC_DIRS | grep -q .; then
  fail "源码中出现遥测/归因域名常量"
else
  pass "源码中无遥测/归因域名常量"
fi

# ---------------------------------------------------------------------------
head2 "R3 零遥测"
TELEMETRY_KEYS='firebase|crashlytics|analytics|sentry|umeng|bugly|appsflyer|adjust|facebook-android|google-services|play-services-ads|ads-identifier|amplitude|mixpanel'
tel=$(grep -inE "$TELEMETRY_KEYS" gradle/libs.versions.toml 2>/dev/null || true)
if [ -z "$tel" ]; then
  pass "版本目录中无遥测/广告 SDK 坐标"
else
  fail "版本目录包含遥测/广告 SDK："; echo "$tel" | sed 's/^/    /'
fi
if [ -f app/src/main/AndroidManifest.xml ]; then
  perms=$(grep -oE 'android:name="android\.permission\.[A-Z_]+"' app/src/main/AndroidManifest.xml | sed 's/.*permission\.//; s/"//' | sort -u)
  note "Manifest 权限：$(echo "$perms" | tr '\n' ' ')"
  bad=$(echo "$perms" | grep -E "AD_ID|READ_PHONE_STATE|QUERY_ALL_PACKAGES|ACCESS_FINE_LOCATION|ACCESS_BACKGROUND_LOCATION|RECORD_AUDIO|READ_SMS|READ_CONTACTS" || true)
  if [ -z "$bad" ]; then
    pass "未申请与功能无关的敏感权限"
  else
    fail "申请了与代理功能无关的敏感权限：$(echo "$bad" | tr '\n' ' ')"
  fi
  if grep -q 'android:allowBackup="false"' app/src/main/AndroidManifest.xml; then
    pass "allowBackup=false（凭据不随系统备份外流）"
  else
    fail "allowBackup 未显式关闭"
  fi
fi
# 应用不应声明 BIND_VPN_SERVICE —— 因为 VpnService 尚未实现。
# 声明了却做不到属于虚假能力声明（R4）。等实现后再放开这条。
if grep -q "BIND_VPN_SERVICE" app/src/main/AndroidManifest.xml 2>/dev/null; then
  if src_grep 'class .*: *VpnService' app/src | grep -q .; then
    pass "声明了 BIND_VPN_SERVICE 且存在 VpnService 实现"
  else
    fail "声明了 BIND_VPN_SERVICE 但没有 VpnService 实现（虚假能力声明）"
  fi
else
  pass "未声明 BIND_VPN_SERVICE（与 VpnService 未实现的状态一致）"
fi

# ---------------------------------------------------------------------------
head2 "R4 不编造：未完成的能力不得声称完成"
# 4a. 成功态文案：只提示，不失败 —— 因为状态映射本身是合法的。
fabricated=$(src_grep '"(已连接|连接成功|已优化|加速中)"' $SRC_DIRS)
if [ -z "$fabricated" ]; then
  pass "无硬编码的成功态文案"
else
  note "以下位置出现成功态文案，请确认由引擎状态驱动而非硬编码："
  echo "$fabricated" | sed 's/^/      /'
fi
# 4b. 版本号单点一致性（规范化后比较：模块侧惯用 vX.Y.Z，应用侧惯用 X.Y.Z）
app_ver=$(grep -oE 'versionName = "[^"]+"' app/build.gradle.kts | head -1 | sed 's/.*"\(.*\)"/\1/')
mod_ver=$(grep -E '^version=' module/module.prop 2>/dev/null | head -1 | cut -d= -f2)
norm() { printf '%s' "${1#v}"; }
note "app.versionName=$app_ver  module.version=$mod_ver"
if [ -n "$app_ver" ] && [ -n "$mod_ver" ] && [ "$(norm "$app_ver")" = "$(norm "$mod_ver")" ]; then
  pass "应用与模块版本号一致（$app_ver == $mod_ver）"
else
  fail "应用与模块版本号不一致（$app_ver vs $mod_ver）"
fi
app_code=$(grep -oE 'versionCode = [0-9]+' app/build.gradle.kts | head -1 | grep -oE '[0-9]+')
mod_code=$(grep -E '^versionCode=' module/module.prop 2>/dev/null | head -1 | cut -d= -f2)
if [ -n "$app_code" ] && [ "$app_code" = "$mod_code" ]; then
  pass "versionCode 一致（$app_code）"
else
  fail "versionCode 不一致（app=$app_code module=$mod_code）"
fi
# 4c. 不允许遗留占位实现
ph=$(src_grep 'TODO\(implement\)|NotImplementedError|throw NotImplemented' $SRC_DIRS)
if [ -z "$ph" ]; then
  pass "无 NotImplemented / TODO(implement) 占位实现"
else
  fail "存在未实现占位："; echo "$ph" | sed 's/^/    /'
fi
# 4d. README 必须声明"尚未交付"清单 —— 防止文档只讲成绩
# README 必须如实披露未完成项。18.7 要求它含「功能可用性报告」；
# 只讲成绩的文档等于编造，所以这里做硬断言。
if [ -f README.md ] && grep -q "功能可用性报告" README.md; then
  pass "README 含「功能可用性报告」"
else
  fail "README 缺少「功能可用性报告」（18.7 要求，只讲成绩的文档等于编造）"
fi
for section in "功能覆盖度对照表" "逐屏界面清单" "规则语法全表" "syfuse.conf 键表" "权限说明"; do
  grep -q "$section" README.md 2>/dev/null && pass "README 含「$section」" \
    || fail "README 缺少「$section」（18.7 要求）"
done
# 覆盖度对照表必须含三态标记
if grep -q "超集增强" README.md && grep -q "不做" README.md; then
  pass "覆盖度对照表含三态标记（复刻/超集增强/不做）"
else
  fail "覆盖度对照表缺少三态标记"
fi

# ---------------------------------------------------------------------------
head2 "R5 凭据不进仓库、不落明文"
if [ -f tools/scan_secrets.py ]; then
  py=""
  for c in "$HOME/.workbuddy-ai/binaries/python/versions"/*/python.exe python3 python; do
    if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then py="$c"; break; fi
  done
  if [ -n "$py" ]; then
    if "$py" tools/scan_secrets.py --quiet; then
      pass "凭据扫描干净"
    else
      fail "凭据扫描命中，见上方输出"
    fi
  else
    fail "找不到 Python，无法执行凭据扫描"
  fi
else
  fail "缺少 tools/scan_secrets.py"
fi
# 注意断言方向：local.properties 是**本机私有**文件（含 SDK 绝对路径），
# 被 .gitignore 排除，因此**干净克隆里它不该存在**。
# 早期版本断言"它必须存在"，在 CI 的干净 checkout 上必然 FAIL ——
# 那是断言写反了，不是环境问题。（CI 第 10 次运行暴露的）
[ -f .gitignore ] && pass ".gitignore 存在" || fail ".gitignore 缺失"
if git ls-files --error-unmatch local.properties >/dev/null 2>&1; then
  fail "local.properties 被 git 跟踪 —— 它会泄漏本机 SDK 绝对路径"
else
  pass "local.properties 未被 git 跟踪"
fi
if git check-ignore -q local.properties 2>/dev/null; then
  pass "local.properties 被 .gitignore 排除"
else
  fail "local.properties 未被 .gitignore 排除"
fi
grep -qE '^\s*\*\.jks|^\s*\*\.keystore|^\s*local\.properties' .gitignore 2>/dev/null \
  && pass ".gitignore 已排除密钥与 local.properties" \
  || fail ".gitignore 未排除密钥文件"

# ---------------------------------------------------------------------------
head2 "R6 危险能力显式披露 + 一键还原"
if [ -f DISCLAIMER.md ]; then
  pass "DISCLAIMER.md 存在"
  grep -qiE "还原|恢复|卸载|restore|uninstall" DISCLAIMER.md && pass "披露文件含还原说明" || fail "披露文件缺少还原说明"
else
  fail "缺少 DISCLAIMER.md"
fi
for f in module/uninstall.sh module/action.sh module/service.sh module/post-fs-data.sh module/customize.sh module/module.prop; do
  [ -s "$f" ] && pass "模块文件存在且非空: $f" || fail "模块文件缺失或为空: $f"
done
grep -qE '^id=syfuse$' module/module.prop 2>/dev/null && pass "module.prop id=syfuse" || fail "module.prop id 不是 syfuse"
grep -qE 'rm -rf|rm -f' module/uninstall.sh 2>/dev/null && pass "uninstall.sh 含清理动作" || fail "uninstall.sh 未做清理"
# 署名必须是**账号**，不能是 AI 工具名或空白。
# 这条是为了防止"顺手把作者写成生成工具"这类看起来无害、实则属于归属造假的问题。
author_line=$(grep -E '^author=' module/module.prop 2>/dev/null | head -1 | cut -d= -f2)
if [ -z "$author_line" ]; then
  fail "module.prop 缺少 author"
elif printf '%s' "$author_line" | grep -qiE 'ai|gpt|claude|copilot|assistant|bot|anthropic|openai|gemini|cursor|codebuddy|workbuddy'; then
  fail "module.prop author 疑似 AI 工具名：$author_line（署名必须是账号）"
else
  pass "module.prop author=$author_line（非 AI 工具名）"
fi
# 包名段必须能从署名规范化得到（连字符去掉 + 小写），保证两者同源
seg_expected=$(printf '%s' "$author_line" | tr '[:upper:]' '[:lower:]' | tr -d -- '-_.')
if grep -q "io\.github\.$seg_expected\.syfuse" app/build.gradle.kts 2>/dev/null; then
  pass "包名段与署名同源（$author_line -> $seg_expected）"
else
  fail "包名段与署名不同源：署名 $author_line 规范化后应为 $seg_expected，但 app 包名不匹配"
fi

# ---------------------------------------------------------------------------
head2 "模块结构完整性（KernelSU / Magisk 规范）"
[ -f module/META-INF/com/google/android/update-binary ] && pass "update-binary 存在" || fail "update-binary 缺失"
[ -f module/META-INF/com/google/android/updater-script ] && pass "updater-script 存在" || fail "updater-script 缺失"
if head -1 module/META-INF/com/google/android/update-binary 2>/dev/null | grep -q '^#!'; then
  pass "update-binary 是 shell 脚本（Magisk 20.x+ 规范）"
else
  fail "update-binary 不是 shell 脚本"
fi
if grep -q '^#MAGISK' module/META-INF/com/google/android/updater-script 2>/dev/null; then
  pass "updater-script 含 #MAGISK 标记"
else
  fail "updater-script 缺少 #MAGISK 标记"
fi
if grep -q 'MODPATH' module/customize.sh 2>/dev/null; then
  pass "customize.sh 使用安装器提供的 \$MODPATH"
else
  fail "customize.sh 未使用 \$MODPATH（安装路径会写错）"
fi
syntax_bad=0
while IFS= read -r s; do
  if ! bash -n "$s" 2>/dev/null; then
    fail "shell 语法错误: $s"; syntax_bad=1
  fi
done < <(find module tests tools -name '*.sh' -type f 2>/dev/null)
[ "$syntax_bad" = "0" ] && pass "全部 shell 脚本通过 bash -n 语法检查"

# 行尾检查：shell 脚本必须是 LF。CRLF 会让 Android 的 sh 报
# "bad interpreter: /system/bin/sh^M"，而且是刷入后才暴露，排查成本极高。
#
# 注意检测方法：本环境（Git Bash）的 `grep -c $'\r'` 会把每一行都算成命中
# （实测 194/194 行"命中"但 tr 数出 0 个 CR 字节）——**必须用 tr 数字节数**。
crlf_bad=0
while IFS= read -r s; do
  n=$(tr -cd '\r' < "$s" | wc -c | tr -d ' ')
  if [ "$n" -gt 0 ]; then
    fail "shell 脚本含 CRLF 行尾（$n 个 CR 字节）: $s"
    crlf_bad=1
  fi
done < <(find module tests tools -name '*.sh' -type f 2>/dev/null)
[ "$crlf_bad" = "0" ] && pass "全部 shell 脚本为 LF 行尾（Android 可执行）"

py=""
for c in "$HOME/.workbuddy-ai/binaries/python/versions"/*/python.exe python3 python; do
  if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then py="$c"; break; fi
done
if [ -f update.json ]; then
  if [ -n "$py" ] && "$py" -c "import json,sys;d=json.load(open('update.json',encoding='utf-8'));sys.exit(0 if all(k in d for k in ('version','versionCode','zipUrl')) else 1)" 2>/dev/null; then
    pass "update.json 合法且含 version/versionCode/zipUrl"
  else
    fail "update.json 缺失、非法或缺字段"
  fi
else
  fail "缺少 update.json"
fi

# ---------------------------------------------------------------------------
head2 "仓库元文件"
for f in LICENSE NOTICE README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md DISCLAIMER.md .gitattributes; do
  [ -s "$f" ] && pass "$f 存在" || fail "$f 缺失或为空"
done
# 提示词 18.1 的根级结构要求
for d in module apk screenshots src tests tools .github; do
  [ -d "$d" ] && pass "目录存在: $d/" || fail "目录缺失: $d/（提示词 18.1 要求）"
done
[ -f package.sh ] && pass "根级 package.sh 存在" || fail "根级 package.sh 缺失（提示词 18.1 要求）"
[ -x package.sh ] || chmod +x package.sh 2>/dev/null
grep -q "GNU GENERAL PUBLIC LICENSE" LICENSE 2>/dev/null && grep -q "Version 3" LICENSE 2>/dev/null \
  && pass "LICENSE 是 GPL-3.0 全文" || fail "LICENSE 不是 GPL-3.0 全文"
if grep -qiE '^#?\s*(The )?MIT License' LICENSE 2>/dev/null; then
  fail "LICENSE 被换成了 MIT —— 这是红线"
else
  pass "LICENSE 未被换成 MIT"
fi
[ -f .github/workflows/ci.yml ] && pass "CI 工作流存在" || pend "CI 工作流缺失"

# ---------------------------------------------------------------------------
head2 "6.4 必须沿用的具体参数（复现硬指标）"
# 6.1 规定「参数同值」是"复现"的四维判定之一。默认值一旦漂移，
# 复现就名存实亡 —— 而默认值是最容易在重构中被顺手改掉的东西。
# 所以这里逐条断言。
CONF="module/config/syfuse.conf"
check_kv() { # $1=键 $2=期望值 $3=依据
  local got
  got="$(grep -E "^$1=" "$CONF" 2>/dev/null | head -1 | cut -d= -f2)"
  if [ "$got" = "$2" ]; then
    pass "6.4 $1=$2（$3）"
  else
    fail "6.4 $1 应为 $2，实际 \"$got\"（$3）"
  fi
}
check_kv PANEL_PORT 8848 "面板端口"
check_kv MITM_PORT 8443 "HTTPS 端口"
check_kv MITM_HTTP_PORT 8080 "HTTP 端口"
check_kv PROXY_PORT 2080 "代理端口"
check_kv DNS_UPSTREAM 1.1.1.1 "DNS 上游"
check_kv DNS_UPSTREAM_ENC "https://dns.alicdn.com/dns-query" "加密上游（URL 形态）"
check_kv DNS_UPDATE_INTERVAL 24 "更新周期（小时）"
check_kv PROXY_SUB_INTERVAL 24 "订阅周期（小时）"
check_kv SCRIPT_UPDATE_INTERVAL 24 "脚本周期（小时）"
check_kv DNS_ENABLE 1 "总开关默认"
check_kv MITM_ENABLE 0 "总开关默认"
check_kv SPLASH_ENABLE 1 "总开关默认"
check_kv NOTIFY_ENABLE 1 "总开关默认"
check_kv PANEL_ENABLE 1 "总开关默认"
check_kv DNSSRV_ENABLE 0 "总开关默认"
check_kv PROXY_ENABLE 0 "总开关默认"
check_kv PROXY_LEAK 0 "总开关默认"
check_kv BLACKLIST_ENABLE 1 "总开关默认"
# RULES_REPO 必须留空（1.2 禁止硬编码账号；实际值由 set_owner.sh 填入）
if grep -qE '^RULES_REPO=$' "$CONF"; then
  pass "6.4 RULES_REPO 留空（账号由 set_owner.sh 运行时填入，未硬编码）"
else
  fail "RULES_REPO 非空 —— 可能硬编码了具体账号（违反 1.2）"
fi
# 开屏跳过延迟 100ms
grep -q "skip 100" module/config/splash_rules.conf 2>/dev/null \
  && pass "6.4 开屏跳过延迟 100ms" || fail "开屏跳过延迟不是 100ms"
# 键的并集：SY 与 SY-Fusion 的键都要能接受
for k in ENGINE_HEURISTIC ENGINE_STRICT_SCOPE ENGINE_CLIENT ENGINE_CTAG HOSTS_EXTRA; do
  grep -qE "^$k=" "$CONF" && pass "并集：含 SY-Fusion 专有键 $k" || fail "并集：缺少 SY-Fusion 专有键 $k"
done

head2 "M1 模块骨架（提示词 13 部分 M1 完成判定）"
# action.sh 必须有 11 个菜单项，一项不少
if [ -f module/action.sh ]; then
  menu_items=$(grep -cE '^ *ui_print " *[0-9]+\)' module/action.sh)
  if [ "$menu_items" -ge 11 ]; then
    pass "action.sh 菜单项 $menu_items 个（要求 ≥11）"
  else
    fail "action.sh 菜单项只有 $menu_items 个（要求 11）"
  fi
  # 每项都要真的接功能：必须出现 syfused 调用或明确的 not_implemented
  if grep -q 'not_implemented' module/action.sh; then
    pass "action.sh 对未实现项有显式 not_implemented 分支（不产生死菜单）"
  else
    fail "action.sh 没有 not_implemented 分支，可能把未实现项伪装成可用"
  fi
  for fn in do_install_apk do_daemon_ctl do_status do_logs do_switch_profile do_cert \
            do_update do_backup do_panel do_uninstall do_selfcheck; do
    grep -q "^$fn()" module/action.sh || fail "action.sh 缺少实现函数 $fn"
  done
  grep -q '^do_selfcheck()' module/action.sh && pass "11 项处理函数齐备" || true
fi
# 还原清单机制
if grep -q 'manifest/installed.json\|MANIFEST' module/customize.sh 2>/dev/null; then
  pass "customize.sh 会创建还原清单"
else
  fail "customize.sh 未创建还原清单 —— 卸载将失去还原依据"
fi
if grep -q 'restorable' module/customize.sh 2>/dev/null; then
  pass "清单条目含 restorable 字段"
else
  fail "清单条目缺少 restorable 字段"
fi
if grep -q 'json_escape' module/customize.sh 2>/dev/null; then
  pass "清单写入前对路径做了 JSON 转义（实测踩过非法转义）"
else
  fail "清单写入未做 JSON 转义，路径含特殊字符时会写出非法 JSON"
fi
# 卸载残留检查脚本
[ -f tests/run_restore_check.sh ] && pass "tests/run_restore_check.sh 存在" || fail "缺少卸载残留检查脚本"
# 守护进程源码
[ -f src/cmd/syfused/main.go ] && pass "守护进程源码存在: src/cmd/syfused" || fail "缺少守护进程源码"
[ -f src/internal/manifest/manifest.go ] && pass "还原清单 Go 实现存在" || fail "缺少还原清单 Go 实现"
[ -f src/internal/ipc/ipc.go ] && pass "UDS 控制通道实现存在" || fail "缺少 UDS 控制通道实现"
# 不可还原的操作必须被代码拒绝
if grep -q 'ErrNotRestorable' src/internal/manifest/manifest.go 2>/dev/null; then
  pass "manifest 对不可还原条目的登记返回错误（硬约束在代码层）"
else
  fail "manifest 未拒绝不可还原条目 —— 提示词 3.5 的硬约束未落地"
fi

# ---------------------------------------------------------------------------
printf '\n===========================================\n'
printf 'module-checks 汇总: PASS=%d FAIL=%d PEND=%d\n' "$PASS" "$FAIL" "$PEND"
if [ "$PEND" -gt 0 ]; then
  printf '注意：PEND 表示里程碑未完成，不是通过。\n'
fi
printf '===========================================\n'
[ "$FAIL" -eq 0 ] || exit 1
exit 0
