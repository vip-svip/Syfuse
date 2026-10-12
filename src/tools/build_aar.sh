#!/usr/bin/env bash
# 构建内核 AAR（gomobile bind）。
#
# 前置：
#   - Go 1.23+
#   - gomobile：go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init
#   - Android NDK（gomobile 需要它做交叉编译）
#
# 本脚本**不会**在缺少前置时静默跳过。缺什么就 exit 2 并说清楚，
# 因为"没构建"和"构建成功"必须能被区分开。
#
# 用法：
#   kernel/tools/build_aar.sh                # 构建全部 ABI
#   kernel/tools/build_aar.sh arm64-v8a      # 只构建一个 ABI
set -uo pipefail

KERNEL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$KERNEL_DIR/out"
mkdir -p "$OUT_DIR"

# ---------- 定位 Go ----------
GO_BIN="${SYFUSE_GO:-}"
if [ -z "$GO_BIN" ]; then
  for c in "$HOME/.workbuddy-ai/toolchain/go/bin/go" "$HOME/.workbuddy-ai/toolchain/go/bin/go.exe" go; do
    if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then GO_BIN="$c"; break; fi
  done
fi
[ -n "$GO_BIN" ] || { echo "ERROR: 找不到 Go。设置 SYFUSE_GO 或把 Go 放到 ~/.workbuddy-ai/toolchain/go" >&2; exit 2; }

export GOROOT="${GOROOT:-$(cd "$(dirname "$GO_BIN")/.." && pwd)}"
export GOFLAGS="${GOFLAGS:--mod=mod}"
export GOPROXY="${GOPROXY:-off}"
export GOCACHE="${GOCACHE:-$HOME/.workbuddy-ai/toolchain/go-cache}"
export GOPATH="${GOPATH:-$HOME/.workbuddy-ai/toolchain/go-path}"

echo "Go: $("$GO_BIN" version)"

# ---------- 定位 gomobile ----------
GOMOBILE="${SYFUSE_GOMOBILE:-}"
if [ -z "$GOMOBILE" ]; then
  for c in "$GOPATH/bin/gomobile" "$GOPATH/bin/gomobile.exe" gomobile; do
    if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then GOMOBILE="$c"; break; fi
  done
fi
if [ -z "$GOMOBILE" ]; then
  cat >&2 <<'EOF'
ERROR: 找不到 gomobile。

gomobile 需要显式安装（本仓库刻意不把它放进 go.mod —— 它是构建工具，不是依赖）：
  go install golang.org/x/mobile/cmd/gomobile@latest
  gomobile init

另外 gomobile 交叉编译 Android 需要 NDK。若尚未安装：
  sdkmanager --install "ndk;27.0.12077973"
  export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/27.0.12077973"
EOF
  exit 2
fi
echo "gomobile: $GOMOBILE"

# ---------- NDK ----------
NDK="${ANDROID_NDK_HOME:-${ANDROID_NDK_ROOT:-}}"
if [ -z "$NDK" ] && [ -n "${ANDROID_HOME:-}" ] && [ -d "$ANDROID_HOME/ndk" ]; then
  NDK="$(ls -1d "$ANDROID_HOME"/ndk/* 2>/dev/null | sort -V | tail -1)"
fi
if [ -z "$NDK" ]; then
  echo "ERROR: 找不到 Android NDK。设置 ANDROID_NDK_HOME，或用 sdkmanager 安装。" >&2
  exit 2
fi
export ANDROID_NDK_HOME="$NDK"
echo "NDK: $NDK"

# ---------- ABI 映射 ----------
# gomobile 的 -target=android/<abi> 取值与 Android ABI 名称不完全一致，必须映射。
abi_to_gomobile() {
  case "$1" in
    arm64-v8a)   echo "arm64" ;;
    armeabi-v7a) echo "arm" ;;
    x86_64)      echo "amd64" ;;
    x86)         echo "386" ;;
    *)           echo "" ;;
  esac
}

TARGET_ABIS=("$@")
if [ "${#TARGET_ABIS[@]}" -eq 0 ]; then
  TARGET_ABIS=(arm64-v8a armeabi-v7a x86_64)
fi

status=0
for abi in "${TARGET_ABIS[@]}"; do
  gm="$(abi_to_gomobile "$abi")"
  if [ -z "$gm" ]; then
    echo "跳过未知 ABI: $abi" >&2
    status=2
    continue
  fi

  # 每个 ABI 单独产出 AAR，再按 ABI 命名，供 package.sh 归集
  outname="syfuse-core-$abi.aar"
  echo
  echo "== 构建 $abi (gomobile target=android/$gm) -> $outname =="
  ( cd "$KERNEL_DIR" && "$GOMOBILE" bind \
      -target="android/$gm" \
      -androidapi 26 \
      -o "$OUT_DIR/$outname" \
      ./syfuse )
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "  构建 $abi 失败 (exit=$rc)" >&2
    status=1
    continue
  fi
  echo "  OK: $(stat -c '%s' "$OUT_DIR/$outname" 2>/dev/null || stat -f '%z' "$OUT_DIR/$outname") 字节"
done

echo
if [ "$status" -eq 0 ]; then
  echo "全部 ABI 构建完成，产物在 $OUT_DIR"
  ls -1 "$OUT_DIR"
else
  echo "存在失败项（exit=$status）" >&2
fi
exit $status
