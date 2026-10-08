# ADR 0003: The session sends the palette in band

Status: accepted, 2026-10-07, with a fallback that is still open. Requirements R28 to R30 of [the specification](../spec-0.1.md) are the binding text.

## Context

Windows Terminal can hold a pair of colour schemes for a profile, one for dark and one for light. It switches the pair by its own theme setting. The specification records that this setting is "dark" in the versions it looked at (1.24, 1.25 and the development branch), so a pair alone does not follow Windows until the user changes a global setting of Windows Terminal. A pair also has no place for a contrast theme.

Changing that setting would mean editing Windows Terminal's `settings.json`, a file sereno does not own. sereno's rule is to create only files it owns, plus one line in a start-up file on request.

Terminals accept colour changes from the program running inside them, through escape sequences (OSC 4, 10, 11, 12 and 17). The specification records that Windows Terminal 1.24 parses them, from a reading of its source code.

## Decision

- A session started from the Serena profile checks the system before each prompt: three values of the current user's registry, read inside the shell, with no other program started. When the variant that follows from them is not the one the session sent last, it writes the palette of that variant to the terminal.
- Where this happens is a setting, `inband`: `profile` (sessions from the Serena profile, the default), `always` (every Windows Terminal session that loads sereno's files) or `off`.
- It never happens in VS Code's terminal, where the editor's theme owns the palette, and never outside Windows Terminal. Painting on request (`sereno theme --paint`) is refused under tmux and screen.
- The fragment still names the dark and light pair, so that a user whose Windows Terminal is set to "Use Windows theme" gets the right scheme from the first frame.

## Consequences

- The command line follows dark, light and both kinds of contrast theme without sereno editing a file of Windows Terminal.
- There is no background process. The cost is paid at the prompt: under 1 ms per prompt on the one machine it was measured on (Windows PowerShell 5.1), and about 70 ms once, when the session file loads.
- A change shows at the next prompt, not while the shell is idle.
- The first frame of a new tab is the emulator's choice, and an open tab that Windows Terminal resets by itself stays reset until the variant changes or `sereno theme` is run in it.
- The same bytes give `sereno theme --paint VARIANT`: a way to try a palette in any terminal that accepts the sequences, with nothing installed.
- The route rests on Windows Terminal honouring the sequences, including for the background of the whole window. That has been read in its source, not seen on a screen. A short trial on the maintainer's machine is planned before the release. If it fails, the fallback is already part of the design: ship with `inband=off` as the default, and follow the system through the scheme pair only.
- macOS and Linux have no detection in 0.1, so nothing is sent in band there; the emulator's own light and dark pair is the route.
