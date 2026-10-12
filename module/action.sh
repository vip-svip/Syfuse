#!/system/bin/sh
# ============================================================
#  action.sh —— 模块管理器中的「操作」按钮入口
#
#  用音量键选择、电源键确认（KernelSU 的标准交互约定）。
#  设计原则：只暴露可逆操作；任何会改系统分区的动作都不在这里提供。
# ============================================================
MODDIR=${0%/*}
DATA_DIR=/data/adb/syfuse

mkdir -p "$DATA_DIR" 2>/dev/null

state() {
  if [ -f "$1" ]; then echo "开"; else echo "关"; fi
}

toggle() {
  if [ -f "$1" ]; then
    rm -f "$1"
    echo "已关闭"
  else
    : > "$1"
    echo "已开启"
  fi
}

show_menu() {
  echo "================================"
  echo " Syfuse 控制台"
  echo "================================"
  echo " 1. hosts 注入       [$(state "$DATA_DIR/enable_hosts_inject")]"
  echo " 2. 开机自启         [$(state "$DATA_DIR/enable_autostart")]"
  echo " 3. 广告过滤         [$(state "$DATA_DIR/enable_filter")]"
  echo " 4. 查看内核状态"
  echo " 5. 查看已安装改动清单"
  echo " 0. 退出"
  echo "================================"
}

wait_key() {
  # 返回 0=电源键确认, 1=音量+, 2=音量-
  key=$(getevent -qlc 1 2>/dev/null | tail -n 1)
  case "$key" in
    *KEY_VOLUMEUP*)   return 1 ;;
    *KEY_VOLUMEDOWN*) return 2 ;;
    *KEY_POWER*)      return 0 ;;
    *)                return 3 ;;
  esac
}

kernel_status() {
  if [ -x "$MODDIR/bin/syfuse" ]; then
    echo "内核二进制: 存在 ($MODDIR/bin/syfuse)"
    pidof syfuse >/dev/null 2>&1 && echo "运行状态: 运行中" || echo "运行状态: 未运行"
  else
    echo "内核二进制: 不存在"
    echo "说明: 本版本尚未随包提供内核，特权层骨架已就位。"
  fi
}

installed_manifest() {
  if [ -f "$DATA_DIR/installed.json" ]; then
    cat "$DATA_DIR/installed.json"
  else
    echo "尚无已安装的改动（manifest/installed.json 不存在）"
  fi
}

while true; do
  show_menu
  echo "音量键移动，电源键确认："
  wait_key
  case $? in
    0)
      echo "已确认，请输入编号（0-5）："
      read -r choice
      case "$choice" in
        1) echo "hosts 注入: $(toggle "$DATA_DIR/enable_hosts_inject")" ;;
        2) echo "开机自启: $(toggle "$DATA_DIR/enable_autostart")" ;;
        3) echo "广告过滤: $(toggle "$DATA_DIR/enable_filter")" ;;
        4) kernel_status ;;
        5) installed_manifest ;;
        0) exit 0 ;;
        *) echo "无效选择" ;;
      esac
      ;;
    *)
      # 未识别按键：重绘菜单而不是退出，避免用户误触后控制台消失
      ;;
  esac
done
