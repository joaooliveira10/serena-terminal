# Security

## Reporting a problem

Please report a security problem privately, not in a public issue.

On the page of this repository on GitHub, open the **Security** tab and choose **Report a vulnerability**. That opens a private report that only the maintainer can read. If that button is not offered, open an issue that says only that you have a security report and ask how to send it; do not put the details in the issue.

A useful report says which version or commit it is about, the system and shell, the exact commands, and what happened against what you expected.

This is a one-person project. Reports are read and answered, but there is no promised response time.

## Supported versions

No version has been released yet. Once there are releases, fixes go into the newest release only.

## What sereno can do on your machine

This describes the code as it is. Each "cannot" below is enforced by a test in `internal/policy`, by the build, or both.

It can:

- create, replace and delete files in its own folder (`%LOCALAPPDATA%\sereno`, or `~/.local/share/sereno`, or where `SERENO_HOME` points);
- on Windows, create, replace and delete one file, `%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\SerenaTerminal\sereno.json`;
- when you pass `--hook`, append one line to the start-up file of a shell, creating the file and its folder when there is none, and later remove that line; a file that was already there is copied first, as it was;
- when you pass `--hook powershell` or `--hook pwsh`, start that PowerShell once, to ask where its profile is and whether it loads profiles; a dry run with that flag does so too. A later apply without the flag looks after the line in the file recorded then and, while that file is there, starts nothing; when the file is gone, it asks again, as if you had passed the flag;
- write theme files into a folder you name with `sereno export --to`;
- read three values of the current user's registry, Windows Terminal's two settings files, and the environment;
- write escape sequences to the terminal it runs in.

It cannot:

- open a network connection. The program links neither `net` nor `net/http`; it cannot download, upload, update itself or report anything;
- start any other program. The one PowerShell question above is the only place that can start a process;
- write anywhere else: not the registry, not `PATH`, not Windows Terminal's settings, not the folder of an emulator;
- ask for administrator rights or `sudo`. It works in the current user's folders with the current user's rights.

The module has no dependencies outside Go's standard library.

## What to keep in mind

- **The session files run in your shell.** After an apply, `init.ps1`, `init.bash` or `init.zsh` in sereno's folder is loaded at the start of every session of the Serena profile, and of every shell that has the hook line. Whoever can write to sereno's folder can therefore run code in those sessions. The folder belongs to your user, like your own shell profile, and carries the same trust.
- **Backups may hold secrets.** The copy of a start-up file in `backups` is as sensitive as the file it was copied from.
- **Text from outside is never run.** The name of a Git branch reaches the prompt as data, with control characters removed, and is not evaluated by the shell. The settings file is accepted only when it is exactly five known lines. A start-up file that is a symbolic link is refused, not followed.
- **`SERENO_SANDBOX` is not a security boundary.** It makes sereno refuse to write outside a folder, and exists so that tests cannot touch a real home folder. It does not confine anything else.
- **The install scripts** run with your rights, download one archive over HTTPS, and install it only when its SHA-256 equals the value the script carries. That value is only as trustworthy as the script, so take the script from the release itself. They never ask for more rights.
- **Releases** are built by a GitHub Actions workflow from a tag, with every action pinned to a commit, and carry a build attestation that `gh attestation verify` checks. The programs are not code-signed.

## In scope

Examples of what counts as a security problem here: a way to make sereno write or delete outside the places listed above; a way to make `uninstall` remove a file sereno did not write; a way to run code through a branch name, a path, the settings file or the content of a start-up file; a way to make an install script place a file that does not match its checksum.
