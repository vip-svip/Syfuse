#!/usr/bin/env python3
"""从提示词文件的附录 A/B/C 里按 base64 还原三个交付脚本。

为什么需要它：附录用 base64 存放，就是为了规避"脚本内含 ``` 导致代码块提前闭合"。
但也因此附录与 tools/ 之间**没有天然的同步保证** —— 提示词被改过而 tools/ 没跟着改，
肉眼是看不出来的（M3 轮实测踩到：附录 A 比磁盘多 12 行、附录 B 多 2 行）。

用法：
    python tools/extract_appendix.py <提示词.md> <输出目录>

输出：<输出目录>/verify_deliverable.ps1、verify_deliverable.sh、run_agent.ps1
逐字节写出，不做任何行尾/编码转换 —— 附录里声明「BOM」「NOBOM-LF」由调用方核对。
"""
from __future__ import annotations

import base64
import hashlib
import re
import sys
from pathlib import Path

# 附录标题 -> 落盘文件名（顺序即 A/B/C）
TARGETS = {
    "附录 A": "verify_deliverable.ps1",
    "附录 B": "verify_deliverable.sh",
    "附录 C": "run_agent.ps1",
}


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2

    prompt = Path(argv[1])
    outdir = Path(argv[2])
    if not prompt.is_file():
        print(f"ERROR: 找不到提示词文件：{prompt}", file=sys.stderr)
        return 2
    outdir.mkdir(parents=True, exist_ok=True)

    lines = prompt.read_text(encoding="utf-8").split("\n")
    heads: list[tuple[str, int]] = []
    for i, ln in enumerate(lines):
        m = re.match(r"^## (附录 [ABC]) ", ln)
        if m:
            heads.append((m.group(1), i))

    if len(heads) != len(TARGETS):
        print(f"ERROR: 期望 3 个附录标题，实际找到 {len(heads)} 个", file=sys.stderr)
        return 1

    rc = 0
    for name, hline in heads:
        filename = TARGETS[name]

        declared = None
        for j in range(hline, min(hline + 6, len(lines))):
            m = re.search(r"base64 字节数\D*(\d+)", lines[j])
            if m:
                declared = int(m.group(1))
                break

        # 代码块以 ```` 开头（四个反引号），以便附录正文里出现 ``` 也不影响
        start = None
        for j in range(hline, min(hline + 12, len(lines))):
            if lines[j].strip().startswith("````"):
                start = j + 1
                break
        if start is None:
            print(f"ERROR: {name} 找不到 base64 代码块", file=sys.stderr)
            rc = 1
            continue

        end = None
        for j in range(start, len(lines)):
            if lines[j].strip().startswith("````"):
                end = j
                break
        if end is None:
            print(f"ERROR: {name} 代码块未闭合", file=sys.stderr)
            rc = 1
            continue

        b64 = "".join(l.strip() for l in lines[start:end])
        try:
            raw = base64.b64decode(b64, validate=True)
        except Exception as exc:  # noqa: BLE001 - 要把原始错误报出来
            print(f"ERROR: {name} base64 解码失败：{exc}", file=sys.stderr)
            rc = 1
            continue

        target = outdir / filename
        target.write_bytes(raw)

        sha = hashlib.sha256(raw).hexdigest()
        bom = raw[:3] == b"\xef\xbb\xbf"
        print(
            f"{name} -> {filename}  bytes={len(raw)}  declared={declared}"
            f"  bom={bom}  crlf={b'\r\n' in raw}  sha256={sha}"
        )
        # 声明的字节数是附录自己的断言，对不上就是提示词文件本身有问题
        if declared is not None and declared != len(raw):
            print(
                f"ERROR: {name} 声明 {declared} 字节，实际解码 {len(raw)} 字节",
                file=sys.stderr,
            )
            rc = 1

    return rc


if __name__ == "__main__":
    sys.exit(main(sys.argv))
