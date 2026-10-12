#!/system/bin/sh
#
# action.sh —— 音量键交互入口（提示词 6.5 要求模块骨架包含此文件）。
#
# 触发方式：在 KernelSU / Magisk 管理器中对模块点「操作」按钮。
# 由于此脚本在管理器环境中执行，无法读取音量键实时输入，
# 因此它的职责是**输出状态信息**，而不是复杂交互。
#
# M1 状态：实现基础状态输出（真实可用的功能）。

MODDIR=${0%/*}
SYFUSE_DATA="/data/adb/syfuse"

echo "================================================"
echo " Syfuse 模块状态"
echo "================================================"
echo ""

# 模块版本
if [ -f "${MODDIR}/module.prop" ]; then
    VERSION=$(grep '^version=' "${MODDIR}/module.prop" | cut -d= -f2)
    echo "版本：       ${VERSION}"
fi

# 安装信息
if [ -f "${SYFUSE_DATA}/install.info" ]; then
    echo "root 提供方： $(grep '^root_provider=' "${SYFUSE_DATA}/install.info" | cut -d= -f2)"
    echo "内核 ABI：    $(grep '^kernel_abi=' "${SYFUSE_DATA}/install.info" | cut -d= -f2)"
    INSTALLED_AT=$(grep '^installed_at=' "${SYFUSE_DATA}/install.info" | cut -d= -f2)
    echo "安装时间：   ${INSTALLED_AT}"
else
    echo "安装信息：   缺失（模块可能未正确安装）"
fi

echo ""

# 能力探测结果
if [ -f "${SYFUSE_DATA}/caps.probe" ]; then
    echo "能力探测："
    while IFS='=' read -r key value; do
        case "${value}" in
            1) status="可用" ;;
            0) status="不可用" ;;
            *) status="未知" ;;
        esac
        echo "  ${key}: ${status}"
    done < "${SYFUSE_DATA}/caps.probe"
else
    echo "能力探测：   尚未执行（重启后生成）"
fi

echo ""

# 开机自启状态
if [ -f "${SYFUSE_DATA}/autostart.enabled" ]; then
    echo "开机自启：   已启用"
else
    echo "开机自启：   未启用（默认）"
fi

echo ""

# 已安装改动清单摘要（提示词 6.9 要求可查）
if [ -f "${SYFUSE_DATA}/manifest/installed.json" ]; then
    ENTRY_COUNT=$(grep -o '"entryId"' "${SYFUSE_DATA}/manifest/installed.json" | wc -l)
    echo "已登记改动： ${ENTRY_COUNT} 项"
else
    echo "已登记改动： 清单缺失"
fi

echo ""
echo "数据目录：   ${SYFUSE_DATA}"
echo "================================================"
