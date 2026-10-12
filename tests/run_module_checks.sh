#!/usr/bin/env bash
#
# 模块级静态校验（提示词第 12 部分）。
#
# 这是最关键的合规检查脚本：红线条 R1（无 WebView）、R5（无明文密钥）、
# R2（内核静态内置）这类约束无法靠单测验证，只能静态扫描源码与配置。
#
# 任一检查失败即阻断 —— 因为这些都是「一旦违反就是产品级缺陷」的约束。

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

FAILED=0

fail() {
    echo "  ✗ $1" >&2
    FAILED=$((FAILED + 1))
}

pass() {
    echo "  ✓ $1"
}

echo "==> 模块级静态校验"

# ---------------------------------------------------------------------------
# R1：主界面必须原生，禁止 WebView 承载 UI
# 判据：源码中不得出现 WebView 类、loadUrl 调用、或 WebView 相关的 XML 组件
# ---------------------------------------------------------------------------
echo "[R1] 检查是否使用 WebView 承载界面"

WEBVIEW_HITS=$(grep -rn --include="*.kt" --include="*.java" --include="*.xml" \
    -E "android\.webkit\.WebView|webView\.loadUrl|WebViewClient|WebChromeClient|CordovaWebView|CapacitorWebView" \
    app ui domain data core engine privileged lsposed-bridge 2>/dev/null || true)

if [ -n "${WEBVIEW_HITS}" ]; then
    fail "发现 WebView 使用（违反红线条 R1）："
    echo "${WEBVIEW_HITS}" | sed 's/^/      /'
else
    pass "未发现任何 WebView 使用"
fi

# 混合框架检查：Flutter Web / Cordova / Capacitor 等价于 WebView 承载
HYBRID_HITS=$(grep -rn --include="*.kts" --include="*.gradle" --include="*.json" \
    -E "io\.flutter|org\.apache\.cordova|com\.getcapacitor" \
    . --exclude-dir=build --exclude-dir=.gradle 2>/dev/null || true)

if [ -n "${HYBRID_HITS}" ]; then
    fail "发现混合框架依赖（违反红线条 R1）："
    echo "${HYBRID_HITS}" | sed 's/^/      /'
else
    pass "未发现 Flutter/Cordova/Capacitor 依赖"
fi

# ---------------------------------------------------------------------------
# R2：内核静态内置，不下载内核二进制
# 判据：不得存在运行时下载可执行文件并执行的逻辑
# ---------------------------------------------------------------------------
echo "[R2] 检查是否存在运行时下载内核二进制的逻辑"

EXEC_DOWNLOAD_HITS=$(grep -rn --include="*.kt" \
    -E "Runtime\.getRuntime\(\)\.exec|ProcessBuilder\(" \
    app ui domain data core engine privileged lsposed-bridge 2>/dev/null \
    | grep -vi "// 允许" || true)

if [ -n "${EXEC_DOWNLOAD_HITS}" ]; then
    fail "发现可能执行外部进程的调用，需人工确认非内核下载："
    echo "${EXEC_DOWNLOAD_HITS}" | sed 's/^/      /'
else
    pass "未发现可疑的外部进程执行"
fi

# ---------------------------------------------------------------------------
# R3：零遥测
# 判据：不得引入常见崩溃上报 / 分析 SDK
# ---------------------------------------------------------------------------
echo "[R3] 检查是否引入遥测或崩溃上报 SDK"

TELEMETRY_HITS=$(grep -rn --include="*.kts" \
    -E "firebase-crashlytics|com\.google\.firebase|sentry|bugsnag|umeng|appsflyer|amplitude|mixpanel" \
    . --exclude-dir=build --exclude-dir=.gradle 2>/dev/null || true)

if [ -n "${TELEMETRY_HITS}" ]; then
    fail "发现遥测/崩溃上报 SDK（违反红线条 R3）："
    echo "${TELEMETRY_HITS}" | sed 's/^/      /'
else
    pass "未引入任何遥测或崩溃上报 SDK"
fi

# ---------------------------------------------------------------------------
# R5：密钥不入库 —— 调用独立扫描器
# ---------------------------------------------------------------------------
echo "[R5] 扫描密钥与本地路径"

PYTHON_BIN="${PYTHON_BIN:-python3}"
if ! command -v "${PYTHON_BIN}" >/dev/null 2>&1; then
    PYTHON_BIN="python"
fi

if command -v "${PYTHON_BIN}" >/dev/null 2>&1; then
    if "${PYTHON_BIN}" tools/scan_secrets.py; then
        pass "密钥扫描通过"
    else
        fail "密钥扫描未通过"
    fi
else
    fail "未找到 Python，无法运行密钥扫描（不允许跳过此检查）"
fi

# ---------------------------------------------------------------------------
# 包名与标识一致性
# ---------------------------------------------------------------------------
echo "[标识] 检查包名与账号标识一致性"

EXPECTED_PKG="io.github.vipsvip.syfuse"
if grep -q "applicationId = \"${EXPECTED_PKG}\"" app/build.gradle.kts; then
    pass "applicationId = ${EXPECTED_PKG}"
else
    fail "app/build.gradle.kts 的 applicationId 不是 ${EXPECTED_PKG}"
fi

# 检查是否残留占位账号名
PLACEHOLDER_ACCOUNT=$(grep -rn --include="*.kt" --include="*.kts" --include="*.prop" \
    -E "\byourname\b|\bexample-owner\b|\buser-owner\b" \
    . --exclude-dir=build --exclude-dir=.gradle --exclude-dir=tools 2>/dev/null || true)

if [ -n "${PLACEHOLDER_ACCOUNT}" ]; then
    fail "发现占位账号名："
    echo "${PLACEHOLDER_ACCOUNT}" | sed 's/^/      /'
else
    pass "未发现占位账号名"
fi

# ---------------------------------------------------------------------------
# ABI 配置
# ---------------------------------------------------------------------------
echo "[ABI] 检查 ABI 过滤与 splits 配置"

for abi in arm64-v8a armeabi-v7a x86_64; do
    if grep -q "\"${abi}\"" app/build.gradle.kts; then
        pass "ABI ${abi} 已配置"
    else
        fail "ABI ${abi} 未在 app/build.gradle.kts 中配置"
    fi
done

# ---------------------------------------------------------------------------
# 字符串资源完整性（中英双语必须键一致）
# ---------------------------------------------------------------------------
echo "[i18n] 检查中英字符串资源键一致性"

EN_STRINGS="ui/src/main/res/values/strings.xml"
ZH_STRINGS="ui/src/main/res/values-zh/strings.xml"

if [ -f "${EN_STRINGS}" ] && [ -f "${ZH_STRINGS}" ]; then
    # 提取 name="..." 并排序比对
    EN_KEYS=$(grep -oE 'name="[^"]+"' "${EN_STRINGS}" | sort -u)
    ZH_KEYS=$(grep -oE 'name="[^"]+"' "${ZH_STRINGS}" | sort -u)

    MISSING_IN_ZH=$(comm -23 <(echo "${EN_KEYS}") <(echo "${ZH_KEYS}") || true)
    MISSING_IN_EN=$(comm -13 <(echo "${EN_KEYS}") <(echo "${ZH_KEYS}") || true)

    if [ -n "${MISSING_IN_ZH}" ]; then
        fail "中文资源缺失以下键："
        echo "${MISSING_IN_ZH}" | sed 's/^/      /'
    fi
    if [ -n "${MISSING_IN_EN}" ]; then
        fail "英文资源缺失以下键："
        echo "${MISSING_IN_EN}" | sed 's/^/      /'
    fi
    if [ -z "${MISSING_IN_ZH}" ] && [ -z "${MISSING_IN_EN}" ]; then
        pass "中英字符串键完全一致（$(echo "${EN_KEYS}" | wc -l | tr -d ' ') 个）"
    fi
else
    fail "字符串资源文件缺失"
fi

# ---------------------------------------------------------------------------
# 汇总
# ---------------------------------------------------------------------------
echo
if [ "${FAILED}" -gt 0 ]; then
    echo "==> 模块级校验失败：${FAILED} 项" >&2
    exit 1
fi

echo "==> 模块级校验全部通过"
