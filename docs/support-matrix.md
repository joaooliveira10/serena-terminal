# Support matrix

What Serena Terminal 0.1 supports, on which system and shell, and how far each part has been checked. Version 0.1 is not released yet; this page describes the state of the code on 2026-10-08.

Two tiers:

- **Tier 1**: the maintainer has it on his own machine, it is tested there, and he is to look at it on screen before a release. That is Windows 11 with Windows Terminal and PowerShell.
- **Tier 2**: generated and tested by programs. Nobody has looked at the result on a screen.

"Run" below means that the automated tests of this repository ran on that combination and passed. "In CI" means on a runner hosted by GitHub, in the runs of 2026-10-08: Windows Server 2025, macOS 26 on arm64 and Ubuntu 24.04. What is marked "in CI only" has never run on a machine of the maintainer. None of it means that a person has looked at the colours. On a screen, one thing has been seen so far: on 2026-10-08 the maintainer ran `sereno theme --paint light` in an open tab of Windows Terminal 1.24.12741.0, and the tab turned light. The rest of that check, and a first apply to a real user profile, are still to come.

## Systems and shells

| System | Shell or emulator | Tier | State |
| :--- | :--- | :--- | :--- |
| Windows 11 | Windows Terminal: the Serena profile and six colour schemes, through a fragment | 1 | The fragment is tested as a file (its bytes, its JSON, its keys against a copy of Windows Terminal's profile schema). Not looked at in Windows Terminal yet. |
| Windows 11 | Windows PowerShell 5.1 | 1 | Run, on the maintainer's machine (5.1.26100, PSReadLine 2.0.0) and in CI. |
| Windows 11 | PowerShell 7 | 1 | Run in CI only (7.6.6, PSReadLine 2.4.5); it is not installed on the maintainer's machine. The session file is the same as for 5.1 and decides by what it finds. |
| Windows 11 | Git Bash (the bash of Git for Windows) | 2 | Run (bash 5.3.15), on the maintainer's machine and in CI. |
| Windows 11 | VS Code's integrated terminal | | Prompt and line-editor colours through a hook line. sereno does not repaint there. Not looked at. |
| Linux | bash | 2 | Run on Ubuntu 26.04 under WSL (bash 5.3.9) and in CI (bash 5.2.21). |
| Linux | zsh | 2 | Run in CI only (zsh 5.9). |
| Linux, macOS | PowerShell 7 | 2 | Run in CI only (7.6.6 on Ubuntu, 7.6.5 on macOS). |
| macOS | bash 3.2 (the one macOS ships) | 2 | Run in CI only (bash 3.2.57). One check is left out there and says so: expanding `PS1` the way a prompt does needs bash 4.4. |
| macOS | zsh | 2 | Run in CI only (zsh 5.9). |
| any | Theme files for Ghostty, kitty, WezTerm, Alacritty (`sereno export`) | 2 | Generated; each file is checked against a line grammar of its format. Not loaded into the emulators. |

The program is built for six targets: Windows, macOS and Linux, each on amd64 and arm64. The builds for Windows on amd64 and Linux on amd64 have been executed on the maintainer's machine and in CI, the build for macOS on arm64 in CI only. The builds for Windows on arm64, Linux on arm64 and macOS on amd64 are compiled in CI and not executed anywhere.

The program is tested with Go 1.27. `go.mod` allows Go 1.24 and newer; a CI job builds and tests with 1.24 (1.24.13 in the runs of 2026-10-08), and it passes.

## What you get, by system

| | Windows 11 | macOS, Linux |
| :--- | :--- | :--- |
| Six colour schemes offered to the emulator | Windows Terminal, by `sereno apply` | by `sereno export`, copied by you |
| A profile that starts a set-up shell | "Serena", in Windows Terminal | no |
| The terminal follows dark, light and contrast themes | yes, at each prompt, in sessions from the Serena profile | no: version 0.1 does not detect the system's appearance; use the light/dark pair of your emulator |
| Prompt with path, branch and result of the last command | PowerShell; Git Bash with a hook line | bash and zsh with a hook line; PowerShell 7 with a hook line |
| Line-editor colours | PowerShell (PSReadLine) | PowerShell 7 |
| Colours of `ls` without unreadable pairs | Git Bash | bash, zsh |
| Trying a palette with nothing installed (`sereno theme --paint`) | in a Windows console or Windows Terminal | in any terminal that accepts the sequences; not inside tmux or screen |

## Known limits

### PowerShell

- **Constrained language mode.** Where an application control policy (AppLocker, App Control for Business) puts PowerShell into constrained language mode, `init.ps1` leaves the session exactly as it is: no colours, no prompt, no message. This was tried by setting the language mode of a test process; it has not been confirmed on a machine that has such a policy. The hook line asks for the language mode first and does nothing there either.
- **Execution policy.** The Serena profile starts PowerShell with `-ExecutionPolicy RemoteSigned` for that one process and stores nothing. By PowerShell's documented rules that is enough under a stored policy of `Restricted`, and a policy set by Group Policy takes precedence over it; neither case has been tried on a machine. A hook line is different: it lives in your profile, and PowerShell does not load a profile under `Restricted` or `AllSigned`. `sereno apply --hook powershell` then refuses, and prints the command that would allow your own scripts (`Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`) without running it.
- **Finding the profile file.** To add a hook line, sereno asks PowerShell where the profile is: once, in the apply that is given `--hook powershell` or `--hook pwsh`, a dry run included. That is the only program sereno ever starts. Later applies look after the line in the file recorded then and start nothing, so they do not notice when PowerShell has come to name another profile since (a Documents folder that moved into OneDrive) or when the execution policy has changed; `sereno apply --hook powershell` asks again, and refuses with the way out when the profile has moved. If PowerShell reports no path (a Documents folder that is missing or out of reach) or does not answer within 15 seconds, the hook is refused; setting `SERENO_PS_PROFILE_PATH` (Windows PowerShell) or `SERENO_PWSH_PROFILE_PATH` (PowerShell 7) to the file tells sereno the answer, and then nothing is started. A Documents folder kept in OneDrive has not been tried.
- **History predictions** (PSReadLine 2.1 and newer) are switched on only where PSReadLine can show them. On Windows it refuses them in a session whose output is redirected and in a console without virtual terminal sequences; `init.ps1` does not ask for them there. Seen in CI under PowerShell 7.6.6 with PSReadLine 2.4.5: refused with the output redirected, accepted in a console.
- **Predictions you switched off** come back in a Serena tab. PSReadLine 2.2.6 and newer switch predictions on by themselves wherever they can show them, so with those versions a `PredictionSource` of `None` is one you set, in your profile. A Serena tab runs your profile first and `init.ps1` after it, and the comfort keys of `init.ps1` set `None` to `History`. `sereno apply --keys off` leaves the keys and this option as your profile set them. A session that loads `init.ps1` through a hook line does not get the comfort keys unless you asked for `--keys always`.
- **A prompt of your own** is never replaced. The Serena prompt is installed only over the stock PowerShell prompt. In a Serena tab your profile runs before `init.ps1`, so a prompt it defines is the one `init.ps1` finds and keeps. A test starts such a tab with a profile in front of it, on the Windows runner of CI only, under both PowerShells; it is the one test in which a profile takes part, and that profile is a file of its own temporary folder.
- **A home folder reached through a symbolic link** (macOS, Linux). The prompt shortens the home folder by its name. PowerShell names the folder a session starts in by its resolved path, so below such a home the prompt of a new session shows the path in full, not `~`. Seen on the macOS runner of CI, where the temporary folder is reached through a link (`/var` is `/private/var`); a home in the usual place is not affected. What the prompt shows after a change of folder by the other name has not been tried.
- **Start-up time.** On the maintainer's machine, Windows PowerShell 5.1 needs 71 to 73 ms to load `init.ps1` in a session from the Serena profile and 59 to 63 ms in a session with a hook line, and the first prompt costs another 58 to 63 ms or 32 to 34 ms, once. Every later prompt costs under 1 ms. A new Serena tab therefore waits about 130 to 135 ms for sereno, and a session with a hook line about 95 ms, on top of the start of PowerShell itself. That figure is `init.ps1` and the first prompt; `scripts/bench.ps1` does not time the hook line in front of them. The line asks for the language mode and whether the file is there and loads no module; the look for the file alone took 2 to 3 ms. A hook line stands in the profile that every `powershell.exe` reads unless it is started with `-NoProfile`, so a script, an editor task or a build tool that starts PowerShell without that switch loads `init.ps1` as well: in temporary homes on the maintainer's machine, which was not idle, `powershell -Command "exit 0"` took 243 to 296 ms with a profile that holds the hook line and 152 to 184 ms without a profile (medians of 25 starts in each of eight series, Windows PowerShell 5.1), 87 to 119 ms more for every such start. A second run of 45 such series, with the line as it is written now, gave 73 to 149 ms more: 73 to 85 ms in its first 26 series, and more in the later ones, in which PowerShell without a profile started more slowly too. None of this was measured in a real profile, and nothing under PowerShell 7. These are nine measurements of 2026-10-08 on a machine that was not always idle; one of them, with other work running, came to 81 ms from the Serena profile. PowerShell 7 has been measured on the runners of CI only, in six runs on 2026-10-08: on the hosted Windows runners it needed 120 to 175 ms from the Serena profile and 107 to 163 ms with a hook line, where Windows PowerShell 5.1 needed 86 to 129 ms and 69 to 99 ms on the same six runners; on the Linux runners 86 to 127 ms and 76 to 115 ms; on the macOS runners 62 to 69 ms and 53 to 72 ms. These are what six runs measured, not limits: each run draws another hosted machine, and one can be half as slow again as the next. On the same machine PowerShell 7 took 1.3 to 1.7 times as long as 5.1 in those runs, about one and a half times. About 30 ms of it (16 ms on macOS) is the look through `$PSStyle`, which 5.1 does not have.
- **Accented letters through a pipe.** sereno writes UTF-8. When Windows PowerShell 5.1 captures the output of a program (in a pipe or a variable) it decodes it in the code page of the console (850 on the maintainer's machine, a Windows in Brazilian Portuguese), and every letter outside ASCII arrives as two other characters: `simulação` as `simula├º├úo`, so a search for the word finds nothing. That is true of the Portuguese messages and just as much of a path: where the home folder has an accented letter in its name, the folders in a captured `sereno status --json` are not the real ones. On the console itself the text is right. Before capturing, tell PowerShell that programs write UTF-8, which lasts for that session: `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8`. With `--lang en` the messages need no such step; a path with an accented letter still does. Tried under Windows PowerShell 5.1 only.
- **A PowerShell that is fed through a pipe.** Inside Windows Terminal the Serena prompt carries its marks, and a session that follows the system writes the palette to standard output; `init.ps1` does not ask whether that is a console. A PowerShell that loads `init.ps1` through a hook line and reads its commands from standard input (not from `-Command` or `-File`) therefore hands the prompt, the marks and, when it was started inside a Serena tab, the palette to whatever reads its output. One that is given a command or a script to run shows no prompt, and its output is unchanged.

### bash and zsh

- **A read-only `PROMPT_COMMAND`** (an audit setting on some shared machines) cannot take sereno's hook. The session then keeps its prompt, does not follow the system, and gets the `ls` colours only. Nothing is printed about it.
- **A read-only `PS1`** keeps the prompt it has; the session still follows the system. A read-only `LS_COLORS` or `LSCOLORS` is left alone.
- **zsh**: the same rules for `precmd_functions`, `PROMPT` and `psvar`. They are written from the zsh manual, and their tests have run in CI only.
- **Another prompt tool.** With Starship or Oh My Posh active (and, in zsh, Oh My Zsh or Powerlevel10k), sereno does not install its prompt.
- **Following the system from bash** works only in Git Bash on Windows, where the registry can be read as files. On Linux and macOS there is nothing to follow in version 0.1.
- **Git Bash's own window.** In the window Git Bash opens by itself (mintty), standard output is not a Windows console. sereno is expected to treat it as "not a terminal" there: no colours in its own output, and `--paint` refused. This has not been checked in that window. Git Bash as a Windows Terminal profile is a console.

### Windows Terminal

- **Detection.** Windows Terminal counts as present when one of three folders exists below `%LOCALAPPDATA%`: `Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState`, `Packages\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\LocalState`, or `Microsoft\Windows Terminal`. Without one, no fragment is written (the plan says so) and `sereno status` warns with `wt.not-detected`.
- **Versions.** The fragment uses only keys that exist in the profile schema of Windows Terminal 1.24. The one thing seen on a screen so far, an open tab repainted (see the top of this page), was in 1.24.12741.0; the profile and the colour schemes of the fragment have not been looked at in any version yet. Versions older than 1.24 are not going to be checked.
- **Its own theme setting** decides whether the dark/light scheme pair follows Windows; see the README. sereno does not change it.
- **Its own settings file.** Windows Terminal adds an entry for the Serena profile to its `settings.json` the first time it sees the profile. sereno neither writes nor removes that entry; see [uninstall.md](uninstall.md).
- **Your defaults win.** Whatever your `settings.json` sets for all profiles (`profiles.defaults`) takes precedence over what the Serena profile asks for: that is how Windows Terminal 1.24 layers a profile that comes from a fragment, as read in its source. `sereno status` looks for three such settings, a colour scheme, a font and an environment, and warns with `wt.defaults-override`; any other key set there wins as well and is not reported.
- **The font** named by the profile is Cascadia Code, which comes with Windows Terminal. sereno installs no font.
- **Where a Serena tab starts.** The profile names your home folder (`%USERPROFILE%`) as its starting folder, as Windows Terminal's own profiles do, so a Serena tab is expected to start there; `wt -p Serena -d FOLDER` chooses another folder. Without that key a tab would start in the folder of whatever started Windows Terminal, which for a window opened from the Start menu is the System32 folder of Windows, by Windows Terminal's source code and Microsoft's documentation. Not seen yet.
- **Windows without Windows Terminal**: the classic console window gets the prompt and the line-editor colours through a hook line, and no palette.

### Hook lines

`--hook` appends one line to a start-up file and removes exactly that line again. It refuses, and prints the line so that you can add it yourself, when the file:

- is a symbolic link, a folder, or cannot be read;
- is read-only;
- is larger than 1 MiB, or would be with the line;
- is encoded as UTF-16 big endian, or holds NUL bytes without being UTF-16 little endian.

A file with CRLF line endings gets the line with CRLF, a UTF-16 little endian file gets it in that encoding, and a byte order mark is left where it is. On Windows, when `--hook bash` creates `~/.bashrc` and there is no `~/.bash_profile`, `~/.bash_login` or `~/.profile`, the plan notes that Git for Windows will create `~/.bash_profile` by itself the next time it starts.

A hook line that was asked for once is checked again by every later `sereno apply`. If you delete it by hand it comes back at the next apply; `sereno uninstall --hook LIST` withdraws it for good.

### Files you edit or protect

A file sereno wrote and that you then edited is left as it is on every system, by `apply` and by `uninstall`, and reported with exit code 4. The one exception is the settings file `mode`: `uninstall` deletes it even when it was edited. `sereno status` lists such files under `owned.modified`.

A file of sereno's that you marked read-only without changing it is left alone by `apply` and `theme` when they would replace it (exit code 4); `uninstall` removes it, because it is still what sereno wrote.

## Not part of 0.1

No emulator is built, bundled, installed or configured beyond the Windows Terminal fragment. Windows Terminal's `settings.json` is never edited. Nothing is installed: no PowerShell 7, no PSReadLine update, no fonts, no prompt tools, no completions or aliases. There is no detection of the system's appearance on macOS and Linux, no handling of tmux, screen, WSL or SSH sessions, and nothing for fish, Nushell, cmd, iTerm2 or Terminal.app. The programs are not signed, and there are no packages for winget, Scoop, Homebrew or Linux distributions.
