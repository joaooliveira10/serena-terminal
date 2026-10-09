# Smoke run of a built sereno on Windows (scripts/smoke.sh is the one for macOS
# and Linux). scripts\test.ps1 builds the program and calls this file; it can
# also be pointed at any other build, for example a release archive:
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\smoke.ps1 -Sereno PATH [-Rendered DIR] [-Version TEXT]
#
# The program never sees the real home. Before its first start this script
# creates a temporary folder and points HOME, USERPROFILE, LOCALAPPDATA,
# APPDATA, XDG_DATA_HOME and the two SERENO_*_PROFILE_PATH variables into it,
# and sets SERENO_SANDBOX to it, so that sereno itself refuses any write
# outside. The folder is removed at the end.
#
# SERENO_HOME is taken out of the environment for the first pass, on purpose:
# sereno's folder is then the default one, %LOCALAPPDATA%\sereno, which is
# what a machine without that variable gets, and it lies in the temporary home
# because LOCALAPPDATA does. Only with the default folder does the program
# write the fragment and the hook lines in the forms of R18 and R21 that name
# the folder through a variable. The third pass sets SERENO_HOME, to a folder
# in the temporary home.
#
# Three passes, each compared with a SHA-256 listing of the whole temporary
# home taken before it:
#   1. a home with a Windows Terminal folder and sereno's folder in its
#      default place: apply --dry-run, apply, apply again, status --json,
#      theme light, check, export --to, apply --hook, uninstall;
#   2. six `apply --hook` started one right after the other, six times over:
#      at least one runs to its end, the others find nothing left to do or
#      make way before their first write, and afterwards nothing counts as
#      edited or missing, an apply has nothing to do and uninstall gives the
#      home back (R13, R39). How many made way is printed, not judged: it
#      depends on how quickly the machine starts a process (the tests of
#      test/e2e start the commands at the same instant);
#   3. sereno's folder under a name with a quote, a space and a dollar sign,
#      with the program in its bin folder: apply --hook, then a PowerShell and
#      a Git Bash session that load the hooked profile and call sereno through
#      the function of the init file, then uninstall.
#
# -Rendered names a folder made by `go run ./internal/tools/golden --out DIR`:
# the init files apply writes are then compared with it byte for byte, which
# is what lets the shell tests run on the rendered files and still speak for
# the installed ones. -Version is the text `sereno version` must print after
# the program name. The exit code is 1 when any check failed.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Sereno,
    [string]$Rendered = '',
    [string]$Version = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent $PSScriptRoot
$script:failed = $false
function Confirm([bool]$condition, [string]$name) {
    if ($condition) { Write-Host "ok - $name" } else { Write-Host "not ok - $name"; $script:failed = $true }
}

$program = (Resolve-Path -LiteralPath $Sereno).Path
$tmp = Join-Path ([IO.Path]::GetTempPath()) ('sereno-smoke-' + [guid]::NewGuid().ToString('N'))
$userHome = Join-Path $tmp 'home'
$local = Join-Path $userHome 'AppData\Local'
foreach ($dir in $userHome, $local, (Join-Path $userHome 'AppData\Roaming'), (Join-Path $userHome '.local\share'), (Join-Path $userHome 'Documents'), (Join-Path $tmp 'bin'), (Join-Path $tmp 'child\Local'), (Join-Path $tmp 'child\Roaming')) {
    $null = New-Item -ItemType Directory -Path $dir -Force
}
# The program is run from a copy, so that nothing here depends on where the
# original is.
$binary = Join-Path $tmp 'bin\sereno.exe'
Copy-Item -LiteralPath $program -Destination $binary

# A listing of every folder and file below a folder, with SHA-256 of the
# files. It is read until two readings agree: a scanner may keep a copy next
# to a file for a few milliseconds.
function Get-Tree([string]$path) {
    $previous = $null
    for ($i = 0; $i -lt 40; $i++) {
        $lines = foreach ($item in @(Get-ChildItem -LiteralPath $path -Recurse -Force | Sort-Object FullName)) {
            $name = $item.FullName.Substring($path.Length)
            if ($item.PSIsContainer) { "$name <folder>" }
            else {
                $hash = 'unreadable'
                try { $hash = (Get-FileHash -LiteralPath $item.FullName -Algorithm SHA256).Hash } catch { }
                "$name $hash"
            }
        }
        $current = @($lines) -join "`n"
        if ($current -ceq $previous) { return $current }
        $previous = $current
        Start-Sleep -Milliseconds 50
    }
    $previous
}

# Confirms that the home is what a listing taken earlier says, and shows the
# entries that differ when it is not.
function Confirm-Tree([string]$expected, [string]$name) {
    $current = Get-Tree $userHome
    Confirm ($current -ceq $expected) $name
    if ($current -cne $expected) {
        $difference = @(Compare-Object -ReferenceObject @($expected -split "`n") -DifferenceObject @($current -split "`n") -CaseSensitive)
        foreach ($entry in $difference) { Write-Host ('#   ' + $entry.SideIndicator + ' ' + $entry.InputObject) }
    }
}

# Runs the program and returns the lines of its standard output, always as an
# array; the exit code is in $script:code. Standard error goes to the console.
function Invoke-Sereno([string[]]$arguments) {
    $lines = @(& $binary @arguments)
    $script:code = $LASTEXITCODE
    , $lines
}

# Starts the program several times, one right after the other and without
# waiting, each as a process of its own without a window, in the environment
# of this script. Returns, for each, the exit code and what it wrote on
# standard error. Whether two of them meet depends on the machine: where a
# process takes longer to start than sereno takes for its work, they do not.
function Start-Together([string[]]$arguments, [int]$count) {
    $started = foreach ($number in 1..$count) {
        $start = New-Object System.Diagnostics.ProcessStartInfo
        $start.FileName = $binary
        $start.Arguments = $arguments -join ' '
        $start.UseShellExecute = $false
        $start.CreateNoWindow = $true
        $start.RedirectStandardOutput = $true
        $start.RedirectStandardError = $true
        $process = [System.Diagnostics.Process]::Start($start)
        @{ Process = $process; Out = $process.StandardOutput.ReadToEndAsync(); Err = $process.StandardError.ReadToEndAsync() }
    }
    foreach ($one in $started) {
        $one.Process.WaitForExit()
        $null = $one.Out.Result
        @{ Code = $one.Process.ExitCode; Err = $one.Err.Result }
    }
}

$sandbox = @{
    HOME = $userHome; USERPROFILE = $userHome; HOMEDRIVE = $userHome.Substring(0, 2); HOMEPATH = $userHome.Substring(2)
    LOCALAPPDATA = $local; APPDATA = (Join-Path $userHome 'AppData\Roaming'); XDG_DATA_HOME = (Join-Path $userHome '.local\share')
    SERENO_HOME = $null; SERENO_SANDBOX = $tmp
    SERENO_PS_PROFILE_PATH = (Join-Path $userHome 'Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1')
    SERENO_PWSH_PROFILE_PATH = (Join-Path $userHome 'Documents\PowerShell\Microsoft.PowerShell_profile.ps1')
    SERENO_OS_MODE = 'dark'; SERENO_SESSION = $null; SERENO_REG_ROOT = $null; SERENO_LANG = $null; ZDOTDIR = $null
    WT_SESSION = $null; WT_PROFILE_ID = $null; TERM_PROGRAM = $null; TMUX = $null; NO_COLOR = $null; HISTFILE = $null
    POWERSHELL_TELEMETRY_OPTOUT = '1'; POWERSHELL_UPDATECHECK = 'Off'
}
$saved = @{}
try {
    foreach ($key in $sandbox.Keys) { $saved[$key] = [Environment]::GetEnvironmentVariable($key); [Environment]::SetEnvironmentVariable($key, $sandbox[$key]) }

    $windowsPowerShell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    # Git Bash is the bash of the Git for Windows that git.exe belongs to; the
    # bash.exe of System32 is the launcher of WSL and must not be taken for it.
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
    $hooks = 'powershell'
    if ($bash) { $hooks = 'powershell,bash' }
    $profilePath = $env:SERENO_PS_PROFILE_PATH

    # -----------------------------------------------------------------------
    # 1. A home with Windows Terminal, sereno's folder in its default place:
    #    SERENO_HOME is not set.
    Write-Host '# pass 1: the default folder: apply, status, theme, check, export, hooks, uninstall'
    $root = Join-Path $local 'sereno'
    $fragment = Join-Path $local 'Microsoft\Windows Terminal\Fragments\SerenaTerminal\sereno.json'
    $null = New-Item -ItemType Directory -Path (Join-Path $local 'Microsoft\Windows Terminal') -Force
    $before = Get-Tree $userHome

    $out = Invoke-Sereno @('version')
    $wanted = '^sereno \S+ \(windows/(amd64|arm64)\)$'
    if ($Version) { $wanted = '^sereno ' + [regex]::Escape($Version) + ' \(windows/(amd64|arm64)\)$' }
    Confirm (($script:code -eq 0) -and ($out.Count -eq 1) -and ($out[0] -cmatch $wanted)) "sereno version prints one line with the version ($($out -join ' '))"

    $out = Invoke-Sereno @('apply', '--dry-run', '--lang', 'en')
    Confirm (($script:code -eq 0) -and (($out | Select-Object -First 1) -clike 'sereno * plan') -and (($out | Select-Object -Last 1) -ceq 'Nothing was changed.')) 'apply --dry-run prints the plan and ends with "Nothing was changed."'
    Confirm-Tree $before 'apply --dry-run changed nothing'

    $out = Invoke-Sereno @('apply', '--lang', 'en')
    $written = @((Join-Path $root 'manifest.json'), (Join-Path $root 'mode'), (Join-Path $root 'shell\init.ps1'), (Join-Path $root 'shell\init.bash'), (Join-Path $root 'shell\init.zsh'), $fragment)
    Confirm (($script:code -eq 0) -and (@($written | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) }).Count -eq 0)) 'apply wrote the manifest, the mode file, three init files and the fragment'
    Confirm (@($out | Where-Object { $_ -clike '  create  *' }).Count -eq $written.Count) 'the plan has one create line for each of them'
    if ($Rendered) {
        $same = $true
        foreach ($name in 'init.ps1', 'init.bash', 'init.zsh') {
            $installed = (Get-FileHash -LiteralPath (Join-Path $root "shell\$name") -Algorithm SHA256).Hash
            if ($installed -ne (Get-FileHash -LiteralPath (Join-Path $Rendered "init\$name") -Algorithm SHA256).Hash) { $same = $false }
        }
        Confirm $same 'the installed init files are the rendered ones, byte for byte'
    }
    # The fragment of a default folder names init.ps1 through %LOCALAPPDATA%
    # (R18), so one fact of the machine is all it depends on: `--shell auto`
    # takes PowerShell 7 where pwsh.exe is on PATH and Windows PowerShell 5.1
    # where it is not. The program had the PATH of this script, so the same
    # question is asked here, and the fragment is one of two golden files.
    $golden = Join-Path $repo 'testdata\golden'
    if ($Rendered) { $golden = $Rendered }
    $wantedFragment = 'fragment\auto-soft.json'
    if (Get-Command pwsh.exe -CommandType Application -ErrorAction SilentlyContinue) { $wantedFragment = 'fragment\pwsh.json' }
    Confirm ((Get-FileHash -LiteralPath $fragment -Algorithm SHA256).Hash -eq (Get-FileHash -LiteralPath (Join-Path $golden $wantedFragment) -Algorithm SHA256).Hash) "the fragment of the default folder is $wantedFragment, byte for byte"
    $applied = Get-Tree $userHome

    $out = Invoke-Sereno @('apply', '--lang', 'en')
    Confirm (($script:code -eq 0) -and (@($out | Where-Object { $_ -cmatch '^  (mkdir|create|replace|append|remove|rmdir) ' }).Count -eq 0) -and ((Get-Tree $userHome) -ceq $applied)) 'a second apply does nothing'

    $status = (Invoke-Sereno @('status', '--json')) -join "`n" | ConvertFrom-Json
    Confirm (($script:code -eq 0) -and $status.applied -and ($status.root -like '*\AppData\Local\sereno') -and ($status.mode.mode -eq 'auto') -and ($status.variant -eq 'dark') -and
        ($status.owned.total -eq 5) -and ($status.owned.modified -eq 0) -and ($status.owned.missing -eq 0) -and
        $status.windowsTerminal.detected -and $status.windowsTerminal.fragment -and $status.windowsTerminal.profile -and (@($status.hooks).Count -eq 0)) 'status --json parses and reports the installation'

    $null = Invoke-Sereno @('theme', 'light', '--lang', 'en')
    $themeCode = $script:code
    $status = (Invoke-Sereno @('status', '--json')) -join "`n" | ConvertFrom-Json
    Confirm (($themeCode -eq 0) -and (@(Get-Content -LiteralPath (Join-Path $root 'mode'))[0] -ceq 'mode=light') -and ($status.variant -eq 'light') -and ($status.owned.modified -eq 0) -and
        ((Get-Content -LiteralPath $fragment -Raw) -clike '*"colorScheme": "Serena Light"*')) 'theme light changes the mode file and the colour scheme of the profile'

    $out = Invoke-Sereno @('check', '--lang', 'en')
    Confirm (($script:code -eq 0) -and ($out.Count -eq 7) -and (($out | Select-Object -Last 1) -ceq 'Every value is at or above its floor.')) 'check passes for the six variants'
    $out = Invoke-Sereno @('check', '--markdown')
    $committed = [IO.File]::ReadAllText((Join-Path $repo 'docs\contrast.md'))
    Confirm (($script:code -eq 0) -and ((($out -join "`n") + "`n") -ceq $committed)) 'check --markdown prints docs\contrast.md'

    $themes = Join-Path $tmp 'themes'
    $out = Invoke-Sereno @('export', '--to', $themes, '--lang', 'en')
    Confirm (($script:code -eq 0) -and (@(Get-ChildItem -LiteralPath $themes -Recurse -File).Count -eq 24)) 'export --to wrote 24 theme files'

    # The hook lines of the default folder are the lines of R21, letter for
    # letter: they name the folder through a variable of the session. Both
    # files are new, so the line is all they hold.
    $out = Invoke-Sereno @('apply', '--hook', $hooks, '--lang', 'en')
    $line = 'if ($ExecutionContext.SessionState.LanguageMode -eq ''FullLanguage'' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1")) { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook'
    Confirm (($script:code -eq 0) -and (Test-Path -LiteralPath $profilePath) -and ([IO.File]::ReadAllText($profilePath) -ceq ($line + "`r`n"))) 'apply --hook wrote the PowerShell hook line of the default folder, as R21 has it'
    if ($bash) {
        $line = '[ -r "$LOCALAPPDATA/sereno/shell/init.bash" ] && . "$LOCALAPPDATA/sereno/shell/init.bash" # sereno:hook'
        $bashrc = Join-Path $userHome '.bashrc'
        Confirm ((Test-Path -LiteralPath $bashrc) -and ([IO.File]::ReadAllText($bashrc) -ceq ($line + "`n"))) 'apply --hook wrote the Git Bash hook line of the default folder, as R21 has it'
    } else {
        Write-Host 'skip - Git Bash was not found next to git.exe: its hook line was not tried'
    }

    # In Portuguese only the exit codes are looked at: Windows PowerShell
    # reads the UTF-8 the program writes in the code page of the console.
    $null = Invoke-Sereno @('status', '--lang', 'pt-BR'); $statusCode = $script:code
    $null = Invoke-Sereno @('help', '--lang', 'pt-BR')
    Confirm (($statusCode -eq 0) -and ($script:code -eq 0)) 'status and help run in pt-BR'

    $out = Invoke-Sereno @('uninstall', '--lang', 'en')
    Confirm (($script:code -eq 0) -and (-not (Test-Path -LiteralPath $root)) -and (-not (Test-Path -LiteralPath $fragment))) 'uninstall removed sereno''s folder and the fragment'
    Confirm-Tree $before 'after uninstall the home is what it was, file for file'

    # -----------------------------------------------------------------------
    # 2. Commands at the same time. One sereno writes the folder; another one
    #    that comes at that moment makes way, with exit code 1 and before its
    #    first write, and says so.
    Write-Host '# pass 2: six applies one right after the other, six times'
    $whole = $true
    $waited = 0
    for ($round = 1; $round -le 6; $round++) {
        $problems = @()
        $results = @(Start-Together @('apply', '--hook', $hooks, '--lang', 'en') 6)
        $done = @($results | Where-Object { $_.Code -eq 0 }).Count
        $madeWay = @($results | Where-Object { ($_.Code -eq 1) -and (($_.Err -clike '*another sereno is working on this folder*') -or ($_.Err -clike '*: changed while sereno was running*')) }).Count
        $waited += $madeWay
        if (($done -lt 1) -or (($done + $madeWay) -ne $results.Count)) {
            $problems += 'exit codes ' + (($results | ForEach-Object { $_.Code }) -join ' ') + '; ' + (($results | ForEach-Object { $_.Err.Trim() } | Where-Object { $_ }) -join ' | ')
        }
        $status = (Invoke-Sereno @('status', '--json')) -join "`n" | ConvertFrom-Json
        if (($status.owned.modified -ne 0) -or ($status.owned.missing -ne 0)) { $problems += "status counts $($status.owned.modified) files as edited and $($status.owned.missing) as missing" }
        $out = Invoke-Sereno @('apply', '--hook', $hooks, '--lang', 'en')
        if (($script:code -ne 0) -or (@($out | Where-Object { $_ -cmatch '^  (mkdir|create|replace|append|remove|rmdir) ' }).Count -ne 0)) { $problems += "the apply that follows ends with exit code $($script:code) or still has something to do" }
        $null = Invoke-Sereno @('uninstall', '--lang', 'en')
        if ($script:code -ne 0) { $problems += "uninstall ends with exit code $($script:code)" }
        if ((Get-Tree $userHome) -cne $before) { $problems += 'after uninstall the home is not what it was' }
        foreach ($problem in $problems) { Write-Host "#   round ${round}: $problem"; $whole = $false }
    }
    Confirm $whole 'each time one apply ran to its end, the others had nothing left to do or made way, and uninstall gave the home back'
    Write-Host "# $waited of 36 applies made way for another one"

    # -----------------------------------------------------------------------
    # 3. A folder with an awkward name, hooks, and sessions that load them.
    Write-Host '# pass 3: hooks from a folder with a quote, a space and a dollar sign'
    $root = Join-Path $userHome "o'neil `$HOME folder\sereno"
    [Environment]::SetEnvironmentVariable('SERENO_HOME', $root)
    # The place the install script will use (R41). apply and uninstall leave
    # it alone (R10), and the init files offer the program from there (R34).
    $null = New-Item -ItemType Directory -Path (Join-Path $root 'bin') -Force
    Copy-Item -LiteralPath $binary -Destination (Join-Path $root 'bin\sereno.exe')
    $before = Get-Tree $userHome

    $out = Invoke-Sereno @('apply', '--hook', $hooks, '--lang', 'en')
    Confirm (($script:code -eq 0) -and (Test-Path -LiteralPath $profilePath) -and ((Get-Content -LiteralPath $profilePath -Raw) -clike '*# sereno:hook*')) 'apply --hook created the PowerShell profile with the hook line'
    $hooked = Get-Tree $userHome

    # The sessions keep their own caches out of the home that is compared:
    # Windows PowerShell stores a start-up profile of itself below the
    # AppData\Local of USERPROFILE. HOME stays, bash finds ~/.bashrc by it.
    $child = @{
        USERPROFILE = (Join-Path $tmp 'child'); LOCALAPPDATA = (Join-Path $tmp 'child\Local'); APPDATA = (Join-Path $tmp 'child\Roaming')
        SERENO_SMOKE_PROFILE = $profilePath
    }
    $outer = @{}
    foreach ($key in $child.Keys) { $outer[$key] = [Environment]::GetEnvironmentVariable($key); [Environment]::SetEnvironmentVariable($key, $child[$key]) }
    try {
        $answer = @(& $windowsPowerShell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command '. $env:SERENO_SMOKE_PROFILE; if ($null -ne $global:__sereno) { sereno version }')
        Confirm (($answer.Count -eq 1) -and ($answer[0] -cmatch '^sereno \S+ \(windows/')) 'a PowerShell session that loads the hooked profile has sereno, and the sereno function reaches the program'
        if ($bash) {
            # No double quote in the command: Windows PowerShell does not
            # escape one when it hands an argument to a program.
            if ([Console]::IsErrorRedirected) { Write-Host '# an interactive bash without a terminal says so twice on standard error; that is expected' }
            $answer = @(& $bash --noprofile --norc -i -c 'unset HISTFILE; . ~/.bashrc; printf ''%s\n'' loaded=${__sereno_loaded-0}; sereno version')
            Confirm (($answer.Count -eq 2) -and ($answer[0] -ceq 'loaded=1') -and ($answer[1] -cmatch '^sereno \S+ \(windows/')) 'a Git Bash session that loads the hooked .bashrc has sereno, and the sereno function reaches the program'
        } else {
            Write-Host 'skip - Git Bash was not found next to git.exe: no bash session was started'
        }
    } finally {
        foreach ($key in $outer.Keys) { [Environment]::SetEnvironmentVariable($key, $outer[$key]) }
    }
    Confirm-Tree $hooked 'the sessions wrote nothing into the home'

    $out = Invoke-Sereno @('uninstall', '--lang', 'en')
    Confirm (($script:code -eq 0) -and (Test-Path -LiteralPath (Join-Path $root 'bin\sereno.exe')) -and (-not (Test-Path -LiteralPath (Join-Path $root 'manifest.json')))) 'uninstall left the bin folder alone and removed the rest'
    Confirm-Tree $before 'after uninstall the home is what it was, file for file'
} finally {
    foreach ($key in $saved.Keys) { [Environment]::SetEnvironmentVariable($key, $saved[$key]) }
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}

if ($script:failed) { Write-Host 'smoke.ps1: FAILED'; exit 1 }
Write-Host 'smoke.ps1: all checks passed'
exit 0
