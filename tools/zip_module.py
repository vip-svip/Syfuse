#!/usr/bin/env python3
"""把 module/ 目录打包为 KernelSU/Magisk 可用的 ZIP。

为什么单独抽出一个脚本，而不是写在 package.sh 里用内联 heredoc：
  - 内联 heredoc 通过 `python - "$A" "$B"` 传参时，在工作区路径包含空格的情况下
    参数会被 shell 按空格拆开，导致 module 目录解析到错误位置并静默产出**空 ZIP**。
    本项目实际工作区路径即含空格，因此这不是假设性问题。
  - 静默产出空 ZIP 是最危险的一类 bug：后续 unzip -t 会通过（空包也是合法 ZIP），
    而必需文件校验又依赖 unzip -l 的输出格式，很容易一并失效。
  - 独立脚本用 sys.argv 直传参数，且显式断言「至少打进了 N 个文件」，杜绝空包。

用法：python tools/zip_module.py <项目根目录> <输出 ZIP 路径>
"""

from __future__ import annotations

import os
import sys
import zipfile


def build_module_zip(project_root: str, out_zip: str) -> int:
    """打包 module/ 并返回写入的文件数。"""
    module_dir = os.path.join(os.path.abspath(project_root), "module")

    if not os.path.isdir(module_dir):
        print(f"错误：模块目录不存在：{module_dir}", file=sys.stderr)
        return -1

    written = 0
    with zipfile.ZipFile(out_zip, "w", zipfile.ZIP_DEFLATED) as zf:
        for root, dirs, files in os.walk(module_dir):
            # 排除 macOS 元数据目录
            dirs[:] = [d for d in dirs if d != "__MACOSX"]
            for name in files:
                if name == ".DS_Store":
                    continue
                full = os.path.join(root, name)
                # 存相对路径，且统一用正斜杠（ZIP 规范要求）
                rel = os.path.relpath(full, module_dir).replace(os.sep, "/")
                zf.write(full, rel)
                written += 1

    return written


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print("用法：zip_module.py <项目根目录> <输出 ZIP 路径>", file=sys.stderr)
        return 2

    project_root, out_zip = argv[1], argv[2]

    out_dir = os.path.dirname(os.path.abspath(out_zip))
    if out_dir:
        os.makedirs(out_dir, exist_ok=True)

    written = build_module_zip(project_root, out_zip)

    if written < 0:
        return 1

    # 关键断言：绝不允许产出空 ZIP 后静默成功。
    # 模块包至少包含 module.prop + 5 个脚本 + 2 个 META-INF 文件 = 8 个。
    if written < 8:
        print(
            f"错误：模块 ZIP 仅写入 {written} 个文件，疑似目录解析错误（预期 >= 8）",
            file=sys.stderr,
        )
        return 1

    print(f"已创建 {out_zip}（{written} 个文件）")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
