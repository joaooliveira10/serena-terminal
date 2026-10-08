package engine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// shellName is the form of a manifest hook key; it also becomes part of a
// backup file name.
var shellName = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// validate rejects a request no plan can be made for. These are mistakes of
// the calling code (a relative path, a hook line without the marker), not
// refusals: they are reported as errors.
func (r Request) validate() error {
	if !filepath.IsAbs(r.Root) {
		return fmt.Errorf("plan: root %q is not an absolute path", r.Root)
	}
	manifest := filepath.Join(r.Root, ManifestName)
	seen := map[string]bool{pathKey(manifest): true}
	for _, f := range r.Files {
		if !filepath.IsAbs(f.Path) {
			return fmt.Errorf("plan: file path %q is not absolute", f.Path)
		}
		if seen[pathKey(f.Path)] {
			return fmt.Errorf("plan: %s is listed twice or is the manifest", f.Path)
		}
		seen[pathKey(f.Path)] = true
	}
	shells := map[string]bool{}
	for _, h := range r.Hooks {
		if !shellName.MatchString(h.Shell) || shells[h.Shell] {
			return fmt.Errorf("plan: hook shell %q is malformed or listed twice", h.Shell)
		}
		shells[h.Shell] = true
		if !filepath.IsAbs(h.Path) || seen[pathKey(h.Path)] {
			return fmt.Errorf("plan: hook path %q is not absolute, is an owned file or is the target of two hooks", h.Path)
		}
		seen[pathKey(h.Path)] = true
		if err := checkHookLine(h.Line); err != nil {
			return fmt.Errorf("plan: hook line for %s: %w", h.Shell, err)
		}
	}
	return nil
}

// checkHookLine enforces R21: one line of printable ASCII that ends with the
// marker.
func checkHookLine(text string) error {
	if !strings.HasSuffix(text, " "+HookMarker) {
		return fmt.Errorf("does not end with %q", HookMarker)
	}
	for i := 0; i < len(text); i++ {
		if text[i] < 0x20 || text[i] > 0x7E {
			return errors.New("is not a single line of printable ASCII")
		}
	}
	return nil
}

// builder collects the operations of an apply in the phases of R13.
type builder struct {
	root    string
	old     *Manifest       // as read; never modified
	next    *Manifest       // as it will be written
	planned map[string]bool // directories an earlier operation creates, by pathKey

	mkdirs, backups, files, hooks []Op
	refusals                      []Refusal
}

// PlanApply is phase 1 of R13: it computes every operation and every refusal
// for a request and writes nothing. Ownership follows R15:
//
//   - a target that exists and is not in the manifest is a refusal;
//   - a recorded file whose hash still matches is replaced when the content
//     changes and kept otherwise;
//   - a recorded file whose hash differs was edited by the user and is skipped;
//   - a recorded file that is gone is created again.
//
// Operations are ordered as phase 2 performs them: directories, backups of
// profiles about to be edited, the manifest (already listing the hashes about
// to be written), owned files, hook lines.
//
// A path the manifest already records is taken in the spelling recorded
// there. On Windows, where file names ignore letter case, the same folder
// named in another case is therefore the same installation and not a foreign
// file in the way.
//
// The error is ErrBusy when another sereno is writing the folder, or has
// written it since the caller read the manifest it passes in the request.
func PlanApply(req Request) (*Plan, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	return confirmed(req.Root, func(made []string) (*Plan, error) { return planApply(req, made) })
}

// confirmed makes a plan with the function it is given and keeps the function
// in the plan, so that the plan can be made again under the lock (see
// Plan.ApplyContext).
//
// A plan is a picture of the folder, and a picture taken while another sereno
// writes the folder shows things that are not so: a file without its record,
// a record without its file. When another command holds the lock at the end
// of planning, the answer is therefore ErrBusy and no plan.
//
// The other command may also have finished a moment before. A plan that
// refuses, that leaves something in place, or that could not be made because
// a file could not be read, says something about the user's files, and each
// of the three is what such a picture looks like. Before anybody is told,
// such a plan is made a second time; when the two do not agree, the answer
// is ErrBusy and not a tale about the user's files.
func confirmed(root string, again func(made []string) (*Plan, error)) (*Plan, error) {
	plan, err := again(nil)
	if errors.Is(err, ErrBusy) || Busy(root) {
		return nil, ErrBusy
	}
	if err == nil && !plan.Refused() && plan.Incomplete() == 0 {
		plan.again = again
		return plan, nil
	}
	second, secondErr := again(nil)
	switch {
	case errors.Is(secondErr, ErrBusy), (err == nil) != (secondErr == nil):
		return nil, ErrBusy
	case err != nil:
		return nil, err
	}
	if _, differs := plan.differs(second, 0); differs || plan.manifest != second.manifest {
		return nil, ErrBusy
	}
	second.again = again
	return second, nil
}

// planApply is PlanApply for a request that has been validated. made lists
// the directories the run has created already, on its way to the lock (see
// Plan.begin): they are recorded like directories that are still to be
// created, and need no operation any more.
func planApply(req Request, made []string) (*Plan, error) {
	current, state, err := readManifestFile(req.Root)
	if err != nil {
		return nil, err
	}
	old := req.Manifest
	switch {
	case old == nil || !old.origin.known:
		if old, err = manifestFrom(current, state); err != nil {
			return nil, err
		}
	case old.origin != state:
		// The caller built the request on a manifest that is no longer the
		// one on disk.
		return nil, ErrBusy
	}
	b := &builder{root: req.Root, old: old, next: old.clone(), planned: map[string]bool{}}
	for _, dir := range made {
		b.planned[pathKey(dir)] = true
		b.next.addDir(dir)
	}
	b.next.Version = req.Version
	if req.Options != nil {
		b.next.Options = Options{Hooks: append([]string{}, req.Options.Hooks...), Profile: req.Options.Profile, Shell: req.Options.Shell}
	}
	b.refusals = append(b.refusals, req.Refusals...)

	manifestPath := filepath.Join(req.Root, ManifestName)
	b.needDirs(manifestPath, b.ownedDir)
	if lockInTheWay(req.Root) {
		// Phase 2 could not take the lock: better said now than then.
		b.refuse(filepath.Join(req.Root, LockName), NewNote(ReasonForeign))
	}
	recorded := map[string]string{}
	for path := range old.Owned {
		recorded[pathKey(path)] = path
	}
	for _, f := range req.Files {
		if spelling, ok := recorded[pathKey(f.Path)]; ok {
			f.Path = spelling
		}
		if err := b.file(f); err != nil {
			return nil, err
		}
	}
	for _, h := range req.Hooks {
		b.hook(h)
	}
	manifestOp, book, err := b.manifest(manifestPath, current, state)
	if err != nil {
		return nil, err
	}

	plan := &Plan{Root: req.Root, Refusals: b.refusals, ledger: book, manifest: state}
	plan.Ops = append(plan.Ops, b.mkdirs...)
	plan.Ops = append(plan.Ops, b.backups...)
	book.index = len(plan.Ops)
	plan.Ops = append(plan.Ops, manifestOp)
	plan.Ops = append(plan.Ops, b.files...)
	plan.Ops = append(plan.Ops, b.hooks...)
	if err := stillAt(req.Root, state); err != nil {
		return nil, err
	}
	return plan, nil
}

// stillAt ends the making of a plan: it returns ErrBusy when the manifest
// file is no longer in the state the plan was started from. Whoever writes
// the manifest writes it before the files it lists and deletes it after
// them, so a manifest that changed while a plan was being made means that
// the files were looked at while they were being written.
func stillAt(root string, state fileState) error {
	if _, now, err := readManifestFile(root); err != nil || now != state {
		return ErrBusy
	}
	return nil
}

func (b *builder) refuse(path string, reason Note) {
	b.refusals = append(b.refusals, Refusal{Path: path, Reason: reason})
}

// ownedDir records a directory that is about to be created for an owned file
// and returns what takes the record back.
func (b *builder) ownedDir(dir string) func(*Manifest) {
	b.next.addDir(dir)
	return func(m *Manifest) { m.dropDir(dir) }
}

// needDirs plans the creation of the directories missing above path, top
// down. record notes each of them where it belongs and returns the function
// that takes the note out of a manifest again (see Op.undo). needDirs returns
// false, after adding a refusal, when something that is not a directory is
// in the way.
func (b *builder) needDirs(path string, record func(dir string) func(*Manifest)) bool {
	var missing []string
	dir := filepath.Dir(path)
	for !b.planned[pathKey(dir)] {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				b.refuse(path, NewNote(ReasonParent, dir))
				return false
			}
			break
		}
		if !isMissing(err) {
			b.refuse(path, NewNote(ReasonUnreadable, cause(err)))
			return false
		}
		missing = append(missing, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			b.refuse(path, NewNote(ReasonParent, dir))
			return false
		}
		dir = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		b.planned[pathKey(missing[i])] = true
		b.mkdirs = append(b.mkdirs, Op{Verb: VerbMkdir, Path: missing[i], act: action{kind: actMkdir}, undo: record(missing[i])})
	}
	return true
}

// file applies the ownership rules of R15 to one owned file.
func (b *builder) file(f File) error {
	if !f.Skip.IsZero() {
		b.files = append(b.files, Op{Verb: VerbSkip, Path: f.Path, Note: f.Skip})
		return nil
	}
	sum := SHA256(f.Data)
	recorded, tracked := b.old.Owned[f.Path]
	// record puts the new hash into the manifest and returns what puts the
	// old state back if the file is not written after all.
	record := func() func(*Manifest) {
		b.next.Owned[f.Path] = sum
		return func(m *Manifest) {
			if tracked {
				m.Owned[f.Path] = recorded
			} else {
				delete(m.Owned, f.Path)
			}
		}
	}
	info, err := os.Lstat(f.Path)
	switch {
	case isMissing(err):
		if b.needDirs(f.Path, b.ownedDir) {
			b.files = append(b.files, Op{Verb: VerbCreate, Path: f.Path, act: writeNew(f.Data, FilePerm), undo: record()})
		}
	case err != nil:
		return fmt.Errorf("inspect %s: %w", f.Path, err)
	case !info.Mode().IsRegular():
		b.refuse(f.Path, NewNote(ReasonNotRegular))
	case !tracked:
		b.refuse(f.Path, NewNote(ReasonForeign))
	default:
		current, err := HashFile(f.Path)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", f.Path, err)
		}
		switch {
		case current != recorded:
			b.files = append(b.files, Op{Verb: VerbSkip, Path: f.Path, Note: NewNote(NoteEdited), Incomplete: true})
		case current == sum:
			b.files = append(b.files, Op{Verb: VerbKeep, Path: f.Path, Note: NewNote(NoteKept)})
		case info.Mode().Perm()&0o200 == 0:
			// The user has marked the file read-only since sereno wrote it.
			// That is a wish, and on Windows also a fact: the file cannot be
			// replaced. It is left as a profile in that state is (R22).
			b.files = append(b.files, Op{Verb: VerbSkip, Path: f.Path, Note: NewNote(NoteReadOnly), Incomplete: true})
		default:
			b.files = append(b.files, Op{Verb: VerbReplace, Path: f.Path, act: writeOver(f.Data, FilePerm, current), undo: record()})
		}
	}
	return nil
}

// hook plans one hook line (R22).
func (b *builder) hook(h Hook) {
	prev, had := b.old.Hook(h.Shell)
	if had && !samePath(prev.Path, h.Path) {
		b.refuse(h.Path, NewNote(ReasonHookMoved, h.Shell, prev.Path))
		return
	}
	if had {
		h.Path = prev.Path
	}
	// A profile sereno will not edit is a refusal that shows the line, so
	// that the user can add it by hand.
	refuse := func(reason Note) {
		b.refusals = append(b.refusals, Refusal{Path: h.Path, Reason: reason, Advice: NewNote(AdviceAddByHand), Text: h.Line})
	}
	p, reason := readProfile(h.Path, MaxProfileSize)
	if !reason.IsZero() {
		refuse(reason)
		return
	}
	record := prev
	record.DirsCreated = append([]string{}, prev.DirsCreated...)
	if !had {
		record = HookRecord{Shell: h.Shell, Path: h.Path, FileCreated: !p.exists}
		if p.exists {
			record.BeforeSHA256 = SHA256(p.data)
		}
	}

	edit := editHook(p, record.Inserted, h.Line, h.NewFileCRLF)
	if edit.action == hookAppend || edit.action == hookReplace {
		switch {
		case p.readOnly:
			refuse(NewNote(ReasonReadOnly))
			return
		case len(edit.after) > MaxProfileSize:
			// The file is within the limit, but would not be with the line:
			// sereno would refuse its own result at the next run.
			refuse(NewNote(ReasonNoRoom))
			return
		}
	}
	record.Inserted = edit.inserted

	// undo takes the line out of a manifest when it did not reach the file.
	// What was created on the way stays recorded, so that uninstall finds
	// it: the directories made for a new profile and the backup of an
	// existing one. A record that holds neither is dropped.
	undo := func(m *Manifest) {
		current, ok := m.Hook(h.Shell)
		if !ok {
			return
		}
		current.Inserted = append([]byte{}, prev.Inserted...)
		if !had && len(current.DirsCreated) == 0 && current.Backup == "" {
			m.removeHook(h.Shell)
			return
		}
		m.setHook(current)
	}

	switch edit.action {
	case hookKeep:
		b.hooks = append(b.hooks, Op{Verb: VerbKeep, Path: h.Path, Note: NewNote(NoteHookPresent)})
	case hookCreate:
		created := func(dir string) func(*Manifest) {
			record.DirsCreated = withPath(record.DirsCreated, dir)
			return func(m *Manifest) { m.dropHookDir(h.Shell, dir) }
		}
		if !b.needDirs(h.Path, created) {
			return
		}
		b.hooks = append(b.hooks, Op{Verb: VerbCreate, Path: h.Path, Note: h.CreateNote, Added: []string{h.Line}, act: writeNew(edit.after, FilePerm), undo: undo})
	case hookAppend, hookReplace:
		current := SHA256(p.data)
		if !record.FileCreated && record.Backup == "" {
			// The first edit of a file that was there before sereno.
			record.BeforeSHA256 = current
			if !b.backup(&record, p, prev.BeforeSHA256) {
				return
			}
		}
		op := Op{Verb: VerbAppend, Path: h.Path, Added: []string{h.Line}, act: writeOver(edit.after, p.perm, current), undo: undo}
		if edit.action == hookReplace {
			op.Note = NewNote(NoteHookReplaces)
			op.Removed = []string{edit.replaced}
		}
		b.hooks = append(b.hooks, op)
	}
	b.next.setHook(record)
}

// backupPerm bounds the permission bits of a backup: those of the profile it
// copies, and never more than an ordinary file has. A profile the user keeps
// private (0600, with tokens in it) gets a private backup.
const backupPerm = FilePerm

// backup plans the copy of a profile into ROOT/backups before its first edit.
// The name carries the hash of the content, so a backup left by an earlier
// installation is never overwritten and an identical one is reused.
// hashBefore is what the record said about the file until now; it comes back
// when the backup cannot be written.
func (b *builder) backup(record *HookRecord, p profile, hashBefore string) bool {
	sum := SHA256(p.data)
	shell := record.Shell
	path := filepath.Join(b.root, BackupsDir, shell+"-"+sum[:16]+".bak")
	info, err := os.Lstat(path)
	switch {
	case isMissing(err):
		if !b.needDirs(path, b.ownedDir) {
			return false
		}
		b.backups = append(b.backups, Op{
			Verb: VerbCreate, Path: path, Note: NewNote(NoteBackupOf, record.Path),
			act: writeNew(p.data, p.perm&backupPerm),
			undo: func(m *Manifest) {
				if current, ok := m.Hook(shell); ok {
					current.Backup, current.BeforeSHA256 = "", hashBefore
					m.setHook(current)
				}
			},
		})
	case err != nil || !info.Mode().IsRegular():
		b.refuse(path, NewNote(ReasonBackupBlocked))
		return false
	default:
		if existing, err := HashFile(path); err != nil || existing != sum {
			b.refuse(path, NewNote(ReasonBackupBlocked))
			return false
		}
	}
	record.Backup = path
	return true
}

// manifest plans the write of the manifest itself and collects what Apply
// needs to correct it if a later operation fails. current and state are the
// manifest file as planning read it, once, at its start.
func (b *builder) manifest(path string, current []byte, state fileState) (Op, *ledger, error) {
	// Every recorded hook is also a recorded option, whatever the caller
	// passed: the next apply must keep it (R13).
	for _, record := range b.next.Hooks {
		b.next.Options.Hooks = append(b.next.Options.Hooks, record.Shell)
	}
	data, err := b.next.Encode()
	if err != nil {
		return Op{}, nil, err
	}
	book := &ledger{path: path, old: b.old, next: b.next, data: data}
	if state.absent {
		book.before = action{absent: true}
		return Op{Verb: VerbCreate, Path: path, act: writeNew(data, FilePerm)}, book, nil
	}
	book.before = action{sum: state.sum}
	if bytes.Equal(current, data) {
		return Op{Verb: VerbKeep, Path: path, Note: NewNote(NoteKept)}, book, nil
	}
	return Op{Verb: VerbReplace, Path: path, act: writeOver(data, FilePerm, state.sum)}, book, nil
}

// PlanUntracked plans files that are written outside the manifest, which is
// what `sereno export --to DIR` does (R38): a missing file is created with its
// missing directories, an identical one is kept, and a different one is a
// refusal. Nothing is recorded, so nothing here is removed by uninstall.
func PlanUntracked(files []File) (*Plan, error) {
	b := &builder{old: NewManifest(), next: NewManifest(), planned: map[string]bool{}}
	unrecorded := func(string) func(*Manifest) { return nil }
	seen := map[string]bool{}
	for _, f := range files {
		if !filepath.IsAbs(f.Path) || seen[pathKey(f.Path)] {
			return nil, fmt.Errorf("plan: file path %q is not absolute or is listed twice", f.Path)
		}
		seen[pathKey(f.Path)] = true
		info, err := os.Lstat(f.Path)
		switch {
		case isMissing(err):
			if b.needDirs(f.Path, unrecorded) {
				b.files = append(b.files, Op{Verb: VerbCreate, Path: f.Path, act: writeNew(f.Data, FilePerm)})
			}
		case err != nil:
			return nil, fmt.Errorf("inspect %s: %w", f.Path, err)
		case !info.Mode().IsRegular():
			b.refuse(f.Path, NewNote(ReasonNotRegular))
		default:
			current, err := os.ReadFile(f.Path)
			if err != nil {
				return nil, fmt.Errorf("inspect %s: %w", f.Path, err)
			}
			if !bytes.Equal(current, f.Data) {
				b.refuse(f.Path, NewNote(ReasonDifferent))
				continue
			}
			b.files = append(b.files, Op{Verb: VerbKeep, Path: f.Path, Note: NewNote(NoteKept)})
		}
	}
	plan := &Plan{Refusals: b.refusals}
	plan.Ops = append(plan.Ops, b.mkdirs...)
	plan.Ops = append(plan.Ops, b.files...)
	return plan, nil
}
