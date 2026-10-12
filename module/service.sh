#!/system/bin/sh
# ============================================================
#  service.sh —— 开机后期执行（zygote 已起，late_start service 阶段）
#
#  负责启动内核（若存在）。当前版本尚未随包提供内核二进制，
#  因此本脚本会如实记录"内核未安装"，而不是伪造启动成功。
# ============================================================
MODDIR=${0%/*}
DATA_DIR=/data/adb/syfuse
LOG="$DATA_DIR/service.log"
BIN="$MODDIR/bin/syfuse"

log() {
  echo "$(date '+%Y-%m-%d %H:%M:%S') $1" >> "$LOG" 2>/dev/null
}

# 等待系统启动完成，避免与系统服务争抢资源
while [ "$(getprop sys.boot_completed 2>/dev/null)" != "1" ]; do
  sleep 2
done

if [ ! -x "$BIN" ]; then
  log "内核二进制不存在（$BIN），本次不启动任何服务"
  exit 0
fi

if [ -f "$DATA_DIR/enable_autostart" ]; then
  # nohup 使内核脱离本脚本的生命周期；日志重定向到数据目录
  nohup "$BIN" serve --config "$DATA_DIR/syfuse.conf" >> "$DATA_DIR/kernel.log" 2>&1 &
  log "内核已启动 pid=$!"
else
  log "开机自启未启用，跳过"
fi

exit 0
