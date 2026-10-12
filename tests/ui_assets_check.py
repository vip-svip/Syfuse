#!/usr/bin/env python3
"""UI 资产回验（提示词第 12 部分）。

目的：把所有可交互控件的 id 与 action 建基线清单，重构后自动比对，
防止功能入口静默丢失。

为什么需要它：Compose 没有 XML 布局文件，控件全靠代码组合，
一次重构很容易「顺手删掉」某个入口而无人察觉 ——
这个脚本把「入口是否存在」变成可自动断言的事实。

M1 实现程度：扫描全部 @Composable 函数与 TopLevelDestination 枚举，
生成基线清单并与之比对。基线文件为 tests/ui_assets_baseline.tsv。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent
BASELINE_FILE = PROJECT_ROOT / "tests" / "ui_assets_baseline.tsv"

UI_SOURCE_DIRS = [
    PROJECT_ROOT / "ui" / "src" / "main" / "kotlin",
    PROJECT_ROOT / "app" / "src" / "main" / "kotlin",
]

# 匹配 @Composable 注解后紧跟的函数声明
COMPOSABLE_PATTERN = re.compile(
    r"@Composable\s+(?:private\s+|internal\s+|public\s+)?fun\s+([A-Za-z_][A-Za-z0-9_]*)"
)

# 匹配 enum 常量条目（用于 TopLevelDestination 等导航入口）
ENUM_ENTRY_PATTERN = re.compile(r"^\s{4}([A-Z][A-Z0-9_]*)\s*\(", re.MULTILINE)


def collect_assets() -> dict[str, set[str]]:
    """收集可交互资产：Composable 名称与导航枚举条目。"""
    assets: dict[str, set[str]] = {"composable": set(), "nav_destination": set()}

    for src_dir in UI_SOURCE_DIRS:
        if not src_dir.exists():
            continue
        for path in src_dir.rglob("*.kt"):
            text = path.read_text(encoding="utf-8", errors="replace")

            for match in COMPOSABLE_PATTERN.finditer(text):
                name = match.group(1)
                # 内部实现细节（以私有小写开头的辅助函数）也纳入：
                # 它们是重构时最容易被改名或删除的，正需要基线锁定
                assets["composable"].add(name)

            if path.name == "TopLevelDestination.kt":
                for match in ENUM_ENTRY_PATTERN.finditer(text):
                    assets["nav_destination"].add(match.group(1))

    return assets


def write_baseline(assets: dict[str, set[str]]) -> None:
    """写入基线清单。"""
    lines: list[str] = []

    for name in sorted(assets["composable"]):
        lines.append(f"composable\t{name}")
    for name in sorted(assets["nav_destination"]):
        lines.append(f"nav_destination\t{name}")

    BASELINE_FILE.write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"已写入基线：{BASELINE_FILE.relative_to(PROJECT_ROOT)}（{len(lines)} 项）")


def read_baseline() -> set[str]:
    """读取基线清单，返回 "{kind}\t{name}" 形式的集合。"""
    if not BASELINE_FILE.exists():
        return set()
    return {
        line.strip()
        for line in BASELINE_FILE.read_text(encoding="utf-8").splitlines()
        if line.strip()
    }


def compare(assets: dict[str, set[str]]) -> int:
    """与基线比对，返回差异数量。"""
    baseline = read_baseline()
    if not baseline:
        print("基线不存在，正在创建…")
        write_baseline(assets)
        return 0

    current = {f"composable\t{n}" for n in assets["composable"]}
    current |= {f"nav_destination\t{n}" for n in assets["nav_destination"]}

    removed = baseline - current
    added = current - baseline

    if removed:
        print("检测到 UI 资产消失（可能导致功能入口静默丢失）：", file=sys.stderr)
        for item in sorted(removed):
            print(f"  - {item}", file=sys.stderr)

    if added:
        print("检测到 UI 资产新增（请确认后更新基线）：", file=sys.stderr)
        for item in sorted(added):
            print(f"  + {item}", file=sys.stderr)

    if removed or added:
        print(
            f"\n共 {len(removed)} 项消失、{len(added)} 项新增。"
            "\n若变更符合预期，请运行：python tools/../tests/ui_assets_check.py --update",
            file=sys.stderr,
        )
        return len(removed) + len(added)

    print(f"UI 资产比对通过：{len(current)} 项与基线完全一致。")
    return 0


def main() -> int:
    assets = collect_assets()

    if "--update" in sys.argv:
        write_baseline(assets)
        return 0

    return compare(assets)


if __name__ == "__main__":
    sys.exit(1 if main() else 0)
