package engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// UninstallRequest describes an uninstall (R39).
type UninstallRequest struct {
	Root    string
	Version string
	// Shells, when not empty, limits the uninstall to the hook lines of these
	// shells (`sereno uninstall --hook LIST`): the manifest is updated and
	// everything else stays.
	Shells []string
	// Manifest is the manifest the caller loaded and decided on, as
	// LoadManifest returned it; see Request.Manifest.
	Manifest *Manifest
}

// remover collects the operations of an uninstall.
type remover struct {
	root string
	ops  []Op
	gone map[string]bool // paths the operations so far delete, by pathKey
	kept map[string]bool // paths that are known to stay, by pathKey
	// absent holds the hook records whose file was not there (Plan.GoneHooks).
	absent []HookRecord
}

// glance is how long planning waits, on Windows, for a file it does not know
// to leave a directory that should end up empty. Scanners that copy a script
// next to the original remove the copy within a few milliseconds.
const glance = 300 * time.Millisecond

// PlanUninstall computes the operations of R39 without writing:
//
//  1. every recorded hook line is taken out of its profile: the exact bytes
//     sereno inserted where they still stand as whole lines, otherwise every
//     line that ends with the marker; a file sereno created is deleted when
//     it ends up empty, with the directories created for it; the backup is
//     deleted when the profile is back to its content from before the hook
//     and kept otherwise;
//  2. every owned file whose hash still matches is deleted; an edited one is
//     skipped, which makes the exit code 4;
//  3. the mode file and the manifest are deleted, then backups/, the
//     directories sereno created (newest first) and ROOT, each only if empty.
//
// Without a manifest there is nothing to do and the plan is empty. The plan
// has a refusal, and must not be applied, when something that is not sereno's
// has the name of the lock file in ROOT (see Lock).
//
// The error is ErrBusy when another sereno is writing the folder, or has
// written it since the caller read the manifest it passes in the request.
func PlanUninstall(req UninstallRequest) (*Plan, error) {
	if !filepath.IsAbs(req.Root) {
		return nil, fmt.Errorf("plan: root %q is not an absolute path", req.Root)
	}
	return confirmed(req.Root, func([]string) (*Plan, error) { return planUninstall(req) })
}

// planUninstall is PlanUninstall for a request that has been validated.
func planUninstall(req UninstallRequest) (*Plan, error) {
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
		return nil, ErrBusy
	}
	plan := &Plan{Root: req.Root, manifest: state}
	if state.absent {
		return plan, nil
	}
	if lockInTheWay(req.Root) {
		// The one refusal an uninstall knows: it could not take the lock.
		plan.Refusals = []Refusal{{Path: filepath.Join(req.Root, LockName), Reason: NewNote(ReasonForeign)}}
	}
	u := &remover{root: req.Root, gone: map[string]bool{}, kept: map[string]bool{}}
	// The lock file is in ROOT while a command writes, this one included; it
	// is gone before ROOT is removed (see Plan.carry).
	u.gone[pathKey(filepath.Join(req.Root, LockName))] = true
	next := old.clone()
	only := map[string]bool{}
	for _, shell := range req.Shells {
		only[shell] = true
	}

	for _, record := range old.Hooks {
		if len(only) > 0 && !only[record.Shell] {
			continue
		}
		if u.hook(record) {
			next.removeHook(record.Shell)
		}
	}

	manifestPath := filepath.Join(req.Root, ManifestName)
	if len(only) > 0 {
		next.Version = req.Version
		data, err := next.Encode()
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(current, data) {
			u.ops = append(u.ops, Op{Verb: VerbReplace, Path: manifestPath, act: writeOver(data, FilePerm, state.sum)})
		}
		plan.Ops, plan.GoneHooks = u.ops, u.absent
		if err := stillAt(req.Root, state); err != nil {
			return nil, err
		}
		return plan, nil
	}

	modePath := filepath.Join(req.Root, ModeName)
	paths := make([]string, 0, len(old.Owned))
	for path := range old.Owned {
		if !samePath(path, modePath) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := u.owned(path, old.Owned[path]); err != nil {
			return nil, err
		}
	}

	// The mode file holds settings, not user content: it goes with the
	// manifest whatever its hash.
	if info, err := os.Lstat(modePath); err == nil {
		if info.IsDir() {
			u.leave(Op{Verb: VerbSkip, Path: modePath, Note: NewNote(NoteNotRegular), Incomplete: true})
		} else {
			u.remove(modePath, action{kind: actRemove, absent: false, sum: hashOrEmpty(modePath)}, Note{}, nil)
		}
	}
	u.remove(manifestPath, action{kind: actRemove, sum: state.sum}, Note{}, nil)
	u.rmdir(filepath.Join(req.Root, BackupsDir))
	for i := len(old.Dirs) - 1; i >= 0; i-- {
		u.rmdir(old.Dirs[i])
	}
	u.rmdir(req.Root)

	plan.Ops, plan.GoneHooks = u.ops, u.absent
	if err := stillAt(req.Root, state); err != nil {
		return nil, err
	}
	return plan, nil
}

// hashOrEmpty returns the hash of a file, or "" when it cannot be read; the
// removal then fails its verification instead of deleting something unknown.
func hashOrEmpty(path string) string {
	sum, err := HashFile(path)
	if err != nil {
		return ""
	}
	return sum
}

func (u *remover) remove(path string, act action, note Note, removed []string) {
	u.ops = append(u.ops, Op{Verb: VerbRemove, Path: path, Note: note, Removed: removed, act: act})
	u.gone[pathKey(path)] = true
}

// leave adds an operation for a path that stays where it is.
func (u *remover) leave(op Op) {
	u.ops = append(u.ops, op)
	u.kept[pathKey(op.Path)] = true
}

// rmdir plans the removal of a directory that will be empty once the
// operations planned so far are done.
func (u *remover) rmdir(dir string) {
	if u.gone[pathKey(dir)] {
		return
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	if !u.willBeEmpty(dir) {
		u.kept[pathKey(dir)] = true
		return
	}
	u.ops = append(u.ops, Op{Verb: VerbRmdir, Path: dir, act: action{kind: actRmdir}})
	u.gone[pathKey(dir)] = true
}

// willBeEmpty reports whether dir holds nothing but what the planned
// operations delete. A subdirectory or a path the plan leaves in place settles
// the question at once. A file the plan knows nothing about does so on macOS
// and Linux; on Windows it is given a glance to disappear, because scanners
// drop short-lived copies next to the files sereno has just read.
func (u *remover) willBeEmpty(dir string) bool {
	deadline := time.Now().Add(glance)
	for pause := 5 * time.Millisecond; ; pause *= 2 {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		strangers := 0
		for _, entry := range entries {
			key := pathKey(filepath.Join(dir, entry.Name()))
			switch {
			case u.gone[key]:
			case u.kept[key] || entry.IsDir():
				return false
			default:
				strangers++
			}
		}
		if strangers == 0 {
			return true
		}
		if !patient || !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(pause)
	}
}

// owned applies step 2 of R39 to one owned file.
func (u *remover) owned(path, recorded string) error {
	info, err := os.Lstat(path)
	switch {
	case isMissing(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect %s: %w", path, err)
	case !info.Mode().IsRegular():
		u.leave(Op{Verb: VerbSkip, Path: path, Note: NewNote(NoteNotRegular), Incomplete: true})
		return nil
	}
	current, err := HashFile(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if current != recorded {
		u.leave(Op{Verb: VerbSkip, Path: path, Note: NewNote(NoteEdited), Incomplete: true})
		return nil
	}
	u.remove(path, action{kind: actRemove, sum: current}, Note{}, nil)
	return nil
}

// hook applies step 1 of R39 to one recorded hook line. It reports false when
// the profile had to be left alone; the record then stays in the manifest of
// a hook-only uninstall, so that a later run can finish the job.
func (u *remover) hook(record HookRecord) bool {
	backup, haveBackup := readBackup(record)
	// keepBackup reports a backup that stays, with the reason: the profile is
	// not what it was before the hook (NoteBackupKept), or the line could
	// not be taken out of it at all (NoteBackupLine).
	keepBackup := func(why string) {
		if record.Backup == "" {
			return
		}
		if _, err := os.Lstat(record.Backup); err == nil {
			u.leave(Op{Verb: VerbKeep, Path: record.Backup, Note: NewNote(why, record.Path)})
		}
	}

	// skip leaves a profile sereno does not edit alone and reports it, with
	// the lines the user may want to delete by hand.
	skip := func(reason Note, lines []string) bool {
		u.leave(Op{Verb: VerbSkip, Path: record.Path, Note: reason, Removed: lines, Incomplete: true})
		keepBackup(NoteBackupLine)
		return false
	}

	// The size limit of apply does not hold here: taking the line out is
	// safe however much the user has added to the file since.
	p, reason := readProfile(record.Path, anySize)
	if !reason.IsZero() {
		// The file became something sereno does not edit (a link, another
		// encoding).
		var lines []string
		if line := insertedText(record.Inserted); line != "" {
			lines = []string{line}
		}
		return skip(reason, lines)
	}
	if !p.exists {
		// Gone already. If sereno had created it, its directories may go.
		// Deleted or moved, that cannot be told from here, and a file that
		// was moved has the line in it still: the plan notes the record, so
		// that the command can say so.
		u.absent = append(u.absent, record)
		if record.FileCreated {
			u.rmdirs(record.DirsCreated)
		} else {
			keepBackup(NoteBackupKept)
		}
		return true
	}

	removal := removeHook(p, record.Inserted, backup, haveBackup)
	if p.readOnly && !bytes.Equal(removal.after, p.data) {
		// The user made the file read-only after the hook line went in.
		return skip(NewNote(ReasonReadOnly), removal.removed)
	}
	current := SHA256(p.data)
	restored := SHA256(removal.after) == record.BeforeSHA256
	switch {
	case record.FileCreated && len(removal.after) == 0:
		u.remove(record.Path, action{kind: actRemove, sum: current}, Note{}, removal.removed)
		u.rmdirs(record.DirsCreated)
		restored = true
	case !bytes.Equal(removal.after, p.data):
		note := NewNote(NoteHookRemoved)
		if removal.restored {
			note = NewNote(NoteHookRestored)
		}
		u.ops = append(u.ops, Op{Verb: VerbReplace, Path: record.Path, Note: note, Removed: removal.removed, act: writeOver(removal.after, p.perm, current)})
	}
	if record.Backup == "" {
		return true
	}
	if !restored {
		keepBackup(NoteBackupKept)
		return true
	}
	if info, err := os.Lstat(record.Backup); err == nil && info.Mode().IsRegular() {
		u.remove(record.Backup, action{kind: actRemove, sum: hashOrEmpty(record.Backup)}, Note{}, nil)
	}
	return true
}

// rmdirs plans the removal of directories in reverse creation order.
func (u *remover) rmdirs(dirs []string) {
	for i := len(dirs) - 1; i >= 0; i-- {
		u.rmdir(dirs[i])
	}
}

// readBackup returns the bytes of a hook's backup when it is still exactly
// what sereno saved. A missing or altered backup is not used for anything.
func readBackup(record HookRecord) ([]byte, bool) {
	if record.Backup == "" {
		return nil, false
	}
	data, err := os.ReadFile(record.Backup)
	if err != nil || SHA256(data) != record.BeforeSHA256 {
		return nil, false
	}
	return data, true
}
