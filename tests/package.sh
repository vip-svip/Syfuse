#!/usr/bin/env bash
# 打包：产出可分发的 APK + KernelSU/Magisk 模块 zip + SHA256 清单。
#
# 产物（全部落在 dist/）：
#   Syfuse-v<ver>-<abi>.apk          分 ABI 的应用包
#   Syfuse-Module-v<ver>.zip         模块包（内含按架构的内核二进制与 APK）
#   MANIFEST.sha256                  逐文件 SHA256
#   package_report.txt               本次打包的原始记录
#
# 设计要点（R2）：模块 zip 里**必须**包含内核二进制（若已构建），
# 安装期不下载任何东西。若内核尚未构建，脚本会**明确打印**"未随包提供内核"，
# 而不是静默产出一个看似完整的包。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
cd "$SYFUSE_ROOT"

VERSION="$(grep -oE 'versionName = "[^"]+"' app/build.gradle.kts | head -1 | sed 's/.*"\(.*\)"/\1/')"
[ -n "$VERSION" ] || syfuse_die "无法从 app/build.gradle.kts 解析 versionName"

DIST="$SYFUSE_ROOT/dist"
STAGE="$DIST/.stage"
REPORT="$DIST/package_report.txt"

rm -rf "$STAGE"
mkdir -p "$DIST" "$STAGE"

exec > >(tee "$REPORT") 2>&1
echo "=== Syfuse 打包 $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
echo "版本: $VERSION"
echo

# ---------------------------------------------------------------------------
echo "---- 1/5 构建 release APK ----"
syfuse_gradle :app:assembleRelease --console=plain || syfuse_die "release 构建失败"

APK_DIR="$SYFUSE_ROOT/app/build/outputs/apk/release"
shopt -s nullglob
apks=("$APK_DIR"/*.apk)
[ "${#apks[@]}" -gt 0 ] || syfuse_die "没有产出 release APK"

echo
echo "---- 2/5 归集 APK ----"
for apk in "${apks[@]}"; do
  base="$(basename "$apk")"
  # AGP 的 ABI 产物名形如 app-arm64-v8a-release.apk
  abi="$(printf '%s' "$base" | sed -nE 's/.*-(arm64-v8a|armeabi-v7a|x86_64|x86)-release\.apk/\1/p')"
  [ -n "$abi" ] || abi="universal"
  out="$DIST/Syfuse-v$VERSION-$abi.apk"
  cp -f "$apk" "$out"
  printf '  %-44s %s\n' "$(basename "$out")" "$(stat -c '%s' "$out" 2>/dev/null || stat -f '%z' "$out") 字节"
  cp -f "$apk" "$STAGE/syfuse-$abi.apk"
done

# ---------------------------------------------------------------------------
echo
echo "---- 3/5 组装模块包 ----"
MOD="$STAGE/module"
mkdir -p "$MOD"
# 模块骨架：整目录拷贝（含 META-INF），保证与 KernelSU 规范一致
cp -a "$SYFUSE_ROOT/module/." "$MOD/"

# 内核二进制（若 M2 已构建）
KERNEL_DIR="$SYFUSE_ROOT/kernel/out"
kernel_found=0
if [ -d "$KERNEL_DIR" ]; then
  mkdir -p "$MOD/bin"
  for f in "$KERNEL_DIR"/syfuse-*; do
    [ -f "$f" ] || continue
    cp -f "$f" "$MOD/bin/"
    kernel_found=1
    echo "  内核: $(basename "$f")"
  done
fi
if [ "$kernel_found" = "0" ]; then
  echo "  !! 未随包提供内核二进制（kernel/out 不存在或为空）"
  echo "  !! 本模块包只包含特权层骨架。这是 M2 的**已知缺口**，不得声称内核可用。"
fi

# 把 APK 一并放进模块，让模块安装时能顺带装应用
cp -f "$DIST"/Syfuse-v"$VERSION"-*.apk "$MOD/" 2>/dev/null || true

chmod 0755 "$MOD"/*.sh 2>/dev/null || true
chmod 0755 "$MOD/META-INF/com/google/android/update-binary" 2>/dev/null || true

ZIP="$DIST/Syfuse-Module-v$VERSION.zip"
rm -f "$ZIP"
( cd "$MOD" && zip -q -r -X "$ZIP" . -x '*.DS_Store' -x '*__MACOSX*' )
echo "  $(basename "$ZIP"): $(stat -c '%s' "$ZIP" 2>/dev/null || stat -f '%z' "$ZIP") 字节"
echo "  内容清单:"
unzip -Z1 "$ZIP" | sed 's/^/    /'

# ---------------------------------------------------------------------------
echo
echo "---- 4/5 生成 SHA256 清单 ----"
MANIFEST="$DIST/MANIFEST.sha256"
( cd "$DIST" && find . -maxdepth 1 -type f \( -name '*.apk' -o -name '*.zip' \) -printf '%P\n' 2>/dev/null \
    | sort | while IFS= read -r f; do sha256sum "$f"; done ) > "$MANIFEST"
# Windows/Git-Bash 的 find 可能不支持 -printf，回退到 shell 展开
if [ ! -s "$MANIFEST" ]; then
  ( cd "$DIST" && for f in *.apk *.zip; do [ -f "$f" ] && sha256sum "$f"; done ) > "$MANIFEST"
fi
cat "$MANIFEST" | sed 's/^/  /'

# ---------------------------------------------------------------------------
echo
echo "---- 5/5 自校验 ----"
py=""
for c in "$HOME/.workbuddy-ai/binaries/python/versions"/*/python.exe python3 python; do
  if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then py="$c"; break; fi
done
if [ -n "$py" ] && [ -f tools/verify_files.py ]; then
  "$py" tools/verify_files.py "$MANIFEST" || syfuse_die "产物清单自校验失败"
else
  echo "  [WARN] 无 Python，跳过清单自校验"
fi

rm -rf "$STAGE"

echo
echo "=== 打包完成 ==="
echo "产物目录: $DIST"
ls -1 "$DIST" | sed 's/^/  /'
if [ "$kernel_found" = "0" ]; then
  echo
  echo "提醒：本次未包含内核二进制，模块包不具备实际代理能力。"
  exit 0
fi
