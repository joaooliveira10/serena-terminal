# Local test run for Windows (scripts/test.sh is the one for macOS and Linux).
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts\test.ps1
#
# In order:
#   1. the guard of test plan T2: SHA-256 listings of the real places sereno
#      must never touch during a test (the Windows Terminal folders and
#      settings, the PowerShell profile folders, PSReadLine's history, the
#      bash files of the home folder), and whether %LOCALAPPDATA%\sereno
#      exists;
#   2. go vet and go test for the whole module, go vet for Linux and macOS,
#      and the six builds with CGO_ENABLED=0 (-SkipGo and -SkipCross leave
#      them out);
#   3. a fresh rendering of everything sereno generates, in a temporary folder;
#   4. the behaviour tests of the init files, each under the shell it is for:
#      test\shell\run.ps1 under Windows PowerShell 5.1 and, when installed,
#      under PowerShell 7; test\shell\run.bash under Git Bash; a shell that
#      is not installed is reported as skipped, not as passed;
#   5. with -Bench, the budgets of scripts\bench.ps1;
#   6. the listings of step 1 again: any difference fails the run.
#
# The shells are started with -NoProfile (bash with --noprofile --norc) and
# with a temporary folder as HOME, USERPROFILE, LOCALAPPDATA and APPDATA;
# SERENO_SANDBOX confines every write of sereno's own code to that folder.
# No Windows setting is changed: dark, light and contrast themes are simulated
# through SERENO_OS_MODE. The exit code is 1 when any step failed.
#
# The guard is strict on purpose. If a terminal of yours writes to one of the
# guarded files while this runs (a command typed into another PowerShell
# window lands in PSReadLine's history), the run fails: run it again.
[CmdletBinding()]
param(
    [switch]$SkipGo,
    [switch]$SkipCross,
    [switch]$Bench
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $repo
$script:failed = $false
function Step([string]$name) { Write-Host ''; Write-Host "== $name" }
function Fail([string]$name) { Write-Host "not ok - $name"; $script:failed = $true }

# ---------------------------------------------------------------------------
# 1. The real places, looked at and never written.

$realLocal = $env:LOCALAPPDATA
$realRoaming = $env:APPDATA
$realHome = $env:USERPROFILE
$documents = [Environment]::GetFolderPath('MyDocuments')
$realSereno = Join-Path $realLocal 'sereno'

function Get-Listing([string]$path, [bool]$recurse) {
    if (-not (Test-Path -LiteralPath $path)) { return 'absent' }
    $item = Get-Item -LiteralPath $path -Force
    if (-not $item.PSIsContainer) { return (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash }
    $files = @(Get-ChildItem -LiteralPath $path -File -Force -Recurse:$recurse | Sort-Object FullName)
    $lines = foreach ($file in $files) {
        $hash = 'unreadable'
        try { $hash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash } catch { }
        $file.FullName.Substring($path.Length) + ' ' + $hash
    }
    'folder with ' + $files.Count + " files`n" + ($lines -join "`n")
}

# Label, path, and whether sub-folders are listed too. The two Documents
# folders may hold whole module trees; the profiles are the files directly
# inside.
$guarded = @(
    @('Windows Terminal folder (fragments)', (Join-Path $realLocal 'Microsoft\Windows Terminal'), $true),
    @('Windows Terminal settings.json', (Join-Path $realLocal 'Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json'), $false),
    @('Windows Terminal state.json', (Join-Path $realLocal 'Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\state.json'), $false),
    @('Windows Terminal Preview settings.json', (Join-Path $realLocal 'Packages\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\LocalState\settings.json'), $false),
    @('Windows Terminal Preview state.json', (Join-Path $realLocal 'Packages\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\LocalState\state.json'), $false),
    @('Documents\WindowsPowerShell (profiles)', (Join-Path $documents 'WindowsPowerShell'), $false),
    @('Documents\PowerShell (profiles)', (Join-Path $documents 'PowerShell'), $false),
    @('PSReadLine history', (Join-Path $realRoaming 'Microsoft\Windows\PowerShell\PSReadLine'), $true),
    @('.bashrc', (Join-Path $realHome '.bashrc'), $false),
    @('.bash_profile', (Join-Path $realHome '.bash_profile'), $false),
    @('.bash_history', (Join-Path $realHome '.bash_history'), $false),
    @('.profile', (Join-Path $realHome '.profile'), $false),
    @('.zshrc', (Join-Path $realHome '.zshrc'), $false),
    @('%LOCALAPPDATA%\sereno', $realSereno, $true)
)
function Get-Guard { $record = [ordered]@{}; foreach ($entry in $guarded) { $record[$entry[0]] = Get-Listing $entry[1] $entry[2] }; $record }

Step 'guard: the real places before the run'
$before = Get-Guard
foreach ($label in $before.Keys) { Write-Host ("recorded - {0}: {1}" -f $label, ($before[$label] -split "`n")[0]) }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('sereno-test-' + [guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $tmp
$saved = @{}
try {
    # -----------------------------------------------------------------------
    # 2. Go.
    function Invoke-Go([string]$name, [string[]]$arguments, [hashtable]$environment = @{}) {
        $old = @{}
        foreach ($key in $environment.Keys) { $old[$key] = [Environment]::GetEnvironmentVariable($key); [Environment]::SetEnvironmentVariable($key, $environment[$key]) }
        try {
            & go @arguments
            if ($LASTEXITCODE -eq 0) { Write-Host "ok - $name" } else { Fail $name }
        } finally {
            foreach ($key in $old.Keys) { [Environment]::SetEnvironmentVariable($key, $old[$key]) }
        }
    }
    if (-not $SkipGo) {
        Step 'go vet ./...'
        Invoke-Go 'go vet ./...' @('vet', './...')
        Step 'go test ./... -count=1'
        Invoke-Go 'go test ./... -count=1' @('test', './...', '-count=1')
        if (-not $SkipCross) {
            Step 'other systems'
            foreach ($os in 'linux', 'darwin') { Invoke-Go "go vet ./... for $os" @('vet', './...') @{ GOOS = $os; GOARCH = 'amd64' } }
            foreach ($os in 'windows', 'darwin', 'linux') {
                foreach ($arch in 'amd64', 'arm64') { Invoke-Go "go build ./... for $os/$arch without cgo" @('build', './...') @{ GOOS = $os; GOARCH = $arch; CGO_ENABLED = '0' } }
            }
        }
    }

    # -----------------------------------------------------------------------
    # 3. Render.
    Step 'render into a temporary folder'
    $rendered = Join-Path $tmp 'rendered'
    Invoke-Go 'everything sereno generates was rendered' @('run', './internal/tools/golden', '--out', $rendered) @{ SERENO_SANDBOX = $tmp }
    $initDir = Join-Path $rendered 'init'

    # -----------------------------------------------------------------------
    # 4. The shells, with a temporary folder as their home.
    $userHome = Join-Path $tmp 'home'
    foreach ($dir in $userHome, (Join-Path $userHome 'AppData\Local'), (Join-Path $userHome 'AppData\Roaming'), (Join-Path $userHome '.local\share'), (Join-Path $userHome 'Documents')) {
        $null = New-Item -ItemType Directory -Path $dir -Force
    }
    $testEnvironment = @{
        HOME = $userHome; USERPROFILE = $userHome; HOMEDRIVE = $userHome.Substring(0, 2); HOMEPATH = $userHome.Substring(2)
        LOCALAPPDATA = (Join-Path $userHome 'AppData\Local'); APPDATA = (Join-Path $userHome 'AppData\Roaming')
        XDG_DATA_HOME = (Join-Path $userHome '.local\share'); HISTFILE = (Join-Path $userHome '.bash_history')
        SERENO_HOME = (Join-Path $userHome 'AppData\Local\sereno'); SERENO_SANDBOX = $tmp
        SERENO_PS_PROFILE_PATH = (Join-Path $userHome 'Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1')
        SERENO_PWSH_PROFILE_PATH = (Join-Path $userHome 'Documents\PowerShell\Microsoft.PowerShell_profile.ps1')
        SERENO_OS_MODE = $null; SERENO_SESSION = $null; SERENO_REG_ROOT = $null; SERENO_LANG = $null
        WT_SESSION = $null; WT_PROFILE_ID = $null; TERM_PROGRAM = $null
        POWERSHELL_TELEMETRY_OPTOUT = '1'; POWERSHELL_UPDATECHECK = 'Off'
    }
    foreach ($key in $testEnvironment.Keys) { $saved[$key] = [Environment]::GetEnvironmentVariable($key); [Environment]::SetEnvironmentVariable($key, $testEnvironment[$key]) }

    $windowsPowerShell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $shells = @(, @('Windows PowerShell 5.1', $windowsPowerShell))
    $pwsh = Get-Command pwsh.exe -ErrorAction SilentlyContinue
    if ($pwsh) { $shells += , @('PowerShell 7', $pwsh.Source) }
    foreach ($shell in $shells) {
        Step "init.ps1 under $($shell[0])"
        & $shell[1] -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $repo 'test\shell\run.ps1') -Init (Join-Path $initDir 'init.ps1') -Golden $rendered
        if ($LASTEXITCODE -ne 0) { Fail "test\shell\run.ps1 under $($shell[0])" }
    }
    if (-not $pwsh) { Step 'init.ps1 under PowerShell 7'; Write-Host 'skip - pwsh is not installed: test\shell\run.ps1 did not run under PowerShell 7' }

    # Git Bash is the bash of the Git for Windows that git.exe belongs to:
    # git.exe is in cmd\ or mingw64\bin\ of that folder, bash.exe in bin\.
    # The bash.exe of System32 is the launcher of WSL and must not be taken
    # for it.
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
    Step 'init.bash under Git Bash'
    if ($bash) {
        $posix = { param([string]$path) $path.Replace('\', '/') }
        & $bash --noprofile --norc (& $posix (Join-Path $repo 'test\shell\run.bash')) --init (& $posix (Join-Path $initDir 'init.bash')) --golden (& $posix $rendered)
        if ($LASTEXITCODE -ne 0) { Fail 'test\shell\run.bash under Git Bash' }
    } else {
        Write-Host 'skip - Git Bash was not found next to git.exe: test\shell\run.bash did not run'
    }
    Step 'init.zsh under zsh'
    Write-Host 'skip - zsh does not run on Windows without MSYS2: test\shell\run.zsh runs in CI on macOS and Linux'

    # -----------------------------------------------------------------------
    # 5. Budgets.
    if ($Bench) {
        Step 'budgets'
        & $windowsPowerShell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $repo 'scripts\bench.ps1') -Init (Join-Path $initDir 'init.ps1') -SkipBash
        if ($LASTEXITCODE -ne 0) { Fail 'scripts\bench.ps1' }
        if ($bash) {
            & $bash --noprofile --norc (Join-Path $repo 'scripts\bench.sh').Replace('\', '/') --init $initDir.Replace('\', '/')
            if ($LASTEXITCODE -ne 0) { Fail 'scripts\bench.sh' }
        }
    }
} finally {
    foreach ($key in $saved.Keys) { [Environment]::SetEnvironmentVariable($key, $saved[$key]) }
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}

# ---------------------------------------------------------------------------
# 6. The real places again.

Step 'guard: the real places after the run'
$after = Get-Guard
foreach ($label in $before.Keys) {
    if ($before[$label] -ceq $after[$label]) { Write-Host "ok - unchanged: $label" }
    else { Fail "changed during the run: $label"; Write-Host "#   before: $(($before[$label] -split "`n")[0])"; Write-Host "#   after:  $(($after[$label] -split "`n")[0])" }
}
if (($before['%LOCALAPPDATA%\sereno'] -eq 'absent') -and ($after['%LOCALAPPDATA%\sereno'] -eq 'absent')) { Write-Host 'ok - %LOCALAPPDATA%\sereno was not created' }

Write-Host ''
if ($script:failed) { Write-Host 'test.ps1: FAILED'; exit 1 }
Write-Host 'test.ps1: all steps passed'
exit 0
