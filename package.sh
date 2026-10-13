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

# Python 解释器定位。打包（便携 zip 回退）与清单自校验共用，因此提前解析。
# 找不到 Python 不是致命错误，但会让便携打包回退不可用 —— 由调用点各自判定。
py=""
for c in "$HOME/.workbuddy-ai/binaries/python/versions"/*/python.exe python3 python; do
  if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then py="$c"; break; fi
done

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

# 便携打包。`zip` 并非到处都有 —— Windows Git Bash 默认就没有。
# 缺它会让**模块 ZIP 静默缺失**，而模块 ZIP 正是本项目的交付主体（提示词 3.5），
# 因此这里必须有等价回退，并且两条路径都失败时要硬报错，不能继续走成功分支。
#
# ⚠️ 可执行位不能依赖文件系统：本仓库主要在 **Windows/NTFS** 上打包，而 NTFS 没有
# Unix 权限位 —— `os.stat().st_mode` 恒为 0666，上面那几条 `chmod` 在 Windows 上是空操作。
# 实测后果：早期产物里 action.sh / customize.sh / bin/syfused-* 全部是 0666，
# 刷进设备后脚本无法执行、守护进程无法启动，**模块形同废包**。
# 因此这里改成按**显式白名单**判定：该是 0755 的条目一律写 0755，与宿主文件系统无关。
zip_ok=0
if command -v zip >/dev/null 2>&1; then
  if ( cd "$MOD" && zip -q -r -X "$ZIP" . -x '*.DS_Store' -x '*__MACOSX*' ) && [ -f "$ZIP" ]; then
    zip_ok=1
  fi
fi
if [ "$zip_ok" -ne 1 ] && [ -n "$py" ]; then
  if "$py" - "$MOD" "$ZIP" <<'PY'
import os, sys, zipfile

src, out = sys.argv[1], sys.argv[2]

def mode_for(rel: str) -> int:
    """返回该条目在模块包内应有的 Unix 权限位。

    不读 st_mode —— 见上方注释：NTFS 上它没有意义。
    规则与 KernelSU/Magisk 的安装器期望一致：
      - 所有 *.sh（customize/post-fs-data/service/uninstall/action）及
        META-INF/.../update-binary  -> 0755（安装器要执行它们）
      - bin/syfused-*             -> 0755（守护进程二进制）
      - 其余（配置、module.prop、内置 APK）-> 0644
    """
    base = os.path.basename(rel)
    if rel == 'META-INF/com/google/android/update-binary':
        return 0o755
    if base.endswith('.sh'):
        return 0o755
    if rel.startswith('bin/') and base.startswith('syfused-'):
        return 0o755
    return 0o644

with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as z:
    for root, dirs, files in os.walk(src):
        dirs[:] = sorted(d for d in dirs if d != '__MACOSX')
        for name in sorted(files):
            if name == '.DS_Store':
                continue
            full = os.path.join(root, name)
            rel = os.path.relpath(full, src).replace(os.sep, '/')
            zi = zipfile.ZipInfo(rel)
            # 目录位 (0o40000) | 权限位，左移 16 位即 Unix mode 的高 16 位。
            zi.external_attr = (0o100000 | mode_for(rel)) << 16
            zi.compress_type = zipfile.ZIP_DEFLATED
            zi.date_time = (1980, 1, 1, 0, 0, 0)  # 固定时间戳：让打包可复现
            with open(full, 'rb') as fh:
                z.writestr(zi, fh.read())

print(f"  已写出 {len(zipfile.ZipFile(out).namelist())} 个条目，权限位按白名单显式写入")
PY
  then
    [ -f "$ZIP" ] && zip_ok=1
  fi
fi

# 可执行位自检：读回刚写的 ZIP 断言关键条目必须是 0755。
# 这条检查存在的理由是"权限位曾经整个丢过"——不在打包现场断言，就要等刷机才发现。
if [ "$zip_ok" -eq 1 ] && [ -n "$py" ]; then
  "$py" - "$ZIP" <<'PY' || zip_ok=0
import sys, zipfile
z = zipfile.ZipFile(sys.argv[1])
need = [
    'customize.sh', 'post-fs-data.sh', 'service.sh', 'uninstall.sh', 'action.sh',
    'META-INF/com/google/android/update-binary',
    'bin/syfused-arm64', 'bin/syfused-arm', 'bin/syfused-amd64',
]
bad = []
for n in need:
    try:
        i = z.getinfo(n)
    except KeyError:
        bad.append(f'{n}: 缺失')
        continue
    m = (i.external_attr >> 16) & 0o777
    if m != 0o755:
        bad.append(f'{n}: {m:04o} (应为 0755)')
if bad:
    print('  [FAIL] 可执行位自检未通过:', file=sys.stderr)
    for b in bad:
        print('    ' + b, file=sys.stderr)
    sys.exit(1)
print('  [PASS] 可执行位自检：9 个关键条目均为 0755')
PY
fi

if [ "$zip_ok" -ne 1 ]; then
  warn_missing "syfuse-$VERSION.zip（模块包）" \
    "模块 ZIP 未生成（zip 缺失且 Python 回退不可用）。提示词 3.5 的交付主体缺失，本次产物不可发布。"
else
  echo "  $(basename "$ZIP"): $(stat -c '%s' "$ZIP" 2>/dev/null || stat -f '%z' "$ZIP" 2>/dev/null || echo '?') 字节"
  echo "  内容清单:"
  if command -v unzip >/dev/null 2>&1; then
    unzip -Z1 "$ZIP" | sed 's/^/    /'
  elif [ -n "$py" ]; then
    "$py" -c "import sys,zipfile;print('\n'.join('    '+n for n in zipfile.ZipFile(sys.argv[1]).namelist()))" "$ZIP"
  fi
fi

# ---------------------------------------------------------------------------
echo
echo "---- 5/6 SHA-256 与 update.json ----"
MANIFEST="$DIST/MANIFEST.sha256"
( cd "$DIST" && for f in *.apk *.zip; do [ -f "$f" ] && sha256sum "$f"; done ) > "$MANIFEST"
sed 's/^/  /' "$MANIFEST"

ZIP_SHA=""
if [ -f "$ZIP" ]; then
  ZIP_SHA="$(sha256sum "$ZIP" | awk '{print $1}')"
fi
if [ -n "$ZIP_SHA" ] && [ -n "$py" ]; then
  # 账号从 git remote 解析（与 tools/set_owner.sh 同源），不硬编码具体账号。
  OWNER="$(git config --get remote.origin.url 2>/dev/null \
    | sed -E 's#(git@|https?://)[^/:]+[:/]##; s#/.*##; s#\.git$##')"
  [ -n "$OWNER" ] || OWNER="vip-svip"
  "$py" - "$SYFUSE_ROOT/update.json" "$VERSION" "$ZIP_SHA" "$OWNER" <<'PY'
import json, sys
path, ver, sha, owner = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
with open(path, encoding='utf-8') as f:
    d = json.load(f)
d['version'] = 'v' + ver
d['zipUrl'] = f"https://github.com/vip-svip/Syfuse/releases/latest/download/syfuse-{ver}.zip"
d['changelog'] = f"https://raw.githubusercontent.com/vip-svip/Syfuse/main/CHANGELOG.md"
d['sha256'] = sha
# newline='' + 显式 '\n'：Windows 上默认会把 '\n' 翻成 CRLF，破坏仓库的 LF 约定。
with open(path, 'w', encoding='utf-8', newline='') as f:
    json.dump(d, f, ensure_ascii=False, indent=2)
    f.write('\n')
print(f"  update.json 已更新: owner={owner} version=v{ver} sha256={sha[:16]}…")
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
  exit 1
fi
exit 0
