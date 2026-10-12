#!/usr/bin/env python3
"""产物清单校验：证明"我交付的东西和我声称的一致"。

用途：`tests/package.sh` 生成产物后写一份 SHA256 清单；本脚本读回清单并逐项校验。
发布前、下载后都能用同一份清单核对，避免"文件被换过"或"打包漏了文件"。

清单格式（兼容 sha256sum 的两种风格）：
    <sha256>  <相对路径>
    <sha256> *<相对路径>

用法：
    python tools/verify_files.py dist/MANIFEST.sha256
    python tools/verify_files.py dist/MANIFEST.sha256 --root dist
    python tools/verify_files.py dist/MANIFEST.sha256 --json
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys

LINE = re.compile(r"^(?P<hash>[0-9a-fA-F]{64})\s+[* ]?(?P<path>.+?)\s*$")


def sha256(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as fh:
        for chunk in iter(lambda: fh.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("manifest", help="清单文件路径")
    ap.add_argument("--root", default=None, help="相对路径的基准目录（默认清单所在目录）")
    ap.add_argument("--json", action="store_true", help="以 JSON 输出结果")
    ap.add_argument("--allow-missing", action="store_true",
                    help="清单里缺失的文件只记 WARN（默认是 FAIL）")
    args = ap.parse_args()

    if not os.path.isfile(args.manifest):
        print(f"清单不存在: {args.manifest}", file=sys.stderr)
        return 2

    root = args.root or os.path.dirname(os.path.abspath(args.manifest)) or "."

    entries: list[tuple[str, str]] = []
    with open(args.manifest, "r", encoding="utf-8") as fh:
        for lineno, raw in enumerate(fh, 1):
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            m = LINE.match(line)
            if not m:
                print(f"[WARN] 第 {lineno} 行格式无法解析，已跳过: {line}", file=sys.stderr)
                continue
            entries.append((m.group("hash").lower(), m.group("path")))

    if not entries:
        print("[VERIFY] FAIL — 清单为空，无法证明任何事", file=sys.stderr)
        return 1

    ok, bad, missing = [], [], []
    for expected, rel in entries:
        p = os.path.join(root, rel)
        if not os.path.isfile(p):
            missing.append(rel)
            continue
        actual = sha256(p)
        (ok if actual == expected else bad).append((rel, expected, actual))

    result = {
        "manifest": os.path.abspath(args.manifest),
        "root": os.path.abspath(root),
        "total": len(entries),
        "ok": len(ok),
        "mismatch": [{"path": p, "expected": e, "actual": a} for p, e, a in bad],
        "missing": missing,
    }

    if args.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
    else:
        print(f"清单: {result['manifest']}")
        print(f"基准目录: {result['root']}")
        print(f"条目: {len(entries)}  校验通过: {len(ok)}  不一致: {len(bad)}  缺失: {len(missing)}")
        for p, e, a in bad:
            print(f"  [MISMATCH] {p}\n     期望 {e}\n     实际 {a}")
        for p in missing:
            print(f"  [MISSING] {p}")

    if bad or (missing and not args.allow_missing):
        print("[VERIFY] FAIL")
        return 1
    print("[VERIFY] PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
