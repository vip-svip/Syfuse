#!/usr/bin/env bash
# 体积预算测量：把"分 ABI 后单包能不能压进预算"变成可核对的字节数。
#
# 只测量、不断言阈值——不同 AGP/依赖版本会让体积漂移，硬阈值会产生假失败。
# 阈值判断交给 README 的性能预算表 + 人。
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"

OUT="$SYFUSE_ROOT/tests/perf_apk_size.txt"

syfuse_info "构建 release（未签名，除非提供了 SYFUSE_KEYSTORE_PATH）"
syfuse_gradle :app:assembleRelease --console=plain
status=$?
[ "$status" -eq 0 ] || { echo "构建失败（exit=$status）" >&2; exit $status; }

echo
syfuse_info "APK 体积（原始字节 / 人类可读）"
{
  echo "=== Syfuse APK 体积 $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
} > "$OUT"

shopt -s nullglob
found=0
for apk in "$SYFUSE_ROOT"/app/build/outputs/apk/release/*.apk; do
  found=1
  size=$(stat -c '%s' "$apk" 2>/dev/null || stat -f '%z' "$apk")
  human=$(numfmt --to=iec --suffix=B "$size" 2>/dev/null || echo "${size}B")
  printf '  %-56s %12s  %s\n' "$(basename "$apk")" "$size" "$human"
  printf '%s\t%s\t%s\n' "$(basename "$apk")" "$size" "$human" >> "$OUT"
done

if [ "$found" -eq 0 ]; then
  echo "  没有找到 release APK" >&2
  exit 1
fi

# 顺带记录 debug 包体积，用于对比 minify/shrink 的实际收益
for apk in "$SYFUSE_ROOT"/app/build/outputs/apk/debug/*.apk; do
  size=$(stat -c '%s' "$apk" 2>/dev/null || stat -f '%z' "$apk")
  human=$(numfmt --to=iec --suffix=B "$size" 2>/dev/null || echo "${size}B")
  printf '  [debug] %-48s %12s  %s\n' "$(basename "$apk")" "$size" "$human"
done

echo
syfuse_info "记录已追加到 $OUT"
exit 0
