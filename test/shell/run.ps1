# Behaviour tests of init.ps1 (test plan T7; requirements R23 to R30 and R34).
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File test\shell\run.ps1
#   pwsh -NoProfile -File test/shell/run.ps1
#
# The file runs under Windows PowerShell 5.1 and PowerShell 7 and tests the
# PowerShell that runs it. It prints one line per check ("ok - ..." or
# "not ok - ...") and ends with exit code 1 when a check failed.
#
# -Init    a rendered init.ps1; default testdata/golden/init/init.ps1, which
#          the Go tests keep equal to what the renderer produces
# -Golden  the folder with palette/<variant>.bin; default testdata/golden
# -Modes   the shared truth table; default testdata/modes.tsv
# -Only    run only the sessions whose name contains this text
#
# How it works: every scenario is a "session", a fresh child process of the
# same PowerShell started with -NoProfile, that dot-sources init.ps1 the way a
# profile would and then looks at the result. Nothing outside a temporary
# folder is written: the children get that folder as HOME, USERPROFILE,
# LOCALAPPDATA and APPDATA, PSReadLine is told to save no history, and PATH is
# an empty folder, so a child that tried to start a program would fail. The
# system's appearance is never changed: SERENO_OS_MODE stands in for it.
#
# One session of the test starts a program all the same, and init.ps1 has no
# part in it: the session without a console compiles one declaration with
# Add-Type, which under Windows PowerShell 5.1 starts the compiler of .NET
# by its full path. init.ps1 itself still starts nothing.
[CmdletBinding()]
param(
    [string]$Init = '',
    [string]$Golden = '',
    [string]$Modes = '',
    [string]$Only = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if (-not $Init) { $Init = Join-Path $repo 'testdata/golden/init/init.ps1' }
if (-not $Golden) { $Golden = Join-Path $repo 'testdata/golden' }
if (-not $Modes) { $Modes = Join-Path $repo 'testdata/modes.tsv' }
$Init = (Resolve-Path -LiteralPath $Init).Path
$Golden = (Resolve-Path -LiteralPath $Golden).Path
$Modes = (Resolve-Path -LiteralPath $Modes).Path

$exe = (Get-Process -Id $PID).Path
$onWindows = ($env:OS -eq 'Windows_NT')
$sep = [string][IO.Path]::DirectorySeparatorChar

if (-not (([Environment]::GetCommandLineArgs() -join ' ') -match '(?i)-nop')) {
    [Console]::Error.WriteLine('run.ps1: start it with -NoProfile, so that no real profile takes part in the test')
    exit 2
}

$script:passed = 0; $script:failed = 0; $script:skipped = 0
function Report([string]$line) {
    if ($line.StartsWith('ok - ')) { $script:passed++ }
    elseif ($line.StartsWith('not ok - ')) { $script:failed++ }
    elseif ($line.StartsWith('skip - ')) { $script:skipped++ }
    [Console]::Out.WriteLine($line)
}
function Check([bool]$condition, [string]$name, [string]$detail = '') {
    if ($condition) { Report "ok - $name" } else { Report "not ok - $name"; if ($detail) { [Console]::Out.WriteLine("#   $detail") } }
}

# ---------------------------------------------------------------------------
# The file itself, before anything runs it.

$tokens = $null; $parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($Init, [ref]$tokens, [ref]$parseErrors)
Check ($parseErrors.Count -eq 0) "init.ps1 parses without errors on PowerShell $($PSVersionTable.PSVersion)" (($parseErrors | ForEach-Object { $_.ToString() }) -join '; ')

$bytes = [IO.File]::ReadAllBytes($Init)
$odd = @($bytes | Where-Object { ($_ -ne 10) -and (($_ -lt 32) -or ($_ -gt 126)) })
Check ($odd.Count -eq 0) 'init.ps1 holds only printable ASCII and line feeds' "$($odd.Count) other bytes"
$text = [IO.File]::ReadAllText($Init)
Check ($text.Contains('[char]27') -and -not $text.Contains('`e')) 'init.ps1 makes the escape character with [char]27, never with the backtick-e escape'

# Every command the file can run, read off the syntax tree: PSReadLine's
# cmdlets and the saved prompt are called through a variable, Get-History by
# its full name. Any other command name would be a program or a module.
$commands = @($ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.CommandAst] }, $true))
$strange = @(foreach ($command in $commands) {
        $name = $command.GetCommandName()
        if ($name) { if ($name -ne 'Microsoft.PowerShell.Core\Get-History') { $name } }
        elseif (-not ($command.CommandElements[0] -is [System.Management.Automation.Language.VariableExpressionAst] -or
                $command.CommandElements[0] -is [System.Management.Automation.Language.IndexExpressionAst] -or
                $command.CommandElements[0] -is [System.Management.Automation.Language.ScriptBlockExpressionAst])) { $command.Extent.Text }
    })
Check ($commands.Count -gt 5 -and $strange.Count -eq 0) 'init.ps1 names no command besides Get-History: it can start no program and import no module' ($strange -join ', ')

# The .NET types it touches: files, the registry (read), the console, text.
$allowedTypes = 'bool', 'byte', 'char', 'Console', 'int', 'long', 'regex', 'ref', 'string', 'uint32',
    'Microsoft.Win32.Registry', 'System.Globalization.CultureInfo', 'System.IO.Directory', 'System.IO.File', 'System.IO.Path', 'System.StringComparison', 'System.Text.Encoding'
$types = @($ast.FindAll({ param($node) ($node -is [System.Management.Automation.Language.TypeExpressionAst]) -or ($node -is [System.Management.Automation.Language.ConvertExpressionAst]) -or ($node -is [System.Management.Automation.Language.TypeConstraintAst]) }, $true) |
        ForEach-Object { if ($_ -is [System.Management.Automation.Language.ConvertExpressionAst]) { $_.Type.TypeName.FullName } else { $_.TypeName.FullName } } | Sort-Object -Unique)
$foreign = @($types | Where-Object { $allowedTypes -notcontains $_ })
Check ($types.Count -gt 8 -and $foreign.Count -eq 0) 'init.ps1 uses no type that could start a process or open a connection' ($foreign -join ', ')

# ---------------------------------------------------------------------------
# The temporary world of the sessions.

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('sereno-ps-' + [guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $tmp -Force
# The folder goes by the name a process started in it gives it. On macOS the
# temporary folder is reached through a symbolic link (/var is /private/var):
# a session started in ~/work is then in /private/var/..., while a HOME taken
# from the name above says /var/... The prompt shortens the home folder by
# its name, as bash and zsh do, so the two names of one folder would make
# every expected prompt wrong. The system resolves the links when it is asked
# for the working directory.
if (-not $onWindows) {
    $startedIn = [Environment]::CurrentDirectory
    try { [Environment]::CurrentDirectory = $tmp; $tmp = [Environment]::CurrentDirectory } finally { [Environment]::CurrentDirectory = $startedIn }
}
$userHome = Join-Path $tmp 'home'
# ROOT in its default place, so that the hook line of R21 finds it:
# %LOCALAPPDATA%\sereno on Windows, ~/.local/share/sereno elsewhere.
$dataHome = Join-Path $userHome '.local/share'
$root = Join-Path $dataHome 'sereno'
$hookLine = 'if ($ExecutionContext.SessionState.LanguageMode -eq ''FullLanguage'' -and [System.IO.File]::Exists("$HOME/.local/share/sereno/shell/init.ps1")) { . "$HOME/.local/share/sereno/shell/init.ps1" } # sereno:hook'
if ($onWindows) {
    $root = Join-Path $userHome 'local\sereno'
    $hookLine = 'if ($ExecutionContext.SessionState.LanguageMode -eq ''FullLanguage'' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1")) { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook'
}
$emptyPath = Join-Path $tmp 'empty-path'
$sessionDir = Join-Path $tmp 'sessions'
foreach ($dir in $tmp, $userHome, $dataHome, (Join-Path $userHome 'local'), $root, (Join-Path $root 'shell'), $emptyPath, $sessionDir,
    (Join-Path $userHome 'roaming'), (Join-Path $userHome 'work'), (Join-Path $tmp 'homex')) {
    $null = New-Item -ItemType Directory -Path $dir -Force
}
$root = (Resolve-Path -LiteralPath $root).Path
$userHome = (Resolve-Path -LiteralPath $userHome).Path
$initCopy = Join-Path $root (Join-Path 'shell' 'init.ps1')
Copy-Item -LiteralPath $Init -Destination $initCopy
$modeFile = Join-Path $root 'mode'
$defaultMode = "mode=auto`nflavor=soft`ninband=profile`nkeys=profile`nprompt=on`n"

# A repository without git: a branch, a detached head, a worktree whose .git
# is a file, and a .git file that leads nowhere.
function New-Tree([string]$base, [hashtable]$files) {
    foreach ($name in $files.Keys) {
        $path = Join-Path $base $name
        $null = New-Item -ItemType Directory -Path (Split-Path -Parent $path) -Force
        [IO.File]::WriteAllText($path, $files[$name])
    }
}
New-Tree $userHome @{
    'repo/.git/HEAD' = "ref: refs/heads/feature/x`n"
    'repo/sub/deep/keep.txt' = ''
    'detached/.git/HEAD' = "0123456789abcdef0123456789abcdef01234567`n"
    'detached/keep.txt' = ''
    'crlf/.git/HEAD' = "ref: refs/heads/main`r`n"
    'crlf/keep.txt' = ''
    'main/.git/worktrees/wt/HEAD' = "ref: refs/heads/side`n"
    'main/.git/HEAD' = "ref: refs/heads/main`n"
    'worktree/.git' = "gitdir: ../main/.git/worktrees/wt`n"
    'worktree/keep.txt' = ''
    'repo/broken/.git' = "gitdir: ../nowhere`n"
    'repo/broken/keep.txt' = ''
    'hostile/.git/HEAD' = "ref: refs/heads/a" + [char]27 + "]0;x" + [char]7 + "b`n"
    'hostile/keep.txt' = ''
}

$baseEnv = @{
    HOME = $userHome; USERPROFILE = $userHome
    LOCALAPPDATA = (Join-Path $userHome 'local'); APPDATA = (Join-Path $userHome 'roaming')
    XDG_DATA_HOME = $dataHome; HISTFILE = (Join-Path $userHome '.bash_history')
    XDG_CACHE_HOME = (Join-Path $userHome '.cache'); XDG_CONFIG_HOME = (Join-Path $userHome '.config')
    SERENO_HOME = $root; SERENO_SANDBOX = $tmp; PATH = $emptyPath
    SERENO_OS_MODE = ''; SERENO_SESSION = ''; SERENO_REG_ROOT = ''; SERENO_LANG = ''
    WT_SESSION = ''; WT_PROFILE_ID = ''; TERM_PROGRAM = ''
    SERENO_T_INIT = $initCopy; SERENO_T_ROOT = $root; SERENO_T_GOLDEN = $Golden; SERENO_T_MODES = $Modes
    SERENO_T_HOME = $userHome; SERENO_T_TMP = $tmp; SERENO_T_HOOK = $hookLine
}
if ($onWindows) { $baseEnv['HOMEDRIVE'] = $userHome.Substring(0, 2); $baseEnv['HOMEPATH'] = $userHome.Substring(2) }

function Start-Child([string]$arguments, [hashtable]$extraEnv, [string]$directory, [bool]$ownConsole = $false) {
    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = $exe
    $info.Arguments = $arguments
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    # A child with a console of its own (Windows): no stream is redirected,
    # so the child gets a console, one without a window, and its output is a
    # console as in a terminal. What it has to say it writes to a file.
    $info.RedirectStandardInput = -not $ownConsole
    $info.RedirectStandardOutput = -not $ownConsole
    $info.RedirectStandardError = -not $ownConsole
    $info.WorkingDirectory = $directory
    foreach ($pair in @($baseEnv.GetEnumerator()) + @($extraEnv.GetEnumerator())) { $info.EnvironmentVariables[$pair.Key] = [string]$pair.Value }
    # The PATH this child is given, once more under a name PowerShell leaves
    # alone (see the start of the library below).
    $info.EnvironmentVariables['SERENO_T_PATH'] = $info.EnvironmentVariables['PATH']
    # Windows PowerShell hands a child the preamble of [Console]::InputEncoding
    # on its redirected input. In a console whose input code page is 65001
    # that is a byte order mark, which a session that reads its commands from
    # the input would take for a command. For the moment of the start the
    # encoding is the same code page without a preamble.
    $inputEncoding = $null
    try {
        if ([Console]::InputEncoding.GetPreamble().Length -gt 0) {
            $inputEncoding = [Console]::InputEncoding
            [Console]::InputEncoding = New-Object System.Text.UTF8Encoding $false
        }
    } catch { $inputEncoding = $null }
    try { $process = [Diagnostics.Process]::Start($info) }
    finally { if ($null -ne $inputEncoding) { [Console]::InputEncoding = $inputEncoding } }
    if ($ownConsole) {
        if (-not $process.WaitForExit(120000)) { $process.Kill(); $process.WaitForExit() }
        $result = [pscustomobject]@{ Out = ''; Err = ''; Code = $process.ExitCode }
        $process.Dispose()
        return $result
    }
    $process.StandardInput.Close()
    $errTask = $process.StandardError.ReadToEndAsync()
    $outTask = $process.StandardOutput.ReadToEndAsync()
    if (-not $process.WaitForExit(120000)) { $process.Kill(); $process.WaitForExit() }
    $result = [pscustomobject]@{ Out = $outTask.Result; Err = $errTask.Result; Code = $process.ExitCode }
    $process.Dispose()
    $result
}

# What every session script starts with.
$library = {
    Set-StrictMode -Version Latest
    $ErrorActionPreference = 'Stop'
    # PowerShell 6 and later start by putting the folder they are installed
    # in at the front of PATH, so that a session finds the same PowerShell
    # again (seen in CI: "<folder of pwsh>;<the empty folder>" on Windows,
    # and the same with a colon on macOS and Linux). Windows PowerShell 5.1
    # does not. The session gets back the PATH the test gave it, on which
    # nothing can be found, before init.ps1 runs: a program it tried to start
    # would not be there, PowerShell itself included.
    $env:PATH = $env:SERENO_T_PATH
    $E = [string][char]27
    $BEL = [string][char]7
    $LF = "`n"
    $T = @{
        Init = $env:SERENO_T_INIT; Root = $env:SERENO_T_ROOT; Golden = $env:SERENO_T_GOLDEN; Modes = $env:SERENO_T_MODES
        Home = $env:SERENO_T_HOME; Tmp = $env:SERENO_T_TMP; Windows = ($env:OS -eq 'Windows_NT'); Sep = [string][IO.Path]::DirectorySeparatorChar
        ModeFile = (Join-Path $env:SERENO_T_ROOT 'mode'); Stamp = [datetime]::UtcNow
    }
    # $HOME is what the prompt shortens to ~. PowerShell 7 does not take it
    # from the environment on Windows, so it is set here.
    Set-Variable -Name HOME -Value $T.Home -Force -Scope Global

    function Show([string]$text) {
        if ($null -eq $text) { return '<null>' }
        $text.Replace([string][char]27, '<ESC>').Replace([string][char]7, '<BEL>').Replace("`r", '<CR>').Replace("`n", '<LF>')
    }
    function Check([bool]$condition, [string]$name, [string]$detail = '') {
        if ($condition) { [Console]::Out.WriteLine("ok - $name") }
        else { [Console]::Out.WriteLine("not ok - $name"); if ($detail) { [Console]::Out.WriteLine("#   $detail") } }
    }
    function CheckEqual([string]$got, [string]$want, [string]$name) {
        Check ($got -ceq $want) $name ("got  " + (Show $got) + "`n#   want " + (Show $want))
    }
    function Skip([string]$name) { [Console]::Out.WriteLine("skip - $name") }
    # Runs a block with [Console]::Out replaced, and returns what was written
    # to the console and what the block returned.
    function Capture([scriptblock]$action) {
        $writer = New-Object System.IO.StringWriter
        $console = [Console]::Out
        [Console]::SetOut($writer)
        try { $output = @(& $action) } finally { [Console]::SetOut($console) }
        [pscustomobject]@{ Console = $writer.ToString(); Output = $output; Text = ($output -join '') }
    }
    function Palette([string]$variant) { [IO.File]::ReadAllText((Join-Path $T.Golden (Join-Path 'palette' ($variant + '.bin')))) }
    # Writes the mode file with a time of its own, as a later `sereno theme` would.
    function Set-Mode([string]$content) {
        [IO.File]::WriteAllText($T.ModeFile, $content)
        $T.Stamp = $T.Stamp.AddSeconds(5)
        [IO.File]::SetLastWriteTimeUtc($T.ModeFile, $T.Stamp)
    }
    function Mode([string]$mode = 'auto', [string]$flavor = 'soft', [string]$inband = 'profile', [string]$keys = 'profile', [string]$prompt = 'on') {
        "mode=$mode`nflavor=$flavor`ninband=$inband`nkeys=$keys`nprompt=$prompt`n"
    }
    # The prompt of R26 for a path, without marks.
    function Plain([string]$path, [string]$branch = '', [string]$took = '', [bool]$ok = $true, [int]$level = 0) {
        $text = $E + '[34m' + $path + $E + '[0m'
        if ($branch) { $text += ' ' + $E + '[35m' + $branch + $E + '[0m' }
        if ($took) { $text += ' ' + $E + '[90m' + $took + $E + '[0m' }
        $colour = '[32m'; if (-not $ok) { $colour = '[31m' }
        $text + $LF + $E + $colour + ('>' * ($level + 1)) + $E + '[0m '
    }
    function Go([string]$relative) { Set-Location -LiteralPath (Join-Path $T.Home $relative) }
    # R25: styles, each without its escape character, and what they are after
    # init.ps1: a background parameter (40-47, 100-107) is the foreground of
    # the same slot; the numbers after 38 and 48 are colour arguments. The
    # last line holds the two ends of each range, each alone in a style or
    # last in it, and their neighbours outside: 39 and 49 are the default
    # colours, which R25 does not name, and 108 is no colour at all.
    function Samples {
        [ordered]@{
            '[44;1m' = '[34;1m'; '[1;103m' = '[1;93m'; '[30;47m' = '[30;37m'; '[100m' = '[90m'; '[32;1m' = '[32;1m'; '[0m' = '[0m'; '[m' = '[m'
            '[38;5;44m' = '[38;5;44m'; '[38;2;40;41;42m' = '[38;2;40;41;42m'; '[48;5;44;41m' = '[48;5;44;31m'; '[38;5;1;44m' = '[38;5;1;34m'
            '[01;044m' = '[01;34m'; '[0100;1m' = '[90;1m'; '[1;4m' = '[1;4m'; '[104m' = '[94m'
            '[40m' = '[30m'; '[1;107m' = '[1;97m'; '[47m' = '[37m'; '[7;100m' = '[7;90m'; '[39;49m' = '[39;49m'; '[1;108m' = '[1;108m'
        }
    }
}

$withReadLine = {
    Import-Module PSReadLine
    if ((Get-Command Set-PSReadLineOption).Parameters.ContainsKey('HistorySaveStyle')) { Set-PSReadLineOption -HistorySaveStyle SaveNothing }
}

# Runs one session: a child process that executes library + body and prints
# check lines.
#
# With $ownConsole the child is not given the pipes of this script but a
# console of its own, without a window: its output is then a console, as in a
# terminal, which is what PSReadLine looks at before it offers predictions.
# The check lines go to a file instead, and so does an error that ends the
# session.
$toFile = {
    $report = New-Object System.IO.StreamWriter $env:SERENO_T_REPORT
    $report.AutoFlush = $true
    [Console]::SetOut($report)
    trap { [Console]::Out.WriteLine('not ok - the session ended with an error: ' + ([string]$_).Replace("`n", ' ')); exit 1 }
}
function Invoke-Session([string]$name, [hashtable]$environment, [string]$mode, [scriptblock]$body, [bool]$readLine = $true, [bool]$ownConsole = $false) {
    if ($Only -and -not $name.Contains($Only)) { return }
    [Console]::Out.WriteLine("# session: $name")
    if ($mode -eq '<none>') { if (Test-Path -LiteralPath $modeFile) { Remove-Item -LiteralPath $modeFile -Force } }
    else { [IO.File]::WriteAllText($modeFile, $mode) }
    $script = ''
    if ($ownConsole) { $script = $toFile.ToString() + "`n" }
    $script += $library.ToString() + "`n"
    if ($readLine) { $script += $withReadLine.ToString() + "`n" }
    $script += $body.ToString() + "`n"
    $file = Join-Path $sessionDir (($name -replace '[^A-Za-z0-9]+', '-') + '.ps1')
    [IO.File]::WriteAllText($file, $script)
    $arguments = '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + $file + '"'
    if (-not $onWindows) { $arguments = '-NoLogo -NoProfile -NonInteractive -File "' + $file + '"' }
    if ($ownConsole) {
        $report = Join-Path $sessionDir 'report.txt'
        if (Test-Path -LiteralPath $report) { Remove-Item -LiteralPath $report -Force }
        $child = Start-Child $arguments ($environment + @{ SERENO_T_REPORT = $report }) (Join-Path $userHome 'work') $true
        if (Test-Path -LiteralPath $report) { $child.Out = [IO.File]::ReadAllText($report) }
    } else {
        $child = Start-Child $arguments $environment (Join-Path $userHome 'work')
    }
    $lines = 0
    foreach ($line in ($child.Out -split "`r?`n")) {
        if ($line -eq '') { continue }
        if ($line -match '^(ok|not ok|skip) - ' -or $line.StartsWith('#')) { Report $line; $lines++ }
        else { Report "not ok - $name wrote something that is not a check line: $line" }
    }
    Check ($child.Code -eq 0 -and $child.Err.Trim() -eq '') "$name ran to its end without an error" ("exit code $($child.Code): " + $child.Err.Trim())
    Check ($lines -gt 0) "$name reported at least one check"
}

$profileSession = @{ WT_SESSION = 'test-session'; SERENO_SESSION = 'wt-profile'; SERENO_OS_MODE = 'dark' }

try {
    # -----------------------------------------------------------------------
    Invoke-Session 'Serena profile session in Windows Terminal' $profileSession $defaultMode {
        # The line editor these sessions have: the documents name its version.
        [Console]::Out.WriteLine('# PSReadLine ' + (Get-Module PSReadLine).Version + ' under PowerShell ' + $PSVersionTable.PSVersion)
        $stock = '"PS $($executionContext.SessionState.Path.CurrentLocation)$(''>'' * ($nestedPromptLevel + 1)) ";'
        $definition = (@($function:prompt.ToString() -split "`r?`n" | Where-Object { -not $_.TrimStart().StartsWith('#') }) -join "`n").Trim()
        CheckEqual $definition $stock 'the stock prompt of this PowerShell is the one R26 expects'

        # Dot-sourcing changes nothing of the caller and prints nothing. It
        # is done here, at the top of the script, as a profile does it: a
        # variable the file created would stay behind in this scope.
        $global:LASTEXITCODE = 7
        $location = (Get-Location).Path
        $preference = $ErrorActionPreference
        $errorsBefore = $Error.Count
        $optionsBefore = Get-PSReadLineOption
        $predictionBefore = $null
        if ($optionsBefore.PSObject.Properties['PredictionSource']) { $predictionBefore = [string]$optionsBefore.PredictionSource }
        $modulesBefore = @(Get-Module | ForEach-Object { $_.Name } | Sort-Object) -join ','
        $writer = New-Object System.IO.StringWriter
        $console = [Console]::Out
        $output = $null; $after = $null; $before = $null
        $before = @(Get-Variable | ForEach-Object { $_.Name })
        [Console]::SetOut($writer)
        try { $output = @(. $T.Init) } finally { [Console]::SetOut($console) }
        $after = @(Get-Variable | ForEach-Object { $_.Name })
        CheckEqual (@($after | Where-Object { $before -notcontains $_ }) -join ',') '__sereno' 'init.ps1 creates exactly one variable, $__sereno'
        CheckEqual $writer.ToString() '' 'init.ps1 writes nothing to the console'
        Check ($output.Count -eq 0) 'init.ps1 returns nothing'
        CheckEqual (Get-Location).Path $location 'init.ps1 leaves the location alone'
        CheckEqual ([string]$ErrorActionPreference) ([string]$preference) 'init.ps1 leaves $ErrorActionPreference alone'
        Check ($global:LASTEXITCODE -eq 7) 'init.ps1 leaves $LASTEXITCODE alone'
        Check ($Error.Count -eq $errorsBefore) 'init.ps1 leaves $Error empty: nothing in it failed' ($(if ($Error.Count) { [string]$Error[0] }))
        CheckEqual (@(Get-Module | ForEach-Object { $_.Name } | Sort-Object) -join ',') $modulesBefore 'init.ps1 imports no module'

        # R24: the colours of the line editor.
        $options = Get-PSReadLineOption
        $wanted = [ordered]@{
            CommandColor = '[34m'; KeywordColor = '[35m'; StringColor = '[32m'; NumberColor = '[93m'; TypeColor = '[33m'; MemberColor = '[36m'
            VariableColor = '[39m'; ParameterColor = '[3;37m'; OperatorColor = '[37m'; CommentColor = '[90m'; ErrorColor = '[91m'
            EmphasisColor = '[96m'; SelectionColor = '[7m'; DefaultTokenColor = '[39m'; ContinuationPromptColor = '[90m'
        }
        $wrong = @(foreach ($name in $wanted.Keys) { if ([string]$options.$name -cne ($E + $wanted[$name])) { $name + '=' + (Show ([string]$options.$name)) } })
        Check ($wrong.Count -eq 0) 'PSReadLine has the fifteen colours of R24' ($wrong -join ', ')
        $later = [ordered]@{ InlinePredictionColor = '[90m'; ListPredictionColor = '[33m'; ListPredictionSelectedColor = '[7m' }
        foreach ($name in $later.Keys) {
            if ($options.PSObject.Properties[$name]) { CheckEqual ([string]$options.$name) ($E + $later[$name]) "PSReadLine has $name, and it is set" }
            else { Check $true "PSReadLine $((Get-Module PSReadLine).Version) has no $name, and init.ps1 did not try to set it" }
        }

        # R25: the pairs measured as unreadable.
        if (Test-Path variable:PSStyle) {
            $values = @($PSStyle.Progress.Style) + @($PSStyle.Formatting.PSObject.Properties | ForEach-Object { $_.Value }) +
                @($PSStyle.FileInfo.Directory, $PSStyle.FileInfo.SymbolicLink, $PSStyle.FileInfo.Executable) + @($PSStyle.FileInfo.Extension.Keys | ForEach-Object { $PSStyle.FileInfo.Extension[$_] })
            $painted = @($values | Where-Object { $_ -is [string] } | Where-Object { [regex]::IsMatch($_, $E + '\[(?:[0-9;]*;)?(4[0-7]|10[0-7])(?:;[0-9;]*)?m') })
            Check ($values.Count -gt 10 -and $painted.Count -eq 0) 'no $PSStyle value of R25 paints a background' (($painted | ForEach-Object { Show $_ }) -join ', ')
            Check (-not $PSStyle.FileInfo.Directory.Contains('[44')) 'directories are no longer listed on a blue background' (Show $PSStyle.FileInfo.Directory)
        } else {
            $raw = $null
            try { $raw = @($Host.UI.RawUI.ForegroundColor, $Host.UI.RawUI.BackgroundColor) } catch { }
            if ($raw) {
                Check (($Host.PrivateData.ProgressForegroundColor -eq $raw[0]) -and ($Host.PrivateData.ProgressBackgroundColor -eq $raw[1])) 'the progress bar has the colours of the console' "$($Host.PrivateData.ProgressForegroundColor) on $($Host.PrivateData.ProgressBackgroundColor), console $($raw[0]) on $($raw[1])"
            } else { Skip 'the progress bar colours: this process has no console to read the colours from' }
        }

        # R27: the comfort set, in a profile session with keys=profile.
        Check (([string]$options.BellStyle -eq 'None') -and $options.HistoryNoDuplicates -and $options.HistorySearchCursorMovesToEnd) 'comfort set: no bell, no duplicates in history, cursor at the end after a search'
        $handlers = @{}
        Get-PSReadLineKeyHandler | Where-Object { $_.Key -in 'Tab', 'UpArrow', 'DownArrow' } | ForEach-Object { $handlers[[string]$_.Key] = [string]$_.Function }
        CheckEqual ("{0} {1} {2}" -f $handlers['Tab'], $handlers['UpArrow'], $handlers['DownArrow']) 'MenuComplete HistorySearchBackward HistorySearchForward' 'comfort set: Tab, UpArrow and DownArrow'
        if ($null -eq $predictionBefore) { Check (-not $options.PSObject.Properties['PredictionSource']) 'this PSReadLine has no PredictionSource, and init.ps1 did not try to set it' }
        elseif ($predictionBefore -eq 'None') {
            # On Windows PSReadLine has predictions only where it can draw
            # them. Asked for them in a session like this one, whose output
            # is not a console, it answers "The predictive suggestion feature
            # cannot be enabled because the console output doesn't support
            # virtual terminal processing or it's redirected" (seen in CI
            # under PowerShell 7.6.6). So PSReadLine itself is asked, after
            # init.ps1 has run: where it takes History, init.ps1 must have
            # set it; where it refuses, the value is still None, and the
            # check of $Error above has shown that init.ps1 did not ask.
            $predictionAfter = [string]$options.PredictionSource
            $taken = $true
            try { Set-PSReadLineOption -PredictionSource History } catch { $taken = $false; $Error.RemoveAt(0) }
            if ($taken) { CheckEqual $predictionAfter 'History' 'comfort set: PredictionSource was None and is History' }
            else { CheckEqual $predictionAfter 'None' 'comfort set: PredictionSource was None and stays None, because PSReadLine refuses predictions where the output is not a console' }
        }
        else { CheckEqual ([string]$options.PredictionSource) $predictionBefore 'comfort set: a PredictionSource that was not None is left alone' }

        # R26 and R30: the first prompt paints, and carries the marks.
        $here = (Get-Location).ProviderPath
        CheckEqual $here (Join-Path $T.Home 'work') 'the session starts in ~/work, and names that folder as the test does'
        $shown = '~' + $T.Sep + 'work'
        $startMarks = $E + ']133;A' + $BEL + $E + ']9;9;"' + $here + '"' + $BEL
        $endMark = $E + ']133;B' + $BEL
        $run = Capture { prompt }
        CheckEqual $run.Console (Palette 'dark') 'the first prompt writes PALETTE of the resolved variant to the console'
        CheckEqual $run.Text ($startMarks + (Plain $shown) + $endMark) 'the first prompt: marks, path with ~, green >'
        Check ($run.Output.Count -eq 1 -and $run.Output[0] -is [string]) 'the prompt returns one string'
        $run = Capture { prompt }
        CheckEqual $run.Console '' 'the second prompt writes nothing: the variant did not change'
        CheckEqual $run.Text ($E + ']133;D;0' + $BEL + $startMarks + (Plain $shown) + $endMark) 'from the second prompt on the end-of-command mark comes first'

        # $? and $LASTEXITCODE.
        $run = Capture { Write-Error 'failed on purpose' -ErrorAction SilentlyContinue; prompt }
        CheckEqual $run.Text ($E + ']133;D;7' + $BEL + $startMarks + (Plain $shown -ok $false) + $endMark) 'after a failed command: red >, and the exit code of the last program in the mark'
        $global:LASTEXITCODE = 0
        $run = Capture { Write-Error 'failed on purpose' -ErrorAction SilentlyContinue; prompt }
        CheckEqual $run.Text ($E + ']133;D;1' + $BEL + $startMarks + (Plain $shown -ok $false) + $endMark) 'after a failed command without an exit code the mark says 1'
        $global:LASTEXITCODE = 3
        $run = Capture { $null = 1; prompt }
        CheckEqual $run.Text ($E + ']133;D;0' + $BEL + $startMarks + (Plain $shown) + $endMark) 'after a successful command the mark says 0 whatever $LASTEXITCODE holds'
        Check ($global:LASTEXITCODE -eq 3) 'the prompt leaves $LASTEXITCODE alone'
        $Error.Clear()

        # R30: following the system, one variant after the other.
        foreach ($step in 'light', 'hc', 'hc-light', 'dark') {
            $env:SERENO_OS_MODE = $step
            $run = Capture { prompt }
            CheckEqual $run.Console (Palette $step) "the system turns $step`: the next prompt writes PALETTE of $step"
            $run = Capture { prompt }
            CheckEqual $run.Console '' "the system stays $step`: nothing is written"
        }

        # R30: the mode file is read again when it changes.
        Set-Mode (Mode 'light' 'vivid')
        CheckEqual (Capture { prompt }).Console (Palette 'light-vivid') 'the mode file now says light and vivid: the next prompt paints light-vivid'
        Set-Mode (Mode 'hc' 'vivid')
        CheckEqual (Capture { prompt }).Console (Palette 'hc') 'the mode file now says hc: the flavour does not matter'
        Set-Mode (Mode 'auto' 'vivid')
        CheckEqual (Capture { prompt }).Console (Palette 'dark-vivid') 'back to auto with vivid: dark-vivid, because the system is dark'
        Set-Mode (Mode 'auto' 'soft' 'off')
        CheckEqual (Capture { prompt }).Console (Palette 'reset') 'inband=off: the palette this session painted is handed back with RESET'
        CheckEqual (Capture { prompt }).Console '' 'inband=off: after that nothing is written'
        Set-Mode (Mode 'auto' 'soft' 'always')
        CheckEqual (Capture { prompt }).Console (Palette 'dark') 'inband=always: painted again'
        Set-Mode ("mode=auto`r`nflavor=vivid`r`ninband=profile`r`nkeys=profile`r`nprompt=on`r`n")
        CheckEqual (Capture { prompt }).Console '' 'a malformed mode file (CRLF) means the defaults: auto and soft, which is what is painted already'
        Set-Mode (Mode 'light')
        CheckEqual (Capture { prompt }).Console (Palette 'light') 'a valid file again: light'
        Remove-Item -LiteralPath $T.ModeFile -Force
        CheckEqual (Capture { prompt }).Console (Palette 'dark') 'a missing mode file means the defaults: auto, so dark'
        Set-Mode (Mode)

        # R26: the branch, read without git (PATH is an empty folder here).
        CheckEqual $env:PATH (Join-Path $T.Tmp 'empty-path') 'PATH is an empty folder in this session'
        $cases = [ordered]@{
            'repo' = 'feature/x'; 'repo/sub/deep' = 'feature/x'; 'detached' = '0123456'; 'crlf' = 'main'; 'worktree' = 'side'; 'repo/broken' = 'feature/x'; 'hostile' = 'a]0;xb'
        }
        foreach ($case in $cases.Keys) {
            Go $case
            $relative = '~' + $T.Sep + $case.Replace('/', $T.Sep)
            $marks = $E + ']133;D;0' + $BEL + $E + ']133;A' + $BEL + $E + ']9;9;"' + (Get-Location).ProviderPath + '"' + $BEL
            CheckEqual (Capture { prompt }).Text ($marks + (Plain $relative $cases[$case]) + $endMark) "branch in ~/$case is $($cases[$case])"
        }
        Go 'work'

        # R26: how long the last command took.
        $t0 = [datetime]'2026-01-01T10:00:00'
        $history = { param([double]$seconds) [pscustomobject]@{ CommandLine = 'wait'; ExecutionStatus = 'Completed'; StartExecutionTime = $t0; EndExecutionTime = $t0.AddSeconds($seconds) } | Add-History }
        $marks = $E + ']133;D;0' + $BEL + $startMarks
        & $history 2.34
        $culture = [Threading.Thread]::CurrentThread.CurrentCulture
        [Threading.Thread]::CurrentThread.CurrentCulture = [Globalization.CultureInfo]::GetCultureInfo('de-DE')
        try { $run = Capture { prompt } } finally { [Threading.Thread]::CurrentThread.CurrentCulture = $culture }
        CheckEqual $run.Text ($marks + (Plain $shown '' '2.3s') + $endMark) 'a command of 2.34 s shows as 2.3s, with a point in every culture'
        CheckEqual (Capture { prompt }).Text ($marks + (Plain $shown) + $endMark) 'the duration is shown once: Enter on an empty line does not repeat it'
        & $history 1.99
        CheckEqual (Capture { prompt }).Text ($marks + (Plain $shown) + $endMark) 'a command of 1.99 s shows no duration'
        & $history 2.0
        CheckEqual (Capture { prompt }).Text ($marks + (Plain $shown '' '2.0s') + $endMark) 'a command of exactly 2 s shows as 2.0s'
        & $history 125.26
        CheckEqual (Capture { prompt }).Text ($marks + (Plain $shown '' '125.3s') + $endMark) 'a long command stays in seconds: 125.3s'

        # R26: nested prompts, other providers, the home folder.
        $global:NestedPromptLevel = 2
        CheckEqual (Capture { prompt }).Text ($marks + (Plain $shown -level 2) + $endMark) 'nested prompt level 2 shows >>>'
        $global:NestedPromptLevel = 0
        Set-Location Env:
        CheckEqual (Capture { prompt }).Text ($E + ']133;D;0' + $BEL + $E + ']133;A' + $BEL + (Plain (Get-Location).Path) + $endMark) 'outside the file system: the location as it is, no working-directory mark, no branch'
        Set-Location -LiteralPath $T.Home
        $marksHome = $E + ']133;D;0' + $BEL + $E + ']133;A' + $BEL + $E + ']9;9;"' + (Get-Location).ProviderPath + '"' + $BEL
        CheckEqual (Capture { prompt }).Text ($marksHome + (Plain '~') + $endMark) 'the home folder itself is ~'
        $sibling = Join-Path $T.Tmp 'homex'
        Set-Location -LiteralPath $sibling
        $run = Capture { prompt }
        Check ($run.Text.Contains($E + '[34m' + $sibling + $E + '[0m')) 'a folder that only starts like the home folder is not shortened' (Show $run.Text)
        if ($T.Windows) {
            Set-Location -LiteralPath (Join-Path $T.Home.ToUpper() 'work')
            $run = Capture { prompt }
            Check ($run.Text.Contains($E + '[34m~' + $T.Sep)) 'on Windows the home folder is recognised whatever its letter case' (Show $run.Text)
        }
        Go 'work'

        # R23: sourcing twice is a no-op.
        $state = $global:__sereno
        $shownBefore = $state['Shown']
        $promptText = $function:prompt.ToString()
        $writer = New-Object System.IO.StringWriter
        $output = $null; $after = $null; $before = $null
        $before = @(Get-Variable | ForEach-Object { $_.Name })
        [Console]::SetOut($writer)
        try { $output = @(. $T.Init) } finally { [Console]::SetOut($console) }
        $after = @(Get-Variable | ForEach-Object { $_.Name })
        Check ([object]::ReferenceEquals($state, $global:__sereno) -and ($state['Shown'] -eq $shownBefore)) 'sourcing init.ps1 a second time keeps the state of the first'
        CheckEqual $function:prompt.ToString() $promptText 'sourcing init.ps1 a second time keeps the prompt'
        Check ($writer.ToString() -eq '' -and $output.Count -eq 0) 'sourcing init.ps1 a second time prints nothing'
        CheckEqual (@($after | Where-Object { $before -notcontains $_ }) -join ',') '' 'sourcing init.ps1 a second time creates no variable'
        Check (-not (Test-Path function:sereno)) 'no sereno function without ROOT/bin/sereno'
        Check ($Error.Count -eq 0) 'nothing failed along the way' ($(if ($Error.Count) { [string]$Error[0] }))
    }

    # -----------------------------------------------------------------------
    Invoke-Session 'resolution table' @{ } $defaultMode {
        . $T.Init
        $rows = @([IO.File]::ReadAllText($T.Modes).TrimEnd("`n").Split("`n"))
        CheckEqual $rows[0] "mode`tflavor`thc_on`thc_light`tapps_light`tvariant" 'modes.tsv has the header the three implementations agree on'
        $wrong = @(foreach ($row in $rows[1..($rows.Count - 1)]) {
                $f = $row.Split("`t")
                $got = & $global:__sereno['Resolve'] $f[0] $f[1] ($f[2] -eq '1') ($f[3] -eq '1') ($f[4] -eq '1')
                if ($got -cne $f[5]) { "$row -> $got" }
            })
        Check ($rows.Count -eq 81 -and $wrong.Count -eq 0) 'init.ps1 resolves all 80 rows of testdata/modes.tsv' ($wrong -join '; ')
        CheckEqual (& $global:__sereno['Resolve'] 'sepia' 'loud' $false $false $true) 'light' 'an unknown mode resolves like auto and an unknown flavour like soft'

        $expected = [ordered]@{ 'dark' = 'False False False'; 'light' = 'False False True'; 'hc' = 'True False False'; 'hc-light' = 'True True False' }
        foreach ($os in $expected.Keys) {
            $env:SERENO_OS_MODE = $os
            CheckEqual ((& $global:__sereno['Signals']) -join ' ') $expected[$os] "SERENO_OS_MODE=$os replaces the signals"
        }
        $env:SERENO_OS_MODE = 'sepia'
        $signals = @(& $global:__sereno['Signals'])
        Check ($signals.Count -eq 3 -and @($signals | Where-Object { $_ -isnot [bool] }).Count -eq 0) 'without a valid SERENO_OS_MODE the signals are read from the system (read only) and are three booleans'
        if (-not $T.Windows) { CheckEqual ($signals -join ' ') 'False False False' 'outside Windows the signals are unknown and count as off' }

        # The mode file: exactly five lines, anything else is the defaults.
        $defaults = 'auto soft profile profile on'
        $files = [ordered]@{
            'a valid file' = @((Mode 'hc-light' 'vivid' 'always' 'off' 'off'), 'hc-light vivid always off off')
            'CRLF line endings' = @("mode=dark`r`nflavor=soft`r`ninband=profile`r`nkeys=profile`r`nprompt=on`r`n", $defaults)
            'no final newline' = @("mode=dark`nflavor=soft`ninband=profile`nkeys=profile`nprompt=on", $defaults)
            'a sixth line' = @(((Mode 'dark') + "extra=1`n"), $defaults)
            'an empty sixth line' = @(((Mode 'dark') + "`n"), $defaults)
            'lines in another order' = @("flavor=soft`nmode=dark`ninband=profile`nkeys=profile`nprompt=on`n", $defaults)
            'an unknown value' = @((Mode 'sepia'), $defaults)
            'upper case' = @((Mode 'DARK'), $defaults)
            'a space after the value' = @("mode=dark `nflavor=soft`ninband=profile`nkeys=profile`nprompt=on`n", $defaults)
            'a carriage return in line 1' = @("mode=light`r`nflavor=vivid`ninband=always`nkeys=off`nprompt=off`n", $defaults)
            'a carriage return in line 2' = @("mode=light`nflavor=vivid`r`ninband=always`nkeys=off`nprompt=off`n", $defaults)
            'a carriage return in line 3' = @("mode=light`nflavor=vivid`ninband=always`r`nkeys=off`nprompt=off`n", $defaults)
            'a carriage return in line 4' = @("mode=light`nflavor=vivid`ninband=always`nkeys=off`r`nprompt=off`n", $defaults)
            'a carriage return in line 5' = @("mode=light`nflavor=vivid`ninband=always`nkeys=off`nprompt=off`r`n", $defaults)
            'an empty file' = @('', $defaults)
            'four lines' = @("mode=light`nflavor=vivid`ninband=always`nkeys=off`n", $defaults)
            'an unknown flavour' = @((Mode 'light' 'loud'), $defaults)
            'a space after the equals sign' = @("mode= light`nflavor=vivid`ninband=always`nkeys=off`nprompt=off`n", $defaults)
            'a valid file again' = @((Mode 'light' 'vivid' 'always' 'off' 'off'), 'light vivid always off off')
        }
        foreach ($case in $files.Keys) {
            [IO.File]::WriteAllText($T.ModeFile, $files[$case][0])
            & $global:__sereno['Read']
            CheckEqual ($global:__sereno['Settings'] -join ' ') $files[$case][1] "mode file with $case"
        }
        # R11: the file is ASCII. Saved by an editor with a byte order mark,
        # or as UTF-16, it is malformed for sereno itself and for bash, which
        # read its bytes; it must be malformed here too, or a PowerShell
        # session would paint one variant while the others assume another.
        $valid = Mode 'light' 'vivid' 'always' 'off' 'off'
        $encodings = [ordered]@{
            'UTF-8 with a byte order mark' = (New-Object System.Text.UTF8Encoding $true)
            'UTF-16 with a byte order mark' = (New-Object System.Text.UnicodeEncoding $false, $true)
            'UTF-16 big endian with a byte order mark' = (New-Object System.Text.UnicodeEncoding $true, $true)
            'UTF-16 without a byte order mark' = (New-Object System.Text.UnicodeEncoding $false, $false)
            'UTF-32 with a byte order mark' = (New-Object System.Text.UTF32Encoding $false, $true)
        }
        foreach ($case in $encodings.Keys) {
            [IO.File]::WriteAllText($T.ModeFile, $valid, $encodings[$case])
            & $global:__sereno['Read']
            CheckEqual ($global:__sereno['Settings'] -join ' ') $defaults "mode file saved as $case"
        }
        Remove-Item -LiteralPath $T.ModeFile -Force
        & $global:__sereno['Read']
        CheckEqual ($global:__sereno['Settings'] -join ' ') $defaults 'no mode file'
        $null = New-Item -ItemType Directory -Path $T.ModeFile
        & $global:__sereno['Read']
        CheckEqual ($global:__sereno['Settings'] -join ' ') $defaults 'a folder where the mode file should be'
        Remove-Item -LiteralPath $T.ModeFile -Force
        Check ($Error.Count -eq 0) 'none of these files made init.ps1 fail' ($(if ($Error.Count) { [string]$Error[0] }))

        # R25: a background becomes the foreground of the same slot.
        $fix = $global:__sereno['Foreground']
        $samples = Samples
        $wrong = @(foreach ($sample in $samples.Keys) { $got = & $fix ($E + $sample); if ($got -cne ($E + $samples[$sample])) { "$sample -> " + (Show $got) } })
        Check ($wrong.Count -eq 0) 'background parameters 40-47 and 100-107 become foregrounds; colour arguments of 38 and 48 are left alone' ($wrong -join ', ')
        CheckEqual (& $fix ($E + '[44;1m' + $E + '[41m')) ($E + '[34;1m' + $E + '[31m') 'every sequence of a value is fixed'
        CheckEqual (& $fix 'plain') 'plain' 'a value without a sequence is left alone'
    }

    # -----------------------------------------------------------------------
    Invoke-Session 'hook session in Windows Terminal' @{ WT_SESSION = 'test-session'; SERENO_OS_MODE = 'light' } $defaultMode {
        # Inside Windows Terminal the colour a console names as its
        # background says nothing about the screen, and is not looked at.
        if ($T.Windows) { $Host.UI.RawUI.BackgroundColor = 'DarkMagenta' }
        $before = Get-PSReadLineOption
        $bell = [string]$before.BellStyle; $moves = $before.HistorySearchCursorMovesToEnd
        $tab = [string](Get-PSReadLineKeyHandler | Where-Object { $_.Key -eq 'Tab' } | Select-Object -First 1).Function
        . $T.Init
        $options = Get-PSReadLineOption
        CheckEqual ([string]$options.CommandColor) ($E + '[34m') 'a hook session gets the colours of the line editor too'
        Check (([string]$options.BellStyle -eq $bell) -and ($options.HistorySearchCursorMovesToEnd -eq $moves)) 'keys=profile outside the Serena profile: the options of the comfort set are untouched'
        CheckEqual ([string](Get-PSReadLineKeyHandler | Where-Object { $_.Key -eq 'Tab' } | Select-Object -First 1).Function) $tab 'keys=profile outside the Serena profile: Tab is untouched'
        $run = Capture { prompt; prompt }
        CheckEqual $run.Console '' 'inband=profile outside the Serena profile: nothing is painted'
        Check ($run.Output[0].StartsWith($E + ']133;A' + $BEL) -and $run.Output[1].StartsWith($E + ']133;D;0' + $BEL)) 'the prompt carries the marks in any Windows Terminal session'
        Set-Mode (Mode 'auto' 'soft' 'always')
        CheckEqual (Capture { prompt }).Console (Palette 'light') 'the mode file turns to inband=always: this session paints from the next prompt on'
    }

    # The console of a session of these tests is one of Windows itself, without
    # a window, and it names a background like any other: the one Windows
    # gives a console of that program (DarkMagenta for powershell.exe on the
    # maintainer's machine, where the window of Windows PowerShell is blue).
    # A session whose result depends on it sets it first; only that console,
    # which ends with the session, is changed.
    Invoke-Session 'session outside Windows Terminal' @{ SERENO_SESSION = 'wt-profile'; SERENO_OS_MODE = 'light' } $defaultMode {
        if ($T.Windows) { $Host.UI.RawUI.BackgroundColor = 'Black' }
        . $T.Init
        Go 'repo'
        $run = Capture { prompt; Write-Error 'failed on purpose' -ErrorAction SilentlyContinue; prompt }
        CheckEqual $run.Console '' 'without WT_SESSION nothing is painted'
        CheckEqual $run.Output[0] (Plain ('~' + $T.Sep + 'repo') 'feature/x') 'without WT_SESSION the prompt has no marks'
        CheckEqual $run.Output[1] (Plain ('~' + $T.Sep + 'repo') 'feature/x' -ok $false) 'without WT_SESSION the failed prompt has no marks either'
        CheckEqual ([string](Get-PSReadLineOption).KeywordColor) ($E + '[35m') 'in a console with a black background the line editor has the colours of R24'
    }

    # R24 and R26: a console window of Windows itself whose background is not
    # black. The window of Windows PowerShell is the known one: entry 5 of
    # its colour table is its background, so ESC[35m is text in the colour it
    # stands on, and ESC[34m is dark blue on dark blue. There a slot colour
    # is one entry of the table on another, and init.ps1 writes none.
    #
    # R25 (b) is part of that: the styles of $PSStyle that paint a background
    # stay as they are. The one PowerShell 7 ships with, bold on blue for
    # directories, would become blue text on the background of the console.
    # Under PowerShell 7 the $PSStyle of the session is looked at, with
    # styles as a profile may have left them; Windows PowerShell has none,
    # and one session gives it an object of the same shape (SERENO_T_STYLE).
    if ($onWindows) {
        $plainConsole = {
            $Host.UI.RawUI.BackgroundColor = $env:SERENO_T_BACKGROUND
            CheckEqual ([string]$Host.UI.RawUI.BackgroundColor) $env:SERENO_T_BACKGROUND 'the console of this session names the background it was given'
            $styled = (Test-Path variable:PSStyle)
            if ((-not $styled) -and ($env:SERENO_T_STYLE -ceq 'simulated')) {
                $global:PSStyle = [pscustomobject]@{
                    Progress = [pscustomobject]@{ Style = '' }
                    Formatting = [pscustomobject]@{ TableHeader = '' }
                    FileInfo = [pscustomobject]@{ Directory = $E + '[44;1m'; SymbolicLink = ''; Executable = ''; Extension = (New-Object 'System.Collections.Generic.Dictionary[string,string]') }
                }
                $styled = $true
            }
            $styles = { '' }
            if ($styled) {
                $PSStyle.Progress.Style = $E + '[1;103m'
                $PSStyle.Formatting.TableHeader = $E + '[102;30m'
                $PSStyle.FileInfo.SymbolicLink = $E + '[48;5;44;41m'
                $PSStyle.FileInfo.Executable = $E + '[01;044m'
                $PSStyle.FileInfo.Extension['.odd'] = $E + '[41;97m'
                $styles = { Show (@($PSStyle.Progress.Style, $PSStyle.Formatting.TableHeader, $PSStyle.FileInfo.Directory, $PSStyle.FileInfo.SymbolicLink, $PSStyle.FileInfo.Executable, $PSStyle.FileInfo.Extension['.odd']) -join ' ') }
            }
            $stylesBefore = & $styles
            $names = 'CommandColor', 'KeywordColor', 'StringColor', 'NumberColor', 'TypeColor', 'MemberColor', 'VariableColor', 'ParameterColor', 'OperatorColor',
                'CommentColor', 'ErrorColor', 'EmphasisColor', 'SelectionColor', 'DefaultTokenColor', 'ContinuationPromptColor'
            $colours = { $options = Get-PSReadLineOption; @(foreach ($name in $names) { $name + '=' + (Show ([string]$options.$name)) }) -join ' ' }
            $before = & $colours
            $run = Capture { . $T.Init }
            Check ($run.Console -eq '' -and $run.Output.Count -eq 0 -and $Error.Count -eq 0) 'init.ps1 prints nothing there and nothing in it fails' ($(if ($Error.Count) { [string]$Error[0] }))
            Check ($global:__sereno['Plain'] -and -not $global:__sereno['Terminal']) 'the session knows that it writes no colour'
            CheckEqual (& $colours) $before 'the line editor keeps the fifteen colours it had'
            if ($styled) {
                CheckEqual (& $styles) $stylesBefore 'a $PSStyle keeps every style it had, those that paint a background too'
                CheckEqual $PSStyle.FileInfo.Directory ($E + '[44;1m') 'and directories stay as PowerShell lists them, bold on a blue background'
            }
            Go 'repo'
            $shown = '~' + $T.Sep + 'repo feature/x'
            $run = Capture { prompt; Write-Error 'failed on purpose' -ErrorAction SilentlyContinue; prompt }
            CheckEqual $run.Output[0] ($shown + $LF + '> ') 'the prompt is the text of R26 without a colour'
            CheckEqual $run.Output[1] ($shown + $LF + '> ') 'and the same text after a failed command'
            [pscustomobject]@{ CommandLine = 'wait'; ExecutionStatus = 'Completed'; StartExecutionTime = [datetime]'2026-01-01T10:00:00'; EndExecutionTime = [datetime]'2026-01-01T10:00:02.34' } | Add-History
            $took = Capture { prompt }
            CheckEqual $took.Text ($shown + ' 2.3s' + $LF + '> ') 'the duration is text without a colour too'
            $global:NestedPromptLevel = 1
            $nested = Capture { prompt }
            $global:NestedPromptLevel = 0
            CheckEqual $nested.Text ($shown + $LF + '>> ') 'a nested prompt shows >>'
            Check (-not ($run.Text + $took.Text + $nested.Text).Contains($E)) 'no prompt of this session holds an escape character'
            CheckEqual ($run.Console + $took.Console) '' 'and nothing is painted'
            if (-not (Test-Path variable:PSStyle)) {
                Check (($Host.PrivateData.ProgressForegroundColor -eq $Host.UI.RawUI.ForegroundColor) -and ([string]$Host.PrivateData.ProgressBackgroundColor -eq $env:SERENO_T_BACKGROUND)) 'the progress bar has the colours of this console, as R25 says' "$($Host.PrivateData.ProgressForegroundColor) on $($Host.PrivateData.ProgressBackgroundColor)"
            }
            $Error.Clear()
        }
        Invoke-Session 'console of Windows with the background of Windows PowerShell' @{ SERENO_OS_MODE = 'dark'; SERENO_T_BACKGROUND = 'DarkMagenta' } $defaultMode $plainConsole
        Invoke-Session 'console of Windows with a light background' @{ SERENO_SESSION = 'wt-profile'; SERENO_OS_MODE = 'light'; SERENO_T_BACKGROUND = 'White' } "mode=auto`nflavor=soft`ninband=always`nkeys=always`nprompt=on`n" $plainConsole
        if (-not (Test-Path variable:PSStyle)) {
            Invoke-Session 'console of Windows with the background of Windows PowerShell and a $PSStyle (simulated)' @{ SERENO_OS_MODE = 'dark'; SERENO_T_BACKGROUND = 'DarkMagenta'; SERENO_T_STYLE = 'simulated' } $defaultMode $plainConsole
        }
    }

    Invoke-Session 'terminal of VS Code' @{ WT_SESSION = 'test-session'; SERENO_SESSION = 'wt-profile'; SERENO_OS_MODE = 'light'; TERM_PROGRAM = 'vscode' } $defaultMode {
        . $T.Init
        $run = Capture { prompt; prompt }
        CheckEqual $run.Console '' 'TERM_PROGRAM=vscode: nothing is painted, Serena Theme owns the palette there'
        CheckEqual $run.Output[1] (Plain ('~' + $T.Sep + 'work')) 'TERM_PROGRAM=vscode: the prompt has no marks'
    }

    # VS Code started from the desktop has no WT_SESSION. Its terminal has a
    # palette of its own, whatever its console names as a background.
    Invoke-Session 'terminal of VS Code without WT_SESSION' @{ SERENO_OS_MODE = 'dark'; TERM_PROGRAM = 'vscode' } $defaultMode {
        if ($T.Windows) { $Host.UI.RawUI.BackgroundColor = 'DarkMagenta' }
        . $T.Init
        CheckEqual ([string](Get-PSReadLineOption).KeywordColor) ($E + '[35m') 'TERM_PROGRAM=vscode alone: the line editor has the colours of R24'
        CheckEqual (Capture { prompt }).Text (Plain ('~' + $T.Sep + 'work')) 'TERM_PROGRAM=vscode alone: the prompt has its colours and no marks'
    }

    # R25: a session that has no console any more has no colours to hand to
    # the progress bar. Nothing may fail there either: a failure that is
    # caught is still an entry in $Error. The session gives its console away
    # with a function of Windows, which it declares with Add-Type; its check
    # lines go on through the pipe it was started with.
    if ($onWindows) {
        Invoke-Session 'session without a console' @{ SERENO_OS_MODE = 'dark' } $defaultMode {
            Add-Type -Namespace SerenoTest -Name Native -MemberDefinition '[DllImport("kernel32.dll")] public static extern bool FreeConsole();'
            $progress = "$($Host.PrivateData.ProgressForegroundColor) on $($Host.PrivateData.ProgressBackgroundColor)"
            Check ([SerenoTest.Native]::FreeConsole()) 'the session gave its console away'
            $colour = 'unread'
            try { $colour = $Host.UI.RawUI.BackgroundColor } catch { $colour = $null }
            Check ($null -eq $colour) 'and the host no longer names a background'
            $Error.Clear()
            $run = Capture { . $T.Init }
            Check ($run.Console -eq '' -and $run.Output.Count -eq 0) 'without a console init.ps1 prints nothing'
            Check ($Error.Count -eq 0) 'and leaves $Error empty' ($(if ($Error.Count) { ([string]$Error[0]).Replace("`n", ' ') }))
            $Error.Clear()
            CheckEqual "$($Host.PrivateData.ProgressForegroundColor) on $($Host.PrivateData.ProgressBackgroundColor)" $progress 'the progress bar keeps the colours it had'
            CheckEqual (Capture { prompt }).Text (Plain ('~' + $T.Sep + 'work')) 'and the prompt is the one of any session'
            Check ($Error.Count -eq 0) 'which fails nowhere either' ($(if ($Error.Count) { ([string]$Error[0]).Replace("`n", ' ') }))
        } $false
    }

    Invoke-Session 'session recognised by WT_PROFILE_ID' @{ WT_SESSION = 'test-session'; WT_PROFILE_ID = '{6B2D8B49-31D3-5DEA-9087-93C26E1A033E}'; SERENO_OS_MODE = 'hc' } $defaultMode {
        . $T.Init
        CheckEqual (Capture { prompt }).Console (Palette 'hc') 'WT_PROFILE_ID with the GUID of the profile, in capitals, makes a Serena profile session'
        CheckEqual ([string](Get-PSReadLineKeyHandler | Where-Object { $_.Key -eq 'Tab' } | Select-Object -First 1).Function) 'MenuComplete' 'and it gets the comfort set'
    }

    Invoke-Session 'another profile of Windows Terminal' @{ WT_SESSION = 'test-session'; WT_PROFILE_ID = '{61c54bbd-c2c6-5271-96e7-009a87ff44bf}'; SERENO_OS_MODE = 'hc' } $defaultMode {
        . $T.Init
        CheckEqual (Capture { prompt }).Console '' 'another WT_PROFILE_ID is not a Serena profile session'
    }

    Invoke-Session 'keys=always and inband=always' @{ WT_SESSION = 'test-session'; SERENO_OS_MODE = 'dark' } "mode=light`nflavor=vivid`ninband=always`nkeys=always`nprompt=on`n" {
        . $T.Init
        $options = Get-PSReadLineOption
        Check (([string]$options.BellStyle -eq 'None') -and $options.HistorySearchCursorMovesToEnd) 'keys=always: the comfort set outside the Serena profile'
        CheckEqual ([string](Get-PSReadLineKeyHandler | Where-Object { $_.Key -eq 'UpArrow' } | Select-Object -First 1).Function) 'HistorySearchBackward' 'keys=always: UpArrow searches the history'
        CheckEqual (Capture { prompt }).Console (Palette 'light-vivid') 'inband=always with a fixed mode: light-vivid although the system is dark'
    }

    Invoke-Session 'keys=off and prompt=off' $profileSession "mode=auto`nflavor=soft`ninband=profile`nkeys=off`nprompt=off`n" {
        $tab = [string](Get-PSReadLineKeyHandler | Where-Object { $_.Key -eq 'Tab' } | Select-Object -First 1).Function
        $stockText = prompt
        . $T.Init
        CheckEqual ([string](Get-PSReadLineKeyHandler | Where-Object { $_.Key -eq 'Tab' } | Select-Object -First 1).Function) $tab 'keys=off: no comfort set even in the Serena profile'
        CheckEqual ([string](Get-PSReadLineOption).CommandColor) ($E + '[34m') 'keys=off: the colours are set all the same'
        $run = Capture { prompt }
        CheckEqual $run.Text $stockText 'prompt=off: the stock prompt stays, and returns what it returned before'
        CheckEqual $run.Console (Palette 'dark') 'prompt=off: the stock prompt is wrapped, so the session still follows the system'
    }

    # R27: History predictions are switched on where PSReadLine has them,
    # they are off, and PSReadLine can show them. Current versions start with
    # predictions on wherever they can draw them, so they are switched off
    # here first. PSReadLine is the judge of what it takes: it is asked
    # directly after init.ps1 has run. Outside Windows it always takes them.
    # On Windows it does not in a session whose output is redirected, like
    # the first of these two, and does in a console, like the second.
    $predictions = {
        [Console]::Out.WriteLine('# the output of this session is ' + $(if ([Console]::IsOutputRedirected) { 'redirected' } else { 'a console' }) + '; the host says virtual terminal sequences are ' + $(if ($Host.UI.SupportsVirtualTerminal) { 'supported' } else { 'not supported' }))
        $has = (Get-Command Set-PSReadLineOption).Parameters.ContainsKey('PredictionSource')
        if ($has) { Set-PSReadLineOption -PredictionSource None }
        $run = Capture { . $T.Init }
        Check ($run.Console -eq '' -and $run.Output.Count -eq 0 -and $Error.Count -eq 0) 'with predictions off, init.ps1 prints nothing and nothing in it fails' ($(if ($Error.Count) { [string]$Error[0] }))
        if (-not $has) {
            Check (-not (Get-PSReadLineOption).PSObject.Properties['PredictionSource']) "PSReadLine $((Get-Module PSReadLine).Version) has no predictions: there is nothing to switch on"
        } else {
            $after = [string](Get-PSReadLineOption).PredictionSource
            $taken = $true
            try { Set-PSReadLineOption -PredictionSource History } catch { $taken = $false }
            if ($taken) { CheckEqual $after 'History' 'PSReadLine takes predictions here, and init.ps1 switched History on' }
            else { CheckEqual $after 'None' 'PSReadLine refuses predictions here, and init.ps1 left them off without asking' }
        }
        CheckEqual ([string](Get-PSReadLineOption).BellStyle) 'None' 'the rest of the comfort set is there either way'
        CheckEqual ([string](Get-PSReadLineOption).CommandColor) ($E + '[34m') 'and so are the colours of the line editor'
    }
    Invoke-Session 'predictions switched off before init.ps1' $profileSession $defaultMode $predictions
    if ($onWindows) { Invoke-Session 'predictions switched off before init.ps1, in a console of its own' $profileSession $defaultMode $predictions $true $true }

    Invoke-Session 'a prompt of the user in Windows Terminal' $profileSession $defaultMode {
        function global:prompt { $status = $?; "[$status] mine> "; 'second object' }
        $mine = $function:prompt
        . $T.Init
        Check ([object]::ReferenceEquals($global:__sereno['Saved'], $mine)) 'a prompt that is not the stock one is kept as it is'
        $run = Capture { $null = 1; prompt }
        CheckEqual $run.Console (Palette 'dark') 'the wrapped prompt follows the system'
        Check ($run.Output.Count -eq 2 -and $run.Output[0] -ceq '[True] mine> ' -and $run.Output[1] -ceq 'second object') "the wrapper returns the user's output unchanged, object by object" (Show ($run.Output -join '|'))
        $run = Capture { Write-Error 'failed on purpose' -ErrorAction SilentlyContinue; prompt }
        CheckEqual $run.Output[0] '[False] mine> ' "the user's prompt still sees the `$? of the last command"
        CheckEqual $run.Console '' 'and nothing is painted when the variant did not change'
    }

    Invoke-Session 'a prompt of the user outside Windows Terminal' @{ SERENO_SESSION = 'wt-profile' } $defaultMode {
        function global:prompt { 'mine> ' }
        $mine = $function:prompt
        . $T.Init
        Check ([object]::ReferenceEquals($function:prompt, $mine) -and ($null -eq $global:__sereno['Saved'])) "outside Windows Terminal the user's prompt is not even wrapped"
        CheckEqual (prompt) 'mine> ' "and it answers as before"
    }

    Invoke-Session 'no PSReadLine' $profileSession $defaultMode {
        Check (-not (Get-Module PSReadLine)) 'this session has no PSReadLine, like a script run or a session with a screen reader'
        . $T.Init
        Check (-not (Get-Module PSReadLine)) 'init.ps1 did not load PSReadLine'
        Check ($Error.Count -eq 0) 'and nothing failed' ($(if ($Error.Count) { [string]$Error[0] }))
        $run = Capture { prompt }
        CheckEqual $run.Console (Palette 'dark') 'the prompt and the follow check work without PSReadLine'
    } $false

    Invoke-Session 'no mode file' $profileSession '<none>' {
        . $T.Init
        CheckEqual ($global:__sereno['Settings'] -join ' ') 'auto soft profile profile on' 'without a mode file the settings are those of a first apply'
        CheckEqual (Capture { prompt }).Console (Palette 'dark') 'and the session follows the system'
    }

    # What an uninstall leaves of a session that is open. The session
    # keeps what it has loaded, the prompt and the colours of the line
    # editor. Its palette goes by the settings of a first apply from the
    # next prompt on, because the mode file is gone and a missing file means
    # those (R11): a Serena tab that ran with the flavour vivid turns to the
    # soft variant, and a session of another profile that was painted under
    # inband=always hands the palette back, once.
    Invoke-Session 'the mode file goes while a Serena tab is open' $profileSession "mode=auto`nflavor=vivid`ninband=profile`nkeys=profile`nprompt=on`n" {
        . $T.Init
        CheckEqual (Capture { prompt }).Console (Palette 'dark-vivid') 'the tab starts on the vivid variant'
        [IO.File]::Delete($T.ModeFile)
        $next = Capture { prompt }
        CheckEqual $next.Console (Palette 'dark') 'without the mode file the next prompt paints what auto and soft give'
        CheckEqual ($global:__sereno['Settings'] -join ' ') 'auto soft profile profile on' 'and the settings are those of a first apply'
        Check ($next.Text.Contains((Plain ('~' + $T.Sep + 'work')))) 'the Serena prompt stays' (Show $next.Text)
        CheckEqual ([string](Get-PSReadLineOption).CommandColor) ($E + '[34m') 'and so do the colours of the line editor'
        CheckEqual (Capture { prompt }).Console '' 'nothing more is painted at the prompt after it'
        Check ($Error.Count -eq 0) 'and nothing failed' ($(if ($Error.Count) { [string]$Error[0] }))
    }

    Invoke-Session 'the mode file goes while a session of another profile is open' @{ WT_SESSION = 'test-session'; SERENO_OS_MODE = 'dark' } "mode=auto`nflavor=soft`ninband=always`nkeys=profile`nprompt=on`n" {
        . $T.Init
        CheckEqual (Capture { prompt }).Console (Palette 'dark') 'under inband=always the session of another profile is painted'
        [IO.File]::Delete($T.ModeFile)
        $next = Capture { prompt }
        CheckEqual $next.Console (Palette 'reset') 'without the mode file the next prompt hands the palette back with RESET'
        Check ($next.Text.Contains((Plain ('~' + $T.Sep + 'work')))) 'the Serena prompt stays' (Show $next.Text)
        CheckEqual (Capture { prompt }).Console '' 'nothing more is written at the prompt after it'
        Check ($Error.Count -eq 0) 'and nothing failed' ($(if ($Error.Count) { [string]$Error[0] }))
    }

    # R30: a changed mode file that cannot be read at one prompt (another
    # program holds it at that moment) is read at the next one.
    Invoke-Session 'mode file held by another program' $profileSession $defaultMode {
        if (-not $T.Windows) { Skip 'files held by another program: only Windows keeps a reader out of a file for certain'; return }
        . $T.Init
        CheckEqual (Capture { prompt }).Console (Palette 'dark') 'the session starts on dark'
        Set-Mode (Mode 'light' 'soft' 'always')
        $lock = [IO.File]::Open($T.ModeFile, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        try { $held = Capture { prompt } } finally { $lock.Dispose() }
        CheckEqual $held.Console '' 'while another program holds the changed mode file, the prompt paints nothing'
        Check ($held.Text.Contains($E + '[32m>')) 'and the prompt itself is shown all the same' (Show $held.Text)
        CheckEqual ($global:__sereno['Settings'] -join ' ') 'auto soft profile profile on' 'the settings read before stay in force meanwhile'
        CheckEqual (Capture { prompt }).Console (Palette 'light') 'the first prompt after that reads the file and paints what it says'
        CheckEqual ($global:__sereno['Settings'] -join ' ') 'light soft always profile on' 'and the settings are those of the file'
        CheckEqual (Capture { prompt }).Console '' 'and nothing is written at the prompt after it'

        # R26: the same for the file the branch is read from. With the
        # preference of a careful profile, one failure would end the prompt.
        Go 'repo'
        $ErrorActionPreference = 'Stop'
        $marks = $E + ']133;D;0' + $BEL + $E + ']133;A' + $BEL + $E + ']9;9;"' + (Get-Location).ProviderPath + '"' + $BEL
        $endMark = $E + ']133;B' + $BEL
        $shown = '~' + $T.Sep + 'repo'
        CheckEqual (Capture { prompt }).Text ($marks + (Plain $shown 'feature/x') + $endMark) 'in a repository the prompt shows the branch'
        $lock = [IO.File]::Open((Join-Path (Get-Location).ProviderPath '.git\HEAD'), [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        $held = $null; $failure = ''
        try { $held = Capture { prompt } } catch { $failure = [string]$_ } finally { $lock.Dispose() }
        Check ($failure -eq '') 'while another program holds HEAD the prompt does not fail' $failure
        if ($held) { CheckEqual $held.Text ($marks + (Plain $shown) + $endMark) 'it is the prompt without the branch' }
        CheckEqual (Capture { prompt }).Text ($marks + (Plain $shown 'feature/x') + $endMark) 'and the next prompt shows the branch again'
    }

    # R23: prints nothing, in any session. Where an application control
    # policy puts PowerShell into ConstrainedLanguage mode, methods of .NET
    # types cannot be called, which is what init.ps1 is made of; it must then
    # leave the session alone instead of failing at every start and at every
    # prompt. The mode is set for this child process only: no policy and no
    # setting of the machine is involved. From that line on this script is
    # constrained too, so it reports with plain strings.
    Invoke-Session 'session in ConstrainedLanguage mode' $profileSession $defaultMode {
        $ErrorActionPreference = 'Continue'
        $stock = $function:prompt.ToString()
        $shown = prompt
        $errorsBefore = $Error.Count
        $ExecutionContext.SessionState.LanguageMode = 'ConstrainedLanguage'
        $output = @(. $T.Init)
        if ([string]$ExecutionContext.SessionState.LanguageMode -ceq 'ConstrainedLanguage') { 'ok - the session is in ConstrainedLanguage mode' } else { 'not ok - the session is in ConstrainedLanguage mode' }
        if ($Error.Count -eq $errorsBefore) { 'ok - in ConstrainedLanguage mode init.ps1 fails nowhere' } else { 'not ok - in ConstrainedLanguage mode init.ps1 fails nowhere'; '#   ' + ([string]$Error[0]).Replace("`n", ' ') }
        if ($output.Count -eq 0) { 'ok - and returns nothing' } else { 'not ok - and returns nothing' }
        if (-not (Test-Path variable:global:__sereno)) { 'ok - and keeps nothing for the session' } else { 'not ok - and keeps nothing for the session' }
        if ($function:prompt.ToString() -ceq $stock) { 'ok - and leaves the prompt alone' } else { 'not ok - and leaves the prompt alone' }
        $again = $null
        try { $again = prompt } catch { $again = 'the prompt failed: ' + [string]$_ }
        if ($again -ceq $shown) { 'ok - which still answers as before' } else { 'not ok - which still answers as before'; '#   ' + $again }
        if (-not (Test-Path function:sereno)) { 'ok - and defines no function' } else { 'not ok - and defines no function' }
    }

    Invoke-Session 'caller without strict mode' $profileSession $defaultMode {
        Set-StrictMode -Off
        $ErrorActionPreference = 'Continue'
        $run = Capture { . $T.Init }
        Check ($run.Console -eq '' -and $run.Output.Count -eq 0 -and $Error.Count -eq 0) 'init.ps1 is as quiet for a caller without strict mode'
        CheckEqual ([string]$ErrorActionPreference) 'Continue' 'and leaves its $ErrorActionPreference alone'
        Check ((Capture { prompt }).Text.Contains($E + '[32m>')) 'and the prompt works'
    }

    if (-not (Test-Path variable:PSStyle)) {
        Invoke-Session 'PowerShell with $PSStyle (simulated)' @{ SERENO_OS_MODE = 'dark' } $defaultMode {
            # Windows PowerShell has no $PSStyle. An object of the same
            # shape lets the rule of R25 for PowerShell 7 run here too. The
            # rule is not for a plain console, so the console of this
            # session is given a black background first.
            if ($T.Windows) { $Host.UI.RawUI.BackgroundColor = 'Black' }
            $extension = New-Object 'System.Collections.Generic.Dictionary[string,string]'
            $extension['.zip'] = $E + '[31;1m'; $extension['.odd'] = $E + '[41;97m'
            $global:PSStyle = [pscustomobject]@{
                Progress = [pscustomobject]@{ Style = $E + '[33;1m'; MaxWidth = 120 }
                Formatting = [pscustomobject]@{ FormatAccent = $E + '[32;1m'; TableHeader = $E + '[102;30m'; Error = $E + '[31;1m'; Odd = 7 }
                FileInfo = [pscustomobject]@{ Directory = $E + '[44;1m'; SymbolicLink = $E + '[36;1m'; Executable = $E + '[32;1m'; Extension = $extension }
            }
            # Every sample of R25 as one more value of Formatting: a style is
            # handed on to be rewritten only when it looks like it holds a
            # background, and none that does may be passed over.
            $samples = Samples
            $number = 0
            foreach ($sample in $samples.Keys) { $global:PSStyle.Formatting | Add-Member -NotePropertyName "Sample$number" -NotePropertyValue ($E + $sample); $number++ }
            $progress = "$($Host.PrivateData.ProgressForegroundColor) $($Host.PrivateData.ProgressBackgroundColor)"
            . $T.Init
            $number = 0
            $wrong = @(foreach ($sample in $samples.Keys) { $got = $PSStyle.Formatting."Sample$number"; $number++; if ($got -cne ($E + $samples[$sample])) { "$sample -> " + (Show $got) } })
            Check ($number -gt 10 -and $wrong.Count -eq 0) 'as values of $PSStyle.Formatting the samples of R25 come out as the rule says' ($wrong -join ', ')
            CheckEqual $PSStyle.FileInfo.Directory ($E + '[34;1m') 'FileInfo.Directory loses its blue background and becomes blue text'
            CheckEqual $PSStyle.Formatting.TableHeader ($E + '[92;30m') 'every Formatting value is looked at'
            CheckEqual $PSStyle.FileInfo.Extension['.odd'] ($E + '[31;97m') 'every FileInfo.Extension entry is looked at'
            CheckEqual ($PSStyle.Progress.Style + $PSStyle.Formatting.FormatAccent + $PSStyle.Formatting.Error + $PSStyle.FileInfo.SymbolicLink + $PSStyle.FileInfo.Executable + $PSStyle.FileInfo.Extension['.zip']) ($E + '[33;1m' + $E + '[32;1m' + $E + '[31;1m' + $E + '[36;1m' + $E + '[32;1m' + $E + '[31;1m') 'values without a background are left as they are'
            Check ($PSStyle.Formatting.Odd -eq 7 -and $PSStyle.Progress.MaxWidth -eq 120) 'members that are not styles are left alone'
            CheckEqual "$($Host.PrivateData.ProgressForegroundColor) $($Host.PrivateData.ProgressBackgroundColor)" $progress 'where $PSStyle exists the host colours of the progress bar are not touched'
            Check ($Error.Count -eq 0) 'and nothing failed' ($(if ($Error.Count) { [string]$Error[0] }))
        }
    }

    if (Test-Path variable:PSStyle) {
        Invoke-Session 'a $PSStyle with styles of the user' @{ SERENO_OS_MODE = 'dark' } $defaultMode {
            # The $PSStyle of this PowerShell, as a profile may have left it
            # before init.ps1 runs: backgrounds in each of the places R25
            # names. Only this process is changed. The rule is not for a
            # plain console, so on Windows the console of this session is
            # given a black background first.
            if ($T.Windows) { $Host.UI.RawUI.BackgroundColor = 'Black' }
            CheckEqual $PSStyle.FileInfo.Directory ($E + '[44;1m') 'PowerShell lists directories bold on a blue background: the one style init.ps1 knows as it stands'
            $PSStyle.Progress.Style = $E + '[1;103m'
            $PSStyle.Formatting.TableHeader = $E + '[102;30m'
            $PSStyle.Formatting.Warning = $E + '[38;5;44m'
            $PSStyle.FileInfo.SymbolicLink = $E + '[48;5;44;41m'
            $PSStyle.FileInfo.Executable = $E + '[01;044m'
            $PSStyle.FileInfo.Extension['.odd'] = $E + '[41;97m'
            $zip = $PSStyle.FileInfo.Extension['.zip']
            # Every sample of R25 as one more entry of FileInfo.Extension:
            # the walk through the real $PSStyle hands a style on to be
            # rewritten only when it looks like it holds a background, and
            # none that does may be passed over, the ends of the two ranges
            # included. PowerShell takes a style only when each sequence in
            # it has a number, which leaves out the one sample that has none.
            $samples = Samples
            $entries = [ordered]@{}
            $number = 0
            foreach ($sample in $samples.Keys) {
                if ($sample -cmatch '\A\[[0-9]+(?:;[0-9]+)*m\z') { $PSStyle.FileInfo.Extension[".sample$number"] = $E + $sample; $entries[".sample$number"] = $E + $samples[$sample] }
                $number++
            }
            . $T.Init
            $wrong = @(foreach ($entry in $entries.Keys) { $got = $PSStyle.FileInfo.Extension[$entry]; if ($got -cne $entries[$entry]) { "$entry -> " + (Show $got) } })
            Check ($entries.Count -ge 20 -and $wrong.Count -eq 0) 'as entries of $PSStyle.FileInfo.Extension the samples of R25 come out as the rule says' ("$($entries.Count) entries; " + ($wrong -join ', '))
            CheckEqual $PSStyle.Progress.Style ($E + '[1;93m') 'Progress.Style loses its background'
            CheckEqual $PSStyle.Formatting.TableHeader ($E + '[92;30m') 'a value of Formatting loses its background'
            CheckEqual $PSStyle.Formatting.Warning ($E + '[38;5;44m') 'a colour argument that only looks like a background is left alone'
            CheckEqual $PSStyle.FileInfo.SymbolicLink ($E + '[48;5;44;31m') 'FileInfo.SymbolicLink: the parameter after the colour arguments loses its background'
            CheckEqual $PSStyle.FileInfo.Executable ($E + '[01;34m') 'FileInfo.Executable: a background written with a leading zero is one too'
            CheckEqual $PSStyle.FileInfo.Extension['.odd'] ($E + '[31;97m') 'an entry of FileInfo.Extension loses its background'
            CheckEqual $PSStyle.FileInfo.Extension['.zip'] $zip 'an entry without a background is left as it is'
            CheckEqual $PSStyle.FileInfo.Directory ($E + '[34;1m') 'and the directories of PowerShell itself are blue text, as in any session'
            Check ($Error.Count -eq 0) 'and nothing failed' ($(if ($Error.Count) { [string]$Error[0] }))
        }
    }

    # R34: the sereno function.
    $binDir = Join-Path $root 'bin'
    $null = New-Item -ItemType Directory -Path $binDir -Force
    $binName = 'sereno'; if ($onWindows) { $binName = 'sereno.exe' }
    [IO.File]::WriteAllText((Join-Path $binDir $binName), '')
    Invoke-Session 'sereno in ROOT bin and not on PATH' @{ } $defaultMode {
        . $T.Init
        $name = 'sereno'; if ($T.Windows) { $name = 'sereno.exe' }
        Check (Test-Path function:sereno) 'init.ps1 defines the function sereno'
        CheckEqual $global:__sereno['Bin'] (Join-Path (Join-Path $T.Root 'bin') $name) 'and it calls the program in ROOT/bin'
        CheckEqual ($function:sereno.ToString().Trim()) '& $global:__sereno[''Bin''] @args' 'with the arguments it was given'
        CheckEqual $env:PATH (Join-Path $T.Tmp 'empty-path') 'PATH is not touched'
    }
    $pathDir = Join-Path $tmp 'on-path'
    $null = New-Item -ItemType Directory -Path $pathDir -Force
    $onPathName = 'sereno'; if ($onWindows) { $onPathName = 'sereno.cmd' }
    [IO.File]::WriteAllText((Join-Path $pathDir $onPathName), '')
    Invoke-Session 'sereno in ROOT bin and on PATH' @{ PATH = ($emptyPath + [IO.Path]::PathSeparator + $pathDir) } $defaultMode {
        . $T.Init
        Check (-not (Test-Path function:sereno)) 'a program called sereno is on PATH: init.ps1 defines no function'
    }
    Remove-Item -LiteralPath $binDir -Recurse -Force

    # R21: the line `sereno apply --hook` puts into a profile, with ROOT in
    # its default place.
    Invoke-Session 'the hook line of R21' $profileSession "mode=auto`nflavor=vivid`ninband=profile`nkeys=profile`nprompt=on`n" {
        $profileFile = Join-Path $T.Tmp 'profile.ps1'
        [IO.File]::WriteAllText($profileFile, "`$mine = 1`r`n" + $env:SERENO_T_HOOK + "`r`n")
        $writer = New-Object System.IO.StringWriter
        $console = [Console]::Out
        [Console]::SetOut($writer)
        try { $output = @(. $profileFile) } finally { [Console]::SetOut($console) }
        Check ($writer.ToString() -eq '' -and $output.Count -eq 0) 'the hook line of R21, in a profile, prints nothing'
        Check ((Test-Path variable:global:__sereno) -and ($global:__sereno['Settings'][1] -ceq 'vivid')) 'and loads init.ps1 from ROOT in its default place'
        CheckEqual (Capture { prompt }).Console (Palette 'dark-vivid') 'and the session behaves like any other'
        Check ($Error.Count -eq 0) 'and nothing failed' ($(if ($Error.Count) { [string]$Error[0] }))
    }

    # R21 and R23: the same line in a session that an application control
    # policy has constrained. The line looks for the file with a method of
    # .NET, and such a method cannot be called there: a line that tried would
    # print an error at every start of PowerShell. So it asks for the language
    # mode first, as init.ps1 does, and then does nothing. The profile is
    # written before the mode is set; from that line on this script is
    # constrained too, so it reports with plain strings, and anything the
    # line printed would be a line of this session that is not a check.
    Invoke-Session 'the hook line of R21 in ConstrainedLanguage mode' $profileSession $defaultMode {
        $ErrorActionPreference = 'Continue'
        $profileFile = Join-Path $T.Tmp 'profile-constrained.ps1'
        [IO.File]::WriteAllText($profileFile, $env:SERENO_T_HOOK + "`r`n")
        $there = [IO.File]::Exists($T.Init)
        $errorsBefore = $Error.Count
        $ExecutionContext.SessionState.LanguageMode = 'ConstrainedLanguage'
        $output = @(. $profileFile)
        if ($there) { 'ok - init.ps1 is where the hook line looks for it' } else { 'not ok - init.ps1 is where the hook line looks for it' }
        if ([string]$ExecutionContext.SessionState.LanguageMode -ceq 'ConstrainedLanguage') { 'ok - the session is in ConstrainedLanguage mode' } else { 'not ok - the session is in ConstrainedLanguage mode' }
        if ($Error.Count -eq $errorsBefore) { 'ok - in ConstrainedLanguage mode the hook line of R21 fails nowhere' } else { 'not ok - in ConstrainedLanguage mode the hook line of R21 fails nowhere'; '#   ' + ([string]$Error[0]).Replace("`n", ' ') }
        if ($output.Count -eq 0) { 'ok - and returns nothing' } else { 'not ok - and returns nothing' }
        if (-not (Test-Path variable:global:__sereno)) { 'ok - and does not load init.ps1' } else { 'not ok - and does not load init.ps1' }
    }

    # R21: the hook line loads no module. Test-Path, with which the line was
    # first written, is the first use of Microsoft.PowerShell.Management in a
    # session and loads that module before the first prompt; a method of .NET
    # loads nothing. The sessions of this file cannot show the difference:
    # their library calls Join-Path, a cmdlet of that module, before a body
    # runs. This one is a child with a script of its own, which names no
    # cmdlet of the module before it loads a profile that holds the line:
    # once with sereno's files where the line looks for them, once in a home
    # that has none.
    if (-not $Only -or 'the hook line of R21 loads no module'.Contains($Only)) {
        [Console]::Out.WriteLine('# session: the hook line of R21 loads no module')
        [IO.File]::WriteAllText($modeFile, $defaultMode)
        $hooked = Join-Path $sessionDir 'hooked-profile.ps1'
        [IO.File]::WriteAllText($hooked, $hookLine + "`r`n")
        $probe = Join-Path $sessionDir 'modules-probe.ps1'
        [IO.File]::WriteAllText($probe, @'
$modules = { $names = [string[]]@(Get-Module | ForEach-Object { $_.Name }); [Array]::Sort($names); $names -join ',' }
$before = & $modules
. $env:SERENO_T_PROFILE
$lines = @(('before=' + $before), ('after=' + (& $modules)))
$lines += 'sereno=' + ($null -ne $ExecutionContext.SessionState.PSVariable.GetValue('global:__sereno'))
$lines += 'errors=' + $Error.Count
[IO.File]::WriteAllLines($env:SERENO_T_REPORT, [string[]]$lines)
'@)
        $report = Join-Path $tmp 'modules-report.txt'
        $arguments = '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + $probe + '"'
        if (-not $onWindows) { $arguments = '-NoLogo -NoProfile -NonInteractive -File "' + $probe + '"' }
        $bare = Join-Path $tmp 'home-without-sereno'
        $null = New-Item -ItemType Directory -Path $bare -Force
        $places = [ordered]@{
            'with the files of sereno in place' = @(@{ }, 'True')
            'in a home without the files of sereno' = @(@{ HOME = $bare; USERPROFILE = $bare; LOCALAPPDATA = $bare }, 'False')
        }
        $told = $true
        foreach ($place in $places.Keys) {
            if (-not $told) { break }
            if (Test-Path -LiteralPath $report) { Remove-Item -LiteralPath $report -Force }
            $child = Start-Child $arguments ($profileSession + $places[$place][0] + @{ SERENO_T_PROFILE = $hooked; SERENO_T_REPORT = $report }) (Join-Path $userHome 'work')
            $found = @{ before = '<no report>'; after = '<no report>'; sereno = '<no report>'; errors = '<no report>' }
            if (Test-Path -LiteralPath $report) { foreach ($line in [IO.File]::ReadAllLines($report)) { $pair = $line.Split([char[]]'=', 2); if ($pair.Count -eq 2) { $found[$pair[0]] = $pair[1] } } }
            $loadedBefore = (',' + $found['before'] + ',').Contains(',Microsoft.PowerShell.Management,')
            if ($loadedBefore -and ($PSVersionTable.PSVersion.Major -gt 5)) {
                # Nothing can be told where the module is there from the start.
                Report "skip - the hook line of R21 loads no module: PowerShell $($PSVersionTable.PSVersion) has Microsoft.PowerShell.Management loaded before a profile runs ($($found['before']))"
                $told = $false
                continue
            }
            Check (-not $loadedBefore -and $found['before'] -ne '<no report>') "$place`: before the profile runs, the session has not loaded Microsoft.PowerShell.Management" ("loaded before: " + $found['before'])
            Check ($found['after'] -ceq $found['before']) "$place`: the profile with the hook line loads no module" ("loaded before: [" + $found['before'] + "], after: [" + $found['after'] + "]")
            Check (($found['sereno'] -ceq $places[$place][1]) -and ($found['errors'] -ceq '0')) "$place`: init.ps1 was loaded ($($places[$place][1])), and nothing failed" ("loaded: " + $found['sereno'] + ", errors: " + $found['errors'])
            Check ($child.Code -eq 0 -and $child.Err.Trim() -eq '' -and $child.Out.Trim() -eq '') "$place`: and the session prints nothing and ends without an error" ("exit code $($child.Code): " + $child.Err.Trim() + ' ' + $child.Out.Trim())
        }
    }

    # R23: only the console host.
    Invoke-Session 'a host that is not the console' @{ } $defaultMode {
        $other = [powershell]::Create()
        $null = $other.AddScript('. $args[0]; $Host.Name; [bool](Get-Variable -Name __sereno -Scope Global -ErrorAction SilentlyContinue); (Get-Command prompt).Definition.Contains("__sereno")').AddArgument($T.Init)
        $answer = @($other.Invoke())
        $other.Dispose()
        Check ($answer.Count -eq 3 -and $answer[0] -ne 'ConsoleHost' -and $answer[1] -eq $false -and $answer[2] -eq $false) 'in a host that is not ConsoleHost init.ps1 returns at once' ($answer -join ', ')
    }

    # The Serena profile itself: -NoExit -File init.ps1, as the fragment starts
    # it. With the input closed the host shows one prompt and ends.
    if (-not $Only -or 'the command line of the Serena profile'.Contains($Only)) {
        [Console]::Out.WriteLine('# session: the command line of the Serena profile')
        [IO.File]::WriteAllText($modeFile, $defaultMode)
        $arguments = '-NoLogo -NoProfile -NoExit -ExecutionPolicy Bypass -File "' + $initCopy + '"'
        if (-not $onWindows) { $arguments = '-NoLogo -NoProfile -NoExit -File "' + $initCopy + '"' }
        $work = Join-Path $userHome 'work'
        $environment = @{ WT_SESSION = 'test-session'; SERENO_SESSION = 'wt-profile'; SERENO_OS_MODE = 'light' }
        if (-not $onWindows) { $environment['PATH'] = $env:PATH }
        $child = Start-Child $arguments $environment $work
        $E = [string][char]27; $BEL = [string][char]7
        $palette = [IO.File]::ReadAllText((Join-Path $Golden 'palette/light.bin'))
        $shownPath = $work
        if ($onWindows) { $shownPath = '~' + $sep + 'work' }
        $prompt = $E + ']133;A' + $BEL + $E + ']9;9;"' + $work + '"' + $BEL + $E + '[34m' + $shownPath + $E + '[0m' + "`n" + $E + '[32m>' + $E + '[0m ' + $E + ']133;B' + $BEL
        $show = { param($value) $value.Replace($E, '<ESC>').Replace($BEL, '<BEL>').Replace("`n", '<LF>').Replace("`r", '<CR>') }
        Check ($child.Out.StartsWith($palette)) '-NoExit -File init.ps1: the first thing the session writes is PALETTE of the system''s variant' (& $show $child.Out.Substring(0, [Math]::Min(80, $child.Out.Length)))
        if ($onWindows -and $PSVersionTable.PSVersion.Major -le 5) {
            Check ($child.Out -ceq ($palette + $prompt)) '-NoExit -File init.ps1: then the Serena prompt, and nothing else' (& $show $child.Out.Substring([Math]::Min($palette.Length, $child.Out.Length)))
        } else {
            # PowerShell 7 does not take the home folder from the environment
            # everywhere, so there only the shape of the prompt is compared.
            Check ($child.Out.Contains($E + ']133;A' + $BEL) -and $child.Out.Contains($E + '[32m>' + $E + '[0m ' + $E + ']133;B' + $BEL)) '-NoExit -File init.ps1: then the Serena prompt' (& $show $child.Out)
        }
        Check ($child.Code -eq 0 -and $child.Err.Trim() -eq '') '-NoExit -File init.ps1: the session ends without an error when its input ends' ("exit code $($child.Code): " + $child.Err.Trim())
    }

    # The same start once more, with a file in front of init.ps1 that looks at
    # the session before and after it. In a session started with -NoExit
    # -File the host has loaded PSReadLine before the file runs, so the
    # colours of R24 and the comfort set of R27 are there at the first prompt.
    if ($onWindows -and (-not $Only -or 'the line editor in a session started from the Serena profile'.Contains($Only))) {
        [Console]::Out.WriteLine('# session: the line editor in a session started from the Serena profile')
        [IO.File]::WriteAllText($modeFile, $defaultMode)
        $work = Join-Path $userHome 'work'
        $report = Join-Path $tmp 'profile-report.txt'
        $probe = Join-Path $sessionDir 'profile-probe.ps1'
        [IO.File]::WriteAllText($probe, @'
$loaded = [bool](Get-Module PSReadLine)
if ($loaded) { Set-PSReadLineOption -HistorySaveStyle SaveNothing }
. $env:SERENO_T_INIT
$lines = @("loaded=$loaded")
if ($loaded) {
    $options = Get-PSReadLineOption
    $lines += 'command=' + ([string]$options.CommandColor).Replace([string][char]27, 'ESC')
    $lines += 'selection=' + ([string]$options.SelectionColor).Replace([string][char]27, 'ESC')
    $lines += 'bell=' + $options.BellStyle
    $lines += 'keys=' + ((Get-PSReadLineKeyHandler | Where-Object { $_.Key -in 'Tab', 'UpArrow', 'DownArrow' } | Sort-Object Key | ForEach-Object { $_.Key + ':' + $_.Function }) -join ',')
}
$lines += 'errors=' + $Error.Count
[IO.File]::WriteAllLines($env:SERENO_T_REPORT, [string[]]$lines)
'@)
        $arguments = '-NoLogo -NoProfile -NoExit -ExecutionPolicy Bypass -File "' + $probe + '"'
        $child = Start-Child $arguments @{ WT_SESSION = 'test-session'; SERENO_SESSION = 'wt-profile'; SERENO_OS_MODE = 'dark'; SERENO_T_REPORT = $report } $work
        $found = ''
        if (Test-Path -LiteralPath $report) { $found = (@([IO.File]::ReadAllLines($report)) -join '|') }
        if ($found.StartsWith('loaded=False')) {
            Report 'skip - the line editor in a session started from the Serena profile: the host loaded no PSReadLine here (a screen reader turns it off)'
        } else {
            Check ($found -ceq 'loaded=True|command=ESC[34m|selection=ESC[7m|bell=None|keys=DownArrow:HistorySearchForward,Tab:MenuComplete,UpArrow:HistorySearchBackward|errors=0') 'started with -NoExit -File, the session has PSReadLine before init.ps1 runs, and after it the colours of R24 and the comfort set of R27' $found
        }
        Check ($child.Code -eq 0 -and $child.Err.Trim() -eq '') 'and that session ends without an error when its input ends' ("exit code $($child.Code): " + $child.Err.Trim())
    }

    # R18: the same start from a ROOT that SERENO_HOME moved. The fragment
    # then names init.ps1 by its absolute path between double quotes. Windows
    # Terminal expands environment strings in the command line and hands it
    # to the system as it is, and so does this test: a folder named with a
    # space, an apostrophe, one percent sign or anything else a command line
    # could stumble on must reach PowerShell as one path. (Two percent signs
    # are refused when the fragment is rendered: between them Windows
    # Terminal would look for a variable.)
    if ($onWindows -and (-not $Only -or 'a moved ROOT in the command line of the Serena profile'.Contains($Only))) {
        [Console]::Out.WriteLine('# session: a moved ROOT in the command line of the Serena profile')
        $work = Join-Path $userHome 'work'
        $palette = [IO.File]::ReadAllText((Join-Path $Golden 'palette/hc.bin'))
        $mark = [string][char]27 + ']133;B' + [string][char]7
        $names = @('plain', 'with space', "it's", '100% mine', 'a&b;c', '(d) [e] {f}', ('$g ' + [char]96 + 'h'), ',@i #j', '^k !l =m +n ~o', 'p -File q', ('letters ' + [char]0xE7 + [char]0xE3 + [char]0xE9))
        foreach ($name in $names) {
            $moved = Join-Path (Join-Path $tmp 'moved') $name
            $null = [IO.Directory]::CreateDirectory((Join-Path $moved 'shell'))
            [IO.File]::Copy($Init, (Join-Path $moved 'shell\init.ps1'))
            [IO.File]::WriteAllText((Join-Path $moved 'mode'), $defaultMode)
            $arguments = '-NoLogo -NoProfile -NoExit -ExecutionPolicy Bypass -File "' + $moved + '\shell\init.ps1"'
            $child = Start-Child ([Environment]::ExpandEnvironmentVariables($arguments)) @{ WT_SESSION = 'test-session'; SERENO_SESSION = 'wt-profile'; SERENO_OS_MODE = 'hc'; SERENO_HOME = $moved } $work
            $shown = $name -replace '[^ -~]', '?'
            Check ($child.Out.StartsWith($palette) -and $child.Out.EndsWith($mark) -and $child.Code -eq 0 -and $child.Err.Trim() -eq '') "ROOT in a folder named <$shown>: the session starts, paints and shows the Serena prompt" ("exit code $($child.Code): " + $child.Err.Trim())
        }
    }
} finally {
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}

[Console]::Out.WriteLine("run.ps1: $($script:passed) passed, $($script:failed) failed, $($script:skipped) skipped ($(Split-Path -Leaf $exe) $($PSVersionTable.PSVersion))")
if ($script:failed -gt 0) { exit 1 }
exit 0
