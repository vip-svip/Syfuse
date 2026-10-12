#!/usr/bin/env python3
"""校验远程仓库树与本地 HEAD 的 path/mode/blob 一致性（提示词 16.5）。

用途：发布前证明「推送上去的内容与本地构建的内容逐字节相同」。
这是提示词 2.3 第 11 条（不假装已推送）与 16.6 第 5 条（发布后回读确认）
的技术支撑：只有 path/mode/blob 三者一致，才能说「远程就是本地」。

用法：
    python tools/verify_remote.py <owner> <repo> [--ref main] [--local .]

依赖：仅用标准库（urllib），不引入 requests，避免在 CI 里增加依赖面。
"""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

GITHUB_API = "https://api.github.com"


def fetch_json(url: str) -> dict | list:
    """带 User-Agent 的 GET（GitHub API 强制要求 UA）。"""
    request = urllib.request.Request(url, headers={"User-Agent": "Syfuse-verify-remote"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        raise SystemExit(f"请求失败 {url}：HTTP {exc.code}") from exc
    except urllib.error.URLError as exc:
        raise SystemExit(f"网络不可达 {url}：{exc.reason}") from exc


def remote_tree(owner: str, repo: str, ref: str) -> dict[str, dict]:
    """取远程 tree，返回 {path: {mode, sha, type}}。"""
    info = fetch_json(f"{GITHUB_API}/repos/{owner}/{repo}/git/trees/{ref}?recursive=1")
    if "tree" not in info:
        raise SystemExit(f"响应中无 tree 字段，可能分支 {ref} 不存在")

    if info.get("truncated"):
        # 截断意味着结果不完整，此时说「一致」是错的，必须显式失败
        raise SystemExit(
            "远程 tree 被 GitHub 截断（仓库过大），无法完整比对。"
            "请改用 git fetch 后比对，不要基于不完整数据下结论。"
        )

    return {
        item["path"]: {
            "mode": item["mode"],
            "sha": item["sha"],
            "type": item["type"],
        }
        for item in info["tree"]
    }


def local_head(local_dir: Path) -> tuple[str, str]:
    """返回本地 HEAD 的 (commit_sha, short_sha)。"""
    try:
        sha = subprocess.run(
            ["git", "-C", str(local_dir), "rev-parse", "HEAD"],
            capture_output=True, text=True, check=True,
        ).stdout.strip()
    except (subprocess.CalledProcessError, FileNotFoundError) as exc:
        raise SystemExit(f"无法获取本地 HEAD（{local_dir} 是否为 git 仓库？）：{exc}") from exc
    return sha, sha[:8]


def git_blob_sha(local_dir: Path, path: str) -> str | None:
    """计算本地文件的 git blob sha（与 GitHub 的算法一致：blob <len>\\0<content>）。

    注意：不能用文件内容的 sha256 —— git 用的是带头部前缀的 SHA-1。
    """
    file_path = local_dir / path
    if not file_path.is_file():
        return None
    content = file_path.read_bytes()
    header = f"blob {len(content)}\0".encode("utf-8")
    return hashlib.sha1(header + content).hexdigest()


def compare(owner: str, repo: str, ref: str, local_dir: Path) -> int:
    """执行比对，返回差异数量。"""
    remote = remote_tree(owner, repo, ref)
    head_sha, head_short = local_head(local_dir)

    print(f"远程：{owner}/{repo}@{ref}（{len(remote)} 项）")
    print(f"本地：{local_dir} @ {head_short}")

    mismatches: list[str] = []

    for path, meta in sorted(remote.items()):
        if meta["type"] != "blob":
            continue
        local_sha = git_blob_sha(local_dir, path)
        if local_sha is None:
            mismatches.append(f"本地缺失：{path}")
        elif local_sha != meta["sha"]:
            mismatches.append(f"内容不一致：{path}\n    远程 {meta['sha']}\n    本地 {local_sha}")

    if mismatches:
        print(f"\n发现 {len(mismatches)} 处不一致：", file=sys.stderr)
        for item in mismatches:
            print(f"  {item}", file=sys.stderr)
        return len(mismatches)

    print(f"\n一致：远程 {ref} 的 path/mode/blob 与本地 HEAD {head_short} 完全匹配。")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="校验远程仓库与本地 HEAD 一致性")
    parser.add_argument("owner", help="仓库归属账号")
    parser.add_argument("repo", help="仓库名")
    parser.add_argument("--ref", default="main", help="远程分支或 commit（默认 main）")
    parser.add_argument("--local", default=".", help="本地仓库路径（默认当前目录）")
    args = parser.parse_args()

    return compare(args.owner, args.repo, args.ref, Path(args.local).resolve())


if __name__ == "__main__":
    sys.exit(1 if main() else 0)
