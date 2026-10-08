# Budgets of requirement R42 for init.ps1, measured on this machine.
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\bench.ps1
#   pwsh -NoProfile -File scripts/bench.ps1
#
# It measures the PowerShell that runs it:
#   1. what dot-sourcing init.ps1 costs at the start of a session: the time
#      around the dot-sourcing, taken inside a fresh process that loaded
#      PSReadLine first, as an interactive session has it before any profile
#      runs; the median of such processes. Each of them starts without the
#      start-up profile PowerShell keeps from the process before it, which
#      makes it the first session of its kind: the slowest case, and one that
#      does not depend on what ran before. There is one budget for a session
#      started from the Serena profile, which also sets the keys, and one for
#      a session that loads the file from a profile of its own (a hook).
#      Printed below it and not judged: what the first prompt of the session
#      costs, once; what init.ps1 costs in a session that follows one of its
#      kind; and the median time of whole processes with and without the
#      file. The difference of those two is what this script used to judge.
#      It cannot decide a budget: a PowerShell start takes several times as
#      long as init.ps1 and varies by more than init.ps1 costs, so the
#      difference moves by tens of milliseconds from one run to the next;
#   2. what one prompt costs with the follow check active: the mean of 200
#      calls, each after a new command in the history;
#   3. that init and prompt also work with PATH emptied, which they cannot do
#      if they start a program.
# On Windows it then hands over to scripts/bench.sh for bash when Git Bash is
# installed.
#
# Budgets on the maintainer's machine: 80 ms (Serena profile session), 65 ms
# (hook session) and 2 ms (prompt). When the variable CI is set, or with -Ci:
# 150 ms, 150 ms and 10 ms, except that a Serena profile session under
# PowerShell 7 on Windows has 210 ms (R42 as amended after the first runs in
# CI: on the hosted Windows runner PowerShell 7 takes 1.3 to 1.4 times as
# long as Windows PowerShell 5.1 for the same file). The exit code is 1 when a
# budget is not met or a measurement could not be made.
#
# The local budgets are judged on an idle machine: with other work running, a
# median over 80 ms says nothing about init.ps1. Run this script again alone
# before reading a failure as a regression.
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
$initBudget = [ordered]@{ 'Serena profile session' = 80.0; 'hook session' = 65.0 }; $promptBudget = 2.0
if ($loose) {
    $initBudget = [ordered]@{ 'Serena profile session' = 150.0; 'hook session' = 150.0 }; $promptBudget = 10.0
    if ($onWindows -and ($PSVersionTable.PSVersion.Major -ge 6)) { $initBudget['Serena profile session'] = 210.0 }
}
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
    # Where the children work: a folder inside a repository, so that the
    # prompt reads a branch as it does for most of a working day.
    $work = Join-Path $userHome 'work'
    foreach ($dir in $userHome, $root, (Join-Path $root 'shell'), $emptyPath, (Join-Path $userHome 'AppData'), (Join-Path $userHome 'AppData\Local'), (Join-Path $userHome 'AppData\Roaming'),
        (Join-Path $userHome '.local'), (Join-Path $userHome '.local\share'), (Join-Path $userHome '.cache'), (Join-Path $userHome '.config'), $work, (Join-Path $work '.git')) {
        $null = New-Item -ItemType Directory -Path $dir -Force
    }
    [IO.File]::WriteAllText((Join-Path (Join-Path $work '.git') 'HEAD'), "ref: refs/heads/main`n")

    # The environment of every child: a home inside the temporary folder.
    $base = @{
        HOME = $userHome; USERPROFILE = $userHome
        LOCALAPPDATA = (Join-Path $userHome 'AppData\Local'); APPDATA = (Join-Path $userHome 'AppData\Roaming')
        XDG_DATA_HOME = (Join-Path $userHome '.local\share'); HISTFILE = (Join-Path $userHome '.bash_history')
        XDG_CACHE_HOME = (Join-Path $userHome '.cache'); XDG_CONFIG_HOME = (Join-Path $userHome '.config')
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
        $info.WorkingDirectory = $work
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
    # The dot-sourcing, timed from inside, and after it the first prompt of
    # the session, whose output to the console is kept away from the answer.
    $timedInit = $prelude + "`n" + @'
$w = [Diagnostics.Stopwatch]::StartNew()
. <<INIT>>
$initMs = $w.Elapsed.TotalMilliseconds
$sink = New-Object System.IO.StringWriter
$console = [Console]::Out
[Console]::SetOut($sink)
$w.Restart()
$null = prompt
$firstMs = $w.Elapsed.TotalMilliseconds
[Console]::SetOut($console)
$c = [Globalization.CultureInfo]::InvariantCulture
'INIT=' + $initMs.ToString('0.000', $c) + ' FIRST=' + $firstMs.ToString('0.000', $c)
'@.Replace('<<INIT>>', $quoted) + "`n"

    $sessions = [ordered]@{
        'Serena profile session' = @{ WT_SESSION = 'bench'; SERENO_SESSION = 'wt-profile' }
        'hook session' = @{ WT_SESSION = 'bench' }
    }

    Write-Output ("sereno bench: {0} {1}, {2} processes per series; budgets: init.ps1 {3} ms in a Serena profile session and {4} ms in a hook session, one prompt {5} ms" -f (Split-Path -Leaf $exe), $PSVersionTable.PSVersion, $Runs,
        (Format-Ms $initBudget['Serena profile session']), (Format-Ms $initBudget['hook session']), (Format-Ms $promptBudget))

    # PowerShell keeps a start-up profile below the home folder: the methods a
    # process compiled while it started, which the next process compiles
    # ahead of their use. A session that follows one of its own kind is
    # therefore faster than the first of its kind. Removing the file makes a
    # child the first of its kind again.
    function Remove-StartupProfile {
        foreach ($file in @(Get-ChildItem -LiteralPath $userHome -Recurse -Force -File -Filter 'StartupProfileData-*')) { Remove-Item -LiteralPath $file.FullName -Force }
    }
    $invariant = [Globalization.CultureInfo]::InvariantCulture
    function Read-Times([string]$text) {
        if ($text -match '\AINIT=([0-9.]+) FIRST=([0-9.]+)\s*\z') { return @([double]::Parse($Matches[1], $invariant), [double]::Parse($Matches[2], $invariant)) }
        throw "init.ps1 printed something: $text"
    }

    # 1. init.ps1. Four processes take turns. The budget (R42) is on the one
    # that times the dot-sourcing from inside with no start-up profile to
    # help it: the first session of its kind, which is the slowest and does
    # not depend on what ran before. For information only: the same process
    # once more, right after it, and whole processes without init.ps1 and
    # with it.
    $null = Invoke-Child $withInit $sessions['Serena profile session']   # warm the file cache
    foreach ($name in $sessions.Keys) {
        $with = @(); $without = @(); $inside = @(); $first = @(); $later = @()
        for ($i = 0; $i -lt $Runs; $i++) {
            $without += (Invoke-Child $prelude $sessions[$name]).Ms
            $child = Invoke-Child $withInit $sessions[$name]
            $with += $child.Ms
            if ($child.Out -ne '') { throw "init.ps1 printed something: $($child.Out)" }
            Remove-StartupProfile
            $times = Read-Times (Invoke-Child $timedInit $sessions[$name]).Out
            $inside += $times[0]; $first += $times[1]
            $later += (Read-Times (Invoke-Child $timedInit $sessions[$name]).Out)[0]
        }
        $cost = Get-Median $inside
        $budget = $initBudget[$name]
        $line = "init.ps1, {0}: {1} ms (median of {2} processes, timed around the dot-sourcing inside each; {3} to {4})" -f $name, (Format-Ms $cost), $Runs,
            (Format-Ms (($inside | Measure-Object -Minimum).Minimum)), (Format-Ms (($inside | Measure-Object -Maximum).Maximum))
        if ($cost -le $budget) { $line += "  ok, budget $(Format-Ms $budget)" } else { $line += "  OVER the budget of $(Format-Ms $budget)"; $failed = $true }
        Write-Output $line
        Write-Output ("    not judged: the first prompt of such a session, in a repository, once: {0} ms" -f (Format-Ms (Get-Median $first)))
        Write-Output ("    not judged: init.ps1 in a session that follows one of its kind, where PowerShell replays its start-up profile: {0} ms" -f (Format-Ms (Get-Median $later)))
        Write-Output ("    not judged: a whole process, {0} ms with init.ps1 and {1} ms without (medians); the difference, {2} ms, moves by more than init.ps1 costs" -f (Format-Ms (Get-Median $with)), (Format-Ms (Get-Median $without)), (Format-Ms ((Get-Median $with) - (Get-Median $without))))
    }

    # 2. the prompt, with the follow check active
    $promptScript = $prelude + "`n. $quoted`n" + @'
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
    $line = "prompt, follow check active: {0} ms (mean of {1} calls, each after a command; {2} ms when nothing ran)" -f (Format-Ms $mean), $Calls, (Format-Ms (& $number $child.Out 'IDLE'))
    if ((& $number $child.Out 'PAINTED') -le 0) { throw 'the follow check did not paint: the measurement would not be of the active check' }
    if ($child.Out -notmatch 'BRANCH=True') { throw 'the prompt did not show the branch' }
    if ($mean -le $promptBudget) { $line += "  ok, budget $(Format-Ms $promptBudget)" } else { $line += "  OVER the budget of $(Format-Ms $promptBudget)"; $failed = $true }
    Write-Output $line

    # 3. the same with PATH emptied. PowerShell 6 and later put their own
    # folder in front of PATH when they start; the child takes it off again
    # before anything else, and says at its end what PATH was by then.
    $extra = @{ WT_SESSION = 'bench'; SERENO_SESSION = 'wt-profile'; PATH = $emptyPath; SERENO_BENCH_PATH = $emptyPath }
    $child = Invoke-Child ("`$env:PATH = `$env:SERENO_BENCH_PATH`n" + $promptScript + "`n'PATH=' + `$env:PATH`n") $extra
    $seen = @($child.Out -split "`r?`n" | Where-Object { $_.StartsWith('PATH=') })
    if (($seen.Count -ne 1) -or ($seen[0] -cne ('PATH=' + $emptyPath))) { throw "PATH was not the empty folder to the end: $($seen -join ' | ')" }
    Write-Output ("with PATH emptied: init and {0} prompts ran, mean {1} ms  ok" -f (2 * $Calls + 1), (Format-Ms (& $number $child.Out 'MEAN')))

    # bash, through scripts/bench.sh, while the rendered files still exist.
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
            # Git Bash takes a path with forward slashes. The rendered
            # init.bash next to the init.ps1 measured above goes along, so
            # that bash measures the same rendering.
            $arguments = @((Join-Path $PSScriptRoot 'bench.sh').Replace('\', '/'))
            $initFolder = Split-Path -Parent $Init
            if (Test-Path -LiteralPath (Join-Path $initFolder 'init.bash')) { $arguments += @('--init', $initFolder.Replace('\', '/')) }
            if ($loose) { $arguments += '--ci' }
            & $bash --noprofile --norc @arguments
            if ($LASTEXITCODE -ne 0) { $failed = $true; Write-Output "bash: scripts/bench.sh ended with exit code $LASTEXITCODE" }
        } else {
            Write-Output 'bash: skipped, Git Bash was not found next to git.exe'
        }
    }
} finally {
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}

if ($failed) {
    Write-Output 'sereno bench: a budget was not met, or a measurement could not be made'
    if (-not $loose) { Write-Output 'sereno bench: the local budgets are for an idle machine; run this again with nothing else running before reading it as a regression' }
    exit 1
}
Write-Output 'sereno bench: all budgets met'
exit 0
