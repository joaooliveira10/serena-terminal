package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Exit codes (R4).
const (
	ExitOK         = 0 // done, including nothing to do
	ExitFailure    = 1 // runtime failure; for check, a value under its floor
	ExitUsage      = 2 // unknown command, flag or value
	ExitRefused    = 3 // refused before writing anything
	ExitIncomplete = 4 // finished, but something was left in place
)

// Verb is the first word of a plan line (R14). Verbs are not translated.
type Verb string

// The verbs of R14.
const (
	VerbMkdir   Verb = "mkdir"   // create a directory
	VerbCreate  Verb = "create"  // write a file that does not exist
	VerbReplace Verb = "replace" // write a file whose content changes
	VerbKeep    Verb = "keep"    // already as wanted
	VerbSkip    Verb = "skip"    // owned but edited by the user, left as is
	VerbAppend  Verb = "append"  // put the hook line into a profile
	VerbRemove  Verb = "remove"  // delete a file
	VerbRmdir   Verb = "rmdir"   // delete an empty directory
)

// Note is a remark for the user in a form that is translated only when it is
// printed: a key of internal/i18n and the arguments of its text.
type Note struct {
	Key  string
	Args []any
}

// NewNote builds a Note. Keys passed here are checked against the catalogues
// by a test in internal/policy.
func NewNote(key string, args ...any) Note { return Note{Key: key, Args: args} }

// IsZero reports whether there is no remark.
func (n Note) IsZero() bool { return n.Key == "" }

// Keys of the notes that plan lines carry.
const (
	NoteKept         = "plan.note.kept"
	NoteEdited       = "plan.note.edited"
	NoteNotRegular   = "plan.note.not-regular"
	NoteBackupOf     = "plan.note.backup-of"
	NoteBackupKept   = "plan.note.backup-kept"
	NoteHookPresent  = "plan.note.hook-present"
	NoteHookReplaces = "plan.note.hook-replaces"
	NoteHookRemoved  = "plan.note.hook-removed"
	NoteHookRestored = "plan.note.hook-restored"
)

// Keys of the reasons a path is refused, and of the advice that goes with a
// refused hook.
const (
	ReasonForeign       = "refusal.foreign"
	ReasonDifferent     = "refusal.different"
	ReasonNotRegular    = "refusal.not-regular"
	ReasonParent        = "refusal.parent"
	ReasonUnreadable    = "refusal.unreadable"
	ReasonSymlink       = "refusal.symlink"
	ReasonTooLarge      = "refusal.too-large"
	ReasonUTF16BE       = "refusal.utf16be"
	ReasonMalformed     = "refusal.malformed"
	ReasonBinary        = "refusal.binary"
	ReasonHookMoved     = "refusal.hook-moved"
	ReasonBackupBlocked = "refusal.backup-blocked"
	AdviceAddByHand     = "refusal.advice.add-by-hand"
)

// Op is one line of a plan and, for the verbs that change something, the
// change itself.
type Op struct {
	Verb Verb
	Path string // absolute
	Note Note   // optional remark, shown in parentheses
	// Added and Removed are hook lines, shown on the lines that follow with
	// "+ " and "- " so the user sees the exact text.
	Added   []string
	Removed []string

	act action
}

// detailIndent aligns the "+ " and "- " lines with the path column.
const detailIndent = "          "

// Lines renders the operation as R14 prescribes: two spaces, the verb padded
// to 8 columns, the absolute path and the note in parentheses, followed by one
// line per hook line added or removed. note is the translated text of o.Note,
// empty for none; the dry run and the real run print the same lines.
func (o Op) Lines(note string) []string {
	head := fmt.Sprintf("  %-8s%s", o.Verb, o.Path)
	if note != "" {
		head += " (" + note + ")"
	}
	lines := []string{head}
	for _, text := range o.Added {
		lines = append(lines, detailIndent+"+ "+text)
	}
	for _, text := range o.Removed {
		lines = append(lines, detailIndent+"- "+text)
	}
	return lines
}

// Changes reports whether carrying out the operation writes anything.
func (o Op) Changes() bool { return o.act.kind != actNone }

// Refusal is a reason not to start (R13): with one or more of them, all are
// printed, nothing is written and the command exits with 3.
type Refusal struct {
	Path   string // what the refusal is about
	Reason Note
	// Advice introduces Text; both are optional. Text is printed verbatim: a
	// hook line the user can add by hand, or a command sereno will not run.
	Advice Note
	Text   string
}

// File is an owned file an apply wants on disk.
type File struct {
	Path string // absolute
	Data []byte
}

// Hook is a hook line an apply wants in a profile or rc file (R20-R22).
type Hook struct {
	Shell string // powershell, pwsh, bash or zsh: the key of the manifest record
	Path  string // absolute path of the profile or rc file
	Line  string // the line of R21, without a line ending
	// NewFileCRLF chooses the line ending of a file sereno has to create:
	// CRLF for the PowerShells on Windows, LF otherwise.
	NewFileCRLF bool
	// CreateNote is shown on the plan line when the file would be created.
	CreateNote Note
}

// Request is what a command wants on disk. Planning turns it into operations
// and refusals without writing.
type Request struct {
	Root    string // ROOT; the manifest lives in it
	Version string // recorded in the manifest
	// Options replaces the recorded options; nil keeps them (R13).
	Options *Options
	// Files are written in this order. Owned files that are not listed stay
	// as they are and stay recorded.
	Files []File
	// Hooks are ensured; recorded hooks that are not listed stay as they are.
	Hooks []Hook
	// Refusals found by the caller (an absent shell, a blocking execution
	// policy) join those the engine finds.
	Refusals []Refusal
}

// Plan is the outcome of planning: the operations in the order they will be
// carried out, or the refusals that prevent all of them.
type Plan struct {
	Root     string
	Ops      []Op
	Refusals []Refusal
}

// Refused reports whether the plan must not be applied.
func (p *Plan) Refused() bool { return len(p.Refusals) > 0 }

// Count returns the number of operations with the given verb.
func (p *Plan) Count(verb Verb) int {
	count := 0
	for _, op := range p.Ops {
		if op.Verb == verb {
			count++
		}
	}
	return count
}

// Changes returns the number of operations that write something. It is zero
// for a second identical apply (R15).
func (p *Plan) Changes() int {
	count := 0
	for _, op := range p.Ops {
		if op.Changes() {
			count++
		}
	}
	return count
}

// ExitCode maps the plan to R4: 3 when refused, 4 when an owned file was left
// in place because the user edited it, otherwise 0. A failure of Apply is the
// caller's exit 1.
func (p *Plan) ExitCode() int {
	switch {
	case p.Refused():
		return ExitRefused
	case p.Count(VerbSkip) > 0:
		return ExitIncomplete
	}
	return ExitOK
}

// ErrRefused is returned by Apply for a plan that has refusals.
var ErrRefused = errors.New("the plan has refusals: nothing was written")

// ErrChanged is returned by Apply when a file is no longer in the state the
// plan was computed for. The operation is not carried out.
var ErrChanged = errors.New("changed while sereno was running")

// Apply carries out the operations in order (phase 2 of R13). It stops at the
// first failure; what was written until then is already described by the
// manifest, which is written before the owned files.
func (p *Plan) Apply() error {
	if p.Refused() {
		return ErrRefused
	}
	var stayed []string // directories that could not be removed
	for _, op := range p.Ops {
		if op.act.kind != actRmdir {
			if err := op.act.run(op.Path); err != nil {
				return err
			}
			continue
		}
		// A directory above one that stayed cannot be empty: no waiting.
		remove := removeEmptiedDir
		for _, dir := range stayed {
			if strings.HasPrefix(dir, op.Path+string(filepath.Separator)) {
				remove = func(path string) error { _, err := RemoveEmptyDir(path); return err }
			}
		}
		if err := remove(op.Path); err != nil {
			return err
		}
		if _, err := os.Lstat(op.Path); err == nil {
			stayed = append(stayed, op.Path)
		}
	}
	return nil
}

type actionKind int

const (
	actNone actionKind = iota
	actMkdir
	actWrite
	actRemove
	actRmdir
)

// action is the change behind an operation, with the state of the path that
// planning saw. Apply verifies that state again right before it writes.
type action struct {
	kind   actionKind
	data   []byte
	perm   fs.FileMode
	absent bool   // the path did not exist
	sum    string // SHA-256 of the file that was there
}

func writeNew(data []byte, perm fs.FileMode) action {
	return action{kind: actWrite, data: data, perm: perm, absent: true}
}

func writeOver(data []byte, perm fs.FileMode, sum string) action {
	return action{kind: actWrite, data: data, perm: perm, sum: sum}
}

// run carries out a change. Directory removals are not handled here: Apply
// does them itself, because it knows which directories had to stay.
func (a action) run(path string) error {
	switch a.kind {
	case actMkdir:
		return Mkdir(path)
	case actWrite:
		if err := a.verify(path); err != nil {
			return err
		}
		return WriteFile(path, a.data, a.perm)
	case actRemove:
		if err := a.verify(path); err != nil {
			return err
		}
		return RemoveFile(path)
	}
	return nil
}

// verify checks that path is still what planning saw.
func (a action) verify(path string) error {
	if a.absent {
		if _, err := os.Lstat(path); !isMissing(err) {
			return fmt.Errorf("%s: %w", path, ErrChanged)
		}
		return nil
	}
	sum, err := HashFile(path)
	if err != nil || sum != a.sum {
		return fmt.Errorf("%s: %w", path, ErrChanged)
	}
	return nil
}

// isMissing reports whether err says that a path does not exist, including
// the case where something above it is a file.
func isMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}
