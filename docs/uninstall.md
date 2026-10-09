# Uninstall

`sereno uninstall` is the undo of `sereno apply`. This page says exactly what it removes, what it leaves, and why. The examples are the output of real runs in a test home folder, which is shown as the home of a user named `user`. Version 0.1 is not released yet, and what Windows Terminal does on its side has not been observed on a real machine yet; those sentences are marked.

## What it removes

sereno keeps a record, `manifest.json` in its own folder, of every file and folder it wrote. Uninstall goes by that record, in this order:

1. **Hook lines.** For each start-up file sereno added a line to, it removes exactly the bytes it added, where they still stand as whole lines. If they do not (you commented the line out, indented it, or the file's line endings changed), it removes every line that ends with ` # sereno:hook`, and nothing else. A start-up file that sereno itself created and that is now empty is deleted, with the folders sereno created for it. If the file is then byte for byte what it was before the hook line, the backup copy is deleted too.
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

When nothing was edited in between, the result is that every file sereno created is gone, every start-up file it touched is byte for byte what it was, and the folders it created are gone. The tests of this repository compare a checksum listing of a whole home folder before the first apply and after the uninstall. A session that was open during the uninstall keeps the prompt and the colours it had loaded until it is closed.

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

### The entry Windows Terminal wrote

sereno writes one file for Windows Terminal, the fragment, and removes it. It never writes Windows Terminal's own `settings.json` or `state.json`. Windows Terminal does: the first time it sees a profile that comes from a fragment, it adds a short entry for it to `settings.json` and notes the profile's identifier in `state.json`. When either file holds that, or cannot be read, uninstall ends with this:

```text
Windows Terminal keeps the short entry it added for the Serena profile to its settings.json, and the profile's GUID in its state.json; sereno does not write those files. Leave the entry: once Windows Terminal has been closed and opened again, it is expected to leave Serena out of its new-tab menu and to go on listing it in its settings, as a profile that is no longer detected, with a button to delete it. If you delete the entry, a later reinstall stays hidden until the entry is added back.
```

What that means in practice. This paragraph describes Windows Terminal's side as read from its source code for version 1.24.12741.0; none of it has been observed on a machine yet.

- A Windows Terminal that is running during the uninstall is expected to go on offering Serena in its new-tab menu until it loads its settings again, which it does when it starts: close every Windows Terminal window and open it again. Choosing Serena before that starts PowerShell with a file that is no longer there.
- After that the profile is expected to be gone from the new-tab menu, and the six colour schemes with it. The entry remains in `settings.json`, and the settings of Windows Terminal go on listing Serena, with a warning sign, on a page that says the profile is no longer detected and has a button to delete it. Leave that button alone, for the reason of the next point. A command line that names the profile (`wt -p Serena`) still finds the entry and opens a tab from it, with Windows Terminal's default command and nothing of sereno's.
- If you delete the entry and later apply sereno again, Windows Terminal treats the profile as one you deleted and keeps it hidden. `sereno status` recognises this (`wt.profile-suppressed`) and prints the object to add back to `profiles.list`: `{ "guid": "{6b2d8b49-31d3-5dea-9087-93c26e1a033e}", "hidden": false, "name": "Serena", "source": "SerenaTerminal" }`.
- When Windows Terminal adds its entry, it writes its whole `settings.json` again. Comments and your own formatting in that file may not survive that. This is true of every fragment profile, not only of sereno's.
- `sereno apply --no-profile` installs the six colour schemes without a profile. Windows Terminal then has no profile to add an entry for.

### The program

Uninstall does not delete the sereno program; it prints where it is. If the install script of a release placed it, it is in the `bin` folder of sereno's own folder, which `apply` and `uninstall` never touch, and uninstall says that the folder was left alone. Remove it with the same script (`install.ps1 -Uninstall`, or `sh install.sh --uninstall`), which also removes `bin` and sereno's folder when they are empty, or delete the file. Folders above sereno's own folder that the script had to create (on a home that had no `~/.local`: `~/.local` and `~/.local/share`) stay. The script keeps no record of them, and `sh install.sh --uninstall` says so when it leaves one empty; `sereno uninstall` leaves them too when the script was there first, because sereno removes only the folders it created itself. Run `sereno uninstall` first: the script refuses to remove the program while `manifest.json` says sereno is still applied.

### What sereno never recorded

- Theme files written with `sereno export --to DIR`. sereno keeps no record of them; they are yours.
- Theme files you copied from sereno's `themes` folder into the folder of an emulator, and the line you added to that emulator's configuration.
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
