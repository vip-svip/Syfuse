#!/usr/bin/env bash
#
# 打包与校验（提示词 16.5）。
#
# 流程：构建 → 分 ABI APK → 模块 ZIP → 校验 ZIP 完整性 → 产出 SHA-256 → dist/
#
# 当前状态（M1）：**构建步骤会失败**，因为本机无 Android SDK 与 JDK。
# 但脚本的**结构与校验逻辑**已完整实现，在 CI（Linux runner）上可直接使用。
# 这一点与 run_e2e.sh 的处理不同：e2e 是「逻辑未实现」，
# 本脚本是「逻辑已实现但当前环境缺工具链」——所以它如实报错而非硬编码失败。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# 本脚本位于仓库根目录，因此 PROJECT_ROOT 就是脚本所在目录本身。
# （先前误写成 ".."，导致在有空格的工作区路径下解析到父目录，报 No such file or directory。）
PROJECT_ROOT="${SCRIPT_DIR}"
cd "${PROJECT_ROOT}"

# 从 gradle 配置读取版本号，避免版本号在多处硬编码。
# 注意：Windows 的 Git Bash 自带 grep 可能不支持 -P（PCRE），
# 因此这里用 sed 而非 grep -oP，保证在 CI（Linux）与本地（Windows）下行为一致。
VERSION_NAME=$(sed -n 's/.*versionName[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' app/build.gradle.kts | head -1)
VERSION_CODE=$(sed -n 's/.*versionCode[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p' app/build.gradle.kts | head -1)

if [ -z "${VERSION_NAME}" ] || [ -z "${VERSION_CODE}" ]; then
    echo "错误：无法从 app/build.gradle.kts 解析版本号" >&2
    exit 1
fi

DIST_DIR="${PROJECT_ROOT}/dist"
MODULE_ZIP="${DIST_DIR}/syfuse-${VERSION_NAME}.zip"

echo "==> Syfuse 打包"
echo "    版本：${VERSION_NAME} (code ${VERSION_CODE})"

# ---------------------------------------------------------------------------
# 步骤 1：构建 release APK（分 ABI）
# ---------------------------------------------------------------------------
echo "[1/5] 构建 release APK"

if [ ! -f "./gradlew" ]; then
    echo "错误：未找到 gradlew，无法构建。" >&2
    echo "      CI 环境应先运行 setup-java 与 setup-android，再执行本脚本。" >&2
    exit 1
fi

./gradlew :app:assembleRelease

# ---------------------------------------------------------------------------
# 步骤 2：构建模块 ZIP
# ---------------------------------------------------------------------------
echo "[2/5] 构建 KernelSU/Magisk 模块 ZIP"

mkdir -p "${DIST_DIR}"

if [ ! -d "${PROJECT_ROOT}/module" ]; then
    echo "错误：module/ 目录不存在" >&2
    exit 1
fi

# 在 module/ 内打包，保证 ZIP 内是相对路径（模块管理器要求）。
# 优先用 zip 命令；不可用时回退到 tools/zip_module.py ——
# Windows 的 Git Bash 通常不带 zip，而 CI（Linux）带，两条路径都必须可用。
#
# 注意：回退路径**不**使用内联 heredoc 传参。当工作区路径包含空格时
# （本项目实际工作区路径即如此），heredoc 的参数会被 shell 按空格拆开，
# 导致 module 目录解析错误并静默产出空 ZIP。独立脚本 + 显式文件数断言可杜绝。
if command -v zip >/dev/null 2>&1; then
    (cd "${PROJECT_ROOT}/module" && zip -r -q "${MODULE_ZIP}" . -x "*.DS_Store" "*__MACOSX*")
else
    echo "  zip 命令不可用，使用 Python zipfile 回退"
    PYTHON_BIN="${PYTHON_BIN:-python3}"
    command -v "${PYTHON_BIN}" >/dev/null 2>&1 || PYTHON_BIN="python"

    if ! command -v "${PYTHON_BIN}" >/dev/null 2>&1; then
        echo "错误：zip 与 Python 均不可用，无法打包" >&2
        exit 1
    fi

    "${PYTHON_BIN}" "${PROJECT_ROOT}/tools/zip_module.py" "${PROJECT_ROOT}" "${MODULE_ZIP}"
fi

# 无论走哪条路径，都校验 ZIP 非空 —— 空包是合法 ZIP，unzip -t 无法发现，
# 但会让模块管理器安装失败且原因难查。
ZIP_FILE_COUNT=$("${PYTHON_BIN:-python3}" -c "
import sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as z:
    print(len([n for n in z.namelist() if not n.endswith('/')]))
" "${MODULE_ZIP}" 2>/dev/null || echo 0)

if [ "${ZIP_FILE_COUNT}" -lt 8 ]; then
    echo "错误：模块 ZIP 仅含 ${ZIP_FILE_COUNT} 个文件，疑似打包不完整" >&2
    exit 1
fi

echo "  ✓ 模块 ZIP 含 ${ZIP_FILE_COUNT} 个文件"

# ---------------------------------------------------------------------------
# 步骤 3：校验 ZIP 完整性
# ---------------------------------------------------------------------------
echo "[3/5] 校验 ZIP 完整性"

if ! unzip -t "${MODULE_ZIP}" >/dev/null 2>&1; then
    echo "错误：模块 ZIP 损坏：${MODULE_ZIP}" >&2
    exit 1
fi

# 校验模块必需文件齐备（提示词 6.5 列出的骨架清单）
REQUIRED_MODULE_FILES=(
    "module.prop"
    "customize.sh"
    "post-fs-data.sh"
    "service.sh"
    "action.sh"
    "uninstall.sh"
)

ZIP_CONTENTS=$(unzip -l "${MODULE_ZIP}" | awk '{print $4}')
MISSING=0
for required in "${REQUIRED_MODULE_FILES[@]}"; do
    if ! echo "${ZIP_CONTENTS}" | grep -qx "${required}"; then
        echo "  ✗ 模块 ZIP 缺少必需文件：${required}" >&2
        MISSING=$((MISSING + 1))
    fi
done

if [ "${MISSING}" -gt 0 ]; then
    echo "错误：模块 ZIP 缺 ${MISSING} 个必需文件" >&2
    exit 1
fi

echo "  ✓ 模块 ZIP 结构完整"

# ---------------------------------------------------------------------------
# 步骤 4：产出 SHA-256
# ---------------------------------------------------------------------------
echo "[4/5] 计算 SHA-256"

# 用 sha256sum（Linux/CI）或 shasum（macOS）或 certutil（Windows）
if command -v sha256sum >/dev/null 2>&1; then
    MODULE_SHA=$(sha256sum "${MODULE_ZIP}" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
    MODULE_SHA=$(shasum -a 256 "${MODULE_ZIP}" | awk '{print $1}')
else
    echo "错误：未找到 sha256sum 或 shasum" >&2
    exit 1
fi

echo "${MODULE_SHA}  $(basename "${MODULE_ZIP}")" > "${DIST_DIR}/syfuse-${VERSION_NAME}.zip.sha256"

for apk in "${PROJECT_ROOT}"/app/build/outputs/apk/release/*.apk; do
    [ -f "${apk}" ] || continue
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "${apk}"
    else
        shasum -a 256 "${apk}"
    fi
done > "${DIST_DIR}/apk-sha256.txt"

echo "  模块 SHA-256：${MODULE_SHA}"

# ---------------------------------------------------------------------------
# 步骤 5：汇总
# ---------------------------------------------------------------------------
echo "[5/5] 产出清单"
ls -la "${DIST_DIR}"

cat <<EOF

==> 打包完成

产物：
  ${MODULE_ZIP}
  ${DIST_DIR}/syfuse-${VERSION_NAME}.zip.sha256
  ${DIST_DIR}/apk-sha256.txt

后续步骤（提示词 16.5）：
  1. 用 zipUrl / sha256 更新 update.json
  2. 运行 tools/verify_remote.py 校验远程树与本地 HEAD
  3. 运行 tools/verify_files.py 比对关键文件
  4. 创建 Release，说明中附完整 SHA-256

模块 ZIP SHA-256：${MODULE_SHA}
EOF
