# ADR 0002: Slots, not hex; reverse, do not paint

Status: accepted, 2026-10-07. Requirements R6, R8 and R9 of [the specification](../spec-0.1.md) are the binding text.

## Context

Serena has six variants: dark and light, each soft or vivid, and two high-contrast ones. Shell-level colour settings (the line editor of PowerShell, prompts, `LS_COLORS`) could name colours directly, as 24-bit values. Then every file above the emulator would exist once per variant, and following the system would mean changing the state of a running shell, not only the colours of the terminal. It would also leave legibility unmeasured: every direct colour would be a new pair to check.

A terminal already has an indirection for this: sixteen numbered colour slots, whose actual colours the emulator decides.

## Decision

- Colour values exist in one source file, `internal/palette/serena.json`, which is generated from Serena Theme. They leave it only towards the emulator level: the Windows Terminal fragment, the exported theme files, and the sequences that hand the palette to the terminal.
- Everything above the emulator (the three session files and sereno's own output) uses the SGR parameters of the sixteen slots and a few attributes, and nothing else. The mapping from roles to slots is one table, `internal/palette/roles.json`.
- A highlighted band uses reverse video. A slot is never written as text on another slot as background.
- `sereno check` measures every slot against the background of its variant, with floors of 4.5 (dark, light) and 7.0 (high contrast), and fails below them. Tests enforce the first three points on the generated files.

## Consequences

- One set of session files serves all six variants. Switching the variant is a change of palette and of nothing else, which is what makes following the system cheap.
- The legibility claim is finite and checked: fifteen slots against one background per variant, plus the foreground, the cursor and the selection. The results are in [contrast.md](../contrast.md).
- Sixteen slots cannot carry the whole role palette of Serena Theme. Numbers and constants share bright yellow, and parameters are italic on slot 7, because neither has a slot of its own.
- Pairs of one slot on another are mostly unreadable, by measurement: of the 120 combinations of a text slot on one of the eight background slots, 21, 21 and 22 reach 4.5 on Serena Dark, Dark Vivid and High Contrast, and none on the three light variants. sereno never writes such a pair and corrects the four it found in the shells it sets up (see [how-it-works.md](../how-it-works.md)); a program that prints its own pairs stays hard to read.
- Slot 0 as text is unreadable on the dark variants (1.21 to 1.52 against the background) and is exempt from the gate there, as a surface colour.
- A change of a colour is made in Serena Theme first and imported; it is not edited here.
