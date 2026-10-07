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
	seen := map[string]bool{manifest: true}
	for _, f := range r.Files {
		if !filepath.IsAbs(f.Path) {
			return fmt.Errorf("plan: file path %q is not absolute", f.Path)
		}
		if seen[f.Path] {
			return fmt.Errorf("plan: %s is listed twice or is the manifest", f.Path)
		}
		seen[f.Path] = true
	}
	shells := map[string]bool{}
	for _, h := range r.Hooks {
		if !shellName.MatchString(h.Shell) || shells[h.Shell] {
			return fmt.Errorf("plan: hook shell %q is malformed or listed twice", h.Shell)
		}
		shells[h.Shell] = true
		if !filepath.IsAbs(h.Path) || seen[h.Path] {
			return fmt.Errorf("plan: hook path %q is not absolute, is an owned file or is the target of two hooks", h.Path)
		}
		seen[h.Path] = true
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
	old     *Manifest // as read; never modified
	next    *Manifest // as it will be written
	planned map[string]bool

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
func PlanApply(req Request) (*Plan, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	old, _, err := LoadManifest(req.Root)
	if err != nil {
		return nil, err
	}
	b := &builder{root: req.Root, old: old, next: old.clone(), planned: map[string]bool{}}
	b.next.Version = req.Version
	if req.Options != nil {
		b.next.Options = Options{Hooks: append([]string{}, req.Options.Hooks...), Profile: req.Options.Profile, Shell: req.Options.Shell}
	}
	b.refusals = append(b.refusals, req.Refusals...)

	manifestPath := filepath.Join(req.Root, ManifestName)
	b.needDirs(manifestPath, b.next.addDir)
	for _, f := range req.Files {
		if err := b.file(f); err != nil {
			return nil, err
		}
	}
	for _, h := range req.Hooks {
		b.hook(h)
	}
	manifestOp, err := b.manifest(manifestPath)
	if err != nil {
		return nil, err
	}

	plan := &Plan{Root: req.Root, Refusals: b.refusals}
	plan.Ops = append(plan.Ops, b.mkdirs...)
	plan.Ops = append(plan.Ops, b.backups...)
	plan.Ops = append(plan.Ops, manifestOp)
	plan.Ops = append(plan.Ops, b.files...)
	plan.Ops = append(plan.Ops, b.hooks...)
	return plan, nil
}

func (b *builder) refuse(path string, reason Note) {
	b.refusals = append(b.refusals, Refusal{Path: path, Reason: reason})
}

// needDirs plans the creation of the directories missing above path, top
// down, and reports each to record. It returns false, after adding a refusal,
// when something that is not a directory is in the way.
func (b *builder) needDirs(path string, record func(string)) bool {
	var missing []string
	dir := filepath.Dir(path)
	for !b.planned[dir] {
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
		b.planned[missing[i]] = true
		b.mkdirs = append(b.mkdirs, Op{Verb: VerbMkdir, Path: missing[i], act: action{kind: actMkdir}})
		record(missing[i])
	}
	return true
}

// file applies the ownership rules of R15 to one owned file.
func (b *builder) file(f File) error {
	sum := SHA256(f.Data)
	recorded, tracked := b.old.Owned[f.Path]
	info, err := os.Lstat(f.Path)
	switch {
	case isMissing(err):
		if b.needDirs(f.Path, b.next.addDir) {
			b.files = append(b.files, Op{Verb: VerbCreate, Path: f.Path, act: writeNew(f.Data, FilePerm)})
			b.next.Owned[f.Path] = sum
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
			b.files = append(b.files, Op{Verb: VerbSkip, Path: f.Path, Note: NewNote(NoteEdited)})
		case current == sum:
			b.files = append(b.files, Op{Verb: VerbKeep, Path: f.Path, Note: NewNote(NoteKept)})
		default:
			b.files = append(b.files, Op{Verb: VerbReplace, Path: f.Path, act: writeOver(f.Data, FilePerm, current)})
			b.next.Owned[f.Path] = sum
		}
	}
	return nil
}

// hook plans one hook line (R22).
func (b *builder) hook(h Hook) {
	record, had := b.old.Hook(h.Shell)
	if had && record.Path != h.Path {
		b.refuse(h.Path, NewNote(ReasonHookMoved, h.Shell, record.Path))
		return
	}
	p, reason := readProfile(h.Path)
	if !reason.IsZero() {
		b.refusals = append(b.refusals, Refusal{Path: h.Path, Reason: reason, Advice: NewNote(AdviceAddByHand), Text: h.Line})
		return
	}
	if !had {
		record = HookRecord{Shell: h.Shell, Path: h.Path, FileCreated: !p.exists}
		if p.exists {
			record.BeforeSHA256 = SHA256(p.data)
		}
	}

	edit := editHook(p, record.Inserted, h.Line, h.NewFileCRLF)
	record.Inserted = edit.inserted
	switch edit.action {
	case hookKeep:
		b.hooks = append(b.hooks, Op{Verb: VerbKeep, Path: h.Path, Note: NewNote(NoteHookPresent)})
	case hookCreate:
		created := func(dir string) {
			for _, known := range record.DirsCreated {
				if known == dir {
					return
				}
			}
			record.DirsCreated = append(record.DirsCreated, dir)
		}
		if !b.needDirs(h.Path, created) {
			return
		}
		b.hooks = append(b.hooks, Op{Verb: VerbCreate, Path: h.Path, Note: h.CreateNote, Added: []string{h.Line}, act: writeNew(edit.after, FilePerm)})
	case hookAppend, hookReplace:
		current := SHA256(p.data)
		if !record.FileCreated && record.Backup == "" {
			// The first edit of a file that was there before sereno.
			record.BeforeSHA256 = current
			if !b.backup(&record, p.data) {
				return
			}
		}
		op := Op{Verb: VerbAppend, Path: h.Path, Added: []string{h.Line}, act: writeOver(edit.after, p.perm, current)}
		if edit.action == hookReplace {
			op.Note = NewNote(NoteHookReplaces)
		}
		b.hooks = append(b.hooks, op)
	}
	b.next.setHook(record)
}

// backup plans the copy of a profile into ROOT/backups before its first edit.
// The name carries the hash of the content, so a backup left by an earlier
// installation is never overwritten and an identical one is reused.
func (b *builder) backup(record *HookRecord, data []byte) bool {
	sum := SHA256(data)
	path := filepath.Join(b.root, BackupsDir, record.Shell+"-"+sum[:16]+".bak")
	info, err := os.Lstat(path)
	switch {
	case isMissing(err):
		if !b.needDirs(path, b.next.addDir) {
			return false
		}
		b.backups = append(b.backups, Op{Verb: VerbCreate, Path: path, Note: NewNote(NoteBackupOf, record.Path), act: writeNew(data, FilePerm)})
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

// manifest plans the write of the manifest itself.
func (b *builder) manifest(path string) (Op, error) {
	// Every recorded hook is also a recorded option, whatever the caller
	// passed: the next apply must keep it (R13).
	for _, record := range b.next.Hooks {
		b.next.Options.Hooks = append(b.next.Options.Hooks, record.Shell)
	}
	data, err := b.next.Encode()
	if err != nil {
		return Op{}, err
	}
	current, err := os.ReadFile(path)
	switch {
	case isMissing(err):
		return Op{Verb: VerbCreate, Path: path, act: writeNew(data, FilePerm)}, nil
	case err != nil:
		return Op{}, fmt.Errorf("read manifest: %w", err)
	case bytes.Equal(current, data):
		return Op{Verb: VerbKeep, Path: path, Note: NewNote(NoteKept)}, nil
	}
	return Op{Verb: VerbReplace, Path: path, act: writeOver(data, FilePerm, SHA256(current))}, nil
}

// PlanUntracked plans files that are written outside the manifest, which is
// what `sereno export --to DIR` does (R38): a missing file is created with its
// missing directories, an identical one is kept, and a different one is a
// refusal. Nothing is recorded, so nothing here is removed by uninstall.
func PlanUntracked(files []File) (*Plan, error) {
	b := &builder{old: NewManifest(), next: NewManifest(), planned: map[string]bool{}}
	seen := map[string]bool{}
	for _, f := range files {
		if !filepath.IsAbs(f.Path) || seen[f.Path] {
			return nil, fmt.Errorf("plan: file path %q is not absolute or is listed twice", f.Path)
		}
		seen[f.Path] = true
		info, err := os.Lstat(f.Path)
		switch {
		case isMissing(err):
			if b.needDirs(f.Path, func(string) {}) {
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
