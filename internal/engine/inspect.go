package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// OwnedState says how a file sereno wrote looks today.
type OwnedState string

// The states Inspect reports for an owned file.
const (
	OwnedIntact   OwnedState = "ok"       // the content is what sereno last wrote
	OwnedModified OwnedState = "modified" // other content, unreadable, or no longer a regular file
	OwnedMissing  OwnedState = "missing"  // the file is gone
)

// OwnedFile is one path of the manifest's owned list and its state.
type OwnedFile struct {
	Path  string
	State OwnedState
}

// HookState is one recorded hook line and whether it is still in its profile.
type HookState struct {
	Shell string // powershell, pwsh, bash or zsh
	Path  string // the profile or rc file
	// Present is true when the file holds a hook line: the bytes sereno
	// recorded, standing as whole lines, or any line that ends with the
	// marker. These are the rules apply and uninstall go by.
	Present bool
	// Problem is set when the file cannot be looked into, with the reason
	// apply would give for not editing it (a symbolic link, an encoding
	// sereno does not edit). Present is then false.
	Problem Note
}

// Inspection is what Inspect found: the manifest and how the things it
// records look on disk.
type Inspection struct {
	// Applied is true when ROOT has a manifest, that is, after an apply and
	// before an uninstall.
	Applied bool
	// Manifest is the manifest as read; an empty one when Applied is false.
	Manifest *Manifest
	// Owned lists every owned path, sorted.
	Owned []OwnedFile
	// Hooks lists every recorded hook line, sorted by shell.
	Hooks []HookState
}

// Count returns the number of owned files in the given state.
func (i *Inspection) Count(state OwnedState) int {
	count := 0
	for _, file := range i.Owned {
		if file.State == state {
			count++
		}
	}
	return count
}

// Inspect reads the manifest of ROOT and compares it with the disk. It writes
// nothing and starts nothing. `sereno status` builds on it (R37): Applied,
// the owned files that were modified or are missing, and whether each hook
// line is present. A missing manifest is not an error; one that cannot be
// read or parsed is.
func Inspect(root string) (*Inspection, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("inspect: root %q is not an absolute path", root)
	}
	manifest, found, err := LoadManifest(root)
	if err != nil {
		return nil, err
	}
	result := &Inspection{Applied: found, Manifest: manifest, Owned: []OwnedFile{}, Hooks: []HookState{}}

	paths := make([]string, 0, len(manifest.Owned))
	for path := range manifest.Owned {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		result.Owned = append(result.Owned, OwnedFile{Path: path, State: ownedState(path, manifest.Owned[path])})
	}
	for _, record := range manifest.Hooks {
		result.Hooks = append(result.Hooks, hookState(record))
	}
	return result, nil
}

// ownedState compares one owned file with the hash the manifest records.
func ownedState(path, recorded string) OwnedState {
	info, err := os.Lstat(path)
	switch {
	case isMissing(err):
		return OwnedMissing
	case err != nil || !info.Mode().IsRegular():
		return OwnedModified
	}
	if current, err := HashFile(path); err != nil || current != recorded {
		return OwnedModified
	}
	return OwnedIntact
}

// hookState looks for a recorded hook line in its profile.
func hookState(record HookRecord) HookState {
	state := HookState{Shell: record.Shell, Path: record.Path}
	p, reason := readProfile(record.Path, anySize)
	if !reason.IsZero() {
		state.Problem = reason
		return state
	}
	if !p.exists {
		return state
	}
	if p.kind.find(p.data, record.Inserted) >= 0 {
		state.Present = true
		return state
	}
	for _, l := range p.kind.lines(p.data) {
		if isHookLine(p.kind.decode(p.data[l.start:l.end])) {
			state.Present = true
			break
		}
	}
	return state
}
