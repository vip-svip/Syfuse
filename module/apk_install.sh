#!/system/bin/sh
# ============================================================================
#  Syfuse 内置 APK 安装逻辑（安装期自动安装 + action.sh 菜单 1 共用）
#
#  为什么单独一个文件：customize.sh（安装时由安装器 source）与 action.sh
#  （菜单里手动重装）都需要同一套「版本判断 → pm install -r → 分类报错」。
#  两处各写一份必然漂移（action.sh 早期版本就因此漏掉了版本判断），
#  所以收敛为单一实现。
#
#  用法（调用方需先提供 ui_print；安装期由安装器提供，action.sh 自带）：
#       . "$MODDIR/apk_install.sh"
#       syfuse_install_apk "$MODDIR"
#
#  退出约定：
#     0 = 安装成功 / 同版本跳过 / 设备版本更新跳过 / 模块包未内置 APK（仅警告）
#     1 = 已尝试安装但失败（调用方负责让失败可见：customize.sh 以非零码收尾）
#
#  mksh（Android sh）兼容：不用 [[ ]]、数组、进程替换；所有变量先赋值。
#  每行 ui_print 只传一个参数（安装器的 ui_print 早期版本只取 $1）。
# ============================================================================

# 调用方（customize.sh 由安装器、action.sh 自身）应已提供 ui_print。
# 这里只做无副作用的兜底：没提供就退回 echo，保证输出绝不消失。
if ! command -v ui_print >/dev/null 2>&1; then
  ui_print() { echo "$@"; }
fi

# ---------- 基本信息解析 ----------
# 包名与 app/build.gradle.kts 的 applicationId 同源：
#   io.github.<author 规范化>.syfuse
# 规范化规则（小写 + 去 -_.）与 tests/run_module_checks.sh 的校验一致，
# 账号改名时两边同步变化，不会各自漂移。
syfuse_apk_init() {
  SYFUSE_HELPER_DIR="$1"
  SYFUSE_APK_PATH="$SYFUSE_HELPER_DIR/apk/syfuse.apk"
  SYFUSE_PKG=$(printf 'io.github.%s.syfuse' \
    "$(sed -n 's/^author=//p' "$SYFUSE_HELPER_DIR/module.prop" 2>/dev/null | head -1 \
      | tr '[:upper:]' '[:lower:]' | tr -d '_.-')")
  case "$SYFUSE_PKG" in
    io.github..syfuse)
      # author 缺失或全是分隔符：回退到当前已知值，并明确说出来
      SYFUSE_PKG="io.github.vipsvip.syfuse"
      ;;
  esac
  # 模块版本：与 app/build.gradle.kts 的 versionName/versionCode 由
  # tests/run_module_checks.sh 强制一致，可作为内置 APK 的版本代理。
  SYFUSE_MOD_VER=$(sed -n 's/^version=//p' "$SYFUSE_HELPER_DIR/module.prop" 2>/dev/null | head -1)
  SYFUSE_MOD_VER=${SYFUSE_MOD_VER#v}
  SYFUSE_MOD_CODE=$(sed -n 's/^versionCode=//p' "$SYFUSE_HELPER_DIR/module.prop" 2>/dev/null | head -1)
}

# ---------- 已安装版本查询 ----------
# 只信 dumpsys package 的结构化输出。不用 grep 全文扫 APK 二进制、
# 不猜文件名里的版本 —— 那些都是不可靠来源（不同 ROM 字段顺序有差异）。
syfuse_installed_name() {
  dumpsys package "$SYFUSE_PKG" 2>/dev/null \
    | sed -n 's/^.*versionName=\([^[:space:]]*\).*/\1/p' | head -1
}

syfuse_installed_code() {
  dumpsys package "$SYFUSE_PKG" 2>/dev/null \
    | sed -n 's/^.*versionCode=\([0-9][0-9]*\).*/\1/p' | head -1
}

# ---------- 主流程 ----------
syfuse_install_apk() {
  syfuse_apk_init "$1"

  if [ ! -f "$SYFUSE_APK_PATH" ]; then
    ui_print "- 未发现内置 APK：$SYFUSE_APK_PATH"
    ui_print "  本模块包构建时未注入 APK（package.sh 会把 dist/ 下第一个 APK"
    ui_print "  复制为 apk/syfuse.apk）。这不影响模块本身安装，但 App 需另行获取；"
    ui_print "  模块装好后可在 action.sh 菜单「1) 安装 / 重装内置 App」重试。"
    return 0
  fi

  inst_name=$(syfuse_installed_name)
  inst_code=$(syfuse_installed_code)

  if [ -n "$inst_name" ]; then
    if [ "$inst_name" = "$SYFUSE_MOD_VER" ] \
       && { [ -z "$inst_code" ] || [ -z "$SYFUSE_MOD_CODE" ] \
            || [ "$inst_code" = "$SYFUSE_MOD_CODE" ]; }; then
      ui_print "- 内置 App 已是同版本（已装 $inst_name/code=$inst_code = 模块 $SYFUSE_MOD_VER/$SYFUSE_MOD_CODE），跳过安装"
      return 0
    fi
    # 设备上装的是更新版本：pm install -r 拒绝降级，硬加 -d 有清数据风险，跳过并说明
    if [ -n "$inst_code" ] && [ -n "$SYFUSE_MOD_CODE" ]; then
      case "$inst_code$SYFUSE_MOD_CODE" in
        *[!0-9]*) ;;   # 非纯数字：不比大小，交给 pm 自行判定
        *) if [ "$inst_code" -gt "$SYFUSE_MOD_CODE" ]; then
             ui_print "- 设备上已安装更新的 App（$inst_name/code=$inst_code > 模块 $SYFUSE_MOD_VER/$SYFUSE_MOD_CODE）"
             ui_print "  pm install -r 不做降级；本模块也不追加 -d（降级开关可能清空 App 数据）。"
             ui_print "  如需改用本模块内置版本，请先卸载设备上的 App（注意会清空其数据）。"
             return 0
           fi ;;
      esac
    fi
  fi

  if ! command -v pm >/dev/null 2>&1; then
    ui_print "- ✗ 找不到 pm 命令，无法安装内置 App"
    ui_print "  pm 是 Android 包管理器的标准入口（/system/bin/pm）。"
    ui_print "  当前环境不是完整的 Android 系统，或 PATH 被裁剪。"
    return 1
  fi

  ui_print "- 正在安装内置 App（pm install -r，包名 $SYFUSE_PKG）…"
  pm_out=$(pm install -r "$SYFUSE_APK_PATH" 2>&1)
  pm_rc=$?
  if [ "$pm_rc" -eq 0 ]; then
    ui_print "- 内置 App 安装成功：$SYFUSE_PKG $SYFUSE_MOD_VER（code=$SYFUSE_MOD_CODE）"
    return 0
  fi

  # ---------- 失败分类：给出「是什么、为什么、怎么办」 ----------
  # 注意 case 分支顺序：PARSE/签名解析类放在签名冲突类**之后**匹配会误判，
  # 这里把解析失败放最后，避免 *CERTIFICATE* 之类的词把两种原因混为一谈。
  case "$pm_out" in
    *INSUFFICIENT_STORAGE*)
      reason="存储空间不足，/data 放不下"
      guidance="清理存储空间后，用 action.sh 菜单「1) 安装 / 重装内置 App」重试。" ;;
    *UPDATE_INCOMPATIBLE*|*"signatures do not match"*)
      reason="签名不一致：设备上存在同包名（$SYFUSE_PKG）但签名不同的应用"
      guidance="pm install -r 不允许跨签名覆盖。请先卸载设备上的旧版（卸载会清空其数据），再重试。" ;;
    *VERSION_DOWNGRADE*)
      reason="系统拒绝版本降级（设备上的 App 比模块内置版本更新）"
      guidance="先卸载设备上的 App 再安装内置版本，或改用与设备版本匹配的模块包。" ;;
    *OLDER_SDK*)
      reason="内置 APK 要求的最低系统版本高于当前设备"
      guidance="设备系统过旧。换一台满足 minSdk 要求的设备安装。" ;;
    *NEWER_SDK*)
      reason="内置 APK 的 targetSdk 高于当前设备系统"
      guidance="设备系统过旧，无法运行该构建产物。升级设备系统。" ;;
    *USER_RESTRICTED*|*DISALLOW_INSTALL*)
      reason="设备策略/用户限制禁止安装应用"
      guidance="检查用户限制（如部分 ROM 的 USB 安装开关、企业设备策略）后重试。" ;;
    *PARSE_FAILED*|*NO_CERTIFICATES*|*INVALID_APK*|*"not yet valid"*)
      reason="APK 解析失败：文件损坏、未签名或证书不在有效期"
      guidance="内置 APK 可能被截断/损坏。用 package.sh 重新打包模块，或从 dist/ 手动安装验证。" ;;
    *)
      reason="未能归类的失败，见下方 pm 原始输出"
      guidance="请连同完整安装日志反馈，不要静默重试。" ;;
  esac
  ui_print "- ✗ 内置 App 安装失败（pm install -r 退出码 $pm_rc）"
  ui_print "  原因：$reason"
  ui_print "  处理：$guidance"
  ui_print "  pm 原始输出：$(printf '%s' "$pm_out" | tr '\n' ' ')"
  return 1
}
