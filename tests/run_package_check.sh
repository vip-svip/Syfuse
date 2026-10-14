#!/usr/bin/env bash
# ============================================================================
#  run_package_check.sh —— M14 打包校验门禁
#
#  对 dist/ 产物与 module/ 结构做一致性校验（不重新打包，package.sh 负责
#  产出，本脚本负责验收）：
#    1. module.prop 必需键与格式
#    2. 模块骨架文件（update-binary / updater-script / customize.sh / service.sh）
#    3. dist/Syfuse-v<ver>-<abi>.apk 三 ABI 齐 + MANIFEST.sha256 自校验
#    4. dist/syfuse-<ver>.zip 内含守护进程二进制
#  退出码 0 通过 / 1 失败
# ============================================================================
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
cd "$SYFUSE_ROOT"

pass_n=0; fail_n=0
pass() { pass_n=$((pass_n+1)); echo "  [PASS] $1"; }
fail() { fail_n=$((fail_n+1)); echo "  [FAIL] $1"; }

# python 解释器：必须**真的能执行**。只判 `command -v python` 会被 Windows 的
# Microsoft Store 应用执行别名桩程序骗过（名字能解析、执行报 "Python was not
# found" 并以 49 退出），于是下面两条断言会把"缺 Python"报成"SHA-256 不符 /
# zip 结构错"——一个把排查方向指向错误一侧的假失败。探测逻辑单点在 tests/env.sh。
syfuse_require_python
PY="$PYTHON_BIN"

echo "== module.prop 必需键 =="
MP="module/module.prop"
for key in id name version versionCode author description; do
  if grep -qE "^${key}=" "$MP"; then pass "module.prop 有 $key"; else fail "module.prop 缺 $key"; fi
done
grep -qE "^id=syfuse$" "$MP" && pass "id=syfuse 固定值" || fail "id 必须是 syfuse"
grep -qE "^versionCode=[0-9]+$" "$MP" && pass "versionCode 为数字" || fail "versionCode 非数字"

echo "== 模块骨架 =="
for f in module/META-INF/com/google/android/update-binary \
         module/META-INF/com/google/android/updater-script \
         module/customize.sh module/service.sh module/uninstall.sh \
         module/action.sh module/post-fs-data.sh; do
  [ -f "$f" ] && pass "有 $(basename $f)" || fail "缺 $f"
done
# update-binary 必须可执行头
head -1 module/META-INF/com/google/android/update-binary | grep -qE "#!|/sbin/sh" \
  && pass "update-binary 有解释器头" || fail "update-binary 无解释器头"

echo "== dist 产物 =="
VERSION="$(grep -oE '^version=v[^ ]+' module/module.prop | head -1 | sed 's/version=//')"
[ -n "$VERSION" ] || VERSION="v0.1.0"
APK_OK=1
for abi in arm64-v8a armeabi-v7a x86_64; do
  f="dist/Syfuse-${VERSION}-${abi}.apk"
  [ -f "$f" ] && pass "有 $(basename $f)" || { fail "缺 $f"; APK_OK=0; }
done
[ -f "dist/MANIFEST.sha256" ] && pass "有 MANIFEST.sha256" || fail "缺 MANIFEST.sha256"
[ -f "dist/syfuse-${VERSION#v}.zip" ] && pass "有模块 zip" \
  || { # 兼容 syfuse-v<ver>.zip 命名
    [ -f "dist/syfuse-${VERSION}.zip" ] && pass "有模块 zip(v 前缀命名)" || fail "缺模块 zip"
  }

echo "== SHA-256 自校验 =="
if [ -f dist/MANIFEST.sha256 ]; then
  if "$PY" - <<'EOF'
import hashlib, sys, os
os.chdir("dist")
bad = 0
for line in open("MANIFEST.sha256", encoding="utf-8"):
    line = line.strip()
    if not line or line.startswith("#"):
        continue
    parts = line.split(None, 1)
    if len(parts) != 2:
        continue
    want, name = parts[0], parts[1].lstrip("*").strip()
    if not os.path.isfile(name):
        print(f"  [FAIL] 清单文件不存在: {name}"); bad = 1; continue
    h = hashlib.sha256(open(name, "rb").read()).hexdigest()
    if h != want:
        print(f"  [FAIL] {name} SHA-256 不符"); bad = 1
sys.exit(bad)
EOF
  then pass "MANIFEST.sha256 全部相符"; else fail "SHA-256 自校验失败"; fi
fi

echo "== 模块 zip 内含守护进程 =="
ZIP=""
for cand in "dist/syfuse-${VERSION#v}.zip" "dist/syfuse-${VERSION}.zip"; do
  [ -f "$cand" ] && ZIP="$cand" && break
done
if [ -n "$ZIP" ]; then
  if "$PY" - "$ZIP" <<'EOF'
import sys, zipfile
z = zipfile.ZipFile(sys.argv[1])
names = z.namelist()
need = ["module.prop", "META-INF/com/google/android/update-binary"]
missing = [n for n in need if not any(x == n or x.endswith("/" + n) for x in names)]
has_bin = any("syfused" in x for x in names)
if missing:
    print(f"  [FAIL] zip 缺: {missing}"); sys.exit(1)
if not has_bin:
    print("  [FAIL] zip 缺守护进程二进制 syfused-*"); sys.exit(1)
EOF
  then pass "zip 结构与守护进程齐备"; else fail "zip 结构校验失败"; fi
fi

echo
echo "package-check: PASS=$pass_n FAIL=$fail_n"
[ "$fail_n" -eq 0 ] && { echo "package-check: PASS"; exit 0; }
echo "package-check: FAIL"
exit 1
