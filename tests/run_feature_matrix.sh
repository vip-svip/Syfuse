#!/usr/bin/env bash
# 功能可用性门禁（提示词 12.2）
#
# 这是本项目的**验收核心**：每个功能必须有「最低可用路径」，
# 且必须真机验证。状态只有三种：可用 / 实验性 / 不可用。
#
# 本脚本分两半：
#   静态部分 —— 在无设备环境下检查"每个功能是否有可执行的验证路径定义"
#   设备部分 —— 需要真机；无设备时 exit 3（SKIP），**不是通过**
#
# 退出码：0 静态全过且设备部分已跑 / 1 有失败 / 3 设备部分跳过
set -uo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/env.sh"
cd "$SYFUSE_ROOT"
PASS=0; FAIL=0; PEND=0; SKIP=0
ok()   { printf '  [PASS] %s\n' "$1"; PASS=$((PASS+1)); }
no()   { printf '  [FAIL] %s\n' "$1"; FAIL=$((FAIL+1)); }
pend() { printf '  [PEND] %s\n' "$1"; PEND=$((PEND+1)); }
skip() { printf '  [SKIP] %s\n' "$1"; SKIP=$((SKIP+1)); }
info() { printf '  :: %s\n' "$1"; }
head2(){ printf '\n== %s ==\n' "$1"; }

head2 "静态部分：最低可用路径是否已定义"
# 12.2 列出的 13 个关键功能，每个都必须有定义好的最低可用路径
MATRIX="$(cat <<'EOF'
代理上网|导入订阅 → 选节点 → 打开开关 → 真机访问网络成功，速率非零|不可用|M3
分流|配规则 → 规则测试显示命中 → 实际请求按规则走对应出口|不可用|M5
模块加载|导入模块 → 启用 → 其对应用生效|不可用|M7
脚本|导入脚本 → 命中匹配 URL → 脚本日志有输出 → 生效结果可观测|不可用|M7
MITM|生成 CA → 安装 → 加入解密主机 → 抓包查看器看到明文|不可用|M8
Hook|检测框架 → 启用 pinning 绕过 → 目标 App 的 HTTPS 明文可见|不可用|M11
过滤拦截|加规则 → 访问目标域名被拦 → 拦截日志与计数增加|不可用|M10
开屏跳过|启用规则 → 打开目标 App 开屏被跳过 → 命中计数增加|不可用|M13
网络封禁|加域名/IP/端口 → 访问被阻断 → 日志可见|不可用|M13
防泄漏|一键自检 → 各维度出结论 → 一键修复后复检通过|不可用|M12
统计|使用 1 分钟 → 统计页出现该 App/域名的流量与连接记录|不可用|M9
备份还原|导出 → 清空 → 导入 → 配置与规则完整恢复|不可用|M13
模块安装卸载|刷入 → 功能可用 → 卸载 → 系统状态完全还原|实验性|M1
EOF
)"

while IFS='|' read -r name path status milestone; do
  [ -z "$name" ] && continue
  if [ -n "$path" ]; then
    ok "$name（最低可用路径已定义，当前状态=$status，里程碑=$milestone）"
  else
    no "$name 缺少最低可用路径定义"
  fi
done <<< "$MATRIX"

head2 "静态部分：README 的功能可用性报告是否覆盖全部功能"
if [ -f README.md ] && grep -q "功能可用性报告" README.md; then
  ok "README 含功能可用性报告"
  # 报告里必须出现三种状态标记，否则说明只写了"能用"的部分
  for s in 可用 实验性 不可用; do
    grep -q "$s" README.md && ok "报告含状态标记「$s」" || no "报告缺少状态标记「$s」"
  done
else
  no "README 缺少功能可用性报告"
fi

head2 "静态部分：死 UI 排查（12.2 第 4 条）"
# 界面上呈现为可用的开关，背后必须有真实实现。
# 静态可查的代理指标：ViewModel 里是否存在"只改状态不做事"的函数。
DEAD=$(grep -rnE "^\s*fun (toggle|enable|start|connect)[A-Za-z]*\(.*\)\s*\{\s*$" ui/src/main --include=*.kt 2>/dev/null | wc -l | tr -d ' ')
info "UI 交互入口函数数：$DEAD（需人工核对是否都接了实现）"
if grep -rq "not_implemented\|尚未实现" module/action.sh 2>/dev/null; then
  ok "action.sh 对未实现项有显式标注（不是死菜单）"
else
  no "action.sh 缺少未实现标注，可能把未实现项伪装成可用"
fi

head2 "设备部分"
DEVICE="$(syfuse_device 2>/dev/null || true)"
if [ -z "$DEVICE" ]; then
  skip "无设备：13 个功能的最低可用路径均未在真机执行"
  info "12.2 第 2 条要求「真机优先；模拟器只能作补充」——"
  info "因此在真机跑通之前，**没有任何功能可以标为「可用」**。"
  info "当前唯一标为「可用」的只有还原清单机制（有 Go 单测 + 沙箱端到端证据）。"
else
  info "设备 $DEVICE"
  ADB="$(syfuse_adb)"
  PKG="io.github.vipsvip.syfuse.debug"
  APK="$(ls -1 app/build/outputs/apk/debug/*.apk 2>/dev/null | head -1)"
  if [ -n "$APK" ]; then
    "$ADB" -s "$DEVICE" install -r -t "$APK" >/dev/null 2>&1 && ok "APK 安装成功" || no "APK 安装失败"
    "$ADB" -s "$DEVICE" shell am start -n "$PKG/io.github.vipsvip.syfuse.MainActivity" >/dev/null 2>&1
    sleep 4
    "$ADB" -s "$DEVICE" shell pidof "$PKG" >/dev/null 2>&1 && ok "应用启动且进程存活" || no "应用启动后进程不存在"
    # 逐功能：代理上网是最关键的一条
    pend "代理上网最低可用路径：需 M3 内核接入后才能执行"
    pend "其余 12 项：需各自里程碑交付后才能执行"
  else
    no "找不到 debug APK，请先 :app:assembleDebug"
  fi
fi

printf '\n===========================================\n'
printf 'feature-matrix 汇总: PASS=%d FAIL=%d PEND=%d SKIP=%d\n' "$PASS" "$FAIL" "$PEND" "$SKIP"
if [ "$FAIL" -gt 0 ]; then printf '结论：不允许交付\n'; printf '===========================================\n'; exit 1; fi
if [ "$SKIP" -gt 0 ]; then
  printf '结论：设备部分未执行 —— **里程碑未达成**，不是通过。\n'
  printf '===========================================\n'
  exit 3
fi
printf '结论：全部通过\n===========================================\n'
exit 0
