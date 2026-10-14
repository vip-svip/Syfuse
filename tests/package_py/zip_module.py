# zip_module.py —— 模块包便携打包（NTFS 无 Unix 权限位，按白名单显式写入）。
# 用法: python zip_module.py <src_dir> <out_zip>
import os
import sys
import zipfile

src, out = sys.argv[1], sys.argv[2]


def mode_for(rel: str) -> int:
    """返回该条目在模块包内应有的 Unix 权限位。

    不读 st_mode —— NTFS 上它没有意义（恒 0666，靠它会让
    action.sh / customize.sh / bin/syfused-* 全部 0666，刷进设备
    脚本无法执行，模块形同废包）。规则与 KernelSU/Magisk 的安装器
    期望一致：
      - 所有 *.sh 及 META-INF/.../update-binary -> 0755
      - bin/syfused-*                           -> 0755
      - 其余（配置、module.prop、内置 APK）      -> 0644
    """
    base = os.path.basename(rel)
    if rel == 'META-INF/com/google/android/update-binary':
        return 0o755
    if base.endswith('.sh'):
        return 0o755
    if rel.startswith('bin/') and base.startswith('syfused-'):
        return 0o755
    return 0o644


with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as z:
    for root, dirs, files in os.walk(src):
        dirs[:] = sorted(d for d in dirs if d != '__MACOSX')
        for name in sorted(files):
            if name == '.DS_Store':
                continue
            full = os.path.join(root, name)
            rel = os.path.relpath(full, src).replace(os.sep, '/')
            zi = zipfile.ZipInfo(rel)
            # 目录位 (0o40000) | 权限位，左移 16 位即 Unix mode 的高 16 位。
            zi.external_attr = (0o100000 | mode_for(rel)) << 16
            zi.compress_type = zipfile.ZIP_DEFLATED
            zi.date_time = (1980, 1, 1, 0, 0, 0)  # 固定时间戳：打包可复现
            with open(full, 'rb') as fh:
                z.writestr(zi, fh.read())

print(f"  已写出 {len(zipfile.ZipFile(out).namelist())} 个条目，权限位按白名单显式写入")
