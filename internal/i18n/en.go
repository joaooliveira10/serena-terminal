package i18n

// english is the English catalogue. Every key must also exist in portuguese
// (ptbr.go) with the same arguments; i18n_test.go enforces both.
var english = map[string]string{
	// Plan output (R13, R14). The verbs themselves are not translated.
	"plan.nothing-changed":    "Nothing was changed.",
	"plan.note.kept":          "already as wanted",
	"plan.note.edited":        "edited after sereno wrote it; left as it is",
	"plan.note.not-regular":   "no longer a regular file; left as it is",
	"plan.note.backup-of":     "copy of %s from before the hook line",
	"plan.note.backup-kept":   "%s changed after the hook line was added, so its backup stays",
	"plan.note.hook-present":  "the hook line is already there",
	"plan.note.hook-replaces": "replaces the sereno line that is there",
	"plan.note.hook-removed":  "hook line removed",
	"plan.note.hook-restored": "back to its content from before the hook line",
	"plan.note.bash-profile":  "Git for Windows will create ~/.bash_profile by itself",

	// Refusals (R4 exit 3, R15, R20, R22). Each follows the path it is about.
	"refusal.foreign":            "exists and was not written by sereno",
	"refusal.different":          "exists with other content",
	"refusal.not-regular":        "is not a regular file",
	"refusal.parent":             "%s is in the way and is not a directory",
	"refusal.unreadable":         "cannot be read (%s)",
	"refusal.symlink":            "is a symbolic link",
	"refusal.read-only":          "is read-only",
	"refusal.too-large":          "is larger than 1 MiB",
	"refusal.no-room":            "would be larger than 1 MiB with the hook line",
	"refusal.utf16be":            "is encoded in UTF-16 big endian",
	"refusal.malformed":          "starts like UTF-16 but is not valid UTF-16 little endian",
	"refusal.binary":             "contains NUL bytes and is not UTF-16 little endian",
	"refusal.hook-moved":         "the %[1]s hook line is recorded in %[2]s; run `sereno uninstall --hook %[1]s` first",
	"refusal.backup-blocked":     "is in the way of a backup and has other content",
	"refusal.shell-absent":       "%s is not on PATH",
	"refusal.shell-windows-only": "Windows PowerShell 5.1 exists only on Windows; use pwsh here",
	"refusal.policy":             "the PowerShell execution policy is %s, so the profile would not be loaded",
	"refusal.no-profile-path":    "%[1]s reported no profile path (its Documents folder may be missing or out of reach); set %[2]s to the profile file sereno should use",
	"refusal.probe-timeout":      "%[1]s did not answer within %[2]s seconds; set %[3]s to the profile file and sereno will not ask it",
	"refusal.root-not-ascii":     "the path of sereno's folder, %s, has characters that a one-line ASCII hook cannot carry",
	"refusal.advice.add-by-hand": "sereno does not edit this file. To use the hook, add this line to it yourself:",
	"refusal.advice.policy":      "sereno does not change the policy. To allow your own scripts, run this yourself:",

	// What apply says about Windows Terminal (R19, wording fixed there).
	"wt.notice.stub": "Windows Terminal adds a short entry for the Serena profile to its own settings.json the first time it sees it, as it does for every fragment profile. sereno does not write that file. Use --no-profile to install only the colour schemes.",
	"wt.notice.open": "Open a new Windows Terminal window and choose Serena in the new-tab menu. If it is not listed, close every Windows Terminal window and open it again.",
}
