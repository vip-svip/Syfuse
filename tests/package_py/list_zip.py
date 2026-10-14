# list_zip.py —— 列出 zip 条目（缩进前缀），unzip 不可用时的回退。
import sys
import zipfile

for n in zipfile.ZipFile(sys.argv[1]).namelist():
    print('    ' + n)
