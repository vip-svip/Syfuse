#!/usr/bin/env bash
# Go 内核验证：格式 / 静态检查 / 单元测试。
#
# 与 tests/run_unit.sh（Kotlin 侧）并列。两者都不需要设备。
# 退出码约定与仓库其他脚本一致：0 通过 / 1 失败 / 2 环境缺失。
set -uo pipefail

KERNEL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../kernel" && pwd)"
cd "$KERNEL_DIR"

# ---------- 定位 Go ----------
GO_BIN="${SYFUSE_GO:-}"
if [ -z "$GO_BIN" ]; then
  for c in "$HOME/.workbuddy-ai/toolchain/go/bin/go" "$HOME/.workbuddy-ai/toolchain/go/bin/go.exe" go; do
    if command -v "$c" >/dev/null 2>&1 || [ -x "$c" ]; then GO_BIN="$c"; break; fi
  done
fi
[ -n "$GO_BIN" ] || { echo "ERROR: 找不到 Go（设置 SYFUSE_GO）" >&2; exit 2; }

GOFMT_BIN="$(dirname "$GO_BIN")/gofmt"
[ -x "$GOFMT_BIN" ] || [ -x "$GOFMT_BIN.exe" ] || GOFMT_BIN="gofmt"

export GOROOT="${GOROOT:-$(cd "$(dirname "$GO_BIN")/.." && pwd)}"
export GOFLAGS="${GOFLAGS:--mod=mod}"
# GOPROXY=off：本模块零第三方依赖，因此不需要网络。
# 关掉它同时起到"证明零依赖"的作用——如果哪天有人加了外部依赖，这里会立刻失败。
export GOPROXY="${GOPROXY:-off}"
export GOCACHE="${GOCACHE:-$HOME/.workbuddy-ai/toolchain/go-cache}"
export GOPATH="${GOPATH:-$HOME/.workbuddy-ai/toolchain/go-path}"

echo "Go: $("$GO_BIN" version)"
echo "模块: $(head -1 go.mod)"
echo

FAIL=0

echo "== gofmt -l（应无输出） =="
unformatted="$("$GOFMT_BIN" -l src)"
if [ -n "$unformatted" ]; then
  echo "$unformatted"
  echo "  [FAIL] 存在未格式化的文件"
  FAIL=1
else
  echo "  [PASS] 全部文件已格式化"
fi

echo
echo "== go vet =="
if "$GO_BIN" vet ./...; then
  echo "  [PASS] vet 通过"
else
  echo "  [FAIL] vet 失败"
  FAIL=1
fi

echo
echo "== go build =="
if "$GO_BIN" build ./...; then
  echo "  [PASS] 构建通过"
else
  echo "  [FAIL] 构建失败"
  FAIL=1
fi

echo
echo "== go test =="
if "$GO_BIN" test ./... -v > /tmp/syfuse_kernel_test.log 2>&1; then
  passed=$(grep -cE '^--- PASS' /tmp/syfuse_kernel_test.log || true)
  failed=$(grep -cE '^--- FAIL' /tmp/syfuse_kernel_test.log || true)
  echo "  [PASS] 用例=$passed 失败=$failed"
else
  echo "  [FAIL] 测试失败，尾部输出："
  tail -40 /tmp/syfuse_kernel_test.log | sed 's/^/    /'
  FAIL=1
fi

echo
echo "== 零第三方依赖校验 =="
if grep -qE '^\s*require\s' go.mod; then
  echo "  [FAIL] go.mod 出现 require —— 内核刻意保持零依赖（R2 供应链面）"
  grep -E '^\s*require\s' go.mod | sed 's/^/    /'
  FAIL=1
else
  echo "  [PASS] go.mod 无 require，内核零第三方依赖"
fi
if [ -f go.sum ]; then
  echo "  [FAIL] 存在 go.sum，说明引入了外部模块"
  FAIL=1
else
  echo "  [PASS] 无 go.sum"
fi

echo
printf '===========================================\n'
if [ "$FAIL" -eq 0 ]; then
  printf 'kernel-checks: PASS\n'
else
  printf 'kernel-checks: FAIL\n'
fi
printf '===========================================\n'
exit $FAIL
