package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
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
	VerbSkip    Verb = "skip"    // left as it is; the note says why
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

// Translate turns a message key and its arguments into text in the user's
// language. The method T of an i18n catalogue has this form, so a command
// passes `catalog.T` wherever a Translate is wanted.
type Translate func(key string, args ...any) string

// Text returns the remark in the user's language, and "" when there is no
// remark: the translation is not asked for a key that does not exist. A nil
// translate gives the key itself, which is enough for logs and tests.
func (n Note) Text(translate Translate) string {
	switch {
	case n.IsZero():
		return ""
	case translate == nil:
		return n.Key
	}
	return translate(n.Key, n.Args...)
}

// Keys of the notes that plan lines carry.
const (
	NoteKept         = "plan.note.kept"
	NoteEdited       = "plan.note.edited"
	NoteReadOnly     = "plan.note.read-only"
	NoteNotRegular   = "plan.note.not-regular"
	NoteBackupOf     = "plan.note.backup-of"
	NoteBackupKept   = "plan.note.backup-kept"
	NoteBackupLine   = "plan.note.backup-kept-line"
	NoteBackupGone   = "plan.note.backup-kept-gone"
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
	ReasonReadOnly      = "refusal.read-only"
	ReasonTooLarge      = "refusal.too-large"
	ReasonNoRoom        = "refusal.no-room"
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
	// Removed and Added are hook lines, shown on the lines that follow with
	// "- " and "+ " so the user sees the exact text.
	Removed []string
	Added   []string
	// Incomplete is true when the line reports something sereno had to leave
	// in a state it did not want: an owned file the user edited, a profile it
	// may not touch. Any such line makes the exit code 4 (R4). A skip without
	// it is information only, for example a fragment that is not written
	// because there is no Windows Terminal (R16).
	Incomplete bool

	act action
	// undo takes what the operation records out of a manifest again. It is
	// how the manifest is corrected when the operation, or one before it,
	// could not be carried out.
	undo func(*Manifest)
}

// detailIndent aligns the "+ " and "- " lines with the path column.
const detailIndent = "          "

// Lines renders the operation as R14 prescribes: two spaces, the verb padded
// to 8 columns, the absolute path and the note in parentheses, followed by one
// line per hook line removed ("- ") and added ("+ "). The dry run and the real
// run print the same lines. See Note.Text for translate.
func (o Op) Lines(translate Translate) []string {
	head := fmt.Sprintf("  %-8s%s", o.Verb, o.Path)
	if note := o.Note.Text(translate); note != "" {
		head += " (" + note + ")"
	}
	lines := []string{head}
	for _, text := range o.Removed {
		lines = append(lines, detailIndent+"- "+text)
	}
	for _, text := range o.Added {
		lines = append(lines, detailIndent+"+ "+text)
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
	// Skip, when set, says that this apply does not write the file, and why:
	// the plan shows a skip line with this note in the file's place, nothing
	// is created, changed or recorded, and the exit code is not affected.
	// This is how the fragment appears on a machine without Windows Terminal
	// (R16). A record the manifest already has for the path stays.
	Skip Note
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
	// Manifest is the manifest the caller loaded to build this request, as
	// LoadManifest returned it. Planning starts from it and stops with
	// ErrBusy when the file on disk is no longer the one it was read from.
	// nil reads the file when planning starts.
	Manifest *Manifest
}

// Plan is the outcome of planning: the operations in the order they will be
// carried out, or the refusals that prevent all of them.
type Plan struct {
	Root     string
	Ops      []Op
	Refusals []Refusal
	// GoneHooks lists, in the plan of an uninstall, the hook lines whose
	// file was not there: nothing could be taken out of it, and no operation
	// stands for it. Where the file went, planning cannot tell. One that was
	// moved has taken the line along, so the command says what it met.
	GoneHooks []HookRecord

	// ledger is set on the plan of an apply: what Apply needs to correct the
	// manifest when an operation fails.
	ledger *ledger
	// again makes the plan once more from the request it was made from, when
	// the run holds the lock of ROOT (see ApplyContext). made lists the
	// directories the run has created by then. nil for a plan that touches
	// nothing sereno keeps a record of.
	again func(made []string) (*Plan, error)
	// manifest is the state of the manifest file the plan was made from.
	manifest fileState
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

// Incomplete returns the number of lines that report something left in a
// state sereno did not want (see Op.Incomplete).
func (p *Plan) Incomplete() int {
	count := 0
	for _, op := range p.Ops {
		if op.Incomplete {
			count++
		}
	}
	return count
}

// ExitCode maps the plan to R4: 3 when refused, 4 when something was left in
// place (an owned file the user edited, a profile sereno may not touch),
// otherwise 0. A failure of Apply is the caller's exit 1.
func (p *Plan) ExitCode() int {
	switch {
	case p.Refused():
		return ExitRefused
	case p.Incomplete() > 0:
		return ExitIncomplete
	}
	return ExitOK
}

// ErrRefused is returned by Apply for a plan that has refusals.
var ErrRefused = errors.New("the plan has refusals: nothing was written")

// ErrChanged is returned by Apply when a file is no longer in the state the
// plan was computed for. The operation is not carried out.
var ErrChanged = errors.New("changed while sereno was running")

// Apply carries out the operations in order (phase 2 of R13). It is
// ApplyContext without a way to interrupt it.
func (p *Plan) Apply() error { return p.ApplyContext(context.Background()) }

// ApplyContext carries out the operations in order (phase 2 of R13) and stops
// at the first one that fails, or before the next one once ctx is done; a
// command passes a context that ends on an interrupt signal, so that Ctrl+C
// stops between two operations and never in the middle of one.
//
// The manifest is written before the owned files and already lists the hashes
// they are about to get (R13). When the run stops after that, the manifest is
// written once more so that it says what is really on disk: an operation that
// did not happen is taken out of it again. A file sereno did not get to
// replace therefore keeps its old hash and is not mistaken for one the user
// edited, and the next apply or uninstall carries on from there. The error
// returned is the one that stopped the run.
//
// What this cannot cover is a process that is killed between the manifest and
// a file it was about to replace: that file then counts as edited (R15) until
// it is deleted by hand. Files that were about to be created are not
// affected; the next apply creates them.
//
// Two commands may run at the same time, and each of them plans before it
// writes. A plan that changes something sereno keeps a record of is therefore
// carried out under the lock of ROOT (see Lock), and under the lock it is
// made once more: only then can no other sereno change what planning looks
// at. The plan that is carried out is the new one, and it has to be the plan
// that was shown. When it is not, nothing is written: the error is ErrBusy
// when the manifest is another one than planning read (another sereno has
// finished in between), as it is when another command holds the lock, and
// ErrChanged with the path that differs when something else changed.
func (p *Plan) ApplyContext(ctx context.Context) error {
	if p.Refused() {
		return ErrRefused
	}
	if p.again == nil || p.Changes() == 0 {
		// Nothing to write, or nothing sereno keeps a record of.
		return p.carry(ctx, nil, false)
	}
	fresh, release, err := p.begin(ctx)
	if err != nil {
		return err
	}
	return fresh.carry(ctx, release, true)
}

// begin takes the lock of ROOT and returns the plan to carry out under it:
// p, made again. release gives the lock back.
//
// The lock is a file in ROOT, so the directories that lead to ROOT have to be
// created before it can be taken; that is the one thing two first applies can
// still do side by side. A directory that is there already when its turn
// comes is no failure, and only the run that created it records it (see
// settle for the run that then has to give up).
//
// The interrupt is asked about once for each of those directories and once
// before the lock is taken; that last time stands for the first operation of
// the plan that comes back.
func (p *Plan) begin(ctx context.Context) (fresh *Plan, release func(), err error) {
	var made []string // the directories above the lock that this run created
	first := 0
	for ; first < len(p.Ops) && p.aboveLock(p.Ops[first]); first++ {
		if err = ctx.Err(); err == nil {
			var created bool
			if created, err = mkdirIfMissing(p.Ops[first].Path); created {
				made = append(made, p.Ops[first].Path)
			}
		}
		if err != nil {
			p.settle(ctx, made, nil)
			return nil, nil, err
		}
	}
	if err = ctx.Err(); err == nil {
		release, fresh, err = p.enter(first, made)
	}
	if err != nil {
		p.settle(ctx, made, release)
		return nil, nil, err
	}
	return fresh, release, nil
}

// carry performs the operations of a plan in order. release, when it is not
// nil, gives back the lock of ROOT, which the run holds; carry calls it when
// it is done, and earlier when ROOT itself is about to be removed, because
// the lock file is in it. asked says that the interrupt was already asked
// about for the first operation.
func (p *Plan) carry(ctx context.Context, release func(), asked bool) error {
	defer func() {
		if release != nil {
			release()
		}
	}()
	var stayed []string // directories that could not be removed
	for i, op := range p.Ops {
		var err error
		if i > 0 || !asked {
			err = ctx.Err()
		}
		if err == nil {
			if release != nil && op.act.kind == actRmdir && samePath(op.Path, p.Root) {
				release()
				release = nil
			}
			err = p.perform(op, &stayed)
		}
		if err == nil {
			continue
		}
		// Without the lock nothing was written that the manifest could be
		// wrong about, and the manifest must not be written.
		if p.ledger != nil && release != nil {
			if repairErr := p.ledger.repair(p.Ops, i); repairErr != nil {
				return fmt.Errorf("%w (and the manifest could not be corrected: %v)", err, repairErr)
			}
		}
		return err
	}
	return nil
}

// aboveLock reports whether an operation creates ROOT or a directory above
// it: what has to be there before the lock can be taken.
func (p *Plan) aboveLock(op Op) bool {
	if op.act.kind != actMkdir {
		return false
	}
	return samePath(op.Path, p.Root) || strings.HasPrefix(pathKey(p.Root), pathKey(op.Path)+string(filepath.Separator))
}

// mkdirIfMissing creates a directory above the lock and reports whether this
// call created it. One that another sereno has created in the meantime is no
// failure; anything else in its place is.
func mkdirIfMissing(path string) (created bool, err error) {
	err = Mkdir(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrExist) {
		if info, statErr := os.Lstat(path); statErr == nil && info.IsDir() {
			return false, nil
		}
	}
	return false, err
}

// enter takes the lock of ROOT and makes the plan again. from is the number
// of operations of p that are done already (the directories above the lock),
// made the directories among them that this run created. The plan that comes
// back is the one to carry out. When the error is not nil, release may still
// be set: the caller has to settle and to let go.
func (p *Plan) enter(from int, made []string) (release func(), fresh *Plan, err error) {
	release, err = Lock(p.Root)
	if isMissing(err) {
		// ROOT is gone, although the plan counted on it or has just seen to
		// it: an uninstall has finished in between.
		err = ErrBusy
	}
	if err != nil {
		return nil, nil, err
	}
	fresh, err = p.again(made)
	switch {
	case err != nil:
	case fresh.manifest != p.manifest:
		// Another sereno has written the manifest since this plan was made.
		err = ErrBusy
	default:
		if path, differs := p.differs(fresh, from); differs {
			err = fmt.Errorf("%s: %w", path, ErrChanged)
		}
	}
	if err != nil {
		return release, nil, err
	}
	return release, fresh, nil
}

// handOver bounds how long a run that has to give up waits for the lock in
// order to leave word of the directories it created. The command that holds
// the lock needs it for a handful of operations.
const handOver = 5 * time.Second

// settle looks after the directories a run created above the lock when the
// run cannot go on: an operation failed, the run was interrupted, another
// command holds the lock, or the folder is no longer what the plan was made
// for. Such a run writes nothing else, but what it created must not be lost
// track of. release gives back the lock when the run holds it.
//
// When there is a manifest, another command has got further, and the
// directories are added to its record. Otherwise they are taken away again,
// newest first, as far as they are empty. One that is not empty is in use:
// another first apply has come through it on its own way to the lock. Then
// this run waits for the lock and leaves word of what it created, in the
// manifest that is there by then, or in one of its own that holds nothing
// else; the command that finds it there makes its plan again.
func (p *Plan) settle(ctx context.Context, made []string, release func()) {
	version := ""
	if p.ledger != nil {
		version = p.ledger.next.Version
	}
	if release != nil {
		recorded := len(made) > 0 && recordDirs(p.Root, made, false, version)
		release()
		if recorded {
			return
		}
	}
	for attempt := 0; attempt < 3 && len(made) > 0; attempt++ {
		if made = removeEmpty(made); len(made) == 0 {
			return
		}
		// ROOT may be gone for a moment, taken away by a run in the same
		// position as this one: then there is no lock to be had, and the
		// directories are looked at again.
		if release := waitForLock(ctx, p.Root); release != nil {
			recorded := recordDirs(p.Root, made, true, version)
			release()
			if recorded {
				return
			}
		}
	}
}

// removeEmpty removes the directories that are empty, newest first, and
// returns the ones that are still there.
func removeEmpty(dirs []string) []string {
	var stay []string
	for i := len(dirs) - 1; i >= 0; i-- {
		RemoveEmptyDir(dirs[i])
		if _, err := os.Lstat(dirs[i]); err == nil {
			stay = append([]string{dirs[i]}, stay...)
		}
	}
	return stay
}

// waitForLock takes the lock of ROOT, waiting for another command to let go
// of it for as long as handOver and the context allow. It returns nil when
// the lock is not to be had.
func waitForLock(ctx context.Context, root string) (release func()) {
	deadline := time.Now().Add(handOver)
	for pause := time.Millisecond; ; pause = min(2*pause, 50*time.Millisecond) {
		release, err := Lock(root)
		if err == nil {
			return release
		}
		if !errors.Is(err, ErrBusy) || !time.Now().Before(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pause):
		}
	}
}

// differs compares p, from operation number from on, with fresh, a plan made
// later from the same request: they must have the same lines for the user and
// the same changes behind them. When they do not, path is the first path they
// disagree about: the one that has an operation in one plan only, or another
// operation in each.
//
// Two things may differ. Removals of directories are left out: whether a
// directory will be empty is a matter of the moment on Windows (see
// willBeEmpty), and removing one only when it is empty needs no agreement.
// And the manifest may get other bytes: p counted every directory above the
// lock as one it would create, while fresh knows which of them the run really
// created.
func (p *Plan) differs(fresh *Plan, from int) (path string, differs bool) {
	if !reflect.DeepEqual(p.Refusals, fresh.Refusals) {
		for _, refusal := range append(append([]Refusal{}, fresh.Refusals...), p.Refusals...) {
			return refusal.Path, true
		}
	}
	mine, theirs := withoutRmdir(p.Ops[from:]), withoutRmdir(fresh.Ops)
	manifest := filepath.Join(p.Root, ManifestName)
	for i := 0; i < len(mine) || i < len(theirs); i++ {
		switch {
		case i >= len(theirs):
			return mine[i].Path, true
		case i >= len(mine):
			return theirs[i].Path, true
		case !mine[i].same(theirs[i], samePath(mine[i].Path, manifest)):
			// When p's operation comes later in fresh, the one fresh has in
			// its place is new.
			for _, later := range theirs[i+1:] {
				if samePath(later.Path, mine[i].Path) && !samePath(theirs[i].Path, mine[i].Path) {
					return theirs[i].Path, true
				}
			}
			return mine[i].Path, true
		}
	}
	return "", false
}

func withoutRmdir(ops []Op) []Op {
	out := make([]Op, 0, len(ops))
	for _, op := range ops {
		if op.Verb != VerbRmdir {
			out = append(out, op)
		}
	}
	return out
}

// same reports whether two operations show the same line and make the same
// change to a path that is in the same state. anyData leaves the bytes to be
// written out of the comparison.
func (o Op) same(other Op, anyData bool) bool {
	if o.Verb != other.Verb || !samePath(o.Path, other.Path) || o.Incomplete != other.Incomplete ||
		!reflect.DeepEqual(o.Note, other.Note) || !reflect.DeepEqual(o.Removed, other.Removed) || !reflect.DeepEqual(o.Added, other.Added) {
		return false
	}
	a, b := o.act, other.act
	if a.kind != b.kind || a.perm != b.perm || a.absent != b.absent || a.sum != b.sum {
		return false
	}
	return anyData || bytes.Equal(a.data, b.data)
}

// perform carries out one operation. Directory removals are done here and
// not in action.run, because only the plan knows which directories had to
// stay.
func (p *Plan) perform(op Op, stayed *[]string) error {
	if op.act.kind != actRmdir {
		return op.act.run(op.Path)
	}
	// A directory above one that stayed cannot be empty: no waiting.
	remove := removeEmptiedDir
	for _, dir := range *stayed {
		if strings.HasPrefix(pathKey(dir), pathKey(op.Path)+string(filepath.Separator)) {
			remove = func(path string) error { _, err := RemoveEmptyDir(path); return err }
		}
	}
	if err := remove(op.Path); err != nil {
		return err
	}
	if _, err := os.Lstat(op.Path); err == nil {
		*stayed = append(*stayed, op.Path)
	}
	return nil
}

// ledger is what the plan of an apply knows about its manifest.
type ledger struct {
	path   string    // ROOT/manifest.json
	index  int       // position of the manifest operation in Plan.Ops
	old    *Manifest // as planning read it
	next   *Manifest // as the manifest operation writes it
	data   []byte    // the encoding of next
	before action    // the state of the manifest file that planning saw
}

// repair brings the manifest in line with the disk after the operation at
// index failed stopped the run: the failed operation and all that would have
// followed are taken out of the manifest the plan wanted, and the result is
// written, provided the file is still what this run left there.
//
// An operation counts as carried out when the file it was to write is on
// disk with exactly the bytes it was to get, whoever wrote them: what the
// manifest says about that file is true, and taking it back would make a
// file that is sereno's to the last byte look like one that is not.
func (l *ledger) repair(ops []Op, failed int) error {
	truth := l.next.clone()
	for _, op := range ops[failed:] {
		if op.undo != nil && !op.act.carriedOut(op.Path) {
			op.undo(truth)
		}
	}
	state := action{sum: SHA256(l.data)} // the manifest operation was carried out
	if failed <= l.index {
		// The manifest is still the one planning read. It needs a word only
		// when this run created something that it has to remember, and it
		// can be given one only when its folder exists.
		if truth.sameFacts(l.old) {
			return nil
		}
		if _, err := os.Stat(filepath.Dir(l.path)); err != nil {
			return nil
		}
		state = l.before
	}
	data, err := truth.Encode()
	if err != nil {
		return err
	}
	if !state.absent && state.sum == SHA256(data) {
		return nil
	}
	state.kind, state.data, state.perm = actWrite, data, FilePerm
	return state.run(l.path)
}

// sameFacts reports whether two manifests record the same files, directories
// and hook lines, whatever their options and versions say.
func (m *Manifest) sameFacts(other *Manifest) bool {
	a, b := m.clone(), other.clone()
	a.Options, a.Version = b.Options, b.Version
	left, errLeft := a.Encode()
	right, errRight := b.Encode()
	return errLeft == nil && errRight == nil && bytes.Equal(left, right)
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

// run carries out a change. Directory removals are not handled here (see
// Plan.perform).
func (a action) run(path string) error {
	switch a.kind {
	case actMkdir:
		return Mkdir(path)
	case actWrite:
		if err := a.verify(path); err != nil {
			if a.carriedOut(path) {
				return nil
			}
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

// carriedOut reports whether the file a write is for already holds exactly
// the bytes the write would give it. It is asked when the file is not in the
// state planning saw: that is a change to stop for, unless the change is the
// one that was wanted.
func (a action) carriedOut(path string) bool {
	if a.kind != actWrite {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	sum, err := HashFile(path)
	return err == nil && sum == SHA256(a.data)
}

// isMissing reports whether err says that a path does not exist, including
// the case where something above it is a file.
func isMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}
