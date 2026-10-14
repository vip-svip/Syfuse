# update_json.py —— 更新 update.json（自更新链路，18.5）。
# 用法: python update_json.py <update_json_path> <version> <zip_sha256> <owner>
# 结构：{owner/仓库}/{分支}/{文件名} 形态的 URL；sha256 字段供自更新校验。
import json
import sys

path, version, zip_sha, owner = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
BRANCH = "main"
FILE = f"syfuse-{version}.zip"

data = {}
try:
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
except Exception:
    data = {}

data["version"] = version
data["versionCode"] = data.get("versionCode", 1)
data["zipUrl"] = f"https://github.com/{owner}/Syfuse/releases/download/v{version}/{FILE}"
data["sha256"] = zip_sha
data["changelog"] = data.get("changelog", "全功能打包：代理/过滤/脚本/MITM/防泄漏/通知净化/备份还原。")

with open(path, "w", encoding="utf-8", newline="\n") as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
    f.write("\n")
print(f"update.json 已更新: owner={owner} version={version} sha256={zip_sha[:12]}…")
