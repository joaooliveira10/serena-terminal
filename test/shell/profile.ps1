# The last check of test plan T7: the command line of the Serena profile,
# `-NoExit -File init.ps1`, keeps the profile of the user loaded, and init.ps1
# runs after it. That order decides whether a prompt of the user's is seen
# and wrapped in a Serena tab (R26, R30).
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File test\shell\profile.ps1 -RunnerProfile
#   pwsh -NoProfile -File test\shell\profile.ps1 -RunnerProfile
#
# This is the one test of the repository that starts a shell without
# -NoProfile, because a profile that takes part is what it is about. T7 puts
# it on the Windows runner of CI only, and so does this file: without
# -RunnerProfile, outside Windows, or anywhere but on a runner hosted by
# GitHub, it prints a skip line, starts nothing and writes nothing. Asked for
# with -RunnerProfile in such a place, it ends with exit code 3 as well, so
# that the step of the workflow cannot pass without the test having run.
#
# How it works. The session gets a temporary folder as its home, like every
# session of run.ps1. The PowerShell under test is asked, with -NoProfile,
# where its profile is in that environment. Windows PowerShell 5.1 and
# PowerShell 7 look for the Documents folder below USERPROFILE, so the answer
# is a file inside the temporary home (seen for both on the runner of CI, and
# for 5.1 on the maintainer's machine). The test insists on it: a PowerShell
# that named a file anywhere else fails the test before a profile is written
# or a session started, so no profile of a real user is ever written or
# loaded, on a runner either. The profile written there holds a marker
# variable and a prompt of its own that notes what it finds when it is
# called. Then the session is started as the fragment of R18 starts it, with
# its input closed: the host shows one prompt and ends.
#
# It prints one line per check ("ok - ..." or "not ok - ...") and ends with
# exit code 1 when a check failed.
[CmdletBinding()]
param(
    # A rendered init.ps1; default testdata/golden/init/init.ps1.
    [string]$Init = '',
    # The folder with palette/<variant>.bin; default testdata/golden.
    [string]$Golden = '',
    # Asked for by the CI workflow, and by nothing else.
    [switch]$RunnerProfile
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$exe = (Get-Process -Id $PID).Path
$shell = "$(Split-Path -Leaf $exe) $($PSVersionTable.PSVersion)"

if (-not (([Environment]::GetCommandLineArgs() -join ' ') -match '(?i)-nop')) {
    [Console]::Error.WriteLine('profile.ps1: start it with -NoProfile; only the session it tests loads a profile')
    exit 2
}

$hosted = ($env:OS -ceq 'Windows_NT') -and ($env:GITHUB_ACTIONS -ceq 'true') -and ($env:RUNNER_ENVIRONMENT -ceq 'github-hosted')
if (-not ($RunnerProfile -and $hosted)) {
    [Console]::Out.WriteLine('skip - a profile of the user runs before init.ps1: this test lets a profile take part, so it runs on the Windows runner of CI only (T7)')
    [Console]::Out.WriteLine("profile.ps1: 0 passed, 0 failed, 1 skipped ($shell)")
    # Asked for and refused is not a pass: the step of the workflow must fail
    # if it ever lands where this test does not run.
    if ($RunnerProfile) { [Console]::Error.WriteLine('profile.ps1: -RunnerProfile was given, but this is not a Windows runner hosted by GitHub; nothing was started'); exit 3 }
    exit 0
}

if (-not $Init) { $Init = Join-Path $repo 'testdata/golden/init/init.ps1' }
if (-not $Golden) { $Golden = Join-Path $repo 'testdata/golden' }
$Init = (Resolve-Path -LiteralPath $Init).Path
$Golden = (Resolve-Path -LiteralPath $Golden).Path

$script:passed = 0; $script:failed = 0
function Check([bool]$condition, [string]$name, [string]$detail = '') {
    if ($condition) { $script:passed++; [Console]::Out.WriteLine("ok - $name") }
    else { $script:failed++; [Console]::Out.WriteLine("not ok - $name"); if ($detail) { [Console]::Out.WriteLine("#   $detail") } }
}
function Show([string]$text) {
    if ($null -eq $text) { return '<null>' }
    $text.Replace([string][char]27, '<ESC>').Replace([string][char]7, '<BEL>').Replace("`r", '<CR>').Replace("`n", '<LF>')
}

# What the profile of the user holds. Its prompt is called at the first
# prompt of the session and writes down what it finds: whether the variable
# of the profile is still there, whether init.ps1 has run, whether init.ps1
# kept this very prompt, and whether the prompt of the session is by now
# another function, which then is the wrapper of R30 that called this one.
# Had init.ps1 run first, it would have found the stock prompt and put the
# Serena prompt in its place, and this one would then have replaced that:
# nothing kept, nothing wrapped, nothing painted.
$profileText = @'
$global:SerenoTestMarker = 'set by the profile of the user'
function global:prompt {
    $state = $ExecutionContext.SessionState.PSVariable.GetValue('global:__sereno')
    $kept = $false
    if ($null -ne $state) { $kept = [object]::ReferenceEquals($state['Saved'], $global:SerenoTestPrompt) }
    $lines = @(
        'marker=' + $ExecutionContext.SessionState.PSVariable.GetValue('global:SerenoTestMarker')
        'sereno=' + ($null -ne $state)
        'kept=' + $kept
        'wrapped=' + (-not [object]::ReferenceEquals($function:prompt, $global:SerenoTestPrompt))
    )
    [IO.File]::WriteAllLines($env:SERENO_T_REPORT, [string[]]$lines)
    'mine> '
}
$global:SerenoTestPrompt = $function:prompt
'@

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('sereno-profile-' + [guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $tmp -Force
try {
    $userHome = Join-Path $tmp 'home'
    $documents = Join-Path $userHome 'Documents'
    $root = Join-Path $userHome 'local\sereno'
    $work = Join-Path $userHome 'work'
    $emptyPath = Join-Path $tmp 'empty-path'
    foreach ($dir in $userHome, $documents, (Join-Path $userHome 'local'), (Join-Path $userHome 'roaming'), $root, (Join-Path $root 'shell'), $work, $emptyPath) {
        $null = New-Item -ItemType Directory -Path $dir -Force
    }
    $initCopy = Join-Path $root 'shell\init.ps1'
    Copy-Item -LiteralPath $Init -Destination $initCopy
    [IO.File]::WriteAllText((Join-Path $root 'mode'), "mode=auto`nflavor=soft`ninband=profile`nkeys=profile`nprompt=on`n")
    $report = Join-Path $tmp 'report.txt'

    # The environment of the session: the temporary home, PATH an empty
    # folder, and what the Serena profile of R18 sets. The system is light.
    $environment = @{
        HOME = $userHome; USERPROFILE = $userHome; HOMEDRIVE = $userHome.Substring(0, 2); HOMEPATH = $userHome.Substring(2)
        LOCALAPPDATA = (Join-Path $userHome 'local'); APPDATA = (Join-Path $userHome 'roaming')
        SERENO_HOME = $root; SERENO_SANDBOX = $tmp; PATH = $emptyPath
        SERENO_SESSION = 'wt-profile'; POWERSHELL_TELEMETRY_OPTOUT = '1'; POWERSHELL_UPDATECHECK = 'Off'
        WT_SESSION = 'test-session'; WT_PROFILE_ID = ''; TERM_PROGRAM = ''
        SERENO_OS_MODE = 'light'; SERENO_REG_ROOT = ''; SERENO_LANG = ''
        SERENO_T_REPORT = $report
    }

    function Start-Child([string]$arguments) {
        $info = New-Object System.Diagnostics.ProcessStartInfo
        $info.FileName = $exe
        $info.Arguments = $arguments
        $info.UseShellExecute = $false
        $info.CreateNoWindow = $true
        $info.RedirectStandardInput = $true
        $info.RedirectStandardOutput = $true
        $info.RedirectStandardError = $true
        $info.WorkingDirectory = $work
        foreach ($pair in $environment.GetEnumerator()) { $info.EnvironmentVariables[$pair.Key] = [string]$pair.Value }
        # Windows PowerShell hands a child the preamble of the console's input
        # encoding on its redirected input; a session that reads its commands
        # from the input would take a byte order mark for a command (see
        # run.ps1).
        $inputEncoding = $null
        try {
            if ([Console]::InputEncoding.GetPreamble().Length -gt 0) {
                $inputEncoding = [Console]::InputEncoding
                [Console]::InputEncoding = New-Object System.Text.UTF8Encoding $false
            }
        } catch { $inputEncoding = $null }
        try { $process = [Diagnostics.Process]::Start($info) }
        finally { if ($null -ne $inputEncoding) { [Console]::InputEncoding = $inputEncoding } }
        $process.StandardInput.Close()
        $errTask = $process.StandardError.ReadToEndAsync()
        $outTask = $process.StandardOutput.ReadToEndAsync()
        if (-not $process.WaitForExit(120000)) { $process.Kill(); $process.WaitForExit() }
        $result = [pscustomobject]@{ Out = $outTask.Result; Err = $errTask.Result; Code = $process.ExitCode }
        $process.Dispose()
        $result
    }

    [Console]::Out.WriteLine("# session: a profile of the user in front of init.ps1, under $shell")

    # Where this PowerShell has its profile in that environment. Asked with
    # -NoProfile: nothing is loaded yet.
    $arguments = '-NoLogo -NoProfile -NonInteractive -Command "$PROFILE.CurrentUserCurrentHost"'
    $asked = Start-Child $arguments
    $profileFile = $asked.Out.Trim()
    Check (($asked.Code -eq 0) -and ($profileFile -cmatch '\A[A-Za-z]:\\[^\r\n]+\.ps1\z')) 'this PowerShell says where the profile of the user is' ("exit code $($asked.Code): " + (Show $asked.Out) + ' ' + $asked.Err.Trim())

    # That file must lie below the Documents folder of the temporary home:
    # this test writes nothing outside its temporary folder, on a runner
    # either, and starts no session that would load a profile from elsewhere.
    # A file put into that Documents folder tells whether the two are the same
    # folder, whatever name it goes by.
    $inside = $false
    if ($script:failed -eq 0) {
        $sentinel = 'sereno-' + [guid]::NewGuid().ToString('N')
        [IO.File]::WriteAllText((Join-Path $documents $sentinel), '')
        $inside = [IO.File]::Exists((Join-Path (Split-Path -Parent (Split-Path -Parent $profileFile)) $sentinel))
        [IO.File]::Delete((Join-Path $documents $sentinel))
        Check $inside 'that profile is a file inside the temporary home, because the Documents folder follows USERPROFILE: nothing outside that home is written' $profileFile
    }

    if ($inside) {
        $null = [IO.Directory]::CreateDirectory((Split-Path -Parent $profileFile))
        [IO.File]::WriteAllText($profileFile, $profileText)

        # The command line of the Serena profile, as the fragment has it
        # (R18). It is the one start of a shell in this repository that does
        # not say -NoProfile.
        $arguments = '-NoLogo -NoExit -ExecutionPolicy RemoteSigned -File "' + $initCopy + '"'
        $child = Start-Child $arguments

        $found = ''
        if ([IO.File]::Exists($report)) { $found = (@([IO.File]::ReadAllLines($report)) -join '|') }
        Check ($found -ne '') 'started with -NoExit -File init.ps1, the session shows the prompt the profile of the user defined' ('no report was written; the session wrote: ' + (Show $child.Out))
        Check ($found.StartsWith('marker=set by the profile of the user|')) 'the variable that profile defined is there at the first prompt: the profile stays loaded' $found
        Check ($found.Contains('|sereno=True|')) 'init.ps1 has run in that session' $found
        Check ($found.Contains('|kept=True|')) 'init.ps1 found the prompt of the profile and kept it: the profile ran first' $found
        Check ($found.EndsWith('|wrapped=True')) 'the prompt of the session is another function by then: the wrapper of R30, which called the one of the profile' $found
        $palette = [IO.File]::ReadAllText((Join-Path $Golden 'palette/light.bin'))
        Check ($child.Out -ceq ($palette + 'mine> ')) 'the session paints the variant of the system, then shows the prompt of the user unchanged, and writes nothing else' (Show $child.Out.Replace($palette, '<PALETTE of light>'))
        Check ($child.Code -eq 0 -and $child.Err.Trim() -eq '') 'the session ends without an error when its input ends' ("exit code $($child.Code): " + $child.Err.Trim())
    }
} finally {
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}
Check (-not (Test-Path -LiteralPath $tmp)) 'the temporary home is gone again, and the profile with it'

[Console]::Out.WriteLine("profile.ps1: $($script:passed) passed, $($script:failed) failed, 0 skipped ($shell)")
if ($script:failed -gt 0) { exit 1 }
exit 0
