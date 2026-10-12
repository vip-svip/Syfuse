#!/usr/bin/env python3
"""性能基准（提示词第 11 部分 + 第 12 部分）。

定位：产出 `tests/perf_results.txt`，多轮取中位数，附原始日志。

当前状态（M1）：**未实现，以明确失败退出**。
原因：全部性能指标都依赖真实内核与真机（冷启动、吞吐、规则匹配、内存），
      M1 尚无内核，无法测量。

重要原则（红线条 R4：不造假）：
  提示词第 11 部分列出的目标值（如「冷启动 < 800ms」「APK < 25MB」）
  是**目标**而非实测。本脚本存在之前，任何文档中都不得把这些数字
  写成 Syfuse 的实测结果。

M1 唯一可实测的相关指标是**静态可算的**：源码文件数与代码行数，
由本脚本的 --static 模式产出，作为后续体积优化的基线参考。
"""

from __future__ import annotations

import argparse
import statistics
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
RESULTS_FILE = PROJECT_ROOT / "tests" / "perf_results.txt"

# 参与静态统计的源码目录与扩展名
SOURCE_DIRS = ["app", "ui", "domain", "data", "core", "engine", "privileged", "lsposed-bridge"]
SOURCE_SUFFIXES = {".kt", ".kts", ".xml", ".toml", ".pro"}

SKIP_DIR_PARTS = {"build", ".gradle", ".kotlin", ".idea"}


def count_sources() -> tuple[int, int, dict[str, int]]:
    """统计源码文件数与有效代码行数（跳过空行与纯注释行）。"""
    file_count = 0
    line_count = 0
    per_module: dict[str, int] = {}

    for module in SOURCE_DIRS:
        module_dir = PROJECT_ROOT / module
        if not module_dir.exists():
            continue
        module_lines = 0

        for path in module_dir.rglob("*"):
            if not path.is_file():
                continue
            if any(part in SKIP_DIR_PARTS for part in path.parts):
                continue
            if path.suffix not in SOURCE_SUFFIXES:
                continue

            file_count += 1
            try:
                text = path.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue

            for line in text.splitlines():
                stripped = line.strip()
                # 跳过空行与纯注释，让数字反映「需要维护的代码量」
                if not stripped:
                    continue
                if stripped.startswith(("//", "*", "/*", "#")):
                    continue
                if stripped.startswith("<!--"):
                    continue
                line_count += 1
                module_lines += 1

        per_module[module] = module_lines

    return file_count, line_count, per_module


def run_static() -> int:
    """产出静态统计。这是 M1 唯一能诚实产出的性能相关数据。"""
    files, lines, per_module = count_sources()

    output_lines = [
        "Syfuse 静态代码统计",
        "=" * 60,
        "",
        "说明：本文件由 tests/perf_*.py --static 生成。",
        "M1 无内核，故无运行时性能数据。此处的数字是静态可算的代码规模，",
        "用于跟踪后续里程碑的复杂度增长，**不是**性能指标。",
        "",
        f"源码文件数：{files}",
        f"有效代码行数：{lines}（已排除空行与纯注释）",
        "",
        "按模块分布：",
    ]

    for module, module_lines in sorted(per_module.items(), key=lambda kv: -kv[1]):
        output_lines.append(f"  {module:<20} {module_lines:>6} 行")

    output_lines.append("")
    output_lines.append("待测量的运行时指标（需 M2+ 真机）：")
    for metric in [
        "冷启动到首页可交互（目标 < 800ms，中端机）",
        "连接建立（目标 < 1.5s）",
        "APK 单 ABI 体积（目标 < 25MB，含内核）",
        "常驻内存空闲（目标 < 80MB）",
        "待机耗电（目标 < 3%/h）",
        "100 万条规则编译（目标 < 3s）",
        "单次规则匹配 p99（目标 < 1ms）",
        "列表滚动丢帧率（目标 < 1% @120Hz）",
        "隧道吞吐（目标 >= 直连的 80%）",
        "崩溃率（目标 0）",
    ]:
        output_lines.append(f"  [ ] {metric}")

    RESULTS_FILE.write_text("\n".join(output_lines) + "\n", encoding="utf-8")

    print(f"已写入 {RESULTS_FILE.relative_to(PROJECT_ROOT)}")
    print(f"  源码文件：{files}")
    print(f"  有效代码行：{lines}")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Syfuse 性能基准")
    parser.add_argument(
        "--static",
        action="store_true",
        help="仅产出静态统计（M1 可用）；不带此参数则尝试运行时基准",
    )
    args = parser.parse_args()

    if args.static:
        return run_static()

    print(
        "运行时性能基准尚未实现：需要真实内核与真机（M2+）。\n"
        "当前可运行：python tests/perf_baseline.py --static",
        file=sys.stderr,
    )
    # 78 = EX_CONFIG，明确区分「未配置」与「测试失败」
    return 78


if __name__ == "__main__":
    sys.exit(main())
