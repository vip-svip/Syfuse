#!/usr/bin/env python3
"""api_push.py —— 当 `git push` 被网络阻断时，用 GitHub Git Data API 推送。

**为什么需要它**：实测环境中 `github.com:443`（git smart HTTP）会被间歇性重置，
但 `api.github.com` 正常。此时 `git push` 报 `Connection was reset`，
而 REST API 仍然可用。

做法：blobs → tree → commit → 更新 ref，并**精确复刻本地的 author/committer/日期**，
使远端 tree SHA（以及在多数情况下 commit SHA）与本地完全一致。

用法:
    GH_TOKEN=xxx python tools/api_push.py [owner/repo] [branch]
默认 owner/repo 从 `git remote get-url origin` 解析，branch=main。

退出码: 0 成功 / 1 失败 / 2 环境或用法错误
"""
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request

def api(base, tok, path, method="GET", body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, method=method, headers={
        "Authorization": "Bearer " + tok,
        "Accept": "application/vnd.github+json",
        "User-Agent": "syfuse-api-push",
        "Content-Type": "application/json",
    })
    try:
        with urllib.request.urlopen(req, timeout=90) as r:
            return json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        print(f"  !! API {method} {path} -> {e.code}: {e.read().decode()[:400]}", file=sys.stderr)
        sys.exit(1)

def git(*a, binary=False):
    return subprocess.run(["git", *a], capture_output=True, check=True, text=not binary).stdout

def main():
    tok = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if not tok:
        print("ERROR: 需要 GH_TOKEN 或 GITHUB_TOKEN", file=sys.stderr)
        return 2
    repo = sys.argv[1] if len(sys.argv) > 1 else None
    if not repo:
        url = git("remote", "get-url", "origin").strip()
        # https://github.com/owner/repo.git 或 git@github.com:owner/repo.git
        part = url.split("github.com")[-1].lstrip(":/").removesuffix(".git")
        repo = part
    branch = sys.argv[2] if len(sys.argv) > 2 else "main"
    base = "https://api.github.com"

    local_head = git("rev-parse", "HEAD").strip()
    local_tree = git("rev-parse", "HEAD^{tree}").strip()
    print(f"仓库       : {repo}")
    print(f"分支       : {branch}")
    print(f"本地 HEAD  : {local_head}")
    print(f"本地 tree  : {local_tree}")

    ref = api(base, tok, f"/repos/{repo}/git/ref/heads/{branch}")
    remote_sha = ref["object"]["sha"]
    print(f"远端 HEAD  : {remote_sha}")
    if remote_sha == local_head:
        print("已同步，无需操作")
        return 0

    remote_tree = api(base, tok, f"/repos/{repo}/git/commits/{remote_sha}")["tree"]["sha"]
    diff = git("diff-tree", "-r", "--no-commit-id", remote_sha, local_head).strip()
    if not diff:
        print("tree 内容相同但 commit 不同 —— 直接更新 ref 指向本地提交不可行（对象不在远端）。")
        print("请改用正常 git push。")
        return 1

    entries = []
    print(f"\n差异文件 {len(diff.splitlines())} 个:")
    for line in diff.splitlines():
        parts = line.split()
        old_mode, new_mode, sha, status = parts[0][1:], parts[1], parts[3], parts[4]
        path = line.split("\t", 1)[1]
        if status == "D":
            # 删除条目必须用**旧模式**：新模式的 000000 会被 API 拒绝
            # （422 Must supply a valid tree.mode）。这是实测踩到的。
            entries.append({"path": path, "mode": old_mode, "type": "blob", "sha": None})
            print(f"  删除 {path}")
        else:
            content = git("cat-file", "blob", sha, binary=True)
            import base64
            b = api(base, tok, f"/repos/{repo}/git/blobs", "POST",
                    {"content": base64.b64encode(content).decode(), "encoding": "base64"})
            entries.append({"path": path, "mode": new_mode, "type": "blob", "sha": b["sha"]})
            print(f"  上传 {path}  {len(content)}B")

    new_tree = api(base, tok, f"/repos/{repo}/git/trees", "POST",
                   {"base_tree": remote_tree, "tree": entries})
    print(f"\n新 tree    : {new_tree['sha']}")
    if new_tree["sha"] != local_tree:
        print("!! tree SHA 与本地不一致 —— 中止，避免产生内容不符的提交", file=sys.stderr)
        print(f"   本地 {local_tree}  远端 {new_tree['sha']}", file=sys.stderr)
        return 1
    print("  ✓ tree SHA 与本地完全一致（内容一致的决定性证据）")

    # 用 git 的格式化输出取身份 —— 不要手工解析 commit 对象
    # （手工解析曾把署名首字母吃掉，把 vip-svip 变成 ip-svip）
    # **必须 strip**：git 的输出带尾换行，直接送进 API 会得到
    # 422 "name can't contain a newline character"。这是实测踩到的。
    def one(fmt):
        return subprocess.run(["git", "log", "-1", "--format=" + fmt],
                              capture_output=True, check=True, text=True).stdout.strip()

    ident = {"name": one("%an"), "email": one("%ae"), "date": one("%aI")}
    cident = {"name": one("%cn"), "email": one("%ce"), "date": one("%cI")}
    msg = subprocess.run(["git", "log", "-1", "--format=%B"], capture_output=True,
                         check=True, text=True).stdout

    commit = api(base, tok, f"/repos/{repo}/git/commits", "POST", {
        "message": msg, "tree": new_tree["sha"], "parents": [remote_sha],
        "author": ident, "committer": cident,
    })
    print(f"新 commit  : {commit['sha']}")
    print(f"  署名     : {commit['author']['name']} <{commit['author']['email']}>")
    print("  ✓ SHA 与本地完全一致" if commit["sha"] == local_head
          else "  注意：SHA 与本地不同，但 tree 一致（内容相同）")

    api(base, tok, f"/repos/{repo}/git/refs/heads/{branch}", "PATCH",
        {"sha": commit["sha"], "force": False})
    print(f"\nrefs/heads/{branch} 已更新 -> {commit['sha'][:12]}")
    print("提示：若本地与远端 SHA 不同，可执行 "
          "`git reset --soft <远端SHA> && git update-ref refs/remotes/origin/main <远端SHA>` 对齐")
    return 0

if __name__ == "__main__":
    sys.exit(main())
