# Privacy

What `sereno` reads, what it writes, what it keeps, and what it never does. This describes the code as it is in version 0.1, which is not released yet.

## What it never does

- **No network.** The program has no network code. It opens no connection, sends no telemetry and no crash report, has no account, and does not look for updates. This is enforced rather than promised: a test fails, and so does a step of the CI workflow, if the program links Go's `net` or `net/http` package on any of the six targets.
- **No program is started**, with one exception that only happens on request and is described below.
- **The session files** that sereno generates for PowerShell, bash and zsh start no program and open no connection. They are searched for every way a script has of doing either, and the tests also run them with `PATH` emptied.
- **Nothing is installed**: no tool, font, shell, emulator or package manager.
- **No elevated rights.** sereno never asks for administrator rights or `sudo`, and works only in the current user's own folders.

## What it reads

- **Environment variables**, to find folders and to recognise the session: `LOCALAPPDATA`, `USERPROFILE`, `HOME`, `XDG_DATA_HOME`, `ZDOTDIR`, `PATH`, `PATHEXT`; `WT_SESSION`, `WT_PROFILE_ID`, `TERM_PROGRAM`, `TERM`, `TMUX`, `NO_COLOR`; `LC_ALL`, `LC_MESSAGES`, `LANG`; and its own: `SERENO_HOME`, `SERENO_LANG`, `SERENO_OS_MODE`, `SERENO_SESSION`, `SERENO_SANDBOX`, `SERENO_PS_PROFILE_PATH`, `SERENO_PWSH_PROFILE_PATH`.
- **On Windows, three values of the current user's registry**, which it only reads: whether applications use the light theme, whether a contrast theme is on, and the window colour of that theme. It also asks Windows for the language of the user interface, to choose between English and Portuguese.
- **Windows Terminal's `settings.json` and `state.json`**, read only, by `sereno status` and `sereno uninstall`: its theme setting, whether your defaults override the profile, and whether the Serena profile is known there.
- **Its own folder**, and the files it is about to write.
- **A start-up file of a shell**, only when you ask for a hook line with `--hook`: the file is read to decide its encoding and line endings, and to find an earlier line of sereno's.
- **In a session with the Serena prompt:** the `.git` folder of the directory you are in, to show the name of the branch. It reads the `HEAD` file; `git` is not started.

## What it writes

Only what the plan lists, and the plan is printed first:

- files in its own folder (`%LOCALAPPDATA%\sereno`, or `~/.local/share/sereno`);
- on Windows, one fragment file for Windows Terminal;
- on request, one line in a start-up file of a shell;
- the theme files of `sereno export`, in its own folder or in a folder you name.

Never: Windows Terminal's settings, the registry, `PATH`, an execution policy, a login shell, or the configuration folder of an emulator. The README has the list with an example.

## What it keeps about you

Everything stays on your machine, in sereno's own folder:

- `manifest.json` holds the full paths of the files and folders sereno wrote (a path contains your user name), the SHA-256 of each file, and the options of the last apply. It holds no time stamp.
- `backups` holds a **copy of each start-up file** that already existed when sereno added a hook line to it, as the file was before. Such a file can contain things you would not publish: tokens, internal host names, aliases. The copy gets permission bits no wider than the file it came from. Uninstall deletes it when the start-up file is back to its earlier content, and keeps it, and says so, when you changed the file in between. Keep this in mind if you copy or share sereno's folder.

## The one program it may start

An apply that names `powershell` or `pwsh` in `--hook`, a dry run too, needs to know where that PowerShell keeps your profile. It asks it once:

```text
powershell.exe -NoProfile -NonInteractive -Command "$PROFILE.CurrentUserCurrentHost; Get-ExecutionPolicy"
```

(or `pwsh` with the same arguments). It reads the two lines of the answer and stops the process if there is none within 15 seconds. Later applies look after the line in the file recorded then and start nothing. Nothing else is ever started. If `SERENO_PS_PROFILE_PATH` or `SERENO_PWSH_PROFILE_PATH` names the profile file, not even this is. sereno writes nothing through that process; PowerShell may write the start-up cache it keeps below `%LOCALAPPDATA%\Microsoft\Windows\PowerShell`, as it does at any start (seen in a temporary home).

## What the Serena profile sets for PowerShell 7

The Serena profile of Windows Terminal starts PowerShell with three environment variables: `SERENO_SESSION=wt-profile`, by which the session knows where it was started from, and `POWERSHELL_TELEMETRY_OPTOUT=1` and `POWERSHELL_UPDATECHECK=Off`, which switch off PowerShell 7's own telemetry and update check in the sessions of that profile. They apply to nothing else, and Windows PowerShell 5.1 has neither feature.

## What the terminal is told

Inside Windows Terminal, the Serena prompt emits the usual marks that let the terminal find prompts (OSC 133) and learn the current folder (OSC 9;9), and the colour sequences described in [how-it-works.md](how-it-works.md). They go to the terminal window the session runs in, on your machine, and nowhere else.

## Getting the program

The program does not use the network, but getting it does:

- `go install` downloads the source through Go's module proxy and checks it against Go's checksum database, as it does for any module. Those are services run by Google; their own privacy terms apply.
- A release archive and the install scripts are downloaded from GitHub. The install script makes one request, for the archive of your system, and compares its SHA-256 with the value the script carries. With `SERENO_INSTALL_FROM` set to a folder that holds the archive, it downloads nothing.

## The repository

The CI and release workflows of this repository use no secret besides the token GitHub gives every run, and every action they use is pinned to a commit. A release is created as a draft that the maintainer publishes by hand.
