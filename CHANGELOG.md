# Changelog

What changed in each version of Serena Terminal. Versions are tags of the form `vMAJOR.MINOR.PATCH`; the section of a version is headed by its number without the `v`.

## Unreleased

Nothing has been released yet. This section describes version 0.1 as the code stands; it becomes the section `0.1.0` when that version is tagged.

### What 0.1 contains

- **`sereno`**, one command for Windows, macOS and Linux on amd64 and arm64, written in Go with the standard library only. It has no network code.
- **`sereno apply`** prints a plan and then writes: three session files (PowerShell, bash, zsh), a settings file and a manifest in sereno's own folder, and on Windows one fragment for Windows Terminal with a "Serena" profile and six colour schemes (Serena Dark, Dark Vivid, Light, Light Vivid, High Contrast, High Contrast Light). `--dry-run` prints the plan only. `--hook` adds one line to the start-up file of PowerShell, bash or zsh, after a backup when that file already exists, and is the only way a file of the user's is edited.
- **`sereno uninstall`** removes what apply wrote and nothing else, by the manifest: hook lines by their exact bytes, in the files the manifest names, files only when they are still as sereno wrote them, folders only when empty.
- **Following the system on Windows.** With `mode=auto`, a session started from the Serena profile checks the system's dark, light and contrast-theme setting before each prompt, inside the shell, and sends the matching palette to Windows Terminal when it changed. No background process.
- **`sereno theme`** shows or changes the mode (`auto`, `dark`, `light`, `hc`, `hc-light`), the flavour (`soft`, `vivid`) and where the palette is sent in band; `--paint` and `--reset` paint the current terminal without writing a file; `--show` prints the slots, the roles and the measured contrast.
- **`sereno status`** reports what is installed and what the session sees, as text or JSON, with eleven kinds of warning, among them Windows Terminal's own theme setting and a Serena profile that Windows Terminal keeps hidden.
- **`sereno check`** measures the contrast of every colour slot of the six variants against its background and fails under 4.5 (dark, light) or 7.0 (high contrast). Its Markdown output is `docs/contrast.md`.
- **`sereno export`** writes theme files for Ghostty, kitty, WezTerm and Alacritty and prints how to switch them on.
- **The session files** use the sixteen colour slots only, never colour values, and reverse video for highlighted bands. For PowerShell: line-editor colours, a two-line prompt with path, branch and result of the last command, a small set of comfort keys, and readable replacements for the progress bar of 5.1 and the directory style of 7. For bash and zsh: the prompt and `ls` colours without unreadable pairs. They print nothing, start no program, and are the same bytes for every user.
- **English and Brazilian Portuguese** for every message, chosen by `--lang`, `SERENO_LANG` or the language of the system.
- **One sereno at a time** writes a folder: a second command started at the same moment writes nothing and asks to be run again.
- **Install scripts** for a release, `install.ps1` and `install.sh`: they check the SHA-256 of the archive against a value they carry, place only the program, and change nothing else. **Release and CI workflows** for GitHub Actions: six archives, checksums, an attestation and a draft release from a tag; tests on Windows, macOS and Linux for every push.

### Not verified yet

- PowerShell 7, zsh and everything on macOS have run in CI only (GitHub-hosted runners; this is said of run 38473863799 of the CI workflow, on 2026-10-14, which ran commit 9977e60, the commit of the acceptance test, and passed in all nine jobs), where the automated tests pass. None of it has run on a machine of the maintainer, and no person has seen it on a screen. That run was the first of the comparison of the three prediction colours of PSReadLine in `test/shell/run.ps1` as it stands, which needs PSReadLine 2.1 or newer and so cannot run on the maintainer's machine; it passed there, under PowerShell 7.6.6 on Windows.
- What was committed after that commit has run on the maintainer's machine only (Windows 11, Windows PowerShell 5.1): sentences of the documents, three pictures, and the sentence `sereno apply` prints on Windows about opening a new window of Windows Terminal, in both languages, with the tests that hold it. No apply outside a test has printed that sentence. No run of CI is of a real user profile, and nothing has been confirmed on a machine that has an application control policy.
- The acceptance test of 2026-10-14 applied the program to the maintainer's real user profile and took it back again: what it wrote, a Serena tab on Serena Dark and what was left after the uninstall were checked from files, hashes and screenshots. Only his report, with no picture and no values recorded: that `sereno theme --paint light` repainted an open Windows Terminal tab (2026-10-08), that in the acceptance test the tab followed Windows to light and through contrast themes, and that VS Code's terminal and two other sessions with a hook line were fine.
- The exported theme files have not been loaded into their emulators.
- The release workflow has not run, and the download step of the install scripts has not been used against a real release.

### Known limits

See [docs/support-matrix.md](docs/support-matrix.md). The main ones: no detection of the system's appearance on macOS and Linux; a new Serena tab is repainted at its first prompt, not before; Windows Terminal adds an entry of its own to its settings for the Serena profile, which sereno neither writes nor removes; after `sereno uninstall`, a Serena scheme that you chose yourself in Windows Terminal's settings makes Windows Terminal report an invalid colorScheme at every start until that name is taken out (see docs/uninstall.md); the programs are not signed.
