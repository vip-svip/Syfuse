#!/system/bin/sh
# ============================================================
#  uninstall.sh —— 卸载时的完整还原
#
#  R6 / 6.9 的硬要求：卸载必须能还原全部改动，且不允许存在
#  "无法自动还原"的操作。本脚本逐项还原并记录结果。
#
#  还原顺序与安装顺序相反：先解挂载，再删数据，最后清理标记文件。
# ============================================================
MODDIR=${0%/*}
DATA_DIR=/data/adb/syfuse
LOG="$DATA_DIR/uninstall.log"

log() {
  echo "$(date '+%Y-%m-%d %H:%M:%S') $1" >> "$LOG" 2>/dev/null
}

mkdir -p "$DATA_DIR" 2>/dev/null
log "===== 开始卸载还原 ====="

# ---------- 1. 解除 hosts 注入 ----------
TARGET=/system/etc/hosts
if mountpoint -q "$TARGET" 2>/dev/null; then
  if umount "$TARGET" 2>/dev/null; then
    log "hosts 挂载已解除，系统原始 hosts 恢复"
  else
    # 先尝试懒卸载：进程仍持有文件句柄时不阻塞卸载流程
    umount -l "$TARGET" 2>/dev/null && log "hosts 挂载已懒卸载" || log "警告：hosts 挂载解除失败"
  fi
else
  log "hosts 未被挂载，无需解除"
fi

# ---------- 2. 移除系统 CA（若曾安装） ----------
CA_FILE=$(cat "$DATA_DIR/installed_ca_path" 2>/dev/null)
if [ -n "$CA_FILE" ] && [ -f "$CA_FILE" ]; then
  if rm -f "$CA_FILE" 2>/dev/null; then
    log "已移除系统 CA: $CA_FILE"
  else
    log "警告：系统 CA 移除失败: $CA_FILE"
  fi
fi

# ---------- 3. 停止内核 ----------
if pidof syfuse >/dev/null 2>&1; then
  pkill -f "$MODDIR/bin/syfuse" 2>/dev/null && log "内核进程已停止" || log "警告：内核进程停止失败"
fi

# ---------- 4. 清理数据目录 ----------
# 保留 uninstall.log 供事后核查，其余全部删除
if [ -d "$DATA_DIR" ]; then
  find "$DATA_DIR" -mindepth 1 -maxdepth 1 ! -name 'uninstall.log' -exec rm -rf {} + 2>/dev/null
  log "数据目录已清理（保留 uninstall.log）"
fi

log "===== 卸载还原结束 ====="
exit 0
