#!/usr/bin/env python3
"""比对关键文件的远程内容与本地（提示词第 12 部分 / 16.5）。

与 verify_remote.py 的分工：
  - verify_remote.py 比对**整棵树**的 path/mode/blob，用于发布前的全局校验
  - 本脚本只比对**关键文件**的文本内容，用于发布后的快速回读确认
    （提示词 16.6 第 5 条：「发布后回读远程仓库首页与 Release 页，确认渲染正常」）

关键文件的定义：一旦这些文件的内容与本地不符，用户看到的产品描述、
许可条款或署名就是错的。比整棵树比对更快，适合每次发布后跑一遍。
"""

from __future__ import annotations

import argparse
import sys
import urllib.error
import urllib.request
from pathlib import Path

RAW_BASE = "https://raw.githubusercontent.com"

# 关键文件清单。之所以是这些而不是全部文件：
# 它们承载「产品定位、许可、署名、构建方式、依约信息」——
# 这些内容出错属于必须立刻发现的合规问题。
CRITICAL_FILES = [
    "README.md",
    "LICENSE",
    "NOTICE",
    "CHANGELOG.md",
    "module/module.prop",
    "update.json",
]


def fetch_raw(owner: str, repo: str, ref: str, path: str) -> str | None:
    """取远程文件原始文本，404 返回 None。"""
    url = f"{RAW_BASE}/{owner}/{repo}/{ref}/{path}"
    request = urllib.request.Request(url, headers={"User-Agent": "Syfuse-verify-files"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            return None
        raise SystemExit(f"请求失败 {url}：HTTP {exc.code}") from exc
    except urllib.error.URLError as exc:
        raise SystemExit(f"网络不可达 {url}：{exc.reason}") from exc


def normalize(text: str) -> str:
    """归一化换行符后比对。

    理由：Windows 本地可能是 CRLF 而仓库内是 LF（.gitattributes 强制 LF）。
    直接逐字节比会全量误报，掩盖真正的差异。
    """
    return text.replace("\r\n", "\n").rstrip("\n")


def compare(owner: str, repo: str, ref: str, local_dir: Path) -> int:
    print(f"比对 {owner}/{repo}@{ref} 的关键文件（{len(CRITICAL_FILES)} 个）")

    mismatches: list[str] = []
    checked = 0

    for path in CRITICAL_FILES:
        remote_text = fetch_raw(owner, repo, ref, path)
        local_path = local_dir / path

        if remote_text is None:
            if local_path.exists():
                mismatches.append(f"远程缺失但本地存在：{path}")
            else:
                print(f"  - {path}：本地与远程均无（跳过）")
            continue

        if not local_path.exists():
            mismatches.append(f"本地缺失但远程存在：{path}")
            continue

        checked += 1
        local_text = local_path.read_text(encoding="utf-8", errors="replace")

        if normalize(local_text) != normalize(remote_text):
            mismatches.append(f"内容不一致：{path}")
        else:
            print(f"  ✓ {path}")

    if mismatches:
        print(f"\n发现 {len(mismatches)} 处不一致：", file=sys.stderr)
        for item in mismatches:
            print(f"  {item}", file=sys.stderr)
        return len(mismatches)

    print(f"\n一致：{checked} 个关键文件与远程 {ref} 内容相同。")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="比对关键文件的远程与本地内容")
    parser.add_argument("owner", help="仓库归属账号")
    parser.add_argument("repo", help="仓库名")
    parser.add_argument("--ref", default="main", help="远程分支（默认 main）")
    parser.add_argument("--local", default=".", help="本地仓库路径（默认当前目录）")
    args = parser.parse_args()

    return compare(args.owner, args.repo, args.ref, Path(args.local).resolve())


if __name__ == "__main__":
    sys.exit(1 if main() else 0)
