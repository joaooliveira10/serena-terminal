# Serena Terminal: session setup for Windows PowerShell 5.1 and PowerShell 7.
#
# Written by `sereno apply`, removed by `sereno uninstall`. Do not edit: the
# next apply would leave an edited copy alone and stop updating it.
# The file is the same for every user. It prints nothing, starts no process,
# opens no connection and paints with the 16 palette slots only. The one
# exception are the palette strings below, which hand the Serena colours to
# Windows Terminal when the session follows the system.
#
# A note for whoever changes this file: in PowerShell the first use of every
# kind of operation costs about a millisecond, so the start-up part below is
# written with as few different operations as possible, and everything that
# can wait is kept in script blocks that are compiled when they first run.

if (-not ($Host.Name -ceq 'ConsoleHost')) { return }
# Where an application control policy constrains the language of a session,
# methods of .NET types cannot be called, and this file is made of such calls:
# the session is then left as it is. Reading a property is allowed there.
if (-not ([string]$ExecutionContext.SessionState.LanguageMode -ceq 'FullLanguage')) { return }
if ($null -ne $ExecutionContext.SessionState.PSVariable.GetValue('global:__sereno')) { return }

# Everything this file keeps for the session lives in this one variable.
$global:__sereno = @{
    # The mode file and its five values in the order of the file: mode,
    # flavor, inband, keys, prompt. Stamp is the time the file had when it
    # was last read by Follow.
    ModeFile = "$PSScriptRoot/../mode"; Stamp = [long]0; Settings = @('auto', 'soft', 'profile', 'profile', 'on')
    # Terminal: inside Windows Terminal and not in VS Code. Profile: started
    # from the Serena profile. Last: the variant painted last, if any.
    Terminal = $false; Profile = $false; Last = ''
    Shown = 0; HistoryId = [long]-1; Saved = $null; Bin = ''
    E = [string][char]27; Bel = [string][char]7

    # PALETTE of each variant and RESET: OSC 10, 11, 12, 17 and 4;0-15, each
    # ending in ESC \. The exclamation mark stands for the escape character.
    Sequences = @{
        'dark' = '!]10;rgb:d4/d4/dc!\!]11;rgb:16/17/1d!\!]12;rgb:c4/a7/e7!\!]17;rgb:3a/3f/5a!\!]4;0;rgb:26/28/33!\!]4;1;rgb:e8/8b/9a!\!]4;2;rgb:a6/d1/89!\!]4;3;rgb:e8/c8/89!\!]4;4;rgb:8c/b4/e8!\!]4;5;rgb:c4/a7/e7!\!]4;6;rgb:91/d1/d7!\!]4;7;rgb:d4/d4/dc!\!]4;8;rgb:7b/7f/94!\!]4;9;rgb:f0/a3/af!\!]4;10;rgb:bc/de/a4!\!]4;11;rgb:f0/d6/a3!\!]4;12;rgb:a9/c8/f0!\!]4;13;rgb:d2/bb/ee!\!]4;14;rgb:ae/e0/e3!\!]4;15;rgb:ee/ee/f3!\'
        'dark-vivid' = '!]10;rgb:d4/d4/dc!\!]11;rgb:13/14/1b!\!]12;rgb:b5/82/ef!\!]17;rgb:30/35/57!\!]4;0;rgb:23/25/31!\!]4;1;rgb:f1/6e/92!\!]4;2;rgb:a3/e2/8d!\!]4;3;rgb:df/bb/5b!\!]4;4;rgb:79/b6/f4!\!]4;5;rgb:b5/82/ef!\!]4;6;rgb:4e/d8/dc!\!]4;7;rgb:d4/d4/dc!\!]4;8;rgb:78/7d/96!\!]4;9;rgb:f9/9b/af!\!]4;10;rgb:bb/f9/a6!\!]4;11;rgb:f5/da/7a!\!]4;12;rgb:a4/d0/fa!\!]4;13;rgb:cc/a7/f8!\!]4;14;rgb:84/f0/f0!\!]4;15;rgb:ee/ee/f3!\'
        'light' = '!]10;rgb:32/34/45!\!]11;rgb:ec/ed/f3!\!]12;rgb:7b/48/9e!\!]17;rgb:d6/de/ff!\!]4;0;rgb:32/34/45!\!]4;1;rgb:9f/21/31!\!]4;2;rgb:30/63/24!\!]4;3;rgb:82/56/0a!\!]4;4;rgb:14/55/a2!\!]4;5;rgb:72/3c/90!\!]4;6;rgb:06/61/66!\!]4;7;rgb:52/54/62!\!]4;8;rgb:67/6a/7d!\!]4;9;rgb:d2/1f/27!\!]4;10;rgb:12/7c/2a!\!]4;11;rgb:7b/6c/00!\!]4;12;rgb:00/6e/be!\!]4;13;rgb:a4/41/b3!\!]4;14;rgb:15/78/77!\!]4;15;rgb:1f/21/30!\'
        'light-vivid' = '!]10;rgb:32/34/45!\!]11;rgb:ec/ed/f3!\!]12;rgb:89/25/b0!\!]17;rgb:d6/de/ff!\!]4;0;rgb:32/34/45!\!]4;1;rgb:9f/21/31!\!]4;2;rgb:25/65/13!\!]4;3;rgb:82/56/0a!\!]4;4;rgb:14/55/a2!\!]4;5;rgb:77/32/9b!\!]4;6;rgb:06/61/66!\!]4;7;rgb:52/54/62!\!]4;8;rgb:67/6a/7d!\!]4;9;rgb:d2/1f/27!\!]4;10;rgb:12/7c/2a!\!]4;11;rgb:7b/6c/00!\!]4;12;rgb:00/6e/be!\!]4;13;rgb:a4/41/b3!\!]4;14;rgb:15/78/77!\!]4;15;rgb:1f/21/30!\'
        'hc' = '!]10;rgb:f2/f2/f8!\!]11;rgb:0b/0c/12!\!]12;rgb:f2/f2/f8!\!]17;rgb:cb/b8/ff!\!]4;0;rgb:2f/31/40!\!]4;1;rgb:ff/7a/8c!\!]4;2;rgb:8f/e0/7a!\!]4;3;rgb:f8/d4/57!\!]4;4;rgb:6a/ae/ff!\!]4;5;rgb:dc/b0/ff!\!]4;6;rgb:4f/d2/d6!\!]4;7;rgb:d8/d9/e2!\!]4;8;rgb:a4/a6/b8!\!]4;9;rgb:ff/a3/ae!\!]4;10;rgb:b6/f0/9a!\!]4;11;rgb:ff/e5/8f!\!]4;12;rgb:9c/c7/ff!\!]4;13;rgb:eb/d2/ff!\!]4;14;rgb:8b/e7/ea!\!]4;15;rgb:ff/ff/ff!\'
        'hc-light' = '!]10;rgb:0b/0c/12!\!]11;rgb:ff/ff/ff!\!]12;rgb:0b/0c/12!\!]17;rgb:3b/2d/86!\!]4;0;rgb:0b/0c/12!\!]4;1;rgb:a1/00/28!\!]4;2;rgb:1f/55/24!\!]4;3;rgb:65/50/00!\!]4;4;rgb:23/43/a3!\!]4;5;rgb:6a/1b/8a!\!]4;6;rgb:00/5d/69!\!]4;7;rgb:50/51/5b!\!]4;8;rgb:4a/4b/5a!\!]4;9;rgb:8f/1d/3f!\!]4;10;rgb:2a/5f/1e!\!]4;11;rgb:5c/4a/00!\!]4;12;rgb:3b/3f/ae!\!]4;13;rgb:7a/2a/9e!\!]4;14;rgb:0b/5a/73!\!]4;15;rgb:55/56/60!\'
        'reset' = '!]104!\!]110!\!]111!\!]112!\!]117!\'
    }

    # Reads the mode file: five lines, anything else means the defaults. The
    # bytes decide, as they do for sereno itself and in bash: read as ASCII,
    # a byte order mark or a letter outside ASCII is a question mark, and
    # the file is malformed.
    Read = {
        $s = $global:__sereno
        $values = 'auto', 'soft', 'profile', 'profile', 'on'
        $file = $s['ModeFile']
        if ($file) {
            if ([System.IO.File]::Exists($file)) {
                if ([System.Text.Encoding]::ASCII.GetString([System.IO.File]::ReadAllBytes($file)) -cmatch '\Amode=(?<m>auto|dark|light|hc|hc-light)\nflavor=(?<f>soft|vivid)\ninband=(?<i>profile|always|off)\nkeys=(?<k>profile|always|off)\nprompt=(?<p>on|off)\n\z') {
                    $values = $Matches['m'], $Matches['f'], $Matches['i'], $Matches['k'], $Matches['p']
                }
            }
        }
        $s['Settings'] = $values
    }

    # The three signals of the system: a contrast theme is on, its window
    # colour is light, applications use the light theme. SERENO_OS_MODE
    # replaces them; where nothing can be read they are all off.
    Signals = {
        $hc = $false; $hcLight = $false; $light = $false
        $os = $env:SERENO_OS_MODE
        if ($os -ceq 'dark') { }
        elseif ($os -ceq 'light') { $light = $true }
        elseif ($os -ceq 'hc') { $hc = $true }
        elseif ($os -ceq 'hc-light') { $hc = $true; $hcLight = $true }
        elseif ($env:OS -ceq 'Windows_NT') {
            $v = [Microsoft.Win32.Registry]::GetValue('HKEY_CURRENT_USER\Control Panel\Accessibility\HighContrast', 'Flags', $null)
            $n = [uint32]0
            if ($v -is [string]) { if ([uint32]::TryParse($v.Trim(), [ref]$n)) { $hc = (($n -band 1) -eq 1) } }
            elseif ($v -is [int]) { $hc = (($v -band 1) -eq 1) }
            $v = [Microsoft.Win32.Registry]::GetValue('HKEY_CURRENT_USER\Control Panel\Colors', 'Window', $null)
            if ($v -is [string]) {
                $c = $v.Trim() -split '\s+'
                $r = [byte]0; $g = [byte]0; $b = [byte]0
                if (($c.Count -eq 3) -and [byte]::TryParse($c[0], [ref]$r) -and [byte]::TryParse($c[1], [ref]$g) -and [byte]::TryParse($c[2], [ref]$b)) {
                    $hcLight = ((2126 * $r + 7152 * $g + 722 * $b) -gt 1270000)
                }
            }
            $v = [Microsoft.Win32.Registry]::GetValue('HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize', 'AppsUseLightTheme', $null)
            if ($v -is [int]) { $light = ($v -eq 1) }
        }
        $hc, $hcLight, $light
    }

    # The resolution table: mode, flavour and the signals give a variant.
    Resolve = {
        param($mode, $flavor, $hc, $hcLight, $light)
        if ($mode -ceq 'hc') { return 'hc' }
        if ($mode -ceq 'hc-light') { return 'hc-light' }
        $v = 'dark'
        if ($mode -ceq 'light') { $v = 'light' }
        elseif ($mode -cne 'dark') {
            if ($hc) { if ($hcLight) { return 'hc-light' }; return 'hc' }
            if ($light) { $v = 'light' }
        }
        if ($flavor -ceq 'vivid') { return $v + '-vivid' }
        $v
    }

    # Following the system, run at every prompt inside Windows Terminal:
    # read the mode file again if it changed, resolve the variant and, only
    # when it is not the one painted last, write its palette to the console.
    # When the mode file no longer allows painting, the palette this session
    # painted is handed back to the emulator's own scheme, once.
    Follow = {
        $s = $global:__sereno
        $file = $s['ModeFile']
        if ($file) {
            # The time is kept only once the file has been read: a file that
            # another program holds at this moment is read at the next prompt.
            $stamp = [System.IO.File]::GetLastWriteTimeUtc($file).Ticks
            if ($stamp -ne $s['Stamp']) { & $s['Read']; $s['Stamp'] = $stamp }
        }
        $set = $s['Settings']
        if (($set[2] -ceq 'always') -or (($set[2] -ceq 'profile') -and $s['Profile'])) {
            $sig = & $s['Signals']
            $v = & $s['Resolve'] $set[0] $set[1] $sig[0] $sig[1] $sig[2]
            if ($v -cne $s['Last']) {
                $s['Last'] = $v
                [Console]::Out.Write($s['Sequences'][$v].Replace('!', $s['E']))
            }
        } elseif ($s['Last']) {
            $s['Last'] = ''
            [Console]::Out.Write($s['Sequences']['reset'].Replace('!', $s['E']))
        }
    }

    # A background parameter of an SGR sequence (40-47, 100-107) becomes
    # the foreground of the same slot. The numbers after 38 and 48 are
    # colour arguments, not parameters, and stay as they are.
    Foreground = {
        param([string]$value)
        [regex]::Replace($value, [regex]::Escape($global:__sereno['E']) + '\[([0-9;]*)m', {
                param($match)
                $skip = 0
                $kept = foreach ($p in $match.Groups[1].Value.Split(';')) {
                    $n = -1
                    if ($skip -eq 9) { if ($p -ceq '5') { $skip = 1 } elseif ($p -ceq '2') { $skip = 3 } else { $skip = 0 } }
                    elseif ($skip -gt 0) { $skip-- }
                    elseif (($p -ceq '38') -or ($p -ceq '48')) { $skip = 9 }
                    elseif ([int]::TryParse($p, [ref]$n) -and ((($n -ge 40) -and ($n -le 47)) -or (($n -ge 100) -and ($n -le 107)))) { $p = [string]($n - 10) }
                    $p
                }
                $global:__sereno['E'] + '[' + ($kept -join ';') + 'm'
            })
    }
}

# The rest runs in a scope of its own so that no variable of the caller is
# created or changed, and inside try/catch so that nothing it does can stop a
# profile that runs with $ErrorActionPreference = 'Stop'.
& {
    $s = $global:__sereno
    $e = $s['E']

    try {
        # ROOT is the folder above the one this file is in. Text that was
        # pasted or evaluated has no file, and then no mode file either.
        if (-not $PSScriptRoot) { $s['ModeFile'] = '' }
        if ($env:WT_SESSION) { if (-not ($env:TERM_PROGRAM -ceq 'vscode')) { $s['Terminal'] = $true } }
        if ($env:SERENO_SESSION -ceq 'wt-profile') { $s['Profile'] = $true }
        elseif ($env:WT_PROFILE_ID -cmatch '\A(?i)\{6b2d8b49-31d3-5dea-9087-93c26e1a033e\}\z') { $s['Profile'] = $true }
        & $s['Read']
    } catch {}
    $settings = $s['Settings']

    # Line editor. PSReadLine is used only when the host already loaded it:
    # a script run or a session with a screen reader does not have it, and
    # this file must not be what brings it in. GetCmdlet looks at what is
    # loaded and loads nothing.
    try {
        $set = $ExecutionContext.InvokeCommand.GetCmdlet('Set-PSReadLineOption')
        $get = $ExecutionContext.InvokeCommand.GetCmdlet('Get-PSReadLineOption')
        if ($set) {
            if ($get) {
                # What this version of PSReadLine has is read off the type of
                # its option object: a colour key or a parameter is used only
                # when the property behind it exists (2.0 has the first
                # fifteen colours and no predictions).
                $options = & $get
                $type = $options.GetType()
                $arguments = @{}

                # Colours through slots only. The fifteen keys PSReadLine has
                # had since 2.0 go in together, the three of the prediction
                # views each when its property exists. PSReadLine 1.x has no
                # -Colors at all, and no CommandColor.
                if ($type.GetProperty('CommandColor')) {
                    $colors = @{
                        Command = "$e[34m"; Keyword = "$e[35m"; String = "$e[32m"
                        Number = "$e[93m"; Type = "$e[33m"; Member = "$e[36m"
                        Variable = "$e[39m"; Parameter = "$e[3;37m"; Operator = "$e[37m"
                        Comment = "$e[90m"; Error = "$e[91m"; Emphasis = "$e[96m"
                        Selection = "$e[7m"; Default = "$e[39m"; ContinuationPrompt = "$e[90m"
                    }
                    if ($type.GetProperty('InlinePredictionColor')) { $colors['InlinePrediction'] = "$e[90m" }
                    if ($type.GetProperty('ListPredictionColor')) { $colors['ListPrediction'] = "$e[33m" }
                    if ($type.GetProperty('ListPredictionSelectedColor')) { $colors['ListPredictionSelected'] = "$e[7m" }
                    $arguments['Colors'] = $colors
                }

                # Comfort set, where the mode file allows it.
                $bind = $null
                if ($settings[3] -ceq 'always') { $bind = $ExecutionContext.InvokeCommand.GetCmdlet('Set-PSReadLineKeyHandler') }
                elseif ($settings[3] -ceq 'profile') { if ($s['Profile']) { $bind = $ExecutionContext.InvokeCommand.GetCmdlet('Set-PSReadLineKeyHandler') } }
                if ($bind) {
                    if ($type.GetProperty('BellStyle')) { $arguments['BellStyle'] = 'None' }
                    if ($type.GetProperty('HistoryNoDuplicates')) { $arguments['HistoryNoDuplicates'] = $true }
                    if ($type.GetProperty('HistorySearchCursorMovesToEnd')) { $arguments['HistorySearchCursorMovesToEnd'] = $true }
                    if ($type.GetProperty('PredictionSource')) {
                        if ([string]$options.PredictionSource -ceq 'None') {
                            # On Windows PSReadLine refuses predictions where
                            # it cannot draw them: with the output redirected,
                            # or in a console without virtual terminal
                            # sequences. It is asked only where it will agree.
                            $predict = $true
                            if ($env:OS -ceq 'Windows_NT') {
                                $predict = $false
                                try { if (-not [Console]::IsOutputRedirected) { $predict = [bool]$Host.UI.SupportsVirtualTerminal } } catch {}
                            }
                            if ($predict) { $arguments['PredictionSource'] = 'History' }
                        }
                    }
                }

                if ($arguments.Count) {
                    try { & $set @arguments } catch {
                        # Not expected. One setting and one colour at a time,
                        # so that one refusal does not cost the others.
                        foreach ($k in @($arguments.Keys)) {
                            if ($k -ceq 'Colors') {
                                foreach ($c in @($arguments[$k].Keys)) { $one = @{ Colors = @{ $c = $arguments[$k][$c] } }; try { & $set @one } catch {} }
                            } else {
                                $one = @{ $k = $arguments[$k] }; try { & $set @one } catch {}
                            }
                        }
                    }
                }
                if ($bind) {
                    try { & $bind -Chord 'Tab' -Function 'MenuComplete' } catch {}
                    try { & $bind -Chord 'UpArrow' -Function 'HistorySearchBackward' } catch {}
                    try { & $bind -Chord 'DownArrow' -Function 'HistorySearchForward' } catch {}
                }
            }
        }
    } catch {}

    # Pairs measured as unreadable on the six variants.
    try {
        $style = $ExecutionContext.SessionState.PSVariable.GetValue('PSStyle')
        if ($null -eq $style) {
            # No $PSStyle: the progress bar is Yellow on DarkCyan. Give it
            # the colours of the console itself.
            $private = $Host.PrivateData
            $private.ProgressForegroundColor = $Host.UI.RawUI.ForegroundColor
            $private.ProgressBackgroundColor = $Host.UI.RawUI.BackgroundColor
        } else {
            # $PSStyle: the styles that paint a background lose it.
            & {
                param($style, $fix)
                $groups = @()
                $progress = $style.PSObject.Properties['Progress']
                if ($progress -and $progress.Value) { $groups += , @($progress.Value, @('Style')) }
                $formatting = $style.PSObject.Properties['Formatting']
                if ($formatting -and $formatting.Value) { $groups += , @($formatting.Value, @(foreach ($member in $formatting.Value.PSObject.Properties) { $member.Name })) }
                $fileInfo = $style.PSObject.Properties['FileInfo']
                if ($fileInfo -and $fileInfo.Value) { $groups += , @($fileInfo.Value, @('Directory', 'SymbolicLink', 'Executable')) }
                foreach ($group in $groups) {
                    foreach ($name in $group[1]) {
                        try {
                            $member = $group[0].PSObject.Properties[$name]
                            if ($member -and ($member.Value -is [string])) {
                                $fixed = & $fix $member.Value
                                if ($fixed -cne $member.Value) { $group[0].$name = $fixed }
                            }
                        } catch {}
                    }
                }
                if ($fileInfo -and $fileInfo.Value) {
                    $extensions = $fileInfo.Value.PSObject.Properties['Extension']
                    if ($extensions -and $extensions.Value) {
                        foreach ($k in @($extensions.Value.Keys)) {
                            try {
                                $old = $extensions.Value[$k]
                                if ($old -is [string]) {
                                    $fixed = & $fix $old
                                    if ($fixed -cne $old) { $extensions.Value[$k] = $fixed }
                                }
                            } catch {}
                        }
                    }
                }
            } $style $s['Foreground']
        }
    } catch {}

    # The prompt. The Serena prompt replaces the stock one only: the stock
    # definition is one line, with comment lines around it. Any other prompt
    # stays, wrapped inside Windows Terminal so that following the system
    # still happens after it has run.
    try {
        $current = $function:prompt
        $serena = $false
        if ($settings[4] -ceq 'on') {
            $serena = ($current -cmatch '\A\s*(?:#[^\n]*\n\s*)*"PS \$\(\$executionContext\.SessionState\.Path\.CurrentLocation\)\$\(''>'' \* \(\$nestedPromptLevel \+ 1\)\) ";\s*(?:#[^\n]*\s*)*\z')
        }
        if ($serena) {
            function global:prompt {
                $ok = $?
                $s = $global:__sereno
                $e = $s['E']
                $out = ''
                $terminal = $s['Terminal']
                if ($terminal) {
                    try { & $s['Follow'] } catch {}
                    if ($s['Shown'] -gt 0) {
                        $code = 0
                        if (-not $ok) {
                            $code = 1
                            $last = $ExecutionContext.SessionState.PSVariable.GetValue('global:LASTEXITCODE')
                            if (($last -is [int]) -and ($last -ne 0)) { $code = $last }
                        }
                        $out = $e + ']133;D;' + $code + $s['Bel']
                    }
                    $out += $e + ']133;A' + $s['Bel']
                }

                $location = $ExecutionContext.SessionState.Path.CurrentLocation
                $path = $location.Path
                $branch = ''
                if ($location.Provider.Name -ceq 'FileSystem') {
                    $dir = $location.ProviderPath
                    if ($terminal) { $out += $e + ']9;9;"' + $dir + '"' + $s['Bel'] }
                    # The branch, read from the files of the repository. A
                    # file that cannot be read at this moment, because
                    # another program holds it, means a prompt without the
                    # branch, not a prompt that fails.
                    try {
                        $head = ''
                        while ($dir) {
                            $git = [System.IO.Path]::Combine($dir, '.git')
                            if ([System.IO.Directory]::Exists($git)) { $head = [System.IO.Path]::Combine($git, 'HEAD'); break }
                            if ([System.IO.File]::Exists($git)) {
                                $text = [System.IO.File]::ReadAllText($git)
                                $end = $text.IndexOf([char]10)
                                if ($end -ge 0) { $text = $text.Substring(0, $end) }
                                $text = $text.TrimEnd([char]13)
                                if ($text.StartsWith('gitdir: ', [System.StringComparison]::Ordinal)) {
                                    $text = $text.Substring(8)
                                    if (-not [System.IO.Path]::IsPathRooted($text)) { $text = [System.IO.Path]::Combine($dir, $text) }
                                    if ([System.IO.Directory]::Exists($text)) { $head = [System.IO.Path]::Combine($text, 'HEAD'); break }
                                }
                            }
                            $dir = [System.IO.Path]::GetDirectoryName($dir)
                        }
                        if ($head -and [System.IO.File]::Exists($head)) {
                            $text = [System.IO.File]::ReadAllText($head)
                            $end = $text.IndexOf([char]10)
                            if ($end -ge 0) { $text = $text.Substring(0, $end) }
                            $text = $text.TrimEnd([char]13)
                            if ($text.StartsWith('ref: refs/heads/', [System.StringComparison]::Ordinal)) { $branch = $text.Substring(16) }
                            elseif ($text.Length -gt 7) { $branch = $text.Substring(0, 7) }
                            else { $branch = $text }
                            $branch = [regex]::Replace($branch, '\p{Cc}', '')
                        }
                    } catch { $branch = '' }
                }

                # A leading home folder is shown as ~.
                $userHome = $HOME
                if ($userHome) {
                    $compare = [System.StringComparison]::Ordinal
                    if ($env:OS -ceq 'Windows_NT') { $compare = [System.StringComparison]::OrdinalIgnoreCase }
                    if ($path.StartsWith($userHome, $compare)) {
                        if ($path.Length -eq $userHome.Length) { $path = '~' }
                        elseif (($path[$userHome.Length] -eq [char]92) -or ($path[$userHome.Length] -eq [char]47)) { $path = '~' + $path.Substring($userHome.Length) }
                    }
                }
                $out += "$e[34m" + $path + "$e[0m"
                if ($branch) { $out += " $e[35m" + $branch + "$e[0m" }

                # How long the last command took, once, when it was 2 s or more.
                $id = $MyInvocation.HistoryId
                if ($id -ne $s['HistoryId']) {
                    $s['HistoryId'] = $id
                    if ($id -gt 1) {
                        $entry = Microsoft.PowerShell.Core\Get-History -Count 1
                        if ($entry) {
                            $seconds = ($entry.EndExecutionTime - $entry.StartExecutionTime).TotalSeconds
                            if ($seconds -ge 2) { $out += " $e[90m" + $seconds.ToString('0.0', [System.Globalization.CultureInfo]::InvariantCulture) + "s$e[0m" }
                        }
                    }
                }

                if ($ok) { $out += "`n$e[32m" } else { $out += "`n$e[31m" }
                $out += ('>' * ($nestedPromptLevel + 1)) + "$e[0m "
                if ($terminal) { $out += $e + ']133;B' + $s['Bel'] }
                $s['Shown'] = $s['Shown'] + 1
                $out
            }
        } elseif ($current) {
            if ($s['Terminal']) {
                $s['Saved'] = $current
                function global:prompt {
                    $out = & $global:__sereno['Saved']
                    try { & $global:__sereno['Follow'] } catch {}
                    $out
                }
            }
        }
    } catch {}

    # The sereno command, when the install script placed it in ROOT\bin and
    # no program of that name is on PATH. PATH itself is not touched.
    try {
        if ($PSScriptRoot) {
            $bin = "$PSScriptRoot/../bin/sereno"
            if ($env:OS -ceq 'Windows_NT') { $bin = "$bin.exe" }
            if ([System.IO.File]::Exists($bin)) {
                & {
                    param($s, $bin)
                    $suffixes = @('')
                    $separator = ':'
                    if ($env:OS -ceq 'Windows_NT') {
                        $separator = ';'
                        $suffixes = @(foreach ($suffix in ([string]$env:PATHEXT).Split(';')) { if ($suffix) { $suffix } })
                        if ($suffixes.Count -eq 0) { $suffixes = @('.COM', '.EXE', '.BAT', '.CMD') }
                    }
                    foreach ($dir in ([string]$env:PATH).Split($separator)) {
                        $dir = $dir.Trim('"')
                        if ((-not $dir) -or (-not [System.IO.Path]::IsPathRooted($dir))) { continue }
                        foreach ($suffix in $suffixes) {
                            if ([System.IO.File]::Exists([System.IO.Path]::Combine($dir, 'sereno' + $suffix))) { return }
                        }
                    }
                    $s['Bin'] = [System.IO.Path]::GetFullPath($bin)
                    function global:sereno { & $global:__sereno['Bin'] @args }
                } $s $bin
            }
        }
    } catch {}
}
