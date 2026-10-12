#!/system/bin/sh
# ============================================================================
#  Syfuse · 融合代理 —— action.sh（音量键菜单）
#
#  KernelSU / Magisk / APatch 的模块管理器点击「动作」按钮时执行本脚本。
#
#  这是「模块必须能独立工作」的入口（提示词 3.1）：
#  没有 App、没有网络、只有一台 root 手机时，用户也能通过本菜单完成
#  安装、启停、看状态、切配置、装证书、更新、备份、开面板、卸载、自检。
#
#  实现约束：
#   1. 提示词 3.5 要求 11 项，一项不少，顺序也按提示词来（用户熟悉）。
#   2. 每项都必须真的调用 syfused 或做真实的文件操作。
#      **不允许出现「按了没反应」的菜单项** —— 那是死 UI 的命令行版本。
#      尚未实现的（证书、Hook、内核）会明确打印「未实现 + 计划里程碑」。
#   3. 本脚本自身不做系统级改动；改动全部由 syfused 完成，
#      以便每一条改动都被登记进还原清单（manifest/installed.json）。
#   4. 只读 /data/adb/syfuse 与 $MODDIR，绝不触碰系统分区。
# ============================================================================

MODDIR=${0%/*}
DATA_DIR="${SYFUSE_DATA_DIR:-/data/adb/syfuse}"
CONF="$DATA_DIR/syfuse.conf"
MANIFEST="$DATA_DIR/manifest/installed.json"

# ---------- 按架构选二进制 ----------
ABI=$(getprop ro.product.cpu.abi 2>/dev/null)
[ -n "$ABI" ] || ABI=$(getprop ro.product.cpu.abilist 2>/dev/null | cut -d, -f1)
case "$ABI" in
  arm64*)           ARCH=arm64 ;;
  armeabi*|armv7*)  ARCH=arm ;;
  x86_64*)          ARCH=amd64 ;;
  *)                ARCH=arm64 ;;
esac
BIN="$MODDIR/bin/syfused"
[ -x "$BIN" ] || BIN="$MODDIR/bin/syfused-$ARCH"

# ---------- 输出 ----------
ui_print() { echo "$@"; }
hr() { ui_print "──────────────────────────────────────────────"; }
title() { clear 2>/dev/null; hr; ui_print "  Syfuse · 融合代理"; ui_print "  $1"; hr; }

# ---------- 按键读取 ----------
# 优先 getevent（真机可靠）；失败回退 stdin（adb shell / 终端）。
# 两条都不可用时返回 "0"，菜单不至于卡死。
read_key() {
  if command -v getevent >/dev/null 2>&1; then
    ev=$(getevent -qlc 1 2>/dev/null | tail -1)
    case "$ev" in
      *KEY_VOLUMEUP*)   echo up;   return ;;
      *KEY_VOLUMEDOWN*) echo down; return ;;
      *KEY_POWER*)      echo power; return ;;
    esac
  fi
  if [ -t 0 ]; then
    read -r line
    echo "${line:-0}"
    return
  fi
  echo 0
}

pause_key() {
  ui_print ""
  ui_print "  按任意键返回菜单…"
  read_key >/dev/null
}

# ---------- 探测 ----------
daemon_running() {
  [ -f "$DATA_DIR/syfused.pid" ] || return 1
  pid=$(cat "$DATA_DIR/syfused.pid" 2>/dev/null)
  [ -n "$pid" ] || return 1
  kill -0 "$pid" 2>/dev/null
}

daemon_pid() { cat "$DATA_DIR/syfused.pid" 2>/dev/null; }

manifest_count() {
  [ -f "$MANIFEST" ] || { echo 0; return; }
  # 不依赖 jq（模块里不保证有）
  grep -c '"id"' "$MANIFEST" 2>/dev/null || echo 0
}

not_implemented() {
  # $1=功能名  $2=计划里程碑  $3=说明
  ui_print "  ⚠ 尚未实现：$1"
  ui_print "    计划里程碑：$2"
  ui_print "    $3"
  ui_print ""
  ui_print "  本项不会做任何事。这样设计是为了不让菜单里出现"
  ui_print "  「按了没反应」的假入口 —— 那等同于欺诈。"
}

# ============================================================================
#  1) 安装 / 重装内置 App
# ============================================================================
do_install_apk() {
  title "1) 安装 / 重装内置 App"
  apk="$MODDIR/apk/syfuse.apk"
  if [ ! -f "$apk" ]; then
    ui_print "  未找到内置 APK：$apk"
    ui_print ""
    ui_print "  原因：本版本的模块包**尚未内置 APK**（sidecar）。"
    ui_print "  内置软件由 package.sh 打包时注入；当前 APK 与内核都未随包交付，"
    ui_print "  详见 README 的「功能可用性报告」。"
    return 1
  fi
  ui_print "  正在安装 $apk …"
  if command -v pm >/dev/null 2>&1; then
    if pm install -r "$apk"; then
      ui_print "  安装完成。"
    else
      ui_print "  安装失败（退出码 $?）。"
    fi
  else
    ui_print "  pm 不可用，无法安装。"
  fi
}

# ============================================================================
#  2) 启动 / 停止 / 重启守护进程
# ============================================================================
do_daemon_ctl() {
  title "2) 守护进程控制"
  if daemon_running; then
    ui_print "  当前状态：运行中 (pid=$(daemon_pid))"
  else
    ui_print "  当前状态：未运行"
  fi
  ui_print ""
  ui_print "  1) 启动"
  ui_print "  2) 停止"
  ui_print "  3) 重启"
  ui_print "  0) 返回"
  printf "  选择（回车=0）: "

  case "$(read_key)" in
    1|up)
      if daemon_running; then ui_print "  已在运行，忽略。"; return; fi
      if [ ! -x "$BIN" ]; then
        not_implemented "启动守护进程" "M3（内核打通）" \
          "bin/syfused-$ARCH 未随包交付，没有可启动的进程。"
        return
      fi
      mkdir -p "$DATA_DIR/log" 2>/dev/null
      nohup "$BIN" daemon >>"$DATA_DIR/log/service.out" 2>&1 &
      sleep 1
      if daemon_running; then
        ui_print "  已启动 (pid=$(daemon_pid))"
      else
        ui_print "  启动失败，见 $DATA_DIR/log/syfused.log"
      fi
      ;;
    2|down)
      if ! daemon_running; then ui_print "  未在运行。"; return; fi
      pid=$(daemon_pid)
      kill -TERM "$pid" 2>/dev/null
      sleep 1
      if daemon_running; then
        kill -9 "$pid" 2>/dev/null
        ui_print "  已强制结束。"
      else
        ui_print "  已停止。"
      fi
      ;;
    3|power)
      pid=$(daemon_pid)
      [ -n "$pid" ] && kill -TERM "$pid" 2>/dev/null
      sleep 1
      if [ -x "$BIN" ]; then
        nohup "$BIN" daemon >>"$DATA_DIR/log/service.out" 2>&1 &
        sleep 1
        ui_print "  已重启。"
      else
        ui_print "  二进制缺失，无法重启。"
      fi
      ;;
    *) ui_print "  已返回。" ;;
  esac
}

# ============================================================================
#  3) 查看状态
# ============================================================================
do_status() {
  title "3) 状态"
  ui_print "  模块版本  : $(grep '^version=' "$MODDIR/module.prop" 2>/dev/null | cut -d= -f2)"
  ui_print "  设备架构  : $ABI -> $ARCH"
  ui_print "  数据目录  : $DATA_DIR"
  if daemon_running; then
    ui_print "  守护进程  : 运行中 (pid=$(daemon_pid))"
  else
    ui_print "  守护进程  : 未运行"
  fi
  ui_print "  还原清单  : $(manifest_count) 条"
  hr
  # 逐项如实标注能力状态。**不写"正常"来蒙混** ——
  # 用户看到的就是真实状态，没有的部分明说。
  ui_print "  内置 APK      : $([ -f "$MODDIR/apk/syfuse.apk" ] && echo 已内置 || echo '未内置（M14 随包）')"
  ui_print "  内核二进制    : $([ -x "$MODDIR/bin/syfused-$ARCH" ] && echo 已内置 || echo '未内置（M3）')"
  ui_print "  代理内核      : 未接入，无法代理流量（M3）"
  ui_print "  系统证书      : 未实现（M8）"
  ui_print "  hosts 注入    : 未实现（M11）"
  ui_print "  Hook 框架     : 未实现（M11）"
  ui_print "  过滤引擎      : 未接入数据面，不产生拦截（M10）"
  ui_print "  防泄漏自检    : 未实现（M12）"
  ui_print "  局域网面板    : 未实现（M13）"
  ui_print "  连接数/拦截数 : 无数据（依赖 M3 / M10）"
  if [ -x "$BIN" ]; then
    hr
    ui_print "  ── syfused status ──"
    "$BIN" status 2>&1 | sed 's/^/  /'
  fi
}

# ============================================================================
#  4) 查看最近日志
# ============================================================================
do_logs() {
  title "4) 最近日志"
  log="$DATA_DIR/log/syfused.log"
  if [ ! -f "$log" ]; then
    ui_print "  尚无日志：$log"
    ui_print "  （守护进程未启动过，或日志级别为 silent）"
    return
  fi
  ui_print "  文件：$log（$(wc -c <"$log" 2>/dev/null | tr -d ' ') 字节）"
  hr
  tail -n 40 "$log" 2>/dev/null
}

# ============================================================================
#  5) 切换配置档案
# ============================================================================
do_switch_profile() {
  title "5) 切换配置档案"
  dir="$DATA_DIR/profiles"
  if [ ! -d "$dir" ] || [ -z "$(ls -A "$dir" 2>/dev/null)" ]; then
    ui_print "  没有配置档案。"
    ui_print ""
    ui_print "  配置档案由 App 创建（配置 → 新建 / 导入）。"
    ui_print "  App 侧的多档案管理尚未实现（计划里程碑 M4）。"
    return
  fi
  i=0
  cur=$(cat "$DATA_DIR/current_profile" 2>/dev/null)
  for f in "$dir"/*; do
    [ -f "$f" ] || continue
    i=$((i+1))
    mark="  "
    [ "$(basename "$f")" = "$cur" ] && mark="* "
    ui_print "  $mark$i) $(basename "$f")"
  done
  ui_print ""
  ui_print "  当前档案：${cur:-（未设置）}"
  ui_print "  切换功能待 App 侧档案管理落地（M4）后启用。"
}

# ============================================================================
#  6) 重装 / 移除系统证书
# ============================================================================
do_cert() {
  title "6) 系统证书"
  not_implemented "重装 / 移除系统证书" "M8（MITM 与抓包）" \
    "需要 CA 生成、系统信任库写入与 OverlayFS 挂载，三者都依赖密码学库与真机验证。"
}

# ============================================================================
#  7) 更新规则集 / 脚本 / 模块订阅
# ============================================================================
do_update() {
  title "7) 更新规则集 / 脚本 / 订阅"
  if [ -x "$BIN" ]; then
    ui_print "  调用：syfused fetch"
    "$BIN" fetch
    rc=$?
    if [ "$rc" -ne 0 ]; then
      ui_print ""
      ui_print "  （退出码 $rc = 未实现。这不是失败，是功能尚未交付。）"
    fi
  else
    not_implemented "更新规则集 / 脚本 / 订阅" "M7 / M10" \
      "syfused 二进制未随包交付，且 fetch 子命令本身也未实现。"
  fi
}

# ============================================================================
#  8) 备份 / 恢复
# ============================================================================
do_backup() {
  title "8) 备份 / 恢复"
  ui_print "  1) 备份（config / rules / profiles）"
  ui_print "  2) 恢复"
  ui_print "  0) 返回"
  printf "  选择（回车=0）: "
  bk="$DATA_DIR/backup"
  stamp=$(date +%Y%m%d-%H%M%S)

  case "$(read_key)" in
    1|up)
      mkdir -p "$bk"
      out="$bk/syfuse-$stamp.tar"
      # 只备份**我们自己创建**的目录，绝不碰系统分区
      tar -cf "$out" -C "$DATA_DIR" syfuse.conf rules profiles 2>/dev/null
      if [ -f "$out" ]; then
        ui_print "  已备份到 $out（$(wc -c <"$out" | tr -d ' ') 字节）"
        # 校验：能列出内容才算备份成功
        if tar -tf "$out" >/dev/null 2>&1; then
          ui_print "  校验通过：归档可读。"
        else
          ui_print "  ⚠ 校验失败：归档不可读，请勿依赖此备份。"
        fi
      else
        ui_print "  备份失败。"
      fi
      ;;
    2|down)
      last=$(ls -1t "$bk"/*.tar 2>/dev/null | head -1)
      if [ -z "$last" ]; then ui_print "  没有可恢复的备份。"; return; fi
      ui_print "  将从 $last 恢复。"
      ui_print "  注意：这会覆盖 $DATA_DIR 下的同名文件。"
      printf "  确认？(y/N) "
      case "$(read_key)" in
        y|Y|up)
          if tar -xf "$last" -C "$DATA_DIR"; then
            ui_print "  已恢复。"
          else
            ui_print "  恢复失败。"
          fi
          ;;
        *) ui_print "  已取消。" ;;
      esac
      ;;
    *) ui_print "  已返回。" ;;
  esac
}

# ============================================================================
#  9) 显示局域网面板地址与 token
# ============================================================================
do_panel() {
  title "9) 局域网面板"
  port=8848
  if [ -f "$CONF" ]; then
    p=$(grep '^PANEL_PORT=' "$CONF" 2>/dev/null | cut -d= -f2)
    [ -n "$p" ] && port="$p"
  fi

  ip=$(ip route get 1.1.1.1 2>/dev/null | sed -n 's/.*src \([0-9.]*\).*/\1/p' | head -1)
  [ -n "$ip" ] || ip="<未获取到>"

  tok=""
  [ -f "$DATA_DIR/api.token" ] && tok=$(cat "$DATA_DIR/api.token" 2>/dev/null)

  ui_print "  面板开关  : $(grep '^PANEL_ENABLE=' "$CONF" 2>/dev/null | cut -d= -f2)"
  ui_print "  端口      : $port"
  ui_print "  本机地址  : http://$ip:$port"
  if [ -n "$tok" ]; then
    # 只显示前 8 位，完整令牌留在文件里 —— 屏幕可能被截图
    ui_print "  令牌前缀  : $(printf '%s' "$tok" | cut -c1-8)…"
    ui_print "  完整令牌  : $DATA_DIR/api.token"
  else
    ui_print "  访问令牌  : 尚未生成（守护进程未启动过）"
  fi
  ui_print ""
  ui_print "  ⚠ 面板服务端**尚未实现**（计划里程碑 M13）。"
  ui_print "    上面是配置与令牌现状，不是「面板已在运行」。"
}

# ============================================================================
#  10) 完全卸载（含还原清单）
# ============================================================================
do_uninstall() {
  title "10) 完全卸载"
  ui_print "  将执行："
  ui_print "   · 停止守护进程"
  ui_print "   · 按还原清单逐条还原（当前 $(manifest_count) 条）"
  ui_print "   · 删除 $DATA_DIR"
  ui_print "   · 删除模块目录"
  ui_print ""
  ui_print "  不会触碰系统分区（本模块不写系统分区）。"
  ui_print ""
  printf "  确认完全卸载？输入 yes 确认: "

  ans=$(read_key)
  if [ "$ans" != "yes" ]; then
    ui_print "  已取消。"
    return
  fi

  # 先按清单还原 —— 顺序很重要：还原要在删数据目录**之前**，
  # 否则清单本身先没了，还原依据就丢了。
  if [ -x "$BIN" ] && [ -f "$MANIFEST" ]; then
    ui_print "  ── 按清单还原 ──"
    "$BIN" restore --apply 2>&1 | sed 's/^/  /'
  fi

  if daemon_running; then
    kill -TERM "$(daemon_pid)" 2>/dev/null
    sleep 1
  fi

  # 最终清理交给 uninstall.sh（单一实现，避免两处逻辑漂移）
  if [ -f "$MODDIR/uninstall.sh" ]; then
    ui_print "  ── 调用 uninstall.sh ──"
    sh "$MODDIR/uninstall.sh" 2>&1 | sed 's/^/  /'
  fi

  ui_print ""
  ui_print "  完成。请在模块管理器中删除本模块以移除挂载。"
}

# ============================================================================
#  11) 自检
# ============================================================================
do_selfcheck() {
  title "11) 自检"
  pass=0; fail=0; pend=0
  chk() {
    case "$1" in
      PASS) ui_print "  [PASS] $2"; pass=$((pass+1)) ;;
      FAIL) ui_print "  [FAIL] $2"; fail=$((fail+1)) ;;
      PEND) ui_print "  [PEND] $2"; pend=$((pend+1)) ;;
    esac
  }

  [ -f "$MODDIR/module.prop" ] && chk PASS "module.prop 存在" || chk FAIL "module.prop 缺失"
  [ "$(grep '^id=' "$MODDIR/module.prop" 2>/dev/null | cut -d= -f2)" = "syfuse" ] \
    && chk PASS "module id = syfuse" || chk FAIL "module id 不正确"
  [ -f "$MODDIR/META-INF/com/google/android/update-binary" ] \
    && chk PASS "update-binary 存在" || chk FAIL "update-binary 缺失"

  if [ -d "$DATA_DIR" ]; then
    perm=$(stat -c '%a' "$DATA_DIR" 2>/dev/null || echo '?')
    [ "$perm" = "700" ] && chk PASS "数据目录权限 700" \
                        || chk FAIL "数据目录权限为 $perm（应为 700）"
  else
    chk FAIL "数据目录不存在"
  fi

  if [ -f "$DATA_DIR/api.token" ]; then
    tperm=$(stat -c '%a' "$DATA_DIR/api.token" 2>/dev/null || echo '?')
    [ "$tperm" = "600" ] && chk PASS "令牌文件权限 600" \
                         || chk FAIL "令牌文件权限为 $tperm（应为 600）"
  else
    chk PEND "令牌尚未生成（守护进程未启动过）"
  fi

  if [ -f "$MANIFEST" ]; then
    if [ -x "$BIN" ]; then
      if "$BIN" restore >/dev/null 2>&1; then
        chk PASS "还原清单校验通过"
      else
        chk FAIL "还原清单存在问题（运行 syfused restore 查看）"
      fi
    else
      chk PEND "有清单但无 syfused 可校验"
    fi
  else
    chk PEND "尚无还原清单（未产生系统级改动）"
  fi

  chk PEND "代理内核未接入（M3）"
  chk PEND "证书与 hosts 注入未实现（M8/M11）"
  chk PEND "Hook 框架未实现（M11）"
  chk PEND "防泄漏自检未实现（M12）"

  hr
  ui_print "  PASS=$pass  FAIL=$fail  PEND=$pend"
  if [ "$fail" -gt 0 ]; then
    ui_print "  自检未通过：有 $fail 项失败。"
  else
    ui_print "  无失败项。注意 PEND 表示功能未交付，**不是**通过。"
  fi
}

# ============================================================================
#  主菜单
# ============================================================================
main_menu() {
  while :; do
    title "主菜单（音量上/下选择，电源键确认）"
    ui_print "  1) 安装 / 重装内置 App"
    ui_print "  2) 启动 / 停止 / 重启守护进程"
    ui_print "  3) 查看状态"
    ui_print "  4) 查看最近日志"
    ui_print "  5) 切换配置档案"
    ui_print "  6) 重装 / 移除系统证书"
    ui_print "  7) 更新规则集 / 脚本 / 模块订阅"
    ui_print "  8) 备份 / 恢复"
    ui_print "  9) 显示局域网面板地址与 token"
    ui_print " 10) 完全卸载（含还原清单）"
    ui_print " 11) 自检"
    ui_print "  0) 退出"
    hr
    printf "  选择: "

    case "$(read_key)" in
      1)  do_install_apk;     pause_key ;;
      2)  do_daemon_ctl;      pause_key ;;
      3)  do_status;          pause_key ;;
      4)  do_logs;            pause_key ;;
      5)  do_switch_profile;  pause_key ;;
      6)  do_cert;            pause_key ;;
      7)  do_update;          pause_key ;;
      8)  do_backup;          pause_key ;;
      9)  do_panel;           pause_key ;;
      10) do_uninstall;       pause_key ;;
      11) do_selfcheck;       pause_key ;;
      0|"") ui_print "  退出。"; return 0 ;;
      *)  ui_print "  无效选择：$1" ;;
    esac
  done
}

# 非交互调用（`sh action.sh status`）直接执行对应项 —— 便于脚本化与自动化测试。
case "${1:-}" in
  install-apk) do_install_apk ;;
  daemon)      do_daemon_ctl ;;
  status)      do_status ;;
  logs)        do_logs ;;
  profile)     do_switch_profile ;;
  cert)        do_cert ;;
  update)      do_update ;;
  backup)      do_backup ;;
  panel)       do_panel ;;
  uninstall)   do_uninstall ;;
  selfcheck)   do_selfcheck ;;
  *)           main_menu ;;
esac
