<#
  verify_deliverable.ps1 —— 交付完整性校验（Windows 版）
  与 verify_deliverable.sh 检查项一致（共 6 组）。

  用法：
    powershell -NoProfile -ExecutionPolicy Bypass -File tools\verify_deliverable.ps1
    ... -Path "F:\Ai\Syfuse-完美提示词.md" -MinLines 1000
    ... -SearchRoot "D:\proj"
  退出码：0 = 全部通过；1 = 存在失败项（CI/交付门禁）

  注意：本文件必须保存为「带 UTF-8 BOM」的 UTF-8，
  否则 Windows PowerShell 5.1 会按系统 ANSI 代码页解码，中文注释会导致语法错误。
#>
param(
  [string]$Path = '',
  [int]$MinLines = 1,
  [string]$SearchRoot = '',
  [switch]$NoRequirements
)

$ErrorActionPreference = 'Stop'
$script:fail = 0
$script:warn = 0

function Ok   ($m) { Write-Host ("  [PASS] " + $m) }
function Bad  ($m) { Write-Host ("  [FAIL] " + $m) -ForegroundColor Red;    $script:fail++ }
function Warn ($m) { Write-Host ("  [WARN] " + $m) -ForegroundColor Yellow; $script:warn++ }
function Head ($m) { Write-Host ""; Write-Host ("== " + $m + " ==") -ForegroundColor Cyan }
function Clip ($s, $n = 64) {
  if ($null -eq $s) { return '' }
  $s = [string]$s
  if ($s.Length -le $n) { return $s }
  return $s.Substring(0, $n)
}

# ---------- 0. 定位目标文件 ----------
if (-not $Path) {
  $roots = @()
  if ($SearchRoot) { $roots += $SearchRoot } else { $roots += (Get-Location).Path }
  $cands = @()
  foreach ($r in $roots) {
    if (Test-Path -LiteralPath $r) {
      $cands += @(Get-ChildItem -LiteralPath $r -Filter *.md -File -ErrorAction SilentlyContinue)
    }
  }
  $md = $cands | Sort-Object Length -Descending | Select-Object -First 1
  if ($md) { $Path = $md.FullName }
}
if (-not $Path -or -not (Test-Path -LiteralPath $Path)) {
  Write-Host "[FAIL] 找不到待校验文件（用 -Path 指定）" -ForegroundColor Red
  exit 1
}
$fi = Get-Item -LiteralPath $Path
$lines = @(Get-Content -LiteralPath $Path -Encoding UTF8)
$text = ($lines -join "`n")

Head "目标文件"
Write-Host ("  " + $fi.FullName)
Write-Host ("  字节 " + $fi.Length + "   行数 " + $lines.Count)

# ---------- 1. 体积与行数 ----------
Head "1. 体积与行数（确认没被截断）"
if ($fi.Length -gt 0) { Ok ("字节数 " + $fi.Length) } else { Bad "文件为空" }
if ($lines.Count -ge $MinLines) { Ok ("行数 " + $lines.Count + " >= 下限 " + $MinLines) }
else { Bad ("行数 " + $lines.Count + " < 下限 " + $MinLines) }

$firstLine = if ($lines.Count -gt 0) { [string]$lines[0] } else { '' }
$lastLine  = if ($lines.Count -gt 0) { [string]$lines[$lines.Count - 1] } else { '' }
if ($firstLine.Trim().Length -gt 0) { Ok ("首行非空：" + (Clip $firstLine 48)) } else { Bad "首行为空（疑似截断）" }
if ($lastLine.Trim().Length -gt 0)  { Ok ("末行非空：" + (Clip $lastLine 48)) }  else { Bad "末行为空（疑似截断）" }

# ---------- 2. 结构完整性 ----------
Head "2. 结构完整性"
$h2 = @($lines | Where-Object { $_ -match '^## ' })
$h3 = @($lines | Where-Object { $_ -match '^### ' })
$fence = @($lines | Where-Object { $_ -match '^```' })
if ($h2.Count -gt 0) { Ok ("## 章节 " + $h2.Count + " 个") } else { Bad "没有任何 ## 章节" }
if ($h3.Count -gt 0) { Ok ("### 小节 " + $h3.Count + " 个") } else { Warn "没有 ### 小节" }
if (($fence.Count % 2) -eq 0) { Ok ("代码围栏 " + $fence.Count + " 个（成对）") }
else { Bad ("代码围栏 " + $fence.Count + " 个（奇数 = 有未闭合代码块）") }

$dup = $h3 | Group-Object | Where-Object { $_.Count -gt 1 }
if ($dup) { foreach ($d in $dup) { Warn ("小节标题重复 " + $d.Count + " 次：" + $d.Name.Trim()) } } else { Ok "无重复小节标题" }

# ---------- 3. 占位符 / 未完成标记 ----------
Head "3. 占位符与未完成标记"
$phPattern = 'TODO|FIXME|<your-name>|待补写|\[略\]|（略）|\.\.\.\s*$|XXX'
$meta = '❌|禁止|不许|不要|除外|占位符|核对|扫描|校验|检测'
# 围栏必须按长度配对：``` 开的块只能被 ``` 关，```` 开的块要被 ```` 关。
# 否则脚本源码里出现的 ``` 会把状态翻转，导致后续正文被漏检或代码块被误判。
$phHits = @()
$inFence = $false
$fenceLen = 0
$inBlockHits = 0
for ($i = 0; $i -lt $lines.Count; $i++) {
  $line = $lines[$i]
  if ($line -match '^(`{3,})') {
    $ticks = $Matches[1].Length
    if (-not $inFence) { $inFence = $true; $fenceLen = $ticks }
    elseif ($ticks -ge $fenceLen) { $inFence = $false; $fenceLen = 0 }
    continue
  }
  if ($line -match $phPattern) {
    if ($inFence) { $inBlockHits++ }          # 代码块内的字样不算文档缺陷，只计数
    else { $phHits += [pscustomobject]@{ Line = $i + 1; Text = $line.Trim() } }
  }
}
if ($phHits.Count -eq 0) { Ok "正文无占位符 / 未完成标记" }
else {
  foreach ($h in $phHits) {
    # 双条件才算违规：① 剥掉行内 `代码段` 后正文仍命中；② 整行不含"在讲规则"的元标记。
    # 已知局限：真残留恰好写在含元标记的行里会被 WARN 放过——所以 WARN 也要人眼扫一遍。
    $prose = [regex]::Replace($h.Text, '`[^`]*`', '')
    if (($prose -match $phPattern) -and ($h.Text -notmatch $meta)) {
      Bad ("L" + $h.Line + " 正文残留占位符：" + (Clip $h.Text 60))
    } else {
      Warn ("L" + $h.Line + " 命中位于行内代码/规则说明，忽略：" + (Clip $h.Text 60))
    }
  }
}
if ($inBlockHits -gt 0) { Warn ("代码块内含占位符类字样 " + $inBlockHits + " 处（示例/源码，已跳过不计失败）") }

# ---------- 4. 密钥 / 令牌残留 ----------
Head "4. 密钥与令牌残留扫描"
$secPattern = 'ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|xox[baprs]-[A-Za-z0-9-]{10,}'
$secHits = @($lines | Select-String -Pattern $secPattern)
if ($secHits.Count -eq 0) { Ok "未发现明文密钥 / 令牌" }
else { foreach ($s in $secHits) { Bad ("L" + $s.LineNumber + " 疑似密钥：" + (Clip $s.Line.Trim() 50)) } }

# ---------- 5. 需求覆盖 ----------
if (-not $NoRequirements) {
  Head "5. 需求逐条覆盖"
  $reqs = @'
1 SY-Fusion 作为参考基线|SY-Fusion
2 对标 Shadowrocket 且只能多不能少|只能多不能少
3 主界面非 Web 端|WebView / H5 / 混合框架承载主界面
4 支持 root 与 LSPosed|LSPosed
5 UI 合理化与深度定制|UI 深度定制
6 极客化与全功能自定义|全功能自定义化
7 交付为模块形态|模块层（systemless，交付主体）
8 模块内置软件并互相增强|内含 sidecar APK
9 新仓库与短英文模块名|syfuse
10 作者署名取 GitHub 账号|账号名运行时解析
11 GitHub 令牌能力与权限|已授权使用
12 反僵化（不许死脑筋）|反僵化条款
13 不设能力上限（完全体）|完全体授权
14 单文件自包含|单文件 · 自包含
15 Shadowrocket 有就不重复造|Shadowrocket 有就不加
16 功能必须真的能用|功能可用性门禁
17 节点与 IP 防泄漏|IP 防泄漏
18 AdGuard Home 功能并入|AdGuard Home
19 uBlock Origin Lite 功能并入|uBlock Origin Lite
20 SY/SY-Fusion 功能全量并入并有逐项映射|逐项复现映射
21 新流行配色|OKLCH
22 透明玻璃（受控）|透明玻璃规范
23 App 内浏览器面板开关|启用浏览器面板
24 交付完整性纪律|把没读完说成读过了
25 界面可重新设计（不必照搬实拍）|仅供参考，可重新设计
26 功能合理分区|UI 深度定制与功能分区
27 交付前必须跑校验门禁|verify_deliverable
28 玻璃性能预算|P90 ≤ 24ms
29 模块 action.sh 菜单|action.sh
30 Hook 内置能力|证书固定绕过
31 多布局预设 + 可自建 Tab|布局预设
32 SY/SY-Fusion 可复现规格|复现规格
33 主流主题预设方案|主流主题预设
34 自定义主题方案（逐角色编辑/取色/分区覆盖/主题包）|自定义主题方案
35 默认 7 Tab 布局|默认使用布局 B
36 作者 GitHub 地址内置到 App|作者与开源信息**内置到 App 里**
'@ -split "`n" | Where-Object { $_.Trim().Length -gt 0 }

  $miss = 0
  foreach ($r in $reqs) {
    $parts = $r -split '\|', 2
    $name = $parts[0].Trim()
    $kw   = $parts[1].Trim()
    if ($text -match [regex]::Escape($kw)) { Ok $name }
    else { Bad ($name + "  —— 未找到关键词：" + $kw); $miss++ }
  }
  Write-Host ("  需求项 " + $reqs.Count + " 条，缺失 " + $miss + " 条")
}

# ---------- 6. 脚本自身编码（踩过的坑，必须成为检查项）----------
Head "6. 脚本编码规范"
$scanRoot = Split-Path -Parent $Path
if (-not $scanRoot -or -not (Test-Path -LiteralPath $scanRoot)) { $scanRoot = (Get-Location).Path }
# 不能用 Get-ChildItem -LiteralPath ... -Include：-Include 在 -LiteralPath 下不生效，
# 结果 .exe/.jpg/.md 都会被当成脚本检查（还会把上百 MB 的 exe 读进内存）。
# 必须自己按扩展名过滤，并剪掉第三方/产物目录。
$pruneDirs = @('.git', 'build', 'out', 'node_modules', 'downloads', 'screenshots', '.gradle', '.idea', 'dist', 'logs')
$scripts = @(
  Get-ChildItem -LiteralPath $scanRoot -Recurse -File -ErrorAction SilentlyContinue |
    Where-Object { $_.Extension -eq '.ps1' -or $_.Extension -eq '.sh' } |
    Where-Object {
      $rel = $_.FullName.Substring($scanRoot.Length).TrimStart('\')
      $parts = $rel -split '\\'
      -not ($parts | Where-Object { $pruneDirs -contains $_ })
    }
)
if ($scripts.Count -eq 0) { Warn "未找到 .ps1/.sh 可检查（跳过）" }
foreach ($s in $scripts) {
  $b = @(Get-Content -LiteralPath $s.FullName -Encoding Byte -TotalCount 3)
  $hasBom = ($b.Count -ge 3 -and $b[0] -eq 239 -and $b[1] -eq 187 -and $b[2] -eq 191)
  if ($s.Extension -eq '.ps1') {
    if ($hasBom) { Ok ($s.Name + " 带 UTF-8 BOM（PS 5.1 解码正确）") }
    else { Bad ($s.Name + " 缺少 UTF-8 BOM：Windows PowerShell 5.1 会按 ANSI 解码中文并报语法错误") }
  } else {
    if (-not $hasBom) { Ok ($s.Name + " 无 BOM（shebang 正确）") }
    else { Bad ($s.Name + " 带 BOM，会破坏 shebang，必须去掉") }
    # 换行符：.sh 混入 CRLF 会让 bash 报 "\r: command not found"（Windows 上极易发生）
    $raw = Get-Content -LiteralPath $s.FullName -Raw -Encoding UTF8
    if ($raw -match "`r`n") { Bad ($s.Name + " 含 CRLF 换行，bash 会报 '\r: command not found'，必须转成 LF") }
    else { Ok ($s.Name + " 换行为 LF（正确）") }
  }
}

# ---------- 汇总 ----------
Head "汇总"
if ($script:fail -eq 0) {
  Write-Host ("  全部通过（WARN " + $script:warn + " 项）") -ForegroundColor Green
  exit 0
} else {
  Write-Host ("  失败 " + $script:fail + " 项，WARN " + $script:warn + " 项 —— 不允许交付") -ForegroundColor Red
  exit 1
}
