#!/system/bin/sh
#
# late_start service 阶段脚本。
#
# 执行时机：系统启动完成后（非阻塞开机流程）。
# 适合做需要完整系统环境的工作：启动守护进程、恢复配置等。
#
# M1 状态：只做日志与会话准备，不启动任何守护进程。
#
# 重要：本脚本运行在**非阻塞**阶段，因此任何耗时操作都不会拖慢开机。
# 提示词 5.9 要求「开机自启」，但自启的对象是用户的代理连接，
# 由 App 侧根据用户设置决定，不是这里无条件启动。

MODDIR=${0%/*}
SYFUSE_DATA="/data/adb/syfuse"

mkdir -p "${SYFUSE_DATA}/logs"

log() {
    echo "[syfuse][service] $(date '+%Y-%m-%d %H:%M:%S') $1" >> "${SYFUSE_DATA}/logs/service.log"
}

# 等待 boot_completed，确保系统环境完全就绪
until [ "$(getprop sys.boot_completed)" = "1" ]; do
    sleep 2
done

log "系统启动完成，进入 service 阶段"

# ---------------------------------------------------------------------------
# 开机自启判断
#
# 严格遵守提示词 5.9 与 R6：默认**不**自动启动。
# 只有用户在 App 内显式开启 autostart 后才启动。
# ---------------------------------------------------------------------------
AUTOSTART_FLAG="${SYFUSE_DATA}/autostart.enabled"

if [ -f "${AUTOSTART_FLAG}" ]; then
    log "检测到 autostart 已启用，准备启动（M11 接入实际启动逻辑）"
else
    log "autostart 未启用（默认状态），不启动任何服务"
fi

log "service 阶段结束（M1 骨架）"
