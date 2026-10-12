#!/system/bin/sh
#
# post-fs-data 阶段脚本。
#
# 执行时机：文件系统挂载完成后、zygote 启动前。
# 这是执行 bind mount / OverlayFS 挂载的唯一正确时机 ——
# 晚于此时机挂载会导致已启动的进程看不到新挂载点。
#
# M1 状态：只做环境准备与自检，不执行任何挂载。
#         实际挂载逻辑在 M11 实现（提示词 6.5）。

MODDIR=${0%/*}
SYFUSE_DATA="/data/adb/syfuse"

# 内核提供的挂载工具路径
MAGISK_MOUNT="${MAGISK_PATH:-/data/adb/magisk}/magisk"
KSU_MOUNT="${MODDIR}/../ksud"

log() {
    echo "[syfuse][post-fs-data] $1" >> "${SYFUSE_DATA}/logs/post-fs-data.log"
}

# 等待数据分区就绪：本阶段极早期，/data 可能尚未完全可写
# 最多等 5 秒，超时则记录并退出（不阻塞开机）
waited=0
while [ ! -w "/data/adb" ] && [ "${waited}" -lt 5 ]; do
    sleep 1
    waited=$((waited + 1))
done

if [ ! -w "/data/adb" ]; then
    log "错误：/data/adb 不可写，跳过本次执行"
    exit 0
fi

mkdir -p "${SYFUSE_DATA}/logs"

log "开始执行"

# ---------------------------------------------------------------------------
# 挂载能力自检
#
# 为什么要在 post-fs-data 做这项检查而不是交给 App：
# App 运行在晚得多的时间点，届时再发现「不支持 OverlayFS」，
# 用户已经看到「已启用」的界面了 —— 那是虚假的启用状态。
# ---------------------------------------------------------------------------
if [ -d "/sys/module/overlay" ]; then
    log "OverlayFS 可用"
    echo "overlayfs=1" >> "${SYFUSE_DATA}/caps.probe"
else
    log "OverlayFS 不可用"
    echo "overlayfs=0" >> "${SYFUSE_DATA}/caps.probe"
fi

# ---------------------------------------------------------------------------
# 实际挂载点（M11 填充）
#
# 预留位置的说明：此处将来会执行
#   1. hosts 注入（bind mount /data/adb/syfuse/hosts → /system/etc/hosts）
#   2. 系统 CA 注入（以独立子模块形式，可单独卸载）
# 每完成一项挂载，必须向 manifest/installed.json 追加一条记录。
# ---------------------------------------------------------------------------

log "执行完毕（M1 骨架，未执行挂载）"
