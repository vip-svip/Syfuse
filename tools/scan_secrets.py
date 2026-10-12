#!/usr/bin/env python3
"""凭据/密钥扫描（R5 的执行体）。

定位：这是一个**提交前闸门**，不是"扫描工具"。它的职责是让「密钥进仓库」这件事
在本地和 CI 上都变成硬失败，而不是靠人的自觉。

扫描对象：git 跟踪的全部文本文件（也可对工作区做全量扫描）。
判定分级：
  BLOCK  确定是密钥/口令 -> 退出码 1
  WARN   形态可疑但可能是占位符/示例 -> 打印但不失败
  INFO   允许出现的环境变量**名**（不是值）

用法：
  python tools/scan_secrets.py              # 扫描 git 跟踪文件
  python tools/scan_secrets.py --all        # 扫描工作区全部文件（含未跟踪）
  python tools/scan_secrets.py --quiet      # 只输出结论（供其他脚本调用）
"""
from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# 扫描器自身相对于仓库根的路径（用于跳过自己，见 main() 里的说明）
SELF_PATH = os.path.relpath(os.path.abspath(__file__), REPO)

# 跳过：二进制、构建产物、版本控制目录、工具链
SKIP_DIRS = {
    ".git", ".gradle", ".kotlin", "build", ".idea", "node_modules",
    ".workbuddy-ai", "toolchain", "gradle-home",
}
SKIP_EXT = {
    ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".so", ".a", ".aar",
    ".jar", ".zip", ".apk", ".jks", ".keystore", ".der", ".p12", ".ttf",
    ".otf", ".woff", ".woff2", ".mp4", ".lock",
}

# 明确允许的占位符（文档/示例里出现是合理的）
PLACEHOLDER = re.compile(
    r"(?i)^(x{3,}|\*{3,}|\.{3,}|<[^>]*>|your[-_ ]?|example|placeholder|"
    r"changeme|redacted|dummy|test|none|null|empty|todo|"
    r"\[.*\]|\$\{.*\}|\$[A-Z_]+|\{\{.*\}\})$"
)

BLOCK_RULES: list[tuple[str, re.Pattern[str]]] = [
    ("私钥块", re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----")),
    ("GitHub Token", re.compile(r"\bgh[pousr]_[A-Za-z0-9]{36,}\b")),
    ("GitHub 细粒度 Token", re.compile(r"\bgithub_pat_[A-Za-z0-9_]{60,}\b")),
    ("AWS Access Key", re.compile(r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b")),
    ("Google API Key", re.compile(r"\bAIza[0-9A-Za-z_\-]{35}\b")),
    ("Slack Token", re.compile(r"\bxox[abprs]-[0-9A-Za-z\-]{10,}\b")),
    ("OpenAI Key", re.compile(r"\bsk-(?:proj-)?[A-Za-z0-9_\-]{32,}\b")),
    ("Anthropic Key", re.compile(r"\bsk-ant-[A-Za-z0-9_\-]{32,}\b")),
    ("JWT", re.compile(r"\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b")),
    ("Android Keystore 口令赋值", re.compile(
        r"(?i)\b(storePassword|keyPassword)\s*[=:]\s*[\"']([^\"']{6,})[\"']")),
    ("硬编码 Bearer", re.compile(r"(?i)\bAuthorization\s*:\s*Bearer\s+[A-Za-z0-9._\-]{20,}")),
]

# 需要单独判定"值是不是占位符"的规则（从第 2 组取值）
VALUE_RULES: list[tuple[str, re.Pattern[str]]] = [
    ("疑似口令赋值", re.compile(
        r"(?i)\b(?:password|passwd|pwd|secret|api[_-]?key|access[_-]?token|private[_-]?key)"
        r"\s*[=:]\s*[\"']([^\"'\s]{8,})[\"']")),
]

WARN_RULES: list[tuple[str, re.Pattern[str]]] = [
    ("疑似订阅链接（含凭据）", re.compile(
        r"(?i)\b(?:ss|ssr|vmess|vless|trojan|hysteria2?)://[A-Za-z0-9@:._\-]{20,}")),
    ("疑似内网/私有地址", re.compile(r"\b(?:10|127)\.\d{1,3}\.\d{1,3}\.\d{1,3}\b")),
]

FORBIDDEN_FILES = [
    ".jks", ".keystore", ".p12", ".pfx",
]

# 豁免标记。设计成**行内、显式、可 grep** 的，而不是路径白名单。
#
# 为什么不按路径豁免（例如"放过所有 *_test.go"）：
# 那会让真实密钥只要放进测试文件就能溜过去，而且没有任何痕迹。
# 行内标记则相反 —— 每处豁免都写在它豁免的那一行上，
# `grep -rn "secretscan:allow"` 就能一次列出全部豁免点供人复核。
#
# 用法：
#   Password: "SUPER_SECRET_PW"  // secretscan:allow 测试夹具
ALLOW_MARKER = "secretscan:allow"
# 整个文件豁免（慎用，会打印出来）。
# 用法：在文件任意位置写 `secretscan:allow-file`
ALLOW_FILE_MARKER = "secretscan:allow-file"


def tracked_files() -> list[str]:
    try:
        out = subprocess.run(
            ["git", "-C", REPO, "ls-files"],
            capture_output=True, text=True, check=True,
        ).stdout
        return [l for l in out.splitlines() if l.strip()]
    except Exception:
        return []


def walk_files() -> list[str]:
    result = []
    for dirpath, dirnames, filenames in os.walk(REPO):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for f in filenames:
            result.append(os.path.relpath(os.path.join(dirpath, f), REPO))
    return result


def is_texty(path: str) -> bool:
    ext = os.path.splitext(path)[1].lower()
    return ext not in SKIP_EXT


def scan(path: str) -> tuple[list[str], list[str], list[str]]:
    """返回 (blocks, warns, allowlisted)，元素为可读描述。"""
    blocks: list[str] = []
    warns: list[str] = []
    allowlisted: list[str] = []
    full = os.path.join(REPO, path)
    try:
        with open(full, "r", encoding="utf-8", errors="ignore") as fh:
            lines = fh.readlines()
    except OSError:
        return blocks, warns, allowlisted

    if any(ALLOW_FILE_MARKER in l for l in lines):
        allowlisted.append(f"{path}: [整文件豁免 secretscan:allow-file]")
        return blocks, warns, allowlisted

    for lineno, line in enumerate(lines, 1):
        stripped = line.strip()
        if not stripped:
            continue
        # 豁免标记可以写在**本行**（行尾注释），也可以写在**上一行**。
        # 支持上一行是因为有些行很长（例如整行构造一个 Node 字面量），
        # 行尾再加注释会难以阅读。两种写法都必须显式写出来。
        prev_line = lines[lineno - 2] if lineno >= 2 else ""
        if ALLOW_MARKER in line or ALLOW_MARKER in prev_line:
            allowlisted.append(f"{path}:{lineno}: [豁免]")
            continue
        for label, rx in BLOCK_RULES:
            m = rx.search(line)
            if m:
                # storePassword 这类规则，占位符要放过
                val = m.group(m.lastindex) if m.lastindex else m.group(0)
                if PLACEHOLDER.match(val.strip()):
                    continue
                blocks.append(f"{path}:{lineno}: [{label}] {_redact(m.group(0))}")
        for label, rx in VALUE_RULES:
            m = rx.search(line)
            if m:
                val = m.group(1).strip()
                if PLACEHOLDER.match(val) or len(set(val)) <= 3:
                    continue
                # 环境变量引用形式是允许的
                if val.startswith("$") or val.startswith("System.getenv"):
                    continue
                blocks.append(f"{path}:{lineno}: [{label}] {_redact(m.group(0))}")
        for label, rx in WARN_RULES:
            m = rx.search(line)
            if m:
                warns.append(f"{path}:{lineno}: [{label}] {_redact(m.group(0))}")
    return blocks, warns, allowlisted


def _redact(s: str) -> str:
    s = s.strip()
    if len(s) <= 12:
        return s
    return s[:6] + "…" + s[-4:] + f" (len={len(s)})"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--all", action="store_true", help="扫描工作区全部文件，而非仅 git 跟踪文件")
    ap.add_argument("--quiet", action="store_true", help="只输出结论")
    args = ap.parse_args()

    files = walk_files() if args.all else (tracked_files() or walk_files())
    files = [f for f in files if is_texty(f)]

    # 扫描器自身必须跳过（见下方说明）。
    files = [f for f in files if os.path.normpath(f) != os.path.normpath(SELF_PATH)]

    if not files:
        # 扫了 0 个文件却报 PASS，是最危险的假绿。宁可失败。
        print("[SECRETS] FAIL — 没有扫描到任何文件（路径或过滤逻辑有问题），不能判定为通过")
        return 1

    all_blocks: list[str] = []
    all_warns: list[str] = []
    all_allowed: list[str] = []

    # 文件级禁止项
    for f in files:
        ext = os.path.splitext(f)[1].lower()
        if ext in FORBIDDEN_FILES:
            all_blocks.append(f"{f}: [密钥库文件出现在仓库中]")

    for f in files:
        b, w, a = scan(f)
        all_blocks.extend(b)
        all_warns.extend(w)
        all_allowed.extend(a)

    if not args.quiet:
        print(f"扫描文件数: {len(files)}（{'工作区全量' if args.all else 'git 跟踪'}）")
        if all_warns:
            print(f"\n-- WARN ({len(all_warns)}) --")
            for w in all_warns[:50]:
                print("  " + w)
            if len(all_warns) > 50:
                print(f"  … 另有 {len(all_warns) - 50} 条")
        if all_allowed:
            # 豁免必须被打印出来。静默豁免等于没有扫描。
            print(f"\n-- 豁免 ({len(all_allowed)}) --")
            for a in all_allowed:
                print("  " + a)
        if all_blocks:
            print(f"\n-- BLOCK ({len(all_blocks)}) --")
            for b in all_blocks:
                print("  " + b)

    if all_blocks:
        print(f"\n[SECRETS] FAIL — 命中 {len(all_blocks)} 条禁止项，禁止提交/发布")
        return 1
    print(f"[SECRETS] PASS — 无禁止项（WARN {len(all_warns)} 条，豁免 {len(all_allowed)} 条）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
