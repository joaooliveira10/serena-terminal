# How it works

This page explains what `sereno` does once it is applied: how the colours reach the screen, why the shell files contain no colour values, what the settings file holds, and what the check before each prompt costs. Version 0.1 is not released yet, and nothing described here has been looked at on a screen in Windows Terminal yet; where a sentence is about what Windows Terminal does rather than about what sereno does, it says so.

The measurements on this page were taken on 2026-10-08 on one machine: Windows 11, Windows PowerShell 5.1.26100 with PSReadLine 2.0.0, and the bash 5.3.15 of Git for Windows. PowerShell 7 and zsh are not installed on that machine: their figures come from the hosted runners of CI and are marked as such.

## Three routes for the palette

A terminal has sixteen numbered colours ("slots" 0 to 15) plus a foreground, a background, a cursor colour and a selection colour. Which actual colour a slot shows is decided in one of three places. `sereno status` and `sereno theme` print the route of the session they run in.

| Route | Who decides the colours | Where it applies |
| :--- | :--- | :--- |
| `emulator` | The emulator's own colour scheme. | Windows Terminal reads the six Serena schemes from the fragment sereno writes; Ghostty, kitty, WezTerm and Alacritty read the files of `sereno export`, once you have copied them. |
| `in-band` | The shell session: sereno's session file sends the colours to the terminal as escape sequences, at the prompt. | Windows Terminal, in sessions that follow the system (see below). |
| `host` | The application the terminal is part of. | VS Code's integrated terminal, where the editor's colour theme owns the palette. sereno never sends colours there. |

The route of a session follows from its environment, in this order:

1. `TERM_PROGRAM` is `vscode`: `host`.
2. `WT_SESSION` is set (the session is inside Windows Terminal), the setting `inband` is not `off`, and either `inband` is `always` or the session was started from the Serena profile: `in-band`.
3. Anything else: `emulator`.

A session counts as started from the Serena profile when `SERENO_SESSION` is `wt-profile`, which the profile sets, or when `WT_PROFILE_ID` is the identifier of the Serena profile.

The in-band route exists because of how Windows Terminal follows the system. The Serena profile names a pair of schemes, one for dark and one for light, and Windows Terminal switches such a pair according to its own theme setting, not according to Windows. That setting is "dark" unless the user changed it to "Use Windows theme", and `sereno status` reports it (`wt.theme-not-system`). The in-band route does not depend on it.

What is sent in band is twenty sequences: OSC 10, 11, 12 and 17 for the foreground, the background, the cursor and the selection background, then OSC 4 for each of the slots 0 to 15. Handing the colours back to the emulator's own scheme is five sequences: OSC 104, 110, 111, 112 and 117. `sereno theme --paint VARIANT` and `sereno theme --reset` write the same bytes on request.

## Slots, not hex

Only the emulator level knows colour values. Everything sereno generates above it (the three session files and sereno's own output) names colours by slot, through the SGR parameters every terminal understands: 30 to 37 and 90 to 97 for the sixteen slots as text colour, 39 for the default, and the attributes 0, 1, 3, 4, 7, 22, 23, 24 and 27. A test renders the session files and fails on anything else, in particular on 38 and 48 (direct colours) and on 40 to 47 and 100 to 107 (backgrounds).

The consequence is that one set of session files serves all six variants: changing from dark to light changes what the slots show, not what the shell asks for. The roles are assigned like this (the table in [contrast.md](contrast.md) is generated from the same file, `internal/palette/roles.json`):

| Role | SGR | Role | SGR |
| :--- | :--- | :--- | :--- |
| function, command | `34` | comment | `90` |
| keyword | `35` | error | `91` |
| string | `32` | emphasis | `96` |
| type | `33` | selection | `7` |
| property, member | `36` | prompt: path | `34` |
| constant, number | `93` | prompt: branch | `35` |
| parameter | `3;37` | prompt: last command succeeded | `32` |
| operator | `37` | prompt: last command failed | `31` |
| variable, default | `39` | prompt: duration | `90` |

The colour values themselves are in one file, `internal/palette/serena.json`. It is generated from the six built theme files of Serena Theme at the tag v1.1.0 by `go run ./internal/tools/importpalette`, and the CI workflow imports it again and compares. No other source file may contain a colour value; a test enforces that too.

`sereno check` measures the palette: every slot from 1 to 15 against the background of its variant, the foreground, the cursor, and the selection pair, with the WCAG contrast formula. The floors are 4.5 for the dark and light variants and 7.0 for the two high-contrast ones (3.0 for the cursor). Slot 0 is a surface colour on dark backgrounds and is exempt there. The result is committed as [contrast.md](contrast.md), and the command fails when a value is under its floor.

## Reverse, do not paint

A highlighted band, such as selected text in the line editor, is drawn with reverse video (SGR 7), which swaps the text colour and the background of the terminal. sereno never writes one slot as text on another slot as background.

The reason is measured on the palette. Of the 120 ways of putting one of the sixteen slots as text on one of the eight background slots (the same slot on itself left out), the number that reaches a contrast of 4.5 is:

| Serena Dark | Serena Dark Vivid | Serena High Contrast | Serena Light | Serena Light Vivid | Serena High Contrast Light |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 21 | 21 | 22 | 0 | 0 | 0 |

The slots are chosen to be read against the background of the theme, not against each other. Programs that print such pairs anyway stay hard to read, and sereno cannot change programs. It does change the four cases it found in the shells it sets up:

| Where | The default | Contrast on the six variants | What sereno sets |
| :--- | :--- | :--- | :--- |
| PSReadLine, selected text | a fixed pair | | reverse video |
| Windows PowerShell 5.1, progress bar | yellow on dark cyan | 1.14 to 1.46 | the console's own text and background colours |
| PowerShell 7, directories in listings | bold on a blue background (`44;1`) | 1.45 to 2.23 | the same slot as text (`34;1`); every `$PSStyle` value with a background parameter is treated the same way |
| GNU `ls`, directories writable by others | blue on green (`34;42`) | 1.01 to 1.44 | text colours only, through `LS_COLORS`; an `LS_COLORS` you already have keeps its entries, and only the ones with a background are rewritten |

The PowerShell 7 row was written from the defaults in its source code and has since been confirmed by the tests in CI under PowerShell 7.6: directories come as `44;1` and leave as `34;1`, and after `init.ps1` no value of `$PSStyle` paints a background.

## The settings file

`mode`, in sereno's own folder, is five lines of ASCII text, always in this order. These are the values of a first apply:

```text
mode=auto
flavor=soft
inband=profile
keys=profile
prompt=on
```

| Line | Values | Meaning |
| :--- | :--- | :--- |
| `mode` | `auto`, `dark`, `light`, `hc`, `hc-light` | `auto` follows the system; the others fix one appearance. `hc` is high contrast. |
| `flavor` | `soft`, `vivid` | The softer or the more vivid colours, for dark and light. The high-contrast variants have one flavour. |
| `inband` | `profile`, `always`, `off` | Where the session sends the colours at the prompt: in sessions started from the Serena profile, in every Windows Terminal session that loads sereno's files, or nowhere. |
| `keys` | `profile`, `always`, `off` | Where the PSReadLine comfort keys are set. |
| `prompt` | `on`, `off` | Whether the Serena prompt is installed. |

`sereno apply` and `sereno theme` are the only things that write the file. The session files read it; a file that is missing or is anything other than those five lines counts as the five values above. That includes a file saved with a byte order mark or as UTF-16: the bytes decide, in the program and in all three shells alike.

With `mode=auto` the variant follows from three facts about the system:

| A contrast theme is on | Its window colour is light | Applications use the light theme | Variant |
| :--- | :--- | :--- | :--- |
| yes | yes | | `hc-light` |
| yes | no | | `hc` |
| no | | yes | `light`, or `light-vivid` with `flavor=vivid` |
| no | | no | `dark`, or `dark-vivid` with `flavor=vivid` |

On Windows the three facts are values under `HKEY_CURRENT_USER` in the registry, which sereno and its session files only read: bit 0 of `Flags` in `Control Panel\Accessibility\HighContrast`; the three numbers of `Window` in `Control Panel\Colors`, weighed as 0.2126 R + 0.7152 G + 0.0722 B and counted as light above 127; and `AppsUseLightTheme` in `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`. A value that cannot be read counts as off, or dark. That the window colour tells a light contrast theme from a dark one is an assumption that has not been checked against real contrast themes yet. Git Bash reads the same values as files below `/proc/registry/HKEY_CURRENT_USER`. The variable `SERENO_OS_MODE` (`dark`, `light`, `hc` or `hc-light`) replaces all three, which is how the tests simulate the system without changing a setting.

On macOS and Linux there is no detection in version 0.1: the facts are unknown, `auto` resolves to dark, and nothing is sent in band.

## The check before each prompt

In a session that follows the system, this runs before every prompt, inside the shell:

1. If the settings file changed since it was last read, read it again. PowerShell compares the time of the last write; bash and zsh read the five lines each time.
2. Read the three facts about the system.
3. Work out the variant.
4. If it is not the variant this session sent last, write the twenty sequences to the console. The first prompt of a session always sends them. Otherwise write nothing.

If the settings no longer allow sending (someone ran `sereno theme --inband off`) and this session has sent colours, it writes the five sequences that hand the palette back, once.

Nothing here starts a program. The tests prove it by running the session files and their prompts with `PATH` emptied. There is no background process either, so nothing changes while the shell sits idle: the change shows at the next prompt. A file that another program holds open at that moment (the settings file, or a file of a Git repository) is read at the following prompt instead; the prompt does not fail.

What it costs, from three runs of `scripts/bench.ps1`:

| | Measured | Budget on the maintainer's machine | Gate in CI |
| :--- | :--- | :--- | :--- |
| Loading `init.ps1`, session started from the Serena profile | 67 to 71 ms | 80 ms | 150 ms |
| Loading `init.ps1`, session with a hook line | 56 to 59 ms | 65 ms | 150 ms |
| One prompt with the check active | 0.8 to 0.9 ms | 2 ms | 10 ms |
| The first prompt of a session, once (not judged) | 57 to 58 ms from the Serena profile, 33 ms with a hook line | | |
| Loading `init.bash` | 3 ms | 60 ms | 60 ms |
| The bash hook before one prompt, check active | 1.9 to 2.0 ms | 10 ms | 10 ms |

Loading is measured inside the process, around the line that loads the file, as the median of nine fresh processes that have no start-up cache of an earlier PowerShell to replay; that is the slowest case. The prompt figure is the mean of 200 calls. A new Serena tab therefore waits about 125 to 130 ms for sereno before its first prompt, on top of the time PowerShell itself needs to start.

PowerShell 7 and zsh were measured in the same way on the runners of CI, in two runs on 2026-10-08. Those are hosted machines, with two processors on Windows and Linux, and one of them can be half as slow again as the next. Loading `init.ps1` under PowerShell 7.6 on Windows: 120 and 170 ms from the Serena profile, 107 and 131 ms with a hook line, where Windows PowerShell 5.1 took 86 and 129 ms, 69 and 99 ms on the same two runners. On Linux: 86 and 118 ms, 76 and 108 ms. On macOS: 62 and 67 ms, 57 and 72 ms. About 30 ms of that (16 ms on macOS) is the look through `$PSStyle`, measured in the first of the two runs. One prompt with the check active: 0.4 to 1.0 ms. Loading `init.zsh`: 1 ms; its hook before one prompt: 0.3 to 0.4 ms. The gates in CI are the same for PowerShell 7 as for 5.1, with one exception: a session from the Serena profile under PowerShell 7 on Windows has 210 ms, because PowerShell 7 takes 1.3 to 1.4 times as long as 5.1 for the same file on the same runner. zsh has the gates of bash.

## The prompt and the line editor

These are set up by the session files, and only in an interactive session.

PowerShell (`init.ps1`, the same file for 5.1 and 7):

- It does nothing unless the host is the console host and the session is in full language mode. A session that an application control policy has put into constrained language mode is left exactly as it is.
- The Serena prompt replaces the stock PowerShell prompt only. It shows the path (the home folder as `~`), the branch of the Git repository when there is one, the duration of the last command when it took 2 seconds or more, and on a second line a `>` in slot 2 after a command that succeeded and in slot 1 after one that failed. The branch is read from the files of the repository; `git` is not started.
- A prompt of your own is kept. Inside Windows Terminal it is wrapped so that the check above still runs after it; its output is returned unchanged.
- Inside Windows Terminal the Serena prompt also emits the marks that let the terminal find prompts and the current folder (OSC 133 and OSC 9;9).
- The colours of PSReadLine, the line editor, are set through slots, when PSReadLine is already loaded. Keys of newer PSReadLine versions are set only where they exist.
- The comfort keys, where the `keys` setting allows them: Tab opens the completion menu, the up and down arrows search the history for what is already typed, the bell is off, duplicates are kept out of the history, and history predictions are switched on where PSReadLine has them, they were off, and PSReadLine can show them (on Windows it cannot where the output is redirected or the console has no virtual terminal sequences). Nothing else is bound, and no module is loaded.

bash (`init.bash`, written for bash 3.2 and newer) and zsh (`init.zsh`):

- The prompt shows the same things on two lines, without the duration. It is installed through `PROMPT_COMMAND` in bash and `precmd_functions` in zsh, in addition to what is already there. It is not installed when Starship or Oh My Posh is active, and in zsh also not under Oh My Zsh or Powerlevel10k.
- The name of the branch reaches the prompt as data, never as text the shell evaluates, so a branch named like a command cannot run anything.
- `LS_COLORS` and `LSCOLORS` are set as described above. No key binding, alias, completion or history setting is touched.
- A variable of yours that is read-only (`PS1`, `PROMPT_COMMAND`, `LS_COLORS` and their zsh counterparts) is left alone, without an error message.

All three files are the same for every user of the same version: no path, user name or fact about the machine is written into them. They find sereno's folder from their own location.

## Plans, the manifest and the lock

Every command that writes works in two phases. First it computes every operation and every reason to refuse, without writing; if there is a reason to refuse, it prints them all and writes nothing (exit code 3). `--dry-run` stops after printing the plan. Then it writes, in this order: folders, backups of files about to be edited, the manifest, sereno's own files, hook lines. Each file is written under a temporary name in the same folder and renamed into place.

`manifest.json` in sereno's folder records what was written: the SHA-256 of every file sereno owns, the folders it created in the order it created them, and for each hook line the file, the exact bytes that were added and the SHA-256 of the file before. It holds no time stamp, so applying twice leaves it byte for byte the same. The rules that follow from it:

- A file that is in the way and is not in the manifest is not sereno's: apply refuses.
- A file in the manifest whose content is still what sereno wrote is replaced when a newer version differs, and otherwise kept.
- A file in the manifest that you edited, or that is read-only when it would be replaced, is left alone and reported (exit code 4).
- A file in the manifest that is gone is written again.

One sereno at a time writes a folder. While a command writes, it holds an operating-system lock on an empty file named `lock` in sereno's folder, and it computes its plan once more under that lock. A second command that arrives in that moment writes nothing and ends with exit code 1 and the sentence "another sereno is working on this folder; run the command again when it has finished". The lock ends with the process that holds it. A command that is killed can leave the empty file behind; the next command that writes takes it away. `sereno status` takes no lock and may, for a moment, describe an installation that another command is half way through writing.

[uninstall.md](uninstall.md) describes the way back.
