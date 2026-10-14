# Uninstall

`sereno uninstall` is the undo of `sereno apply`. This page says exactly what it removes, what it leaves, and why. The examples are the output of real runs in a test home folder, which is shown as the home of a user named `user`. Version 0.1 is not released yet. What Windows Terminal does on its side was read from its source code; the acceptance test of 2026-10-14 on the maintainer's machine looked at part of it, and the sentences say which part, and whether it was measured or is his report.

## What it removes

sereno keeps a record, `manifest.json` in its own folder, of every file and folder it wrote. Uninstall goes by that record, in this order:

1. **Hook lines.** For each start-up file sereno added a line to, it removes exactly the bytes it added, where they still stand as whole lines. If they do not (you commented the line out, indented it, or the file's line endings changed), it removes every line that ends with ` # sereno:hook`, and nothing else. A start-up file that sereno itself created and that is now empty is deleted, with the folders sereno created for it. If the file is then byte for byte what it was before the hook line, the backup copy is deleted too. Uninstall looks in the file the line was recorded in, and nowhere else; a file that has moved since is a case of its own, further down.
2. **sereno's own files.** Each file sereno wrote is deleted if it is still what sereno wrote (its SHA-256 matches the record): the three session files, the Windows Terminal fragment, and the theme files of `sereno export`.
3. **The rest of its folder.** The settings file `mode` and `manifest.json` are deleted, then `backups` if it is empty, then every folder sereno created, newest first, each only if it is empty, and finally sereno's own folder if it is empty.

`sereno uninstall --dry-run` prints the plan and changes nothing. This is an uninstall after an apply with two hook lines and an export for kitty:

```text
sereno dev: plan
  replace C:\Users\user\.bashrc (hook line removed)
          - [ -r "$LOCALAPPDATA/sereno/shell/init.bash" ] && . "$LOCALAPPDATA/sereno/shell/init.bash" # sereno:hook
  remove  C:\Users\user\AppData\Local\sereno\backups\bash-27d68d07becf90a4.bak
  replace C:\Users\user\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1 (hook line removed)
          - if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1")) { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook
  remove  C:\Users\user\AppData\Local\sereno\backups\powershell-4071979914a35c40.bak
  remove  C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal\sereno.json
  remove  C:\Users\user\AppData\Local\sereno\shell\init.bash
  remove  C:\Users\user\AppData\Local\sereno\shell\init.ps1
  remove  C:\Users\user\AppData\Local\sereno\shell\init.zsh
  remove  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Dark Vivid.conf
  remove  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Dark.conf
  remove  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena High Contrast Light.conf
  remove  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena High Contrast.conf
  remove  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Light Vivid.conf
  remove  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Light.conf
  remove  C:\Users\user\AppData\Local\sereno\mode
  remove  C:\Users\user\AppData\Local\sereno\manifest.json
  rmdir   C:\Users\user\AppData\Local\sereno\backups
  rmdir   C:\Users\user\AppData\Local\sereno\themes\kitty
  rmdir   C:\Users\user\AppData\Local\sereno\themes
  rmdir   C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal
  rmdir   C:\Users\user\AppData\Local\sereno\shell
  rmdir   C:\Users\user\AppData\Local\sereno
Total: 2 replace, 14 remove, 6 rmdir.
The sereno program itself stays where it is: C:\Users\user\go\bin\sereno.exe. Delete that file to remove it.
This was a dry run. Run the same command without --dry-run to carry it out.
Nothing was changed.
```

When nothing was edited or moved in between, the result is that every file sereno created is gone, every start-up file it touched is byte for byte what it was, and the folders it created are gone. The tests of this repository compare a checksum listing of a whole home folder before the first apply and after the uninstall. A session that was open during the uninstall keeps sereno's prompt and line-editor colours until it is closed: it has loaded them. Its palette goes by the default settings from its next prompt on, because the settings file is gone: a Serena tab that ran with `--flavor vivid` or with a fixed mode changes to what `auto` and `soft` give, a Serena tab that ran with `--inband off`, and so was never painted, is painted for the first time, and a session of another profile that was painted under `--inband always` hands the palette back to the colour scheme of its profile. That is how the tests of `init.ps1` find it, with the palette captured. In the acceptance test of 2026-10-14 a Serena tab that ran with `--flavor vivid` was open during the uninstall, and what it showed at its next prompt was not reported, so none of this has been seen on a screen.

`sereno uninstall --hook LIST` does only step 1, for the shells you name (`powershell`, `pwsh`, `bash`, `zsh`), and keeps everything else installed.

## What it leaves, and tells you about

### A file you edited

A file sereno wrote and you changed afterwards is not deleted. The plan shows it as `skip`, the run ends with exit code 4, and the folders around it stay:

```text
sereno dev: plan
  remove  C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal\sereno.json
  skip    C:\Users\user\AppData\Local\sereno\shell\init.bash (edited after sereno wrote it; left as it is)
  remove  C:\Users\user\AppData\Local\sereno\shell\init.ps1
  remove  C:\Users\user\AppData\Local\sereno\shell\init.zsh
  remove  C:\Users\user\AppData\Local\sereno\mode
  remove  C:\Users\user\AppData\Local\sereno\manifest.json
  rmdir   C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal
Total: 5 remove, 1 rmdir, 1 skip.
Something was left as it is: see the skip lines above. sereno did not edit or delete it, and it is no longer recorded: remove it yourself (a hook line is the one shown after `-`).
Done.
```

After a complete uninstall the record is gone, so what was left is yours to remove: running the command again finds nothing. A file you only marked read-only, without changing it, is not such a case: it is still what sereno wrote, and uninstall deletes it.

There is one exception. The settings file `mode` is always deleted, also when you edited it. `sereno status` says so in its `owned.modified` warning.

### A hook line it could not take out

If the start-up file has become read-only, or is no longer a regular file, sereno does not edit it. The plan shows the file as `skip` with the line after `-`, the run ends with exit code 4, and the backup stays. Remove that one line yourself. Because the line tests for sereno's file before loading it, it does nothing once sereno's files are gone.

### The backup of a start-up file you changed

Before sereno first adds a line to an existing start-up file, it copies the file to `backups` in its own folder. If you changed the start-up file afterwards, uninstall still removes the hook line, but it keeps the backup, because the file is no longer what the backup holds:

```text
  replace C:\Users\user\.bashrc (hook line removed)
          - [ -r "$LOCALAPPDATA/sereno/shell/init.bash" ] && . "$LOCALAPPDATA/sereno/shell/init.bash" # sereno:hook
  keep    C:\Users\user\AppData\Local\sereno\backups\bash-27d68d07becf90a4.bak (C:\Users\user\.bashrc changed after the hook line was added, so its backup stays)
```

That is not an error: the exit code is 0. sereno's folder then remains with `backups` in it; delete it when you no longer want the copy. One side effect: the folders sereno created above its own folder (on a new Linux home, `~/.local` and `~/.local/share`) are no longer recorded after such an uninstall. If you apply and uninstall again later, they stay behind as empty folders.

### A hook line in a start-up file that has moved

Uninstall looks for a hook line in the file it was recorded in, and nowhere else. When that file is no longer there, it takes nothing out, and after the plan it says so, with the shell and the recorded file:

```text
The hook line for powershell was recorded in C:\Users\user\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1, and no file is there now. If the file was moved, the line may still be in it: uninstall does not look for the file, and a line ending in `# sereno:hook` is then yours to delete.
```

For a file you deleted that is all there is to it. A file that was moved has taken the line along, and it stays in the moved file; sereno cannot tell the two cases apart, which is why the sentence is the same for both and the exit code stays 0. Where the file was there before sereno, the copy of it stays in `backups`, and sereno's folder with it, as in the case above; the plan shows the copy as `keep`, with the note that the file is no longer there. The move this is expected from is the one of a Documents folder into OneDrive, which takes the PowerShell profile along; it has not been tried on a machine, only with a folder renamed in a test home.

`sereno apply` refuses a profile that has moved and names `sereno uninstall --hook powershell`, which ends with the sentence above; after that, `sereno apply --hook powershell` goes by the file PowerShell names now. If sereno's line is still in that file, the apply keeps it, and from then on the line counts as one that was there before sereno: a later uninstall leaves it, and the file, also a file sereno itself had created, and says nothing about either. To avoid that, take the line out of the moved file between the two commands. sereno then adds it again itself, and uninstall takes it out again. One thing stays that way too, where the moved file is one sereno itself had created: it held nothing but that line, so it is an empty file now, and an empty file that apply finds is a file of yours. The uninstall takes the line out again and leaves the empty file, in the folder sereno had made for it if it made one; its plan shows the line going and has no word for what stays. Both are yours to delete.

A line that is left behind this way is yours to delete. Because it tests for sereno's file before loading it, it does nothing once sereno's files are gone.

### The entry Windows Terminal wrote

sereno writes one file for Windows Terminal, the fragment, and removes it. It never writes Windows Terminal's own `settings.json` or `state.json`. Windows Terminal does: the first time it sees a profile that comes from a fragment, it adds a short entry for it to `settings.json` and notes the profile's identifier in `state.json`, where it also changes the value `settingsHash`. On the maintainer's machine, in the acceptance test of 2026-10-14, a comparison of the two files before and after measured just that: the entry appended to `profiles.list` with the rest of the JSON unchanged, and the identifier added to `generatedProfiles`. When either file holds that, or cannot be read, uninstall ends with this:

```text
Windows Terminal keeps the short entry it added for the Serena profile to its settings.json, and the profile's GUID in its state.json; sereno does not write those files. Leave the entry: once Windows Terminal has been closed and opened again, it is expected to leave Serena out of its new-tab menu and to go on listing it in its settings, as a profile that is no longer detected, with a button to delete it. If you delete the entry, a later reinstall stays hidden until the entry is added back.
```

What that means in practice. This describes Windows Terminal's side as read from its source code for version 1.24.12741.0 and, where it says so, what the maintainer reported from his machine in the acceptance test of 2026-10-14.

- A Windows Terminal that is running during the uninstall is expected, by its source, to go on offering Serena in its new-tab menu until it loads its settings again: close every Windows Terminal window and open it again. The same reading expected a new window not to list Serena after an apply, and on the maintainer's machine one did, by his report ([support-matrix.md](support-matrix.md) has it); what a running Windows Terminal offered after the uninstall was not recorded. Choosing Serena from a menu that still offers it starts PowerShell with a file that is no longer there.
- After that the profile is gone from the new-tab menu, and the six colour schemes with it. The entry remains in `settings.json`, and the settings of Windows Terminal go on listing Serena, on a page that says the profile is no longer detected. The maintainer reported this from his machine after an uninstall and a restart of Windows Terminal; his Windows Terminal, in Portuguese, listed the profile as "Perfil não detectado". By the source the entry also has a warning sign there and the page a button to delete it; those were not recorded. Leave that button alone, for the reason of the next point. By the source, a command line that names the profile (`wt -p Serena`) still finds the entry and opens a tab from it, with Windows Terminal's default command and nothing of sereno's; that was not tried.
- If you delete the entry and later apply sereno again, Windows Terminal treats the profile as one you deleted and keeps it hidden. `sereno status` recognises this (`wt.profile-suppressed`) and prints the object to add back to `profiles.list`: `{ "guid": "{6b2d8b49-31d3-5dea-9087-93c26e1a033e}", "hidden": false, "name": "Serena", "source": "SerenaTerminal" }`.
- When Windows Terminal adds its entry, it writes its whole `settings.json` again, by its source, so comments and your own formatting in that file may not survive that. This is true of every fragment profile, not only of sereno's. On the maintainer's machine the comparison of the file before and after found the entry appended and the rest of the JSON unchanged.
- `sereno apply --no-profile` installs the six colour schemes without a profile. Windows Terminal then has no profile to add an entry for.

### A Serena colour scheme you chose in Windows Terminal's settings

If you chose one of the six Serena colour schemes for a profile in Windows Terminal's settings, the Serena profile or any other, that choice is yours: Windows Terminal wrote it into its own `settings.json` (as `"colorScheme": "Serena Dark Vivid"`, for instance), and sereno does not know about it: uninstall neither removes it nor mentions it. Once the fragment is gone, the scheme is gone too, and the name no longer matches any scheme. Windows Terminal then reports, when it starts, that a profile has an invalid `colorScheme`. On the maintainer's machine, after the uninstall of the acceptance test of 2026-10-14, it showed that error in a dialog when it was opened again, for the left-over Serena entry, which named Serena Dark Vivid (his report). It is expected to show it at every start until the name is taken out of the profile or sereno is applied again, which brings the six schemes back.

To take the name out: in Windows Terminal's settings, open that profile, go to its Appearance page, use the reset button next to Color scheme, then Save. That way was not tried in the acceptance test. On the Serena entry, which the settings list as a profile that is no longer detected, never use the Delete button instead: if you delete the entry, a later reinstall stays hidden (the point above). A warning from `sereno uninstall` when your settings name a Serena scheme is planned for version 0.2.

### The program

Uninstall does not delete the sereno program; it prints where it is. If the install script of a release placed it, it is in the `bin` folder of sereno's own folder, which `apply` and `uninstall` never touch, and uninstall says that the folder was left alone. Remove it with the same script (`install.ps1 -Uninstall`, or `sh install.sh --uninstall`), which also removes `bin` and sereno's folder when they are empty, or delete the file. Folders above sereno's own folder that the script had to create (on a home that had no `~/.local`: `~/.local` and `~/.local/share`) stay. The script keeps no record of them, and `sh install.sh --uninstall` says so when it leaves one empty; `sereno uninstall` leaves them too when the script was there first, because sereno removes only the folders it created itself. Run `sereno uninstall` first: the script refuses to remove the program while `manifest.json` says sereno is still applied.

### What sereno never recorded

- Theme files written with `sereno export --to DIR`. sereno keeps no record of them; they are yours.
- Theme files you copied from sereno's `themes` folder into the folder of an emulator, and the line you added to that emulator's configuration.
- A Serena colour scheme you chose for a profile in Windows Terminal's settings (see above).
- Anything Windows Terminal, your shell or another program wrote on its own.

## The lock file

While a command writes, sereno's folder holds an empty file named `lock`, which that command locks through the operating system and deletes when it is done. The manifest does not list it. A command that is killed can leave the empty file behind; the next `apply`, `theme`, `export` or `uninstall` takes it away. `sereno status` does not. A file named `lock` with content, or a folder of that name, is not sereno's, and the commands refuse to work around it (exit code 3).

Two commands on the same folder at the same moment are safe: one writes, and the other ends with exit code 1, having written nothing, and says

```text
sereno: another sereno is working on this folder; run the command again when it has finished
```

On a file system that offers no locks, a command goes on without one.

## When a command is interrupted

Ctrl+C takes effect between two operations. The manifest is then written once more so that it records only what was carried out, and the next `sereno apply` finishes the work or `sereno uninstall` takes it back.

One gap is known. If the process is killed outright (not interrupted) in the instant between writing the manifest and replacing a file, that one file is recorded with the new content while it still has the old. sereno then takes it for a file you edited and leaves it alone, until you delete it; the next apply writes it again.

## When the record itself is lost or unreadable

Without a readable `manifest.json`, sereno cannot tell which files are its own, so it changes nothing and says so. Restore the file, or remove things by hand:

- sereno's own folder: `%LOCALAPPDATA%\sereno` on Windows, `~/.local/share/sereno` on macOS and Linux (or wherever `SERENO_HOME` or `XDG_DATA_HOME` put it);
- on Windows, the folder `%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\SerenaTerminal`;
- any line that ends with `# sereno:hook` in your PowerShell profile, `~/.bashrc` or `~/.zshrc`.

A manifest written by a newer version of sereno is not touched by an older one: use the newer version, also to uninstall.
