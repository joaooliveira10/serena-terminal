# ADR 0001: Serena Terminal is not a terminal emulator

Status: accepted, 2026-10-07. The reasoning and the evidence of that day are in [the specification](../spec-0.1.md), sections "Decision", "Key choices" and "Where the research disagreed".

## Context

The goal of version 0.1 is one promise: the command line follows the system's dark, light and high-contrast setting, with a legibility that is measured, on a stock Windows 11 machine with nothing else installed, and one command removes everything again.

There were four shapes the project could take: build or fork a terminal emulator; bundle and configure an existing one (WezTerm was the candidate); publish a dotfiles repository; or write a small companion command that configures what people already run. The project has one maintainer. The people it is for already have a terminal, and on Windows 11 that terminal is Windows Terminal.

## Decision

Serena Terminal is a companion command, `sereno`, and the configuration it generates. It is not an emulator, not a fork of one, not a dotfiles repository and not a collection of shell plugins.

- No emulator is installed, bundled or required. The tier 1 host is Windows Terminal, reached through one fragment file that sereno owns. Ghostty, kitty, WezTerm and Alacritty get generated theme files that the user copies.
- The shell the user already runs is configured, never replaced: Windows PowerShell 5.1 and PowerShell 7 with one session file, bash and zsh with a small one. No login shell or default shell is changed.
- The program is called `sereno`. A command called `serena` is never shipped, because the Oraios coding tool of that name installs one. The first sentence of the README says what the project is not.

## Consequences

- It works with what is on the machine, and the set of files it writes is small enough to record completely, which is what makes an exact uninstall possible.
- It can do only what the host allows. Windows Terminal decides the first frame of a new tab, follows its own theme setting rather than Windows, and writes an entry of its own to its settings when it sees the profile. sereno documents these instead of working around them by editing files it does not own.
- Version 0.1 is thin on macOS and Linux: a shell layer and exported themes, and no detection of the system's appearance. It may be read as a Windows project; the support matrix says plainly what each system gets.
- The word "Terminal" in the name still suggests an emulator. The command name and the README carry the correction.
- To keep one maintainer's project from growing sideways, this rule holds until version 0.4: nothing is added unless it serves the one promise above. Tool bundles, fonts, completions, key maps and emulator configuration are deferred, each to a named later version in the specification.
