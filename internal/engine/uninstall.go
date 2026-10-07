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
}

// remover collects the operations of an uninstall.
type remover struct {
	root string
	ops  []Op
	gone map[string]bool // paths the operations so far delete
	kept map[string]bool // paths that are known to stay
}

// glance is how long planning waits, on Windows, for a file it does not know
// to leave a directory that should end up empty. Scanners that copy a script
// next to the original remove the copy within a few milliseconds.
const glance = 300 * time.Millisecond

// PlanUninstall computes the operations of R39 without writing:
//
//  1. every recorded hook line is taken out of its profile: the exact bytes
//     sereno inserted, or any line with the marker when those bytes are no
//     longer there; a file sereno created is deleted when it ends up empty,
//     with the directories created for it; the backup is deleted when the
//     profile is back to its content from before the hook and kept otherwise;
//  2. every owned file whose hash still matches is deleted; an edited one is
//     skipped, which makes the exit code 4;
//  3. the mode file and the manifest are deleted, then backups/, the
//     directories sereno created (newest first) and ROOT, each only if empty.
//
// Without a manifest there is nothing to do and the plan is empty.
func PlanUninstall(req UninstallRequest) (*Plan, error) {
	if !filepath.IsAbs(req.Root) {
		return nil, fmt.Errorf("plan: root %q is not an absolute path", req.Root)
	}
	old, found, err := LoadManifest(req.Root)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Root: req.Root}
	if !found {
		return plan, nil
	}
	u := &remover{root: req.Root, gone: map[string]bool{}, kept: map[string]bool{}}
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
	current, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(only) > 0 {
		next.Version = req.Version
		data, err := next.Encode()
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(current, data) {
			u.ops = append(u.ops, Op{Verb: VerbReplace, Path: manifestPath, act: writeOver(data, FilePerm, SHA256(current))})
		}
		plan.Ops = u.ops
		return plan, nil
	}

	modePath := filepath.Join(req.Root, ModeName)
	paths := make([]string, 0, len(old.Owned))
	for path := range old.Owned {
		if path != modePath {
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
			u.leave(Op{Verb: VerbSkip, Path: modePath, Note: NewNote(NoteNotRegular)})
		} else {
			u.remove(modePath, action{kind: actRemove, absent: false, sum: hashOrEmpty(modePath)}, Note{}, nil)
		}
	}
	u.remove(manifestPath, action{kind: actRemove, sum: SHA256(current)}, Note{}, nil)
	u.rmdir(filepath.Join(req.Root, BackupsDir))
	for i := len(old.Dirs) - 1; i >= 0; i-- {
		u.rmdir(old.Dirs[i])
	}
	u.rmdir(req.Root)

	plan.Ops = u.ops
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
	u.gone[path] = true
}

// leave adds an operation for a path that stays where it is.
func (u *remover) leave(op Op) {
	u.ops = append(u.ops, op)
	u.kept[op.Path] = true
}

// rmdir plans the removal of a directory that will be empty once the
// operations planned so far are done.
func (u *remover) rmdir(dir string) {
	if u.gone[dir] {
		return
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	if !u.willBeEmpty(dir) {
		u.kept[dir] = true
		return
	}
	u.ops = append(u.ops, Op{Verb: VerbRmdir, Path: dir, act: action{kind: actRmdir}})
	u.gone[dir] = true
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
			path := filepath.Join(dir, entry.Name())
			switch {
			case u.gone[path]:
			case u.kept[path] || entry.IsDir():
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
		u.leave(Op{Verb: VerbSkip, Path: path, Note: NewNote(NoteNotRegular)})
		return nil
	}
	current, err := HashFile(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if current != recorded {
		u.leave(Op{Verb: VerbSkip, Path: path, Note: NewNote(NoteEdited)})
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
	keepBackup := func() {
		if record.Backup == "" {
			return
		}
		if _, err := os.Lstat(record.Backup); err == nil {
			u.leave(Op{Verb: VerbKeep, Path: record.Backup, Note: NewNote(NoteBackupKept, record.Path)})
		}
	}

	p, reason := readProfile(record.Path)
	if !reason.IsZero() {
		// The file became something sereno does not edit (a link, another
		// encoding): it is left alone and reported, with the line to delete.
		op := Op{Verb: VerbSkip, Path: record.Path, Note: reason}
		if line := insertedText(record.Inserted); line != "" {
			op.Removed = []string{line}
		}
		u.leave(op)
		keepBackup()
		return false
	}
	if !p.exists {
		// Gone already. If sereno had created it, its directories may go.
		if record.FileCreated {
			u.rmdirs(record.DirsCreated)
		} else {
			keepBackup()
		}
		return true
	}

	removal := removeHook(p, record.Inserted, backup, haveBackup)
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
		keepBackup()
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
