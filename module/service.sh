#!/system/bin/sh
# ============================================================================
#  Syfuse service.sh
#
#  执行时机：late_start service 阶段（zygote 已起，网络通常可用）。
#
#  职责：拉起守护进程 syfused，并做**存活监护**。
#
#  为什么要有监护循环：service.sh 只在开机时执行一次。
#  如果守护进程之后被杀（低内存、用户手动、崩溃），没有监护就再也不会起来，
#  用户表现为"代理莫名其妙断了"，且无法自行恢复。
# ============================================================================
MODDIR=${0%/*}
DATA_DIR="${SYFUSE_DATA_DIR:-/data/adb/syfuse}"
BIN="$MODDIR/bin/syfused"
PIDFILE="$DATA_DIR/syfused.pid"
LOGDIR="$DATA_DIR/log"

# 按架构兜底选二进制
if [ ! -x "$BIN" ]; then
  ABI=$(getprop ro.product.cpu.abi 2>/dev/null)
  case "$ABI" in
    arm64*)           BIN="$MODDIR/bin/syfused-arm64" ;;
    armeabi*|armv7*)  BIN="$MODDIR/bin/syfused-arm" ;;
    x86_64*)          BIN="$MODDIR/bin/syfused-amd64" ;;
    *)                BIN="$MODDIR/bin/syfused-arm64" ;;
  esac
fi

log() { echo "$(date '+%Y-%m-%d %H:%M:%S') [service] $1"; }

mkdir -p "$LOGDIR" 2>/dev/null
chmod 0700 "$LOGDIR" 2>/dev/null

# 等待系统启动完成，避免与系统服务争抢资源
i=0
while [ "$(getprop sys.boot_completed 2>/dev/null)" != "1" ]; do
  sleep 2
  i=$((i+1))
  [ "$i" -gt 150 ] && break   # 最多等 5 分钟，超时就继续（不要永远卡住）
done

if [ ! -x "$BIN" ]; then
  log "守护进程二进制不存在（$BIN），本次不启动任何服务"
  log "这是 M1 骨架版本的预期状态：内核随包交付在 M3。"
  exit 0
fi

# ---------- 启动 + 监护 ----------
# 监护策略：每 30 秒检查一次；连续崩溃时退避（最多退避到 5 分钟），
# 避免"启动即崩"的进程变成 CPU 打满的忙循环。
backoff=30
while :; do
  if [ -f "$PIDFILE" ]; then
    pid=$(cat "$PIDFILE" 2>/dev/null)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      # 已在运行，重置退避并继续监护
      backoff=30
      sleep 30
      continue
    fi
  fi

  log "启动 syfused（退避间隔 ${backoff}s）"
  start_ts=$(date +%s)
  nohup "$BIN" daemon >>"$LOGDIR/service.out" 2>&1 &
  sleep 5

  # 运行超过 60 秒视为"稳定启动"，退避归零；
  # 否则按指数退避，防止崩溃循环。
  now_ts=$(date +%s)
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE" 2>/dev/null)" 2>/dev/null; then
    if [ $((now_ts - start_ts)) -ge 60 ]; then
      backoff=30
    fi
    sleep 30
  else
    log "syfused 启动后 5 秒内退出，退避到 $((backoff * 2))s"
    backoff=$((backoff * 2))
    [ "$backoff" -gt 300 ] && backoff=300
    sleep "$backoff"
  fi
done
