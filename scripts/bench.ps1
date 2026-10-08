# Budgets of requirement R42 for init.ps1, measured on this machine.
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\bench.ps1
#   pwsh -NoProfile -File scripts/bench.ps1
#
# It measures the PowerShell that runs it:
#   1. what dot-sourcing init.ps1 adds to the start of a session: the median
#      of fresh processes with it minus the median of fresh processes without
#      it. Both load PSReadLine first, as an interactive session has it before
#      any profile runs. The budget is on that difference, for a session
#      started from the Serena profile, which does the most. Next to it the
#      time of the dot-sourcing itself is printed, taken inside further
#      processes: a whole PowerShell start takes several times as long as
#      init.ps1 and varies by more than init.ps1 costs, so the difference of
#      the medians moves a lot from run to run and the inside time does not;
#   2. what one prompt costs with the follow check active: the mean of 200
#      calls, each after a new command in the history;
#   3. that init and prompt also work with PATH emptied, which they cannot do
#      if they start a program.
# On Windows it then hands over to scripts/bench.sh for bash when Git Bash is
# installed.
#
# Budgets: 60 ms and 2 ms on the owner's machine, 150 ms and 10 ms when the
# variable CI is set (or with -Ci). The exit code is 1 when a budget is not
# met.
#
# Everything is rendered into, and run from, a temporary folder that is
# removed at the end. The children get that folder as their home; PSReadLine
# is told to save no history. No setting of the machine is changed: the
# session facts are simulated through environment variables.
[CmdletBinding()]
param(
    # A rendered init.ps1 to measure. Default: render one from the sources with
    # `go run ./internal/tools/golden`.
    [string]$Init = '',
    # Fresh processes per series.
    [int]$Runs = 9,
    # Prompt calls per series.
    [int]$Calls = 200,
    # Use the looser gates of CI.
    [switch]$Ci,
    # Do not run scripts/bench.sh afterwards.
    [switch]$SkipBash
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent $PSScriptRoot
$exe = (Get-Process -Id $PID).Path
$onWindows = ($env:OS -eq 'Windows_NT')
$loose = $Ci -or [bool]$env:CI
$initBudget = 60.0; $promptBudget = 2.0
if ($loose) { $initBudget = 150.0; $promptBudget = 10.0 }
$failed = $false

function Get-Median([double[]]$values) {
    $sorted = @($values | Sort-Object)
    $n = $sorted.Count
    if ($n % 2) { return $sorted[($n - 1) / 2] }
    return ($sorted[$n / 2 - 1] + $sorted[$n / 2]) / 2
}

function Format-Ms([double]$value) { $value.ToString('0.0', [Globalization.CultureInfo]::InvariantCulture) }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('sereno-bench-' + [guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $tmp
try {
    $userHome = Join-Path $tmp 'home'
    $root = Join-Path $userHome 'sereno'
    $emptyPath = Join-Path $tmp 'empty-path'
    foreach ($dir in $userHome, $root, (Join-Path $root 'shell'), $emptyPath, (Join-Path $userHome 'AppData'), (Join-Path $userHome 'AppData\Local'), (Join-Path $userHome 'AppData\Roaming'), (Join-Path $userHome '.local'), (Join-Path $userHome '.local\share')) {
        $null = New-Item -ItemType Directory -Path $dir -Force
    }

    # The environment of every child: a home inside the temporary folder.
    $base = @{
        HOME = $userHome; USERPROFILE = $userHome
        LOCALAPPDATA = (Join-Path $userHome 'AppData\Local'); APPDATA = (Join-Path $userHome 'AppData\Roaming')
        XDG_DATA_HOME = (Join-Path $userHome '.local\share'); HISTFILE = (Join-Path $userHome '.bash_history')
        SERENO_HOME = $root; SERENO_SANDBOX = $tmp
        SERENO_OS_MODE = ''; SERENO_SESSION = ''; SERENO_REG_ROOT = ''; SERENO_LANG = ''
        WT_SESSION = ''; WT_PROFILE_ID = ''; TERM_PROGRAM = ''
    }
    if ($onWindows) { $base['HOMEDRIVE'] = $userHome.Substring(0, 2); $base['HOMEPATH'] = $userHome.Substring(2) }

    if (-not $Init) {
        $rendered = Join-Path $tmp 'rendered'
        $old = $env:SERENO_SANDBOX
        $env:SERENO_SANDBOX = $tmp
        Push-Location $repo
        try {
            $output = & go run ./internal/tools/golden --out $rendered 2>&1
            if ($LASTEXITCODE -ne 0) { throw "go run ./internal/tools/golden failed: $output" }
        } finally {
            Pop-Location
            $env:SERENO_SANDBOX = $old
        }
        $Init = Join-Path $rendered 'init\init.ps1'
    }
    $initCopy = Join-Path $root 'shell\init.ps1'
    Copy-Item -LiteralPath $Init -Destination $initCopy
    [IO.File]::WriteAllText((Join-Path $root 'mode'), "mode=auto`nflavor=soft`ninband=profile`nkeys=profile`nprompt=on`n")

    # Runs one child and returns its wall-clock time in milliseconds and what
    # it wrote.
    function Invoke-Child([string]$script, [hashtable]$extra) {
        $file = Join-Path $tmp ('child-' + [guid]::NewGuid().ToString('N') + '.ps1')
        [IO.File]::WriteAllText($file, $script)
        $info = New-Object System.Diagnostics.ProcessStartInfo
        $info.FileName = $exe
        $info.Arguments = '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + $file + '"'
        if (-not $onWindows) { $info.Arguments = '-NoLogo -NoProfile -NonInteractive -File "' + $file + '"' }
        $info.UseShellExecute = $false
        $info.CreateNoWindow = $true
        $info.RedirectStandardOutput = $true
        $info.RedirectStandardError = $true
        $info.WorkingDirectory = $userHome
        foreach ($pair in @($base.GetEnumerator()) + @($extra.GetEnumerator())) { $info.EnvironmentVariables[$pair.Key] = [string]$pair.Value }
        $watch = [Diagnostics.Stopwatch]::StartNew()
        $process = [Diagnostics.Process]::Start($info)
        $errTask = $process.StandardError.ReadToEndAsync()
        $out = $process.StandardOutput.ReadToEnd()
        $process.WaitForExit()
        $watch.Stop()
        $err = $errTask.Result
        $code = $process.ExitCode
        $process.Dispose()
        Remove-Item -LiteralPath $file -Force
        if ($code -ne 0) { throw "a child ended with code $code`n$out`n$err" }
        [pscustomobject]@{ Ms = $watch.Elapsed.TotalMilliseconds; Out = $out; Err = $err }
    }

    # What a session has before any profile runs.
    $prelude = @'
Import-Module PSReadLine
Set-PSReadLineOption -HistorySaveStyle SaveNothing
'@
    $quoted = "'" + $initCopy.Replace("'", "''") + "'"
    $withInit = $prelude + "`n. $quoted`n"
    $timedInit = $prelude + "`n`$w = [Diagnostics.Stopwatch]::StartNew()`n. $quoted`n'INIT=' + `$w.Elapsed.TotalMilliseconds.ToString([Globalization.CultureInfo]::InvariantCulture)`n"

    $sessions = [ordered]@{
        'Serena profile session' = @{ WT_SESSION = 'bench'; SERENO_SESSION = 'wt-profile' }
        'hook session' = @{ WT_SESSION = 'bench' }
    }

    Write-Output ("sereno bench: {0} {1}, {2} processes per series, budgets {3} ms and {4} ms" -f (Split-Path -Leaf $exe), $PSVersionTable.PSVersion, $Runs, (Format-Ms $initBudget), (Format-Ms $promptBudget))

    # 1. init.ps1. Three kinds of process take turns: one without init.ps1,
    # one that dot-sources it, and one that also times the dot-sourcing from
    # inside. The budget is on the difference of the first two, as R42 says;
    # the third is printed next to it because a whole process takes five
    # times as long as init.ps1 and varies by more than init.ps1 costs.
    $null = Invoke-Child $withInit $sessions['Serena profile session']   # warm the file cache
    foreach ($name in $sessions.Keys) {
        $with = @(); $without = @(); $inside = @()
        for ($i = 0; $i -lt $Runs; $i++) {
            $without += (Invoke-Child $prelude $sessions[$name]).Ms
            $child = Invoke-Child $withInit $sessions[$name]
            $with += $child.Ms
            if ($child.Out -ne '') { throw "init.ps1 printed something: $($child.Out)" }
            $child = Invoke-Child $timedInit $sessions[$name]
            if ($child.Out -match '\AINIT=([0-9.]+)\s*\z') { $inside += [double]::Parse($Matches[1], [Globalization.CultureInfo]::InvariantCulture) }
            else { throw "init.ps1 printed something: $($child.Out)" }
        }
        $added = (Get-Median $with) - (Get-Median $without)
        $line = "init.ps1, {0}: adds {1} ms (median of {2} processes with {3}, without {4}); timed inside the process {5} ms (median; {6} to {7})" -f $name, (Format-Ms $added), $Runs,
            (Format-Ms (Get-Median $with)), (Format-Ms (Get-Median $without)), (Format-Ms (Get-Median $inside)), (Format-Ms (($inside | Measure-Object -Minimum).Minimum)), (Format-Ms (($inside | Measure-Object -Maximum).Maximum))
        if ($name -eq 'Serena profile session') {
            if ($added -le $initBudget) { $line += "  ok, budget $(Format-Ms $initBudget)" } else { $line += "  OVER the budget of $(Format-Ms $initBudget)"; $failed = $true }
        }
        Write-Output $line
    }

    # 2. the prompt, with the follow check active
    $promptScript = $prelude + "`n. $quoted`n" + @'
$null = New-Item -ItemType Directory -Path (Join-Path $HOME 'work\.git') -Force
[IO.File]::WriteAllText((Join-Path $HOME 'work\.git\HEAD'), "ref: refs/heads/main`n")
Set-Location (Join-Path $HOME 'work')
$sink = New-Object System.IO.StringWriter
$console = [Console]::Out
[Console]::SetOut($sink)
$w = [Diagnostics.Stopwatch]::StartNew()
$first = prompt
$firstMs = $w.Elapsed.TotalMilliseconds
$t0 = [datetime]'2026-01-01T00:00:00'
$timer = New-Object System.Diagnostics.Stopwatch
for ($i = 0; $i -lt CALLS; $i++) {
    [pscustomobject]@{ CommandLine = "command $i"; ExecutionStatus = 'Completed'; StartExecutionTime = $t0; EndExecutionTime = $t0 } | Add-History
    $timer.Start(); $null = prompt; $timer.Stop()
}
$idle = [Diagnostics.Stopwatch]::StartNew()
for ($i = 0; $i -lt CALLS; $i++) { $null = prompt }
$idle.Stop()
[Console]::SetOut($console)
$c = [Globalization.CultureInfo]::InvariantCulture
'FIRST=' + $firstMs.ToString($c)
'MEAN=' + ($timer.Elapsed.TotalMilliseconds / CALLS).ToString($c)
'IDLE=' + ($idle.Elapsed.TotalMilliseconds / CALLS).ToString($c)
'PAINTED=' + $sink.ToString().Length
'BRANCH=' + $first.Contains('main')
'@.Replace('CALLS', [string]$Calls)
    $number = { param($text, $key) if ($text -match ($key + '=([0-9.]+)')) { [double]::Parse($Matches[1], [Globalization.CultureInfo]::InvariantCulture) } else { throw "no $key in: $text" } }
    $child = Invoke-Child $promptScript $sessions['Serena profile session']
    $mean = & $number $child.Out 'MEAN'
    $line = "prompt, follow check active: {0} ms (mean of {1} calls, each after a command; {2} ms when nothing ran; first call {3} ms)" -f (Format-Ms $mean), $Calls, (Format-Ms (& $number $child.Out 'IDLE')), (Format-Ms (& $number $child.Out 'FIRST'))
    if ((& $number $child.Out 'PAINTED') -le 0) { throw 'the follow check did not paint: the measurement would not be of the active check' }
    if ($child.Out -notmatch 'BRANCH=True') { throw 'the prompt did not show the branch' }
    if ($mean -le $promptBudget) { $line += "  ok, budget $(Format-Ms $promptBudget)" } else { $line += "  OVER the budget of $(Format-Ms $promptBudget)"; $failed = $true }
    Write-Output $line

    # 3. the same with PATH emptied
    $extra = @{ WT_SESSION = 'bench'; SERENO_SESSION = 'wt-profile'; PATH = $emptyPath }
    $child = Invoke-Child $promptScript $extra
    Write-Output ("with PATH emptied: init and {0} prompts ran, mean {1} ms  ok" -f (2 * $Calls + 1), (Format-Ms (& $number $child.Out 'MEAN')))
} finally {
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}

# bash, through scripts/bench.sh.
if ($onWindows -and -not $SkipBash) {
    # Git Bash is the bash of the Git for Windows that git.exe belongs to:
    # git.exe is in cmd\ or mingw64\bin\ of that folder, bash.exe in bin\.
    $bash = $null
    $git = Get-Command git.exe -ErrorAction SilentlyContinue
    if ($git) {
        $folder = Split-Path -Parent $git.Source
        for ($up = 0; ($up -lt 3) -and $folder -and (-not $bash); $up++) {
            $candidate = Join-Path $folder 'bin\bash.exe'
            if (Test-Path -LiteralPath $candidate) { $bash = $candidate }
            $folder = Split-Path -Parent $folder
        }
    }
    if ($bash) {
        $arguments = @((Join-Path $PSScriptRoot 'bench.sh'))
        if ($loose) { $arguments += '--ci' }
        & $bash --noprofile --norc @arguments
        if ($LASTEXITCODE -ne 0) { $failed = $true }
    } else {
        Write-Output 'bash: skipped, Git Bash was not found next to git.exe'
    }
}

if ($failed) { Write-Output 'sereno bench: a budget was not met'; exit 1 }
Write-Output 'sereno bench: all budgets met'
exit 0
