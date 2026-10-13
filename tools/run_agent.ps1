<#
  run_agent.ps1 —— 自动续跑驱动：循环调用 AI，直到 PROGRESS.md 报告全部里程碑完成。

  为什么需要它：AI 单轮输出预算有限，不可能一轮做完 14 个里程碑；
  但"每轮之间要人手动催"可以消除——由本脚本代替你按"继续"。

  设计要点（都是踩过坑之后改的）：
    1. 续跑指令只落盘成 CONTINUE.md，每轮只传一句短指令给 AI。
       原因：多行带引号的文本传给 .cmd 包装器会被命令行解析吃掉。
    2. -AgentArgs 是「一个字符串」，内部按空格拆分。
       原因：数组经 powershell -File 传参只会绑定第一个元素。
    3. AI 连续失败 2 次才停；PROGRESS.md 连续 StallRounds 轮无变化才判定卡死。
    4. 每轮原始输出写 logs\round-NNN.log；不同退出码区分失败原因。

  用法：
    powershell -NoProfile -ExecutionPolicy Bypass -File tools\run_agent.ps1
    ... -ProjectDir "D:\Syfuse" -MaxRounds 30
    ... -AgentCmd "claude" -AgentArgs "-p"                       # 换别的 AI CLI
    ... -AgentCmd "powershell" -AgentArgs "-NoProfile -ExecutionPolicy Bypass -File .\stub.ps1"

  退出码：0 = 全部完成；2 = 达到最大轮数仍未完成；3 = AI 连续调用失败；4 = 判定卡死
#>
param(
  [string]$ProjectDir = '',
  [int]$MaxRounds = 30,
  [string]$AgentCmd = 'dsh',
  [string]$AgentArgs = 'headless',
  [int]$StallRounds = 3,
  [int]$FailRounds = 2,
  [string]$DoneMarker = 'NEXT_ACTION:\s*(DONE|全部完成)',
  [string]$GatePath = '',
  [string]$GateDoc = '',
  [int]$GateMinLines = 100,
  [switch]$Quiet
)

$ErrorActionPreference = 'Stop'

if (-not $ProjectDir) { $ProjectDir = (Get-Location).Path }
if (-not (Test-Path -LiteralPath $ProjectDir)) { throw "项目目录不存在: $ProjectDir" }
$ProjectDir = (Resolve-Path -LiteralPath $ProjectDir).Path

$progressPath = Join-Path $ProjectDir 'PROGRESS.md'
$continuePath = Join-Path $ProjectDir 'CONTINUE.md'
$logDir = Join-Path $ProjectDir 'logs'
if (-not (Test-Path -LiteralPath $logDir)) { New-Item -ItemType Directory -Path $logDir | Out-Null }

function Say($m) { if (-not $Quiet) { Write-Host $m } }

# ---- 续跑指令落盘（AI 每轮读它）----
$continueBody = @'
# 续跑指令（由 run_agent.ps1 自动下发，用户不需要编辑）

按 Syfuse 提示词继续执行，不要问我任何问题。

1. 读 PROGRESS.md，从 NEXT_ACTION 开工。
2. 做完这一轮能做的全部事，允许一轮跨多个里程碑。
3. 每轮结束前更新 PROGRESS.md：里程碑状态表（未开始/进行中/待外部验证/已完成/受阻）+ NEXT_ACTION + BLOCKERS，
   并把证据（门禁原始输出、功能可用性报告、性能数据）追加到 docs/EVIDENCE.md。
4. 遇到缺设备、缺凭据、缺 SDK、无网络这类外部阻塞：标「待外部验证」，逐项写进 PENDING-EXTERNAL.md
   （缺什么 / 怎么补 / 补齐后跑哪条命令 / 期望结果），然后继续做不依赖它的里程碑，不许整体停摆。
5. 外部环境能自动补齐就补齐（装 JDK / Android SDK / Go / 模拟器，连 adb / 设备），补完继续原流程。
6. 结尾不要写"要不要我继续""请确认"。本轮预算耗尽就写：
   本轮预算耗尽，进度已落盘到 PROGRESS.md；下一轮直接开始 M<n>，无需用户输入。
7. 全部里程碑完成后，把 PROGRESS.md 里的 NEXT_ACTION 写成 DONE。
'@
$continueBody | Set-Content -LiteralPath $continuePath -Encoding UTF8

# 每轮只传这一句（短、单行、无引号）
$roundPrompt = '按 CONTINUE.md 的指令继续执行，不要问我任何问题。'

$argList = @($AgentArgs -split '\s+' | Where-Object { $_ -ne '' })

Say "项目目录 : $ProjectDir"
Say "进度文件 : $progressPath"
Say "续跑指令 : $continuePath"
Say "驱动命令 : $AgentCmd $($argList -join ' ')"
Say "最大轮数 : $MaxRounds   卡死阈值: $StallRounds 轮无变化   连续失败上限: $FailRounds"
Say ("=" * 72)

$lastSnapshot = ''
$stall = 0
$fails = 0
$round = 0

while ($round -lt $MaxRounds) {
  $round++
  Say ""
  Say ("---- 第 {0}/{1} 轮 ----" -f $round, $MaxRounds)

  $logFile = Join-Path $logDir ("round-{0:D3}.log" -f $round)
  $out = ''
  $exit = 0
  try {
    # 先清零：$LASTEXITCODE 会保留上一次外部命令的值，
    # 若本轮调用的是 cmdlet / 函数，就会读到陈旧值造成误判
    $global:LASTEXITCODE = 0
    $out = & $AgentCmd @argList $roundPrompt 2>&1 | Out-String
    if ($null -ne $LASTEXITCODE) { $exit = [int]$LASTEXITCODE }
  } catch {
    $out = "AGENT_INVOKE_FAILED: " + $_.Exception.Message
    $exit = -1
  }
  ("exit=" + $exit + "`n" + $out) | Set-Content -LiteralPath $logFile -Encoding UTF8
  Say ("  输出 " + $out.Length + " 字符 -> " + (Split-Path -Leaf $logFile) + "   exit=" + $exit)

  $badPattern = 'AGENT_INVOKE_FAILED|CommandNotFoundException|is not recognized|不是内部或外部命令|无法将.*项识别'
  if ($exit -ne 0 -and $out -match $badPattern) {
    $fails++
    Say ("  [FAIL] AI 调用失败（连续 " + $fails + "/" + $FailRounds + "）")
    if ($fails -ge $FailRounds) {
      Say "  [STOP] 连续调用失败，检查 -AgentCmd / -AgentArgs，或先手动跑一次确认可用"
      Say ("  手动验证: " + $AgentCmd + " " + ($argList -join ' ') + " `"$roundPrompt`"")
      exit 3
    }
  } else {
    $fails = 0
  }

  if (-not (Test-Path -LiteralPath $progressPath)) {
    Say "  [WARN] 本轮没有生成 PROGRESS.md（协议要求必须生成）"
    $stall++
    if ($stall -ge $StallRounds) { Say "  [STOP] 连续 $stall 轮没有 PROGRESS.md，判定卡死"; exit 4 }
    continue
  }

  $snapshot = (Get-FileHash -LiteralPath $progressPath -Algorithm SHA256).Hash
  $content = Get-Content -LiteralPath $progressPath -Encoding UTF8 -Raw

  # 每轮跑一次交付门禁并留档；AI 不能只凭自己写的 PROGRESS.md 自证完成
  $gateExit = -99
  if ($GatePath -and (Test-Path -LiteralPath $GatePath)) {
    $gateOut = ''
    $global:LASTEXITCODE = 0
    $gateArgs = @('-NoProfile','-ExecutionPolicy','Bypass','-File',$GatePath)
    if ($GateDoc) { $gateArgs += @('-Path',$GateDoc,'-MinLines',"$GateMinLines") }
    $gateOut = & 'powershell' @gateArgs 2>&1 | Out-String
    $gateExit = [int]$LASTEXITCODE
    ("exit=" + $gateExit + "`n" + $gateOut) | Set-Content -LiteralPath (Join-Path $logDir ("gate-{0:D3}.log" -f $round)) -Encoding UTF8
    if ($gateExit -eq 0) { Say "  门禁: 通过 (exit=0)" } else { Say ("  门禁: 未通过 (exit=" + $gateExit + ") -> gate-" + ("{0:D3}" -f $round) + ".log") }
  }

  if ($content -match $DoneMarker) {
    if ($GatePath -and $gateExit -ne 0) {
      Say "  [STOP] AI 声称完成，但交付门禁未通过（exit=$gateExit），判定为虚假完成"
      exit 5
    }
    Say "  [DONE] PROGRESS.md 报告已完成"
    Say ("总计 " + $round + " 轮；日志: " + $logDir)
    exit 0
  }

  # 只认「里程碑行」：首列为 M+数字（M1 / M10 / M0），
  # 不能用"排除含'里程碑'的行"来过滤——那样产出列里恰好写了"里程碑文档"的行会被漏掉，
  # 极端情况下会把未完成的行全部滤掉，从而误判全部完成。
  $rows = @($content -split "`n" | Where-Object { $_ -match '^\|\s*M\d+\s*\|' })
  if ($rows.Count -gt 0) {
    $notDone = @($rows | Where-Object { $_ -notmatch '已完成' })
    Say ("  里程碑 " + $rows.Count + " 条，未完成 " + $notDone.Count + " 条")
    if ($notDone.Count -eq 0) {
      Say "  [DONE] 全部里程碑标记为已完成"
      Say ("总计 " + $round + " 轮；日志: " + $logDir)
      exit 0
    }
  }

  if ($snapshot -eq $lastSnapshot) {
    $stall++
    Say ("  [WARN] PROGRESS.md 与上一轮完全相同（连续 " + $stall + "/" + $StallRounds + "）")
    if ($stall -ge $StallRounds) {
      Say "  [STOP] 连续 $stall 轮没有任何进度变化，判定卡死；请看最后一轮日志定位原因"
      exit 4
    }
  } else {
    $stall = 0
    $lastSnapshot = $snapshot
    Say "  进度已更新，继续下一轮"
  }
}

Say ""
Say "  [STOP] 达到最大轮数 $MaxRounds 仍未完成；最后状态见 $progressPath"
exit 2
