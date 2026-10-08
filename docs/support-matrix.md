# Support matrix

What Serena Terminal 0.1 supports, on which system and shell, and how far each part has been checked. Version 0.1 is not released yet; this page describes the state of the code on 2026-10-08.

Two tiers:

- **Tier 1**: the maintainer has it on his own machine, it is tested there, and he is to look at it on screen before a release. That is Windows 11 with Windows Terminal and PowerShell.
- **Tier 2**: generated and tested by programs. Nobody has looked at the result on a screen.

"Run" below means that the automated tests of this repository ran on that combination and passed. It does not mean that a person has looked at the colours. That check, on screen in Windows Terminal, and a first apply to a real user profile are both still to come.

## Systems and shells

| System | Shell or emulator | Tier | State |
| :--- | :--- | :--- | :--- |
| Windows 11 | Windows Terminal: the Serena profile and six colour schemes, through a fragment | 1 | The fragment is tested as a file (its bytes, its JSON, its keys against a copy of Windows Terminal's profile schema). Not looked at in Windows Terminal yet. |
| Windows 11 | Windows PowerShell 5.1 | 1 | Run, on the maintainer's machine (5.1.26100, PSReadLine 2.0.0). |
| Windows 11 | PowerShell 7 | 1 | Not run yet. The session file is the same as for 5.1 and decides by what it finds. CI will be the first run. |
| Windows 11 | Git Bash (the bash of Git for Windows) | 2 | Run (bash 5.3.15). |
| Windows 11 | VS Code's integrated terminal | | Prompt and line-editor colours through a hook line. sereno does not repaint there. Not looked at. |
| Linux | bash | 2 | Run on Ubuntu 26.04 under WSL (bash 5.3.9). |
| Linux | zsh | 2 | Not run yet. CI will be the first run. |
| Linux, macOS | PowerShell 7 | 2 | Not run yet. |
| macOS | bash 3.2 (the one macOS ships) | 2 | Not run yet. The file is written for bash 3.2. |
| macOS | zsh | 2 | Not run yet. |
| any | Theme files for Ghostty, kitty, WezTerm, Alacritty (`sereno export`) | 2 | Generated; each file is checked against a line grammar of its format. Not loaded into the emulators. |

The program is built for six targets: Windows, macOS and Linux, each on amd64 and arm64. Only the builds for Windows on amd64 and Linux on amd64 have been executed so far. The CI runners add macOS on arm64. The builds for Windows on arm64, Linux on arm64 and macOS on amd64 are compiled in CI and not executed anywhere.

The program is tested with Go 1.27. `go.mod` allows Go 1.24 and newer; a CI job builds and tests with 1.24, and that job has not run yet either.

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

- **Constrained language mode.** Where an application control policy (AppLocker, App Control for Business) puts PowerShell into constrained language mode, `init.ps1` leaves the session exactly as it is: no colours, no prompt, no message. This was tried by setting the language mode of a test process; it has not been confirmed on a machine that has such a policy.
- **Execution policy.** The Serena profile starts PowerShell with `-ExecutionPolicy RemoteSigned` for that one process and stores nothing. By PowerShell's documented rules that is enough under a stored policy of `Restricted`, and a policy set by Group Policy takes precedence over it; neither case has been tried on a machine. A hook line is different: it lives in your profile, and PowerShell does not load a profile under `Restricted` or `AllSigned`. `sereno apply --hook powershell` then refuses, and prints the command that would allow your own scripts (`Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`) without running it.
- **Finding the profile file.** To add a hook line, sereno asks PowerShell once where the profile is. That is the only program sereno ever starts. If PowerShell reports no path (a Documents folder that is missing or out of reach) or does not answer within 15 seconds, the hook is refused; setting `SERENO_PS_PROFILE_PATH` (Windows PowerShell) or `SERENO_PWSH_PROFILE_PATH` (PowerShell 7) to the file tells sereno the answer, and then nothing is started. A Documents folder kept in OneDrive has not been tried.
- **A prompt of your own** is never replaced. The Serena prompt is installed only over the stock PowerShell prompt.
- **Start-up time.** On the maintainer's machine, Windows PowerShell 5.1 needs 67 to 71 ms to load `init.ps1` in a session from the Serena profile and 56 to 59 ms in a session with a hook line, and the first prompt costs another 57 to 58 ms or 33 ms, once. Every later prompt costs under 1 ms. A new Serena tab therefore waits about 125 to 130 ms for sereno, and a session with a hook line about 90 ms, on top of the start of PowerShell itself. PowerShell 7 has not been measured.
- **Text in Portuguese through a pipe.** sereno writes UTF-8. When Windows PowerShell 5.1 captures the output of a program (in a pipe or a variable) it decodes it in the code page of the console, and accented letters come out wrong. On the console itself the text is right.

### bash and zsh

- **A read-only `PROMPT_COMMAND`** (an audit setting on some shared machines) cannot take sereno's hook. The session then keeps its prompt, does not follow the system, and gets the `ls` colours only. Nothing is printed about it.
- **A read-only `PS1`** keeps the prompt it has; the session still follows the system. A read-only `LS_COLORS` or `LSCOLORS` is left alone.
- **zsh**: the same rules for `precmd_functions`, `PROMPT` and `psvar`. They are written from the zsh manual and have not run.
- **Another prompt tool.** With Starship or Oh My Posh active (and, in zsh, Oh My Zsh or Powerlevel10k), sereno does not install its prompt.
- **Following the system from bash** works only in Git Bash on Windows, where the registry can be read as files. On Linux and macOS there is nothing to follow in version 0.1.
- **Git Bash's own window.** In the window Git Bash opens by itself (mintty), standard output is not a Windows console. sereno is expected to treat it as "not a terminal" there: no colours in its own output, and `--paint` refused. This has not been checked in that window. Git Bash as a Windows Terminal profile is a console.

### Windows Terminal

- **Detection.** Windows Terminal counts as present when one of three folders exists below `%LOCALAPPDATA%`: `Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState`, `Packages\Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe\LocalState`, or `Microsoft\Windows Terminal`. Without one, no fragment is written (the plan says so) and `sereno status` warns with `wt.not-detected`.
- **Versions.** The fragment uses only keys that exist in the profile schema of Windows Terminal 1.24. Nothing has been looked at in any version yet; versions older than 1.24 are not going to be checked.
- **Its own theme setting** decides whether the dark/light scheme pair follows Windows; see the README. sereno does not change it.
- **Its own settings file.** Windows Terminal adds an entry for the Serena profile to its `settings.json` the first time it sees the profile. sereno neither writes nor removes that entry; see [uninstall.md](uninstall.md).
- **Your defaults win.** If your `settings.json` sets a colour scheme, a font or an environment for all profiles (`profiles.defaults`), that takes precedence over what the Serena profile asks for. `sereno status` warns with `wt.defaults-override`.
- **The font** named by the profile is Cascadia Code, which comes with Windows Terminal. sereno installs no font.
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
