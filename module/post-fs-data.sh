#!/system/bin/sh
# ============================================================
#  post-fs-data.sh —— 开机早期执行（/data 已挂载，zygote 未起）
#
#  只做"必须在早期完成、且可完整还原"的事：
#    - 准备数据目录权限
#    - 恢复/安装 hosts 注入（走 bind mount，不写系统分区）
#
#  明确不做的事：任何直接写入 /system 的操作（6.9 侵入原则）。
# ============================================================
MODDIR=${0%/*}
DATA_DIR=/data/adb/syfuse
LOG="$DATA_DIR/post-fs-data.log"

log() {
  echo "$(date '+%Y-%m-%d %H:%M:%S') $1" >> "$LOG" 2>/dev/null
}

mkdir -p "$DATA_DIR" 2>/dev/null
chmod 0700 "$DATA_DIR" 2>/dev/null

# hosts 注入：仅在开关文件存在且 overlayfs 可用时执行
if [ -f "$DATA_DIR/enable_hosts_inject" ] && [ -f "$DATA_DIR/hosts" ]; then
  TARGET=/system/etc/hosts
  # 先判断是否已经是我们的 bind mount，避免重复挂载
  if ! mountpoint -q "$TARGET" 2>/dev/null; then
    if mount -o bind "$DATA_DIR/hosts" "$TARGET" 2>/dev/null; then
      log "hosts 注入成功（bind mount）"
    else
      log "hosts 注入失败：bind mount 被拒绝（设备可能不支持）"
    fi
  else
    log "hosts 已是挂载点，跳过"
  fi
else
  log "hosts 注入未启用，跳过"
fi

exit 0
