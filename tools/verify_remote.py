#!/usr/bin/env python3
"""远端仓库状态核验：把"我已经推送/我已经设置成私有/许可证是 GPL"变成可核对的 API 事实。

提示词的三条硬要求靠本脚本落地：
  - 仓库**默认私有**，且不得自我升级为公开；
  - 许可证必须落在 GPL-3.0-or-later 族，**不得**被换成 MIT；
  - 关键文件（module.prop / update.json / LICENSE / README.md 等）确实存在于远端。

没有 token 时本脚本 exit 3（SKIP），**不**返回 0 —— 不允许把"没检查"当成"检查通过"。

用法：
    GITHUB_TOKEN=... python tools/verify_remote.py --repo vip-svip/SY-Fusion
    python tools/verify_remote.py --repo owner/name --expect-public false
    python tools/verify_remote.py --repo owner/name --json
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

API = "https://api.github.com"

REQUIRED_PATHS = [
    "README.md",
    "LICENSE",
    "NOTICE",
    "CHANGELOG.md",
    "CONTRIBUTING.md",
    "SECURITY.md",
    "DISCLAIMER.md",
    "module/module.prop",
    "module/customize.sh",
    "module/service.sh",
    "module/post-fs-data.sh",
    "module/action.sh",
    "module/uninstall.sh",
    "module/META-INF/com/google/android/update-binary",
    "module/META-INF/com/google/android/updater-script",
    "update.json",
    "tests/run_unit.sh",
    "tests/run_e2e.sh",
    "tests/run_module_checks.sh",
    "tools/scan_secrets.py",
    "tools/verify_remote.py",
    "tools/verify_files.py",
    "tools/set_owner.sh",
]

GPL_OK = ("GPL-3.0", "GPL-3.0-only", "GPL-3.0-or-later", "GPL-3.0+")
GPL_BAD = ("MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "Unlicense", "0BSD")


def token() -> str | None:
    for k in ("GITHUB_TOKEN", "GH_TOKEN", "SYFUSE_GITHUB_TOKEN"):
        v = os.environ.get(k)
        if v:
            return v.strip()
    return None


def api(path: str, tok: str) -> tuple[int, object]:
    req = urllib.request.Request(API + path, headers={
        "Accept": "application/vnd.github+json",
        "User-Agent": "syfuse-verify-remote",
        "Authorization": f"Bearer {tok}",
        "X-GitHub-Api-Version": "2022-11-28",
    })
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, json.loads(r.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", "ignore")
        try:
            return e.code, json.loads(body)
        except Exception:
            return e.code, {"raw": body}
    except Exception as e:  # 网络层
        return 0, {"error": str(e)}


# 可重试的瞬时错误：限流（403/429）与服务端 5xx。
# **为什么必须区分**：把"没查成"当成"文件不存在"会产出**假失败** ——
# 本脚本曾经因为一次限流把确实存在于远端的 tools/set_owner.sh 报成 MISSING，
# 进而让整个核验 FAIL。验证脚本报假失败比不报还糟：它会把真问题淹没掉。
TRANSIENT_STATUS = {0, 403, 429, 500, 502, 503, 504}


def api_retry(path: str, tok: str, attempts: int = 3) -> tuple[int, object]:
    """带退避重试的 api()。只对瞬时错误重试；404 等确定性结果立即返回。"""
    last: tuple[int, object] = (0, {})
    for i in range(attempts):
        st, body = api(path, tok)
        if st == 200 or st not in TRANSIENT_STATUS:
            return st, body
        last = (st, body)
        if i < attempts - 1:
            time.sleep(1.0 * (i + 1))  # 1s / 2s
    return last


def tree_paths(repo: str, branch: str, tok: str) -> tuple[set[str] | None, str]:
    """一次调用取回分支上全部文件路径。

    返回 (路径集合, 说明)。路径集合为 None 表示取不到（调用方应回退到逐路径查询）。

    为什么优先用 tree 而不是逐个 contents：
      1. 23 个关键文件 = 23 次请求，很容易撞限流，而**限流会被误读成文件缺失**；
      2. tree 一次请求就能拿到全量路径，还能顺带发现"文件被改名/挪走"这类整体漂移；
      3. 少 22 次请求，对 API 配额友好。
    """
    st, body = api_retry(f"/repos/{repo}/git/trees/{branch}?recursive=1", tok)
    if st != 200 or not isinstance(body, dict):
        return None, f"tree API 不可用（HTTP {st}）"
    if body.get("truncated"):
        # 树被截断时不能用它判定"缺失"——否则会假报
        return None, "tree 被 GitHub 截断（仓库文件过多）"
    entries = body.get("tree") or []
    paths = {e.get("path") for e in entries if isinstance(e, dict) and e.get("path")}
    return paths, f"tree 取回 {len(paths)} 个路径"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True, help="owner/name")
    ap.add_argument("--branch", default=None, help="默认取仓库 default_branch")
    ap.add_argument("--expect-public", choices=["true", "false", "any"], default="false",
                    help="期望的可见性，默认 false（私有）")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()

    tok = token()
    if not tok:
        print("[REMOTE] SKIP — 未提供 GITHUB_TOKEN/GH_TOKEN，无法核验远端状态。"
              "\n         这不是通过；请在有 token 的环境重跑。", file=sys.stderr)
        return 3

    findings: list[str] = []
    fails: list[str] = []

    status, repo = api(f"/repos/{args.repo}", tok)
    if status == 0:
        print(f"[REMOTE] SKIP — 网络不可达: {repo.get('error')}", file=sys.stderr)
        return 3
    if status == 404:
        print(f"[REMOTE] FAIL — 仓库 {args.repo} 不存在或 token 无权限访问（404）")
        return 1
    if status != 200:
        print(f"[REMOTE] FAIL — 查询仓库失败 HTTP {status}: {json.dumps(repo, ensure_ascii=False)[:400]}")
        return 1

    private = bool(repo.get("private"))
    visibility = repo.get("visibility") or ("private" if private else "public")
    findings.append(f"仓库可见性: {visibility}")
    if args.expect_public == "false" and not private:
        fails.append("仓库是**公开**的，但要求默认私有（不得自我升级为公开）")
    if args.expect_public == "true" and private:
        fails.append("仓库是私有的，但期望公开")

    default_branch = repo.get("default_branch", "main")
    branch = args.branch or default_branch
    findings.append(f"默认分支: {default_branch}")

    # ---------- 许可证 ----------
    lic = (repo.get("license") or {})
    spdx = (lic.get("spdx_id") or "NOASSERTION")
    lic_name = lic.get("name") or "无"
    findings.append(f"许可证: {spdx} ({lic_name})")
    if spdx in GPL_BAD:
        fails.append(f"许可证被换成了 {spdx}，要求 GPL-3.0-or-later 族")
    elif spdx in GPL_OK:
        pass
    else:
        fails.append(f"许可证为 {spdx}，不在 GPL-3.0-or-later 族内（NOASSERTION 通常意味着仓库里没有 LICENSE 文件）")

    # ---------- 关键文件存在性 ----------
    #
    # 三种结果必须分开，不能都算"缺失"：
    #   存在        → 计入通过
    #   404 确实缺失 → FAIL
    #   查不出来     → **无法确定**（限流/5xx/网络），既不算通过也不算缺失，
    #                 整体判为"未完成核验"并 exit 3（沿用本仓库的 SKIP 约定）
    missing: list[str] = []
    undetermined: list[str] = []

    paths, why = tree_paths(args.repo, branch, tok)
    if paths is not None:
        findings.append(f"关键文件核验方式: {why}（单次调用，不受逐路径限流影响）")
        for p in REQUIRED_PATHS:
            if p not in paths:
                missing.append(p)
    else:
        # 回退：逐个 contents 查询（带重试）。此时更要严格区分 404 与其他错误。
        findings.append(f"关键文件核验方式: 逐路径 contents 查询（回退；{why}）")
        # 一旦确认 API 整体不可用（瞬时错误），**不要**再对剩下每个路径各重试一轮：
        # 24 个路径 × 3 次 × 退避会跑好几分钟，且结果注定相同。
        # 直接把剩余路径全部记为"无法确定"，既快又不会把"没查成"混成"缺失"。
        api_down = False
        for p in REQUIRED_PATHS:
            if api_down:
                undetermined.append(f"{p}（未查询：API 已确认不可用）")
                continue
            st, _ = api_retry(f"/repos/{args.repo}/contents/{p}?ref={branch}", tok)
            if st == 200:
                continue
            if st == 404:
                missing.append(p)
            else:
                undetermined.append(f"{p}（HTTP {st}）")
                api_down = True

    findings.append(f"关键文件: {len(REQUIRED_PATHS) - len(missing) - len(undetermined)}/{len(REQUIRED_PATHS)} 存在")
    if missing:
        fails.append(f"远端缺少 {len(missing)} 个关键文件")
    if undetermined:
        fails.append(
            f"有 {len(undetermined)} 个关键文件**无法确定**（非 404，多为限流或服务端错误）——"
            "这不是「缺失」，但也不是「通过」"
        )

    # ---------- 发布物 ----------
    st, rel = api(f"/repos/{args.repo}/releases", tok)
    if st == 200 and isinstance(rel, list):
        findings.append(f"Release 数: {len(rel)}")
        if rel:
            latest = rel[0]
            findings.append(f"最新 Release: {latest.get('tag_name')} assets={len(latest.get('assets') or [])}")
    else:
        findings.append("Release 列表: 不可读（权限或未发布）")

    # ---------- 输出 ----------
    result = {
        "repo": args.repo,
        "branch": branch,
        "private": private,
        "visibility": visibility,
        "license": spdx,
        "missing_paths": missing,
        "undetermined_paths": undetermined,
        "findings": findings,
        "failures": fails,
    }
    if args.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
    else:
        print(f"== 远端核验 {args.repo}@{branch} ==")
        for f in findings:
            print("  - " + f)
        if missing:
            print("  -- 缺失文件（404，确认不存在）--")
            for m in missing:
                print("    [MISSING] " + m)
        if undetermined:
            print("  -- 无法确定（非 404，不能当作缺失，也不能当作通过）--")
            for u in undetermined:
                print("    [UNDETERMINED] " + u)
        if fails:
            print("  -- 违规 --")
            for f in fails:
                print("    [FAIL] " + f)
            # 区分「真违规」与「没查成」：前者 exit 1，后者 exit 3（沿用 SKIP 约定）。
            # 不能都返回 1 —— 那会让人以为仓库真的有问题。
            if missing or (fails and not undetermined):
                print("\n[REMOTE] FAIL")
                return 1
            print("\n[REMOTE] INCONCLUSIVE — 核验未完成，**不是通过**")
            return 3
        print("\n[REMOTE] PASS")

    if missing:
        return 1
    if undetermined:
        return 3
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
