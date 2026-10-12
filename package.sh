#!/usr/bin/env bash
# ============================================================================
#  Syfuse 打包（提示词 18.1 / 18.5）
#
#  产物（全部落在 dist/）：
#    Syfuse-v<ver>-<abi>.apk       分 ABI 的应用包
#    syfuse-<ver>.zip              模块包（含按架构的守护进程二进制 + 内置 APK）
#    MANIFEST.sha256               逐文件 SHA256
#    package_report.txt            本次打包的原始记录
#
#  设计要点：
#   - 模块 ZIP 里**必须**包含守护进程二进制（若已构建）与内置 APK。
#     缺失时脚本会明确打印缺什么，而不是产出一个看似完整的包（R2）。
#   - 打包结束跑 SHA-256 自校验，失败即非零退出。
#   - 更新 update.json 的 sha256 字段（自更新链路要求，18.5）。
#
#  退出码：0 成功 / 1 打包失败 / 2 环境缺失
# ============================================================================
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/tests/env.sh"
cd "$SYFUSE_ROOT"

VERSION="$(grep -oE 'versionName = "[^"]+"' app/build.gradle.kts | head -1 | sed 's/.*"\(.*\)"/\1/')"
[ -n "$VERSION" ] || { echo "ERROR: 无法从 app/build.gradle.kts 解析 versionName" >&2; exit 2; }

DIST="$SYFUSE_ROOT/dist"
STAGE="$DIST/.stage"
REPORT="$DIST/package_report.txt"
rm -rf "$STAGE"
mkdir -p "$DIST" "$STAGE"

exec > >(tee "$REPORT") 2>&1
echo "=== Syfuse 打包 $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
echo "版本: $VERSION"
echo

missing=0
warn_missing() { echo "  !! 缺失：$1"; echo "     $2"; missing=$((missing+1)); }

# ---------------------------------------------------------------------------
echo "---- 1/6 构建 release APK ----"
syfuse_gradle :app:assembleRelease --console=plain || { echo "release 构建失败" >&2; exit 1; }

APK_DIR="$SYFUSE_ROOT/app/build/outputs/apk/release"
shopt -s nullglob
apks=("$APK_DIR"/*.apk)
[ "${#apks[@]}" -gt 0 ] || { echo "没有产出 release APK" >&2; exit 1; }

# ---------------------------------------------------------------------------
echo
echo "---- 2/6 归集 APK ----"
mkdir -p "$SYFUSE_ROOT/apk"
for apk in "${apks[@]}"; do
  base="$(basename "$apk")"
  abi="$(printf '%s' "$base" | sed -nE 's/.*-(arm64-v8a|armeabi-v7a|x86_64|x86)-release(-unsigned)?\.apk/\1/p')"
  [ -n "$abi" ] || abi="universal"
  out="$DIST/Syfuse-v$VERSION-$abi.apk"
  cp -f "$apk" "$out"
  printf '  %-46s %s 字节\n' "$(basename "$out")" "$(stat -c '%s' "$out" 2>/dev/null || stat -f '%z' "$out")"
  cp -f "$apk" "$STAGE/syfuse-$abi.apk"
done

# ---------------------------------------------------------------------------
echo
echo "---- 3/6 交叉编译守护进程（src/ -> module/bin/） ----"
GO_BIN="${SYFUSE_GO:-}"
if [ -z "$GO_BIN" ]; then
  for c in "$HOME/.workbuddy-ai/toolchain/go/bin/go" "$HOME/.workbuddy-ai/toolchain/go/bin/go.exe" go; do
    if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then GO_BIN="$c"; break; fi
  done
fi

BIN_STAGE="$STAGE/bin"
mkdir -p "$BIN_STAGE"
built_any=0
if [ -n "$GO_BIN" ]; then
  export GOROOT="${GOROOT:-$(cd "$(dirname "$GO_BIN")/.." && pwd)}"
  export GOFLAGS="${GOFLAGS:--mod=mod}"
  export GOPROXY="${GOPROXY:-off}"
  export GOCACHE="${GOCACHE:-$HOME/.workbuddy-ai/toolchain/go-cache}"
  export GOPATH="${GOPATH:-$HOME/.workbuddy-ai/toolchain/go-path}"
  export CGO_ENABLED=0

  LDFLAGS="-s -w -X main.buildVersion=$VERSION -X main.buildCommit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown) -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  # GOOS=android 需要 NDK 的 cgo；本项目守护进程是纯 stdlib、无 cgo，
  # 因此 GOOS=linux + CGO_ENABLED=0 产出的静态二进制可直接在 Android 上运行。
  # 这是有依据的选择，不是"凑合"；README 的偏离登记里说明了这一点。
  for pair in "arm64:arm64" "arm:arm" "amd64:amd64"; do
    name="${pair%%:*}"; arch="${pair##*:}"
    out="$BIN_STAGE/syfused-$name"
    if ( cd "$SYFUSE_ROOT/src" && GOOS=linux GOARCH="$arch" "$GO_BIN" build -trimpath -ldflags "$LDFLAGS" -o "$out" ./cmd/syfused ); then
      printf '  syfused-%-6s %s 字节\n' "$name" "$(stat -c '%s' "$out" 2>/dev/null || stat -f '%z' "$out")"
      built_any=1
    else
      echo "  构建 syfused-$name 失败"
    fi
  done
else
  warn_missing "Go 工具链" "设置 SYFUSE_GO，或把 Go 放到 ~/.workbuddy-ai/toolchain/go"
fi
if [ "$built_any" = "0" ]; then
  warn_missing "module/bin/syfused-{arm64,arm,amd64}" \
    "守护进程二进制未随包。模块包将只有骨架，**不具备代理能力**。"
fi

# ---------------------------------------------------------------------------
echo
echo "---- 4/6 组装模块包 ----"
MOD="$STAGE/module"
mkdir -p "$MOD"
# 模块骨架（含 META-INF）；bin/ 与 webroot/ 里的 README 是源码树占位，不打包
cp -a "$SYFUSE_ROOT/module/." "$MOD/"
rm -f "$MOD/bin/README.md" "$MOD/webroot/README.md"
if [ -d "$BIN_STAGE" ] && [ -n "$(ls -A "$BIN_STAGE" 2>/dev/null)" ]; then
  mkdir -p "$MOD/bin"
  cp -f "$BIN_STAGE"/syfused-* "$MOD/bin/"
fi
# 内置软件（sidecar APK）：提示词 3.1 的产品形态要求
if [ -n "$(ls -A "$STAGE"/*.apk 2>/dev/null)" ]; then
  mkdir -p "$MOD/apk"
  first_apk="$(ls -1 "$STAGE"/syfuse-*.apk 2>/dev/null | head -1)"
  cp -f "$first_apk" "$MOD/apk/syfuse.apk"
  echo "  内置 APK: $(basename "$first_apk") -> apk/syfuse.apk"
else
  warn_missing "apk/syfuse.apk（内置软件）" \
    "模块包里没有 APK。提示词 3.1 的产品形态要求模块内置 APK。"
fi

chmod 0755 "$MOD"/*.sh 2>/dev/null || true
[ -d "$MOD/bin" ] && chmod 0755 "$MOD/bin"/syfused-* 2>/dev/null
chmod 0755 "$MOD/META-INF/com/google/android/update-binary" 2>/dev/null || true

ZIP="$DIST/syfuse-$VERSION.zip"
rm -f "$ZIP"
( cd "$MOD" && zip -q -r -X "$ZIP" . -x '*.DS_Store' -x '*__MACOSX*' )
echo "  $(basename "$ZIP"): $(stat -c '%s' "$ZIP" 2>/dev/null || stat -f '%z' "$ZIP") 字节"
echo "  内容清单:"
unzip -Z1 "$ZIP" | sed 's/^/    /'

# ---------------------------------------------------------------------------
echo
echo "---- 5/6 SHA-256 与 update.json ----"
MANIFEST="$DIST/MANIFEST.sha256"
( cd "$DIST" && for f in *.apk *.zip; do [ -f "$f" ] && sha256sum "$f"; done ) > "$MANIFEST"
sed 's/^/  /' "$MANIFEST"

ZIP_SHA="$(sha256sum "$ZIP" | awk '{print $1}')"
py=""
for c in "$HOME/.workbuddy-ai/binaries/python/versions"/*/python.exe python3 python; do
  if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then py="$c"; break; fi
done
if [ -n "$ZIP_SHA" ] && [ -n "$py" ]; then
  "$py" - "$SYFUSE_ROOT/update.json" "$VERSION" "$ZIP_SHA" <<'PY'
import json, sys
path, ver, sha = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path, encoding='utf-8') as f:
    d = json.load(f)
d['version'] = 'v' + ver
d['zipUrl'] = f"https://github.com/vip-svip/Syfuse/releases/latest/download/syfuse-{ver}.zip"
d['sha256'] = sha
with open(path, 'w', encoding='utf-8') as f:
    json.dump(d, f, ensure_ascii=False, indent=2)
    f.write('\n')
print(f"  update.json 已更新: version=v{ver} sha256={sha[:16]}…")
PY
else
  echo "  [WARN] 无 Python 或无 zip 哈希，跳过 update.json 更新"
fi

# ---------------------------------------------------------------------------
echo
echo "---- 6/6 自校验 ----"
if [ -n "$py" ] && [ -f tools/verify_files.py ]; then
  "$py" tools/verify_files.py "$MANIFEST" || { echo "产物清单自校验失败" >&2; exit 1; }
else
  echo "  [WARN] 无 Python，跳过清单自校验"
fi

rm -rf "$STAGE"

echo
echo "=== 打包完成 ==="
echo "产物目录: $DIST"
ls -1 "$DIST" | sed 's/^/  /'

if [ "$missing" -gt 0 ]; then
  echo
  echo "==========================================================="
  echo " 本次打包有 $missing 项缺失，产物**不完整**。"
  echo " 不要把它当作可用的发布物 —— 见上方 '!!' 标记。"
  echo "==========================================================="
fi
exit 0
