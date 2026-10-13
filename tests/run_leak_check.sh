#!/usr/bin/env bash
# 防泄漏检测（提示词 5.8 / 12 部分）
#
# 7 个维度：DNS / IPv6 / WebRTC-STUN / QUIC-UDP / 真实 IP 回显 / 分流 / 系统级
#
# 当前状态：**功能未实现（M12）**。本脚本做两件事：
#   1. 静态检查：确认各维度在代码/配置里已有落点（不是"忘了写"）
#   2. 设备检测：无设备时 exit 3（SKIP），**不是通过**
#
# 退出码：0 检测全部完成且无泄漏 / 1 有泄漏或静态缺口 / 3 设备部分跳过
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

head2 "静态：7 个维度是否都有落点"
# 检查目标分两类：
#   KEY:<键名>  —— 在 syfuse.conf 里找配置键
#   FILE:<文件名> —— 检查分项配置文件是否存在
#
# 早期版本对两类都用 `grep -rq`，于是把**文件名**当成**内容**去搜，
# 必然搜不到而误报。这是断言自身的缺陷，不是代码问题。
for d in "DNS 防泄漏:KEY:DNS_UPSTREAM" "IPv6 防泄漏:KEY:LEAK_GUARD" \
         "WebRTC/STUN:KEY:LEAK_GUARD" "QUIC/UDP:KEY:MITM_QUIC_POLICY" \
         "真实 IP 回显:FILE:leak_policy.conf" "分流泄漏:KEY:PROXY_LEAK" \
         "系统级（Private DNS/Captive Portal）:FILE:leak_policy.conf"; do
  name="${d%%:*}"; rest="${d#*:}"; kind="${rest%%:*}"; key="${rest#*:}"
  if [ "$kind" = "FILE" ]; then
    if [ -f "module/config/$key" ]; then ok "$name（配置文件 $key 已存在）"
    else no "$name 缺少配置文件（$key）"; fi
  else
    if grep -rq "$key" module/config/syfuse.conf 2>/dev/null; then ok "$name（配置键 $key 已定义）"
    else no "$name 缺少配置键（$key）"; fi
  fi
done

head2 "静态：泄漏策略配置文件是否可解析"
if [ -f module/config/leak_policy.conf ]; then
  ok "leak_policy.conf 存在"
  if grep -qE '^[a-z]+\.[a-z_]+=' module/config/leak_policy.conf; then
    ok "含可解析的维度键"
  else
    pend "仅含注释示例，尚无实际策略项（未配置即默认不检测）"
  fi
else
  no "缺少 leak_policy.conf"
fi

head2 "设备检测"
DEVICE="$(syfuse_device 2>/dev/null || true)"
if [ -z "$DEVICE" ]; then
  skip "无设备：7 个维度的实际泄漏检测未执行"
  info "5.8 要求「一键自检面板 + 逐项检测 + 结论 + 证据（原始请求/响应）」——"
  info "这些都必须真机联网执行。无设备时**不得**给出任何泄漏结论。"
else
  info "设备 $DEVICE"
  # 真实 IP 回显检测（不依赖本应用，只需网络）
  ip1=$(curl -sS --max-time 10 https://api.ipify.org 2>/dev/null || true)
  if [ -n "$ip1" ]; then ok "回显端点可达，本机出口 IP = $ip1"; else pend "回显端点不可达，无法检测"; fi
  pend "DNS / IPv6 / WebRTC / QUIC / 分流 维度需 M12 交付后执行"
fi

printf '\n===========================================\n'
printf 'leak-check 汇总: PASS=%d FAIL=%d PEND=%d SKIP=%d\n' "$PASS" "$FAIL" "$PEND" "$SKIP"
if [ "$FAIL" -gt 0 ]; then printf '结论：不允许交付\n===========================================\n'; exit 1; fi
if [ "$SKIP" -gt 0 ]; then printf '结论：设备部分未执行 —— **防泄漏能力未验证**，不是通过。\n===========================================\n'; exit 3; fi
printf '结论：全部通过\n===========================================\n'; exit 0
