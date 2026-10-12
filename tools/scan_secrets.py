#!/usr/bin/env python3
"""密钥与本地路径扫描（提示词第 12 部分 + 红线条 R5）。

用途：证明仓库内不存在密钥、签名文件、本地路径残留。
这是「红线条 R5」的唯一可验证证据，因此脚本本身必须足够严格 ——
宁可误报也不要漏报，误报由人工白名单处理，漏报则是真实的合规风险。

检查项：
  1. 已知的密钥文件类型（*.jks / *.keystore / local.properties / *.p12 / *.pem 私钥）
  2. 常见 token 字面量（ghp_ / github_pat_ / AKIA / sk- 等）
  3. 硬编码的绝对路径（Windows 盘符路径、macOS /Users/、Linux /home/）
  4. 未替换的占位符（<your-name> / TODO / FIXME / yourname / example.com 之类除外）
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parent.parent

# ---- 检查 1：禁止存在的文件 ----
FORBIDDEN_FILE_PATTERNS = [
    "*.jks",
    "*.keystore",
    "*.p12",
    "*.pfx",
    "local.properties",
    "*.hprof",
]

# ---- 检查 2：token 字面量 ----
# 注意：这些正则本身包含 token 前缀字符串，扫描时必须跳过本文件自身，
# 否则脚本会自我误报。见 SKIP_FILES。
TOKEN_PATTERNS = [
    (re.compile(r"ghp_[A-Za-z0-9]{20,}"), "GitHub personal access token (classic)"),
    (re.compile(r"github_pat_[A-Za-z0-9_]{20,}"), "GitHub fine-grained token"),
    (re.compile(r"gho_[A-Za-z0-9]{20,}"), "GitHub OAuth token"),
    (re.compile(r"AKIA[0-9A-Z]{16}"), "AWS access key id"),
    (re.compile(r"sk-[A-Za-z0-9]{32,}"), "OpenAI-style secret key"),
    (re.compile(r"-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----"), "PEM private key"),
]

# ---- 检查 3：硬编码绝对路径 ----
# 注意正则中的反斜杠：Windows 路径用 [\\/]+ 同时匹配两种分隔符，
# 因为在 Kotlin/Java 字符串字面量里路径常被写成双反斜杠或正斜杠两种形态。
ABSOLUTE_PATH_PATTERNS = [
    (re.compile(r"[A-Za-z]:[\\/]{1,2}(?:Users|Documents|Projects)[\\/]{1,2}"), "Windows absolute user path"),
    (re.compile(r"/Users/[A-Za-z0-9._-]+/"), "macOS absolute user path"),
    (re.compile(r"/home/[A-Za-z0-9._-]+/"), "Linux absolute user path"),
]

# ---- 检查 4：未替换占位符 ----
PLACEHOLDER_PATTERNS = [
    (re.compile(r"<your[-_]?name>", re.IGNORECASE), "未替换的账号占位符"),
    (re.compile(r"\bTODO\b"), "TODO 占位"),
    (re.compile(r"\bFIXME\b"), "FIXME 占位"),
    (re.compile(r"\byourname\b", re.IGNORECASE), "yourname 占位符"),
]

# 跳过的目录：构建产物与依赖不该被扫描
SKIP_DIRS = {
    ".git", "build", ".gradle", ".idea", "node_modules",
    "__pycache__", ".kotlin", "dist",
}

# 跳过本文件：它自身包含 token 正则前缀，扫描会自我误报
SKIP_FILES = {Path(__file__).resolve()}

# 二进制与不可读扩展名
SKIP_SUFFIXES = {
    ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico",
    ".zip", ".jar", ".aar", ".apk", ".so", ".srs", ".mrs",
    ".ttf", ".otf", ".woff", ".woff2",
}


def iter_source_files():
    """遍历应被扫描的源文件。"""
    for path in PROJECT_ROOT.rglob("*"):
        if not path.is_file():
            continue
        if path.resolve() in SKIP_FILES:
            continue
        if any(part in SKIP_DIRS for part in path.parts):
            continue
        if path.suffix.lower() in SKIP_SUFFIXES:
            continue
        yield path


def scan() -> int:
    """执行全部检查，返回发现的违规数量。"""
    violations: list[str] = []

    # 检查 1：禁止文件
    for pattern in FORBIDDEN_FILE_PATTERNS:
        for path in PROJECT_ROOT.rglob(pattern):
            if any(part in SKIP_DIRS for part in path.parts):
                continue
            violations.append(f"[禁止文件] {path.relative_to(PROJECT_ROOT)} 匹配 {pattern}")

    # 检查 2-4：内容扫描
    for path in iter_source_files():
        rel = path.relative_to(PROJECT_ROOT)
        try:
            text = path.read_text(encoding="utf-8", errors="strict")
        except (UnicodeDecodeError, OSError):
            # 非文本文件跳过；已在 SKIP_SUFFIXES 里过滤大部分，这里是兜底
            continue

        for lineno, line in enumerate(text.splitlines(), start=1):
            for regex, label in TOKEN_PATTERNS:
                if regex.search(line):
                    violations.append(f"[密钥] {rel}:{lineno} 疑似 {label}")
            for regex, label in ABSOLUTE_PATH_PATTERNS:
                if regex.search(line):
                    violations.append(f"[绝对路径] {rel}:{lineno} {label}")
            for regex, label in PLACEHOLDER_PATTERNS:
                if regex.search(line):
                    violations.append(f"[占位符] {rel}:{lineno} {label}")

    if violations:
        print("密钥扫描失败，发现以下问题：", file=sys.stderr)
        for item in violations:
            print(f"  {item}", file=sys.stderr)
        print(
            f"\n共 {len(violations)} 处。若为误报，请在脚本白名单中显式排除并说明理由。",
            file=sys.stderr,
        )
        return len(violations)

    print("密钥扫描通过：未发现密钥、签名文件、绝对路径或未替换占位符。")
    return 0


if __name__ == "__main__":
    sys.exit(1 if scan() else 0)
