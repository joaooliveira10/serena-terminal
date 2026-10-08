# TEMPORARY: diagnostics for the first CI runs of build-0.1. Not part of the
# product or of its tests; removed again before the branch is done.
#
#   pwsh -NoProfile -File scripts/diag.ps1
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\diag.ps1
#
# It says where the time of init.ps1 goes under the PowerShell that runs it:
# a copy of the file gets a time stamp between its sections, fresh processes
# dot-source it the way scripts/bench.ps1 does, and the medians are printed.
# Everything runs in a temporary folder that is the home of the children.
[CmdletBinding()]
param(
    [string]$Init = '',
    [int]$Runs = 7
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent $PSScriptRoot
if (-not $Init) { $Init = Join-Path $repo 'testdata/golden/init/init.ps1' }
$exe = (Get-Process -Id $PID).Path
$onWindows = ($env:OS -eq 'Windows_NT')
$invariant = [Globalization.CultureInfo]::InvariantCulture

function Get-Median([double[]]$values) {
    $sorted = @($values | Sort-Object)
    $n = $sorted.Count
    if ($n % 2) { return $sorted[($n - 1) / 2] }
    return ($sorted[$n / 2 - 1] + $sorted[$n / 2]) / 2
}
function Format-Ms([double]$value) { $value.ToString('0.0', $invariant) }

Write-Output ("diag: {0} {1}, {2} processors, {3}" -f (Split-Path -Leaf $exe), $PSVersionTable.PSVersion, [Environment]::ProcessorCount, [Environment]::OSVersion.VersionString)
if (Test-Path -LiteralPath '/proc/cpuinfo') {
    $model = @([IO.File]::ReadAllLines('/proc/cpuinfo') | Where-Object { $_ -like 'model name*' } | Select-Object -First 1)
    if ($model.Count) { Write-Output ("diag: " + $model[0]) }
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('sereno-diag-' + [guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $tmp
try {
    $userHome = Join-Path $tmp 'home'
    $root = Join-Path $userHome 'sereno'
    $work = Join-Path $userHome 'work'
    foreach ($dir in $userHome, $root, (Join-Path $root 'shell'), (Join-Path $userHome 'AppData'), (Join-Path $userHome 'AppData\Local'), (Join-Path $userHome 'AppData\Roaming'),
        (Join-Path $userHome '.local'), (Join-Path $userHome '.local\share'), (Join-Path $userHome '.cache'), (Join-Path $userHome '.config'), $work, (Join-Path $work '.git')) {
        $null = New-Item -ItemType Directory -Path $dir -Force
    }
    [IO.File]::WriteAllText((Join-Path (Join-Path $work '.git') 'HEAD'), "ref: refs/heads/main`n")
    [IO.File]::WriteAllText((Join-Path $root 'mode'), "mode=auto`nflavor=soft`ninband=profile`nkeys=profile`nprompt=on`n")
    $base = @{
        HOME = $userHome; USERPROFILE = $userHome
        LOCALAPPDATA = (Join-Path $userHome 'AppData\Local'); APPDATA = (Join-Path $userHome 'AppData\Roaming')
        XDG_DATA_HOME = (Join-Path $userHome '.local\share'); HISTFILE = (Join-Path $userHome '.bash_history')
        XDG_CACHE_HOME = (Join-Path $userHome '.cache'); XDG_CONFIG_HOME = (Join-Path $userHome '.config')
        SERENO_HOME = $root; SERENO_SANDBOX = $tmp
        SERENO_OS_MODE = ''; SERENO_SESSION = ''; SERENO_REG_ROOT = ''; SERENO_LANG = ''
        WT_SESSION = ''; WT_PROFILE_ID = ''; TERM_PROGRAM = ''
        POWERSHELL_TELEMETRY_OPTOUT = '1'; POWERSHELL_UPDATECHECK = 'Off'
    }
    if ($onWindows) { $base['HOMEDRIVE'] = $userHome.Substring(0, 2); $base['HOMEPATH'] = $userHome.Substring(2) }

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
        $process = [Diagnostics.Process]::Start($info)
        $errTask = $process.StandardError.ReadToEndAsync()
        $out = $process.StandardOutput.ReadToEnd()
        $process.WaitForExit()
        $err = $errTask.Result
        $code = $process.ExitCode
        $process.Dispose()
        Remove-Item -LiteralPath $file -Force
        if ($code -ne 0) { throw "a child ended with code $code`n$out`n$err" }
        $out
    }
    function Remove-StartupProfile {
        foreach ($file in @(Get-ChildItem -LiteralPath $userHome -Recurse -Force -File -Filter 'StartupProfileData-*')) { Remove-Item -LiteralPath $file.FullName -Force }
    }

    # The sections of the file: a name, and the text its first line holds.
    $anchors = [ordered]@{
        'the three guards' = "if (-not (`$Host.Name -ceq 'ConsoleHost')) { return }"
        'the state table' = '$global:__sereno = @{'
        'enter the block' = '# The rest runs in a scope of its own'
        'session facts, mode file' = "    `$s = `$global:__sereno`n    `$e = `$s['E']`n`n    try {"
        'find the cmdlets' = "    `$settings = `$s['Settings']"
        'Get-PSReadLineOption' = '                $options = & $get'
        'colour table' = '                $type = $options.GetType()'
        'comfort decisions' = '                # Comfort set, where the mode file allows it.'
        'Set-PSReadLineOption' = '                if ($arguments.Count) {'
        'three key handlers' = "                if (`$bind) {`n                    try { & `$bind -Chord 'Tab'"
        'progress colours or PSStyle' = '    # Pairs measured as unreadable on the six variants.'
        'prompt' = '    # The prompt. The Serena prompt replaces the stock one only'
        'sereno function, leave' = '    # The sereno command, when the install script placed it'
    }
    $text = [IO.File]::ReadAllText($Init)
    function Add-Stamps([string]$source) {
        $index = 0
        foreach ($name in $anchors.Keys) {
            $at = $source.IndexOf($anchors[$name])
            if ($at -lt 0) { throw "no anchor for: $name" }
            if ($source.IndexOf($anchors[$name], $at + 1) -ge 0) { throw "two anchors for: $name" }
            $lineStart = $source.LastIndexOf("`n", $at) + 1
            $source = $source.Substring(0, $lineStart) + "`$global:__sereno_d[$index] = `$global:__sereno_w.ElapsedTicks`n" + $source.Substring($lineStart)
            $index++
        }
        $source
    }
    # The variant: a style of $PSStyle is handed to the function that rewrites
    # it only when it holds something that looks like a background.
    function Add-Precheck([string]$source) {
        $a = '$fixed = & $fix $member.Value'
        $b = '$fixed = & $fix $old'
        if ($source.IndexOf($a) -lt 0 -or $source.IndexOf($b) -lt 0) { throw 'the lines of the variant were not found' }
        $test = "-cmatch '[\[;](?:4[0-7]|10[0-7])[;m]'"
        $source.Replace($a, "`$fixed = `$member.Value; if (`$fixed $test) { `$fixed = & `$fix `$fixed }").Replace($b, "`$fixed = `$old; if (`$fixed $test) { `$fixed = & `$fix `$fixed }")
    }

    $files = [ordered]@{
        'as it is' = $text
        'stamped' = (Add-Stamps $text)
        'variant' = (Add-Precheck $text)
        'variant stamped' = (Add-Stamps (Add-Precheck $text))
        # What dot-sourcing any file costs in a fresh process.
        'a file of one line' = "`$null = 1`n"
    }
    $paths = @{}
    foreach ($name in $files.Keys) {
        $folder = Join-Path $root ('shell-' + ($name -replace ' ', '-'))
        $null = New-Item -ItemType Directory -Path $folder -Force
        $paths[$name] = Join-Path $folder 'init.ps1'
        [IO.File]::WriteAllText($paths[$name], $files[$name])
    }
    # The mode file is looked for one folder above the file.
    $prelude = "Import-Module PSReadLine`nSet-PSReadLineOption -HistorySaveStyle SaveNothing`n"
    $count = $anchors.Count
    $child = $prelude + @'
$global:__sereno_d = New-Object 'long[]' COUNT
$c = [Globalization.CultureInfo]::InvariantCulture
$global:__sereno_w = [Diagnostics.Stopwatch]::StartNew()
. 'FILE'
$end = $global:__sereno_w.ElapsedTicks
$sink = New-Object System.IO.StringWriter
$console = [Console]::Out
[Console]::SetOut($sink)
$global:__sereno_w.Restart()
$null = prompt
$first = $global:__sereno_w.ElapsedTicks
[Console]::SetOut($console)
$f = 1000.0 / [Diagnostics.Stopwatch]::Frequency
'T=' + ((@($global:__sereno_d) + @($end, $first) | ForEach-Object { ($_ * $f).ToString('0.000', $c) }) -join ',')
'P=' + (Get-Module PSReadLine).Version + ' ' + $(if ((Get-PSReadLineOption).PSObject.Properties['PredictionSource']) { (Get-PSReadLineOption).PredictionSource } else { 'no predictions' })
'@.Replace('COUNT', [string]$count)

    $sessions = [ordered]@{
        'Serena profile session' = @{ WT_SESSION = 'diag'; SERENO_SESSION = 'wt-profile' }
        'hook session' = @{ WT_SESSION = 'diag' }
    }
    foreach ($session in $sessions.Keys) {
        $series = @{}
        foreach ($name in $files.Keys) { $series[$name] = New-Object System.Collections.Generic.List[object] }
        $facts = ''
        $null = Invoke-Child ($child.Replace('FILE', $paths['as it is'].Replace("'", "''"))) $sessions[$session]
        for ($i = 0; $i -lt $Runs; $i++) {
            foreach ($name in $files.Keys) {
                Remove-StartupProfile
                $out = Invoke-Child ($child.Replace('FILE', $paths[$name].Replace("'", "''"))) $sessions[$session]
                if ($out -notmatch '(?m)^T=([0-9.,]+)\s*$') { throw "no times in: $out" }
                $series[$name].Add(@($Matches[1].Split(',') | ForEach-Object { [double]::Parse($_, $invariant) }))
                if ($out -match '(?m)^P=(.+?)\s*$') { $facts = $Matches[1] }
            }
        }
        Write-Output ''
        Write-Output "diag: $session, $Runs processes per file, PSReadLine $facts"
        foreach ($name in $files.Keys) {
            $rows = $series[$name]
            $total = Get-Median @($rows | ForEach-Object { $_[$count] })
            $prompt = Get-Median @($rows | ForEach-Object { $_[$count + 1] })
            $all = @($rows | ForEach-Object { $_[$count] } | Sort-Object)
            Write-Output ("  {0}: init.ps1 {1} ms (median; {2} to {3}), first prompt {4} ms" -f $name, (Format-Ms $total), (Format-Ms $all[0]), (Format-Ms $all[$all.Count - 1]), (Format-Ms $prompt))
            if ($name -like '*stamped') {
                # A stamp is taken where its section starts: the section
                # costs the next stamp minus its own.
                Write-Output ("      {0,8} ms  before the first line runs (read, parse, compile)" -f (Format-Ms (Get-Median @($rows | ForEach-Object { $_[0] }))))
                $index = 0
                foreach ($section in $anchors.Keys) {
                    $from = $index; $to = $index + 1
                    Write-Output ("      {0,8} ms  {1}" -f (Format-Ms (Get-Median @($rows | ForEach-Object { $_[$to] - $_[$from] }))), $section)
                    $index++
                }
            }
        }
    }
} catch {
    # A diagnostic never fails a run.
    Write-Output ("diag: stopped: " + [string]$_)
    Write-Output $_.ScriptStackTrace
} finally {
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}
exit 0
