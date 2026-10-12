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
    missing: list[str] = []
    for p in REQUIRED_PATHS:
        st, body = api(f"/repos/{args.repo}/contents/{p}?ref={branch}", tok)
        if st != 200:
            missing.append(p)
    findings.append(f"关键文件: {len(REQUIRED_PATHS) - len(missing)}/{len(REQUIRED_PATHS)} 存在")
    if missing:
        fails.append(f"远端缺少 {len(missing)} 个关键文件")

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
            print("  -- 缺失文件 --")
            for m in missing:
                print("    [MISSING] " + m)
        if fails:
            print("  -- 违规 --")
            for f in fails:
                print("    [FAIL] " + f)
            print("\n[REMOTE] FAIL")
            return 1
        print("\n[REMOTE] PASS")

    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
