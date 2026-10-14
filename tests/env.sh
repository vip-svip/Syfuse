#!/usr/bin/env bash
# 所有验证脚本共享的工具链定位逻辑。
#
# 设计约束（R4）：脚本**不得**在找不到工具链时静默降级为"通过"。
# 定位失败一律 exit 2，并把"缺什么"打到 stderr，让调用方无法把 SKIP 误当成 PASS。
#
# 优先级：显式环境变量 > 仓库内 local.properties > 本地工具链目录 > PATH。
# 用法：在脚本顶部 `source "$(dirname "$0")/env.sh"`。

set -uo pipefail

# ---------- 仓库根 ----------
SYFUSE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export SYFUSE_ROOT

# 本地工具链默认位置（由 bootstrap 脚本安装，见 README「构建环境」）
TOOLCHAIN="${SYFUSE_TOOLCHAIN:-$HOME/.workbuddy-ai/toolchain}"

syfuse_die() { printf 'ERROR: %s\n' "$*" >&2; exit 2; }
syfuse_skip() { printf 'SKIP: %s\n' "$*" >&2; exit 3; }
syfuse_info() { printf ':: %s\n' "$*"; }

# ---------- JDK ----------
if [ -z "${JAVA_HOME:-}" ]; then
  for candidate in "$TOOLCHAIN"/jdk-* ; do
    if [ -x "$candidate/bin/java" ] || [ -f "$candidate/bin/java.exe" ]; then
      JAVA_HOME="$candidate"; break
    fi
  done
fi
export JAVA_HOME="${JAVA_HOME:-}"
if [ -z "$JAVA_HOME" ] || { [ ! -x "$JAVA_HOME/bin/java" ] && [ ! -f "$JAVA_HOME/bin/java.exe" ]; }; then
  syfuse_die "找不到 JDK（需要 17+）。设置 JAVA_HOME，或把 JDK 放到 $TOOLCHAIN/jdk-*"
fi
export PATH="$JAVA_HOME/bin:$PATH"

# ---------- Gradle ----------
SYFUSE_GRADLE="${SYFUSE_GRADLE:-}"
if [ -z "$SYFUSE_GRADLE" ]; then
  # 仓库自带的 wrapper 优先。
  #
  # **不要用 `-x` 判定**：gradlew 的可执行位依赖 git 存储的文件模式，
  # 而 Windows 上无法设置该位 —— 在 Windows 提交、Linux 克隆时
  # gradlew 会是 644，`-x` 判定失败，于是脚本去别处找 Gradle 并报"找不到"。
  # 实测这导致 CI 的单元测试 job 直接失败（exit 2）。
  # 改用 `-f` 判定存在性，并在调用时用 `sh` 兜底（见 syfuse_gradle）。
  if [ -f "$SYFUSE_ROOT/gradlew" ]; then
    SYFUSE_GRADLE="$SYFUSE_ROOT/gradlew"
  else
    for candidate in "$TOOLCHAIN"/gradle-*/bin/gradle ; do
      [ -x "$candidate" ] && SYFUSE_GRADLE="$candidate" && break
    done
  fi
fi
export SYFUSE_GRADLE
export GRADLE_USER_HOME="${GRADLE_USER_HOME:-$TOOLCHAIN/gradle-home}"

# ---------- Android SDK ----------
if [ -z "${ANDROID_HOME:-}" ]; then
  for candidate in "$TOOLCHAIN/android-sdk" "$HOME/Android/Sdk" "$HOME/Library/Android/sdk" ; do
    [ -d "$candidate" ] && ANDROID_HOME="$candidate" && break
  done
fi
export ANDROID_HOME="${ANDROID_HOME:-}"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
[ -n "$ANDROID_HOME" ] && export PATH="$ANDROID_HOME/platform-tools:$PATH"

# ---------- 子命令 ----------
# 需要 Gradle 的脚本调用它；不需要的（纯静态检查）不调用，从而在无 SDK 环境也能跑。
syfuse_gradle() {
  [ -n "$SYFUSE_GRADLE" ] || syfuse_die "找不到 Gradle。设置 SYFUSE_GRADLE 或把 Gradle 放到 $TOOLCHAIN/gradle-*/bin/gradle"
  if [ -x "$SYFUSE_GRADLE" ]; then
    ( cd "$SYFUSE_ROOT" && "$SYFUSE_GRADLE" "$@" )
  else
    # 无执行位时用 sh 调用（跨平台克隆的常见情形）
    ( cd "$SYFUSE_ROOT" && sh "$SYFUSE_GRADLE" "$@" )
  fi
}

# adb 定位。设备类脚本用它。
syfuse_adb() {
  if command -v adb >/dev/null 2>&1; then command -v adb; return; fi
  if [ -n "$ANDROID_HOME" ] && [ -f "$ANDROID_HOME/platform-tools/adb.exe" ]; then
    echo "$ANDROID_HOME/platform-tools/adb.exe"; return
  fi
  if [ -n "$ANDROID_HOME" ] && [ -x "$ANDROID_HOME/platform-tools/adb" ]; then
    echo "$ANDROID_HOME/platform-tools/adb"; return
  fi
  return 1
}

# 返回第一个处于 device 状态的序列号；没有则返回空。
syfuse_device() {
  local adb_bin
  adb_bin="$(syfuse_adb)" || return 1
  "$adb_bin" devices | awk 'NR>1 && $2=="device" { print $1; exit }'
}

# ---------- Python ----------
# 探测一个**真的能执行**的 Python 3。
#
# 为什么不能只判 `command -v python`：
# Windows 上只要开启过 Microsoft Store 的 python 应用执行别名，PATH 里就会有
# C:\Users\<u>\AppData\Local\Microsoft\WindowsApps\python.exe —— 一个桩程序，
# 执行时只打印 "Python was not found; run without arguments to install from
# the Microsoft Store..." 并以 49 退出，**但 `command -v` 依然成功**。
# 只判"名字能解析"的脚本会顺利通过前置检查，然后在真正调用解释器时失败，
# 于是把"没装 Python"报成"清单/产物不合规"——一个把排查方向指向错误一侧的
# 假失败。验证脚本报假失败比不报更糟，所以这里只认实测结果：
# 唯一判据是"跑一段真代码且退出码为 0"。
#
# 顺序：显式 SYFUSE_PYTHON > python3 > python > py。
# （python3 优先——Windows 别名桩最常见的就是裸 `python`。）
syfuse_python() {
  local cand p
  for cand in "${SYFUSE_PYTHON:-}" python3 python py; do
    [ -n "$cand" ] || continue
    if [ -f "$cand" ]; then
      p="$cand"
    else
      p="$(command -v "$cand" 2>/dev/null)" || continue
    fi
    [ -n "$p" ] || continue
    if "$p" -c 'import sys; raise SystemExit(0 if sys.version_info[0] == 3 else 1)' \
         >/dev/null 2>&1; then
      printf '%s\n' "$p"
      return 0
    fi
  done
  return 1
}

# 诊断输出：把"被解析到但跑不起来"的候选逐个点名，让判定可复核。
syfuse_python_diagnose() {
  local cand p found=0
  for cand in python python3 py; do
    p="$(command -v "$cand" 2>/dev/null)" || continue
    [ -n "$p" ] || continue
    found=1
    if "$p" -c 'import sys; raise SystemExit(0 if sys.version_info[0] == 3 else 1)' \
         >/dev/null 2>&1; then
      printf '  - %s -> %s（可用）\n' "$cand" "$p" >&2
    else
      printf '  - %s -> %s（不可执行，已忽略）\n' "$cand" "$p" >&2
    fi
  done
  [ "$found" -eq 1 ] || printf '  - PATH 上没有任何 python/python3/py\n' >&2
}

# 必须拿到可用 Python，否则按 R4 以 exit 2 终止（**不得**降级成跳过或失败）。
# 注意：这里刻意**不用命令替换**取返回值——`exit` 在子 shell 里不会终止父进程，
# 那会让"缺工具"变成静默继续。结果通过全局 PYTHON_BIN 传出。
syfuse_require_python() {
  local p
  if ! p="$(syfuse_python)"; then
    printf 'ERROR: 找不到可用的 Python 3。逐项探测结果：\n' >&2
    syfuse_python_diagnose
    syfuse_die "缺少 Python 3。怎么补：设置 SYFUSE_PYTHON 指向 python.exe，
  或安装 Python 3 并确保它在 PATH 上。
  Windows 若 PATH 里的 python 是 Microsoft Store 应用执行别名（执行时报
  'Python was not found' 并以 49 退出），请在「设置 → 应用 → 高级应用设置 →
  应用执行别名」中关闭 python.exe / python3.exe 两项。"
  fi
  PYTHON_BIN="$p"
  export PYTHON_BIN
}
