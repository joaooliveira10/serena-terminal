# Tests of the install script for Windows (scripts/install.ps1.tmpl) against a
# folder that stands in for a release. No network and no real home:
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File test\install\run.ps1 -Release DIR
#   pwsh -NoProfile -File test/install/run.ps1 -Release DIR
#
# DIR is a folder written by `go run ./internal/tools/release`, which renders
# install.ps1 next to the six archives and checksums.txt. scripts\test.ps1
# makes one from stand-in programs: the install script never starts the
# program, so any file will do in its place, and no built sereno is needed.
#
# The script under test runs in a process of its own, started by the
# PowerShell that runs this file (so Windows PowerShell 5.1 tests it under
# 5.1 and PowerShell 7 under 7), with -NoProfile and without a window. That
# process gets a temporary folder as LOCALAPPDATA, APPDATA, TEMP, HOME and
# USERPROFILE, and SERENO_INSTALL_FROM names the release folder, so nothing is
# downloaded. The download itself is not tried here: it needs the network.
#
# Outside Windows the script must refuse, and that is all that is checked
# there. The exit code is 1 when any check failed.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Release
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$exe = (Get-Process -Id $PID).Path
$onWindows = ($env:OS -eq 'Windows_NT')
$script:failed = $false
$script:last = $null
function Confirm([bool]$condition, [string]$name) {
    if ($condition) { Write-Host "ok - $name"; return }
    Write-Host "not ok - $name"
    $script:failed = $true
    if ($script:last) {
        Write-Host "#   exit code $($script:last.Code)"
        foreach ($line in @($script:last.Out -split "`r?`n" | Where-Object { $_ })) { Write-Host "#   out: $line" }
        foreach ($line in @($script:last.Err -split "`r?`n" | Where-Object { $_ })) { Write-Host "#   err: $line" }
    }
}

$Release = (Resolve-Path -LiteralPath $Release).Path
$rendered = Join-Path $Release 'install.ps1'
if (-not (Test-Path -LiteralPath $rendered -PathType Leaf)) { Write-Host "run.ps1: $Release holds no install.ps1"; exit 2 }
$text = [IO.File]::ReadAllText($rendered)
if ($text -notmatch '(?m)^\$version = ''([^'']+)''\r?$') { Write-Host 'run.ps1: install.ps1 names no version'; exit 2 }
$version = $Matches[1]

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('sereno-install-test-' + [guid]::NewGuid().ToString('N'))
$scratch = Join-Path $tmp 'scratch'
$profileFolder = Join-Path $tmp 'profile'
foreach ($dir in $tmp, $scratch, $profileFolder) { $null = New-Item -ItemType Directory -Path $dir -Force }

# What a case sets before it calls Invoke-Script: the home, and through it
# the folder that stands for %LOCALAPPDATA%.
$script:userHome = ''
function New-Home([string]$name) {
    $script:userHome = Join-Path $tmp $name
    $null = New-Item -ItemType Directory -Path (Join-Path $script:userHome 'AppData\Local') -Force
    $null = New-Item -ItemType Directory -Path (Join-Path $script:userHome 'AppData\Roaming') -Force
}
function Get-Local { Join-Path $script:userHome 'AppData\Local' }

# Runs a script once, in a process of its own. $environment overrides what
# the process gets; a value of $null takes the variable out.
function Invoke-Script([string]$file, [string]$arguments, [hashtable]$environment = @{}) {
    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = $exe
    $info.Arguments = '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + $file + '" ' + $arguments
    if (-not $onWindows) { $info.Arguments = '-NoLogo -NoProfile -NonInteractive -File "' + $file + '" ' + $arguments }
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.WorkingDirectory = $tmp
    # Windows PowerShell keeps a start-up cache of its own below the
    # AppData\Local of USERPROFILE: it gets a folder that is not looked at.
    $base = @{
        LOCALAPPDATA = (Get-Local); APPDATA = (Join-Path $script:userHome 'AppData\Roaming')
        USERPROFILE = $profileFolder; HOME = $script:userHome; TEMP = $scratch; TMP = $scratch
        SERENO_INSTALL_FROM = $Release; SERENO_HOME = $null; XDG_DATA_HOME = $null
        PROCESSOR_ARCHITECTURE = 'AMD64'; PROCESSOR_ARCHITEW6432 = $null
        POWERSHELL_TELEMETRY_OPTOUT = '1'; POWERSHELL_UPDATECHECK = 'Off'
    }
    foreach ($table in $base, $environment) {
        foreach ($key in @($table.Keys)) {
            if ($null -eq $table[$key]) { $info.EnvironmentVariables.Remove($key) }
            else { $info.EnvironmentVariables[$key] = [string]$table[$key] }
        }
    }
    $process = [Diagnostics.Process]::Start($info)
    $errTask = $process.StandardError.ReadToEndAsync()
    $out = $process.StandardOutput.ReadToEnd()
    $process.WaitForExit()
    $script:last = [pscustomobject]@{ Code = $process.ExitCode; Out = $out; Err = $errTask.Result }
    $process.Dispose()
    $script:last
}
function Test-Said([string]$piece) { $script:last.Out.Contains($piece) }
function Test-Complained([string]$piece) { $script:last.Err.Contains($piece) }
function Test-Tidy { @(Get-ChildItem -LiteralPath $scratch -Force).Count -eq 0 }

# A listing of every folder and file below the folder that stands for
# %LOCALAPPDATA%, with SHA-256 of the files. A PowerShell keeps a start-up
# cache of its own below Microsoft\Windows\PowerShell (5.1) or
# Microsoft\PowerShell (7); should one land here, it is not the doing of the
# script under test and is left out.
function Get-Tree {
    $path = Get-Local
    $lines = foreach ($item in @(Get-ChildItem -LiteralPath $path -Recurse -Force | Sort-Object FullName)) {
        $name = $item.FullName.Substring($path.Length)
        if ($name -match '^\\Microsoft(\\Windows)?(\\PowerShell(\\.*)?)?$') { continue }
        if ($item.PSIsContainer) { "$name <folder>" } else { "$name " + (Get-FileHash -LiteralPath $item.FullName -Algorithm SHA256).Hash }
    }
    @($lines) -join "`n"
}

try {
    if (-not $onWindows) {
        New-Home 'elsewhere'
        $null = Invoke-Script $rendered ''
        Confirm (($script:last.Code -eq 1) -and (Test-Complained 'this is not Windows: use install.sh')) 'outside Windows the script refuses and names install.sh'
    } else {
        Write-Host "# install.ps1 of $version, run by $(Split-Path -Leaf $exe) $($PSVersionTable.PSVersion)"
        Add-Type -AssemblyName System.IO.Compression
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        # The SHA-256 of the program inside the archive of a processor.
        function Get-Member([string]$arch) {
            $zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $Release "sereno_${version}_windows_$arch.zip"))
            try {
                $entry = $zip.Entries | Where-Object { $_.FullName -ceq 'sereno.exe' }
                $stream = $entry.Open()
                try { $sha = [Security.Cryptography.SHA256]::Create(); ([BitConverter]::ToString($sha.ComputeHash($stream))).Replace('-', '') } finally { $stream.Dispose() }
            } finally { $zip.Dispose() }
        }
        function Test-Placed([string]$file, [string]$arch) {
            (Test-Path -LiteralPath $file -PathType Leaf) -and ((Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash -eq (Get-Member $arch))
        }

        # -------------------------------------------------------------------
        # What the script refuses before it looks at anything.
        New-Home 'refusals'
        $empty = Get-Tree
        # PowerShell runs a file only under the extension .ps1.
        $template = Join-Path $tmp 'install-template.ps1'
        Copy-Item -LiteralPath (Join-Path $repo 'scripts\install.ps1.tmpl') -Destination $template
        $null = Invoke-Script $template ''
        Confirm (($script:last.Code -eq 1) -and (Test-Complained 'this is the template of the install script') -and ((Get-Tree) -ceq $empty)) 'the template itself, not rendered by a release, installs nothing'

        $null = Invoke-Script $rendered '' @{ PROCESSOR_ARCHITECTURE = 'x86' }
        Confirm (($script:last.Code -eq 1) -and (Test-Complained 'no sereno is built for the processor x86') -and ((Get-Tree) -ceq $empty)) 'a processor without a build is refused'

        $null = Invoke-Script $rendered '' @{ SERENO_HOME = 'relative\folder' }
        $first = ($script:last.Code -eq 1) -and (Test-Complained 'SERENO_HOME must be an absolute path')
        $null = Invoke-Script $rendered '' @{ LOCALAPPDATA = $null }
        Confirm ($first -and ($script:last.Code -eq 1) -and (Test-Complained 'LOCALAPPDATA is not set to an absolute path') -and ((Get-Tree) -ceq $empty)) 'with a relative SERENO_HOME, or without LOCALAPPDATA, nothing is installed'

        # -------------------------------------------------------------------
        # A first install into the default folder, and the same again.
        New-Home 'default'
        $local = Get-Local
        $root = Join-Path $local 'sereno'
        $target = Join-Path $root 'bin\sereno.exe'
        $null = Invoke-Script $rendered ''
        Confirm (($script:last.Code -eq 0) -and (Test-Placed $target 'amd64')) 'the program of windows/amd64 is placed in %LOCALAPPDATA%\sereno\bin'
        $wanted = @('\sereno <folder>', '\sereno\bin <folder>', ('\sereno\bin\sereno.exe ' + (Get-Member 'amd64'))) -join "`n"
        Confirm ((Get-Tree) -ceq $wanted) 'nothing else is written: no LICENSE, no README.md, no other file'
        Confirm ((Test-Said "sereno $version (windows/amd64) was placed at") -and (Test-Said "  $target") -and (Test-Said "  & '$target' apply --dry-run") -and
            (Test-Said '-File install.ps1 -Uninstall') -and (Test-Said "The SHA-256 of sereno_${version}_windows_amd64.zip is the one of the release")) 'it says where the program is, what was checked, the next command by its full path and how to remove it'
        Confirm (Test-Tidy) 'its temporary folder is gone'

        $installed = Get-Tree
        $null = Invoke-Script $rendered ''
        Confirm (($script:last.Code -eq 0) -and (Test-Said 'replaced the program that was at') -and ((Get-Tree) -ceq $installed) -and (Test-Tidy)) 'a second run replaces the program with the same bytes and changes nothing else'

        # -------------------------------------------------------------------
        # What must stop it, with a program already in place: it stays.
        $bad = Join-Path $tmp 'corrupted'
        $null = New-Item -ItemType Directory -Path $bad
        $archive = Join-Path $bad "sereno_${version}_windows_amd64.zip"
        [IO.File]::WriteAllBytes($archive, [IO.File]::ReadAllBytes((Join-Path $Release "sereno_${version}_windows_amd64.zip")) + [byte[]](120))
        $null = Invoke-Script $rendered '' @{ SERENO_INSTALL_FROM = $bad }
        Confirm (($script:last.Code -eq 1) -and (Test-Complained 'is not the one of release') -and (Test-Complained 'nothing was installed') -and (Test-Complained '  expected ') -and (Test-Complained '  found    ') -and
            ((Get-Tree) -ceq $installed) -and (Test-Tidy)) 'an archive with one more byte is refused: the checksum is named, and the program that was there stays'

        New-Home 'corrupted-first'
        $empty = Get-Tree
        $null = Invoke-Script $rendered '' @{ SERENO_INSTALL_FROM = $bad }
        Confirm (($script:last.Code -eq 1) -and (Test-Complained 'nothing was installed') -and ((Get-Tree) -ceq $empty) -and (Test-Tidy)) 'the same archive on a home without sereno leaves that home empty'

        [IO.File]::WriteAllText($archive, "not an archive`n")
        $null = Invoke-Script $rendered '' @{ SERENO_INSTALL_FROM = $bad }
        Confirm (($script:last.Code -eq 1) -and (Test-Complained 'is not the one of release') -and ((Get-Tree) -ceq $empty) -and (Test-Tidy)) 'a file that is no archive is refused by its checksum, before anything opens it'

        $nowhere = Join-Path $tmp 'nowhere'
        $null = New-Item -ItemType Directory -Path $nowhere
        $null = Invoke-Script $rendered '' @{ SERENO_INSTALL_FROM = $nowhere }
        Confirm (($script:last.Code -eq 1) -and (Test-Complained "holds no sereno_${version}_windows_amd64.zip") -and ((Get-Tree) -ceq $empty) -and (Test-Tidy)) 'a folder without the archive installs nothing'

        # A folder where the program goes.
        New-Home 'in-the-way'
        $blocked = Join-Path (Get-Local) 'sereno\bin\sereno.exe'
        $null = New-Item -ItemType Directory -Path $blocked -Force
        [IO.File]::WriteAllText((Join-Path $blocked 'notes.txt'), "mine`n")
        $inTheWay = Get-Tree
        $null = Invoke-Script $rendered ''
        Confirm (($script:last.Code -eq 1) -and (Test-Complained 'is in the way and is not a file') -and ((Get-Tree) -ceq $inTheWay) -and (Test-Tidy)) 'a folder under the name of the program is left alone, and nothing is installed'

        # A script whose list of checksums has no line for this processor.
        New-Home 'without-line'
        $empty = Get-Tree
        $without = Join-Path $tmp 'install-without-line.ps1'
        $kept = @($text -split "`n" | Where-Object { $_ -notlike "*sereno_${version}_windows_amd64.zip*" })
        [IO.File]::WriteAllText($without, ($kept -join "`n"))
        $null = Invoke-Script $without ''
        $first = ($script:last.Code -eq 1) -and (Test-Complained "carries no checksum for sereno_${version}_windows_amd64.zip") -and ((Get-Tree) -ceq $empty) -and (Test-Tidy)
        $null = Invoke-Script $without '' @{ PROCESSOR_ARCHITECTURE = 'ARM64' }
        Confirm ($first -and ($script:last.Code -eq 0) -and (Test-Placed (Join-Path (Get-Local) 'sereno\bin\sereno.exe') 'arm64')) 'without a checksum line for the archive nothing is installed; the other processor of that script still is'

        # -------------------------------------------------------------------
        # Each processor takes its own archive.
        New-Home 'arm64'
        $null = Invoke-Script $rendered '' @{ PROCESSOR_ARCHITECTURE = 'ARM64' }
        $first = ($script:last.Code -eq 0) -and (Test-Placed (Join-Path (Get-Local) 'sereno\bin\sereno.exe') 'arm64') -and (Test-Said '(windows/arm64)')
        New-Home 'arm64-from-32-bit'
        $null = Invoke-Script $rendered '' @{ PROCESSOR_ARCHITECTURE = 'x86'; PROCESSOR_ARCHITEW6432 = 'ARM64' }
        $second = ($script:last.Code -eq 0) -and (Test-Placed (Join-Path (Get-Local) 'sereno\bin\sereno.exe') 'arm64')
        New-Home 'amd64-from-32-bit'
        $null = Invoke-Script $rendered '' @{ PROCESSOR_ARCHITECTURE = 'x86'; PROCESSOR_ARCHITEW6432 = 'AMD64' }
        Confirm ($first -and $second -and ($script:last.Code -eq 0) -and (Test-Placed (Join-Path (Get-Local) 'sereno\bin\sereno.exe') 'amd64')) 'ARM64 gets the archive built for it, also when a 32-bit PowerShell asks; AMD64 likewise'

        # -------------------------------------------------------------------
        # SERENO_HOME moves the folder, as it does for sereno itself.
        New-Home 'moved'
        $empty = Get-Tree
        $moved = Join-Path $script:userHome "o'neil `$HOME folder\sereno"
        $null = Invoke-Script $rendered '' @{ SERENO_HOME = $moved }
        Confirm (($script:last.Code -eq 0) -and (Test-Placed (Join-Path $moved 'bin\sereno.exe') 'amd64') -and ((Get-Tree) -ceq $empty)) 'SERENO_HOME moves the folder, also with a quote, a space and a dollar sign in its name'
        # The next command is printed so that PowerShell reads the path back
        # as it is.
        $line = @($script:last.Out -split "`r?`n" | Where-Object { $_ -like '* apply --dry-run' })
        $tokens = @()
        if ($line.Count -eq 1) { $tokens = @([System.Management.Automation.PSParser]::Tokenize($line[0], [ref]$null)) }
        Confirm (($tokens.Count -eq 4) -and ($tokens[0].Content -ceq '&') -and ($tokens[1].Type -eq 'String') -and ($tokens[1].Content -ceq (Join-Path $moved 'bin\sereno.exe')) -and
            ($tokens[2].Content -ceq 'apply') -and ($tokens[3].Content -ceq '--dry-run')) 'the next command names that path in a form PowerShell takes'

        # -------------------------------------------------------------------
        # -Uninstall.
        New-Home 'removal'
        $empty = Get-Tree
        $root = Join-Path (Get-Local) 'sereno'
        $target = Join-Path $root 'bin\sereno.exe'
        $null = Invoke-Script $rendered '-Uninstall'
        Confirm (($script:last.Code -eq 0) -and (Test-Said 'Nothing to remove') -and ((Get-Tree) -ceq $empty)) '-Uninstall on a home without sereno removes nothing and creates nothing'

        $null = Invoke-Script $rendered ''
        [IO.File]::WriteAllText((Join-Path $root 'manifest.json'), "{}`n")
        $null = Invoke-Script $rendered '-Uninstall'
        Confirm (($script:last.Code -eq 1) -and (Test-Complained 'sereno is still applied here') -and (Test-Complained "& '$target' uninstall") -and (Test-Path -LiteralPath $target)) '-Uninstall leaves the program while a manifest says sereno is applied, and names the command to run first'
        Remove-Item -LiteralPath (Join-Path $root 'manifest.json')

        $notes = Join-Path $root 'bin\notes.txt'
        [IO.File]::WriteAllText($notes, "mine`n")
        $null = Invoke-Script $rendered '-Uninstall'
        Confirm (($script:last.Code -eq 0) -and (-not (Test-Path -LiteralPath $target)) -and ([IO.File]::ReadAllText($notes) -ceq "mine`n") -and (Test-Said "$(Join-Path $root 'bin') was left: it holds other files.")) '-Uninstall removes the program and leaves a bin folder that holds a file of the user'
        Remove-Item -LiteralPath $notes

        $null = Invoke-Script $rendered ''
        $null = Invoke-Script $rendered '-Uninstall'
        Confirm (($script:last.Code -eq 0) -and (Test-Said "Removed $target") -and (Test-Said "Removed $(Join-Path $root 'bin')") -and (Test-Said "Removed $root") -and ((Get-Tree) -ceq $empty) -and (Test-Tidy)) '-Uninstall removes the program, its bin folder and the empty folder of sereno: the home is what it was'
    }
} finally {
    for ($i = 0; $i -lt 20; $i++) {
        try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction Stop; break } catch { Start-Sleep -Milliseconds 100 }
    }
}

if ($script:failed) { Write-Host 'test/install/run.ps1: FAILED'; exit 1 }
Write-Host 'test/install/run.ps1: all checks passed'
exit 0
