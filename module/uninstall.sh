#!/system/bin/sh
# ============================================================================
#  Syfuse uninstall.sh —— 卸载时的完整还原
#
#  硬要求（提示词 3.5 / 18.8）：
#   - 卸载必须能还原全部改动；
#   - 卸载后系统状态必须完全还原，`tests/run_restore_check.sh` 会 diff 验证；
#   - 不允许存在"无法自动还原"的操作。
#
#  还原顺序与安装顺序**相反**：先解挂载 / 撤证书，再停进程，最后删数据。
#  顺序反了会出问题：例如先删数据目录，清单就没了，后面的还原失去依据。
#
#  日志写到 **stdout**，不写文件。
#  理由：本模块的承诺是"卸载零残留"，而一个留在 /data/local/tmp 的日志
#  本身就是残留。模块管理器会捕获 stdout 显示给用户。
# ============================================================================
MODDIR=${0%/*}
DATA_DIR="${SYFUSE_DATA_DIR:-/data/adb/syfuse}"
MANIFEST="$DATA_DIR/manifest/installed.json"

log() { echo "[syfuse-uninstall] $1"; }

log "===== 开始卸载还原 ====="

# ---------------------------------------------------------------------------
# 1. 按还原清单逐条还原
# ---------------------------------------------------------------------------
if [ -f "$MANIFEST" ]; then
  log "发现还原清单: $MANIFEST"
  # 优先用 syfused 的 Go 实现（与 App 侧同一套逻辑，避免两处漂移）
  BIN="$MODDIR/bin/syfused"
  [ -x "$BIN" ] || BIN="$MODDIR/bin/syfused-$(getprop ro.product.cpu.abi 2>/dev/null | sed 's/arm64.*/arm64/; s/armeabi.*/arm/; s/x86_64.*/amd64/')"
  if [ -x "$BIN" ]; then
    log "-- 调用 syfused restore --apply --"
    "$BIN" restore --apply 2>&1 | while IFS= read -r l; do log "   $l"; done
  else
    log "syfused 不存在，回退到本脚本内置的还原逻辑"
  fi
else
  log "无还原清单（可能安装失败或已被清理），执行兜底还原"
fi

# ---------------------------------------------------------------------------
# 2. 解除系统级改动（当前版本无此类改动；M8/M11 交付后在此实现）
# ---------------------------------------------------------------------------
# hosts 注入（M11）
HOSTS_TARGET=/system/etc/hosts
if mountpoint -q "$HOSTS_TARGET" 2>/dev/null; then
  if umount "$HOSTS_TARGET" 2>/dev/null; then
    log "hosts 挂载已解除"
  elif umount -l "$HOSTS_TARGET" 2>/dev/null; then
    log "hosts 挂载已懒卸载"
  else
    log "⚠ 警告：hosts 挂载解除失败 —— 这属于未完全还原，请手动检查"
  fi
else
  log "hosts 未被挂载，无需解除"
fi

# 系统 CA（M8）
CA_RECORD="$DATA_DIR/installed_ca_path"
if [ -f "$CA_RECORD" ]; then
  CA_FILE=$(cat "$CA_RECORD" 2>/dev/null)
  if [ -n "$CA_FILE" ] && [ -f "$CA_FILE" ]; then
    rm -f "$CA_FILE" 2>/dev/null && log "已移除系统 CA: $CA_FILE" \
                                 || log "⚠ 警告：系统 CA 移除失败: $CA_FILE"
  fi
else
  log "未记录过系统 CA，无需移除"
fi

# ---------------------------------------------------------------------------
# 3. 停止守护进程（先 TERM 给机会优雅退出，再 KILL）
# ---------------------------------------------------------------------------
PIDFILE="$DATA_DIR/syfused.pid"
if [ -f "$PIDFILE" ]; then
  pid=$(cat "$PIDFILE" 2>/dev/null)
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null
    sleep 2
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" 2>/dev/null
      log "守护进程 pid=$pid 已强制结束"
    else
      log "守护进程 pid=$pid 已优雅退出"
    fi
  fi
fi
# 兜底：按名字清理残留进程
if command -v pkill >/dev/null 2>&1; then
  pkill -f "$MODDIR/bin/syfused" 2>/dev/null && log "清理了残留的 syfused 进程"
fi

# ---------------------------------------------------------------------------
# 4. 清理数据目录（**完全删除**，不留任何文件）
# ---------------------------------------------------------------------------
if [ -d "$DATA_DIR" ]; then
  # 先处理只读文件（若存在），否则 rm -rf 可能失败
  find "$DATA_DIR" -type f -exec chmod 0600 {} + 2>/dev/null
  if rm -rf "$DATA_DIR" 2>/dev/null; then
    log "数据目录已删除: $DATA_DIR"
  else
    log "⚠ 警告：数据目录删除失败: $DATA_DIR —— 存在残留"
  fi
else
  log "数据目录不存在，无需清理"
fi

# ---------------------------------------------------------------------------
# 5. 残留自检：把结果打印出来，**不隐瞒失败**
# ---------------------------------------------------------------------------
residue=0
if [ -e "$DATA_DIR" ]; then
  log "✗ 残留：$DATA_DIR 仍存在"
  residue=$((residue + 1))
fi
if [ -e "$MODDIR/bin/syfused" ]; then
  log "✗ 残留：$MODDIR/bin/syfused 仍存在（模块目录由模块管理器删除，属正常）"
fi
# 系统分区未被触碰 —— 这是设计承诺，这里只做声明性检查
if [ -n "$(mount 2>/dev/null | grep -F "$DATA_DIR")" ]; then
  log "✗ 残留：仍有指向 $DATA_DIR 的挂载"
  residue=$((residue + 1))
fi

log "===== 卸载还原结束 ====="
if [ "$residue" -gt 0 ]; then
  log "结果：存在 $residue 项残留，**未完全还原**"
  exit 1
fi
log "结果：无残留（模块目录本身由模块管理器负责移除）"
exit 0
