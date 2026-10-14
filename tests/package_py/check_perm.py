# check_perm.py —— 模块包可执行位自检：关键条目必须是 0755。
# 用法: python check_perm.py <zip_path>
# 背景：权限位曾经整个丢过 —— 不在打包现场断言，就要等刷机才发现。
import sys
import zipfile

z = zipfile.ZipFile(sys.argv[1])
need = [
    'customize.sh', 'post-fs-data.sh', 'service.sh', 'uninstall.sh', 'action.sh',
    'META-INF/com/google/android/update-binary',
    'bin/syfused-arm64', 'bin/syfused-arm', 'bin/syfused-amd64',
]
bad = []
for n in need:
    try:
        mode = (z.getinfo(n).external_attr >> 16) & 0o777
    except KeyError:
        bad.append(f"{n} 缺失")
        continue
    if mode != 0o755:
        bad.append(f"{n} 权限位 {oct(mode)} != 0755")
if bad:
    for b in bad:
        print(f"  [FAIL] {b}")
    sys.exit(1)
print("  关键条目权限位全部 0755")
