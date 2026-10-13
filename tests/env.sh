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
