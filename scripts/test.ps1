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
#   4. the program itself: sereno.exe is built into the temporary folder and
#      scripts\smoke.ps1 runs it there, in a home of its own (apply --dry-run,
#      apply, status --json, theme, check, export, hooks, uninstall, with the
#      whole tree of that home compared before and after);
#   5. the behaviour tests of the init files, each under the shell it is for:
#      test\shell\run.ps1 under Windows PowerShell 5.1 and, when installed,
#      under PowerShell 7; test\shell\run.bash under Git Bash; a shell that
#      is not installed is reported as skipped, not as passed. They run on
#      the rendered files, which step 4 has compared byte for byte with the
#      ones `sereno apply` installs;
#   6. the install scripts of a release (scripts\install.ps1.tmpl and
#      scripts\install.sh.tmpl): a folder of six stand-in programs is packed
#      the way a release is, and test\install\run.ps1 and test\install\run.sh
#      run the rendered scripts against it, in a home of their own and without
#      the network (a good archive, a corrupted one, a missing checksum line,
#      a second run, the removal);
#   7. with -Bench, the budgets of scripts\bench.ps1, which goes on to
#      scripts\bench.sh for bash;
#   8. the listings of step 1 again: any difference fails the run.
#
# The program and the shells never see the real home: they are started with
# a temporary folder as HOME, USERPROFILE, LOCALAPPDATA and APPDATA (the
# shells with -NoProfile, bash with --noprofile --norc), and SERENO_SANDBOX
# confines every write of sereno's own code to that folder.
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
    @('.bash_login', (Join-Path $realHome '.bash_login'), $false),
    @('.bash_history', (Join-Path $realHome '.bash_history'), $false),
    @('.profile', (Join-Path $realHome '.profile'), $false),
    @('.zshrc', (Join-Path $realHome '.zshrc'), $false),
    @('%LOCALAPPDATA%\sereno', $realSereno, $true),
    @('%USERPROFILE%\.local\share\sereno', (Join-Path $realHome '.local\share\sereno'), $true)
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

    # A folder that stands in for a release, for step 6: six stand-in
    # programs, packed with LICENSE and README.md, with checksums.txt and the
    # two install scripts rendered for the version v0.0.0-test. The install
    # scripts never start the program, so a line of text will do in its place.
    Step 'a release folder of stand-in programs'
    $standIns = Join-Path $tmp 'stand-ins'
    foreach ($target in 'darwin_amd64', 'darwin_arm64', 'linux_amd64', 'linux_arm64', 'windows_amd64', 'windows_arm64') {
        $folder = Join-Path $standIns $target
        $null = New-Item -ItemType Directory -Path $folder -Force
        $name = 'sereno'
        if ($target -like 'windows_*') { $name = 'sereno.exe' }
        [IO.File]::WriteAllText((Join-Path $folder $name), "stand-in for the program on $target`n")
    }
    $release = Join-Path $tmp 'release'
    Invoke-Go 'the release folder was packed' @('run', './internal/tools/release', '--version', 'v0.0.0-test', '--binaries', $standIns, '--out', $release) @{ SERENO_SANDBOX = $tmp }

    $windowsPowerShell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'

    # -----------------------------------------------------------------------
    # 4. The program, built into the temporary folder and run in a home of
    #    its own. scripts\smoke.ps1 makes that home and points the program at
    #    it before the first start.
    Step 'sereno: the program is built into the temporary folder'
    $sereno = Join-Path $tmp 'bin\sereno.exe'
    Invoke-Go 'sereno.exe was built' @('build', '-trimpath', '-ldflags', '-X main.version=0.0.0-test', '-o', $sereno, './cmd/sereno')
    Step 'sereno: smoke run in a temporary home'
    if (Test-Path -LiteralPath $sereno) {
        & $windowsPowerShell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $repo 'scripts\smoke.ps1') -Sereno $sereno -Rendered $rendered -Version '0.0.0-test'
        if ($LASTEXITCODE -ne 0) { Fail 'scripts\smoke.ps1' }
    } else {
        Fail 'scripts\smoke.ps1 did not run: there is no program to run'
    }

    # -----------------------------------------------------------------------
    # 5. The shells, with a temporary folder as their home.
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
    # 6. The install scripts, against the release folder of stand-ins. The
    #    environment is still the one of step 5: a temporary folder is the
    #    home of everything that is started here.
    foreach ($shell in $shells) {
        Step "install.ps1 under $($shell[0])"
        & $shell[1] -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $repo 'test\install\run.ps1') -Release $release
        if ($LASTEXITCODE -ne 0) { Fail "test\install\run.ps1 under $($shell[0])" }
    }
    Step 'install.sh under the sh of Git Bash'
    $sh = $null
    if ($bash) { $sh = Join-Path (Split-Path -Parent $bash) 'sh.exe' }
    if ($sh -and (Test-Path -LiteralPath $sh)) {
        & $sh (& $posix (Join-Path $repo 'test\install\run.sh')) --release (& $posix $release)
        if ($LASTEXITCODE -ne 0) { Fail 'test\install\run.sh under the sh of Git Bash' }
    } else {
        Write-Host 'skip - the sh of Git Bash was not found next to git.exe: test\install\run.sh did not run'
    }

    # -----------------------------------------------------------------------
    # 7. Budgets.
    if ($Bench) {
        # bench.ps1 is started the way its header says and hands over to
        # scripts\bench.sh by itself: a hand-over that does not reach bash
        # ends with an exit code that is not 0 and fails this step.
        Step 'budgets'
        & $windowsPowerShell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File (Join-Path $repo 'scripts\bench.ps1') -Init (Join-Path $initDir 'init.ps1')
        if ($LASTEXITCODE -ne 0) { Fail 'scripts\bench.ps1 (with scripts\bench.sh for bash)' } else { Write-Host 'ok - scripts\bench.ps1 (with scripts\bench.sh for bash)' }
    }
} finally {
    foreach ($key in $saved.Keys) { [Environment]::SetEnvironmentVariable($key, $saved[$key]) }
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}

# ---------------------------------------------------------------------------
# 8. The real places again.

Step 'guard: the real places after the run'
$after = Get-Guard
foreach ($label in $before.Keys) {
    if ($before[$label] -ceq $after[$label]) { Write-Host "ok - unchanged: $label" }
    else { Fail "changed during the run: $label"; Write-Host "#   before: $(($before[$label] -split "`n")[0])"; Write-Host "#   after:  $(($after[$label] -split "`n")[0])" }
}
foreach ($label in '%LOCALAPPDATA%\sereno', '%USERPROFILE%\.local\share\sereno') {
    if (($before[$label] -eq 'absent') -and ($after[$label] -eq 'absent')) { Write-Host "ok - $label was not created" }
}

Write-Host ''
if ($script:failed) { Write-Host 'test.ps1: FAILED'; exit 1 }
Write-Host 'test.ps1: all steps passed'
exit 0
