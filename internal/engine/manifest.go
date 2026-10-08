package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Names of the entries of ROOT that the engine itself knows (R10).
const (
	ManifestName = "manifest.json" // what sereno wrote, with hashes
	ModeName     = "mode"          // the five settings lines of R11
	BackupsDir   = "backups"       // copies of profiles taken before the first edit
)

// Schema is the manifest format this version reads and writes.
const Schema = 1

// Manifest is the record of everything sereno wrote (R12). Its encoding is
// deterministic: UTF-8, LF, two-space indent, keys sorted, no timestamps, so a
// second identical apply leaves the file byte-identical.
//
// The fields are declared in the order of their JSON keys; a test checks that
// the encoded keys are sorted at every level.
type Manifest struct {
	// Dirs lists the directories sereno created, in creation order.
	Dirs []string `json:"dirs"`
	// Hooks has one record per hook line, sorted by shell.
	Hooks []HookRecord `json:"hooks"`
	// Options are the choices of the last apply, kept for the next one (R13).
	Options Options `json:"options"`
	// Owned maps the absolute path of every file sereno wrote to the SHA-256
	// of what it last wrote there.
	Owned map[string]string `json:"owned"`
	// Schema is the format number.
	Schema int `json:"schema"`
	// Version is the sereno version that wrote the manifest.
	Version string `json:"version"`
}

// Options are the recorded choices of apply (R12, R13).
type Options struct {
	// Hooks lists the shells that have a hook line, sorted.
	Hooks []string `json:"hooks"`
	// Profile is false after --no-profile.
	Profile bool `json:"profile"`
	// Shell is auto, powershell or pwsh.
	Shell string `json:"shell"`
}

// DefaultOptions returns the options of a first apply: shell auto, profile on,
// no hooks (R13).
func DefaultOptions() Options {
	return Options{Hooks: []string{}, Profile: true, Shell: "auto"}
}

// HookRecord describes one hook line and what is needed to take it out again
// (R12, R39).
type HookRecord struct {
	// Backup is the copy of the file taken before its first edit; empty when
	// the file did not exist or has not been edited.
	Backup string `json:"backup"`
	// BeforeSHA256 is the hash of the file before the hook; empty when the
	// file did not exist.
	BeforeSHA256 string `json:"beforeSha256"`
	// DirsCreated lists the directories created for the file, in creation
	// order.
	DirsCreated []string `json:"dirsCreated"`
	// FileCreated is true when sereno created the file.
	FileCreated bool `json:"fileCreated"`
	// Inserted holds the exact bytes sereno added; JSON stores them in
	// base64. Empty when the line was already there.
	Inserted []byte `json:"inserted"`
	// Path is the profile or rc file.
	Path string `json:"path"`
	// Shell is powershell, pwsh, bash or zsh.
	Shell string `json:"shell"`
}

// NewManifest returns an empty manifest with first-apply options.
func NewManifest() *Manifest {
	m := &Manifest{Options: DefaultOptions(), Schema: Schema}
	m.normalize()
	return m
}

// normalize gives the manifest its canonical form: no nil lists (they would
// encode as null), hooks sorted by shell, option hooks sorted and unique.
func (m *Manifest) normalize() {
	if m.Dirs == nil {
		m.Dirs = []string{}
	}
	if m.Hooks == nil {
		m.Hooks = []HookRecord{}
	}
	if m.Owned == nil {
		m.Owned = map[string]string{}
	}
	for i := range m.Hooks {
		if m.Hooks[i].DirsCreated == nil {
			m.Hooks[i].DirsCreated = []string{}
		}
		if m.Hooks[i].Inserted == nil {
			m.Hooks[i].Inserted = []byte{}
		}
	}
	sort.SliceStable(m.Hooks, func(i, j int) bool { return m.Hooks[i].Shell < m.Hooks[j].Shell })
	m.Options.Hooks = sortedUnique(m.Options.Hooks)
}

func sortedUnique(list []string) []string {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

// Encode returns the canonical bytes of the manifest.
func (m *Manifest) Encode() ([]byte, error) {
	m.normalize()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// ParseManifest decodes a manifest. Unknown fields and other schema numbers
// are errors: rewriting a manifest this version does not fully understand
// would lose what a newer sereno recorded.
func ParseManifest(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if dec.More() {
		return nil, errors.New("parse manifest: unexpected data after the object")
	}
	if m.Schema != Schema {
		return nil, fmt.Errorf("parse manifest: schema %d is not supported by this version (it reads schema %d)", m.Schema, Schema)
	}
	for path := range m.Owned {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("parse manifest: owned path %q is not absolute", path)
		}
	}
	for _, hook := range m.Hooks {
		if hook.Shell == "" || !filepath.IsAbs(hook.Path) {
			return nil, fmt.Errorf("parse manifest: hook %q has no absolute path", hook.Shell)
		}
	}
	m.normalize()
	return &m, nil
}

// LoadManifest reads ROOT/manifest.json. A missing file is not an error: it
// yields an empty manifest and found is false, which is how "no previous
// apply" looks.
func LoadManifest(root string) (m *Manifest, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(root, ManifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return NewManifest(), false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read manifest: %w", err)
	}
	m, err = ParseManifest(data)
	if err != nil {
		return nil, false, err
	}
	return m, true, nil
}

// Hook returns the record of a shell's hook line.
func (m *Manifest) Hook(shell string) (HookRecord, bool) {
	for _, hook := range m.Hooks {
		if hook.Shell == shell {
			return hook, true
		}
	}
	return HookRecord{}, false
}

// setHook adds or replaces the record of a shell.
func (m *Manifest) setHook(record HookRecord) {
	for i := range m.Hooks {
		if m.Hooks[i].Shell == record.Shell {
			m.Hooks[i] = record
			return
		}
	}
	m.Hooks = append(m.Hooks, record)
}

// removeHook drops the record of a shell and the shell from the options.
func (m *Manifest) removeHook(shell string) {
	hooks := m.Hooks[:0:0]
	for _, hook := range m.Hooks {
		if hook.Shell != shell {
			hooks = append(hooks, hook)
		}
	}
	m.Hooks = hooks
	options := m.Options.Hooks[:0:0]
	for _, name := range m.Options.Hooks {
		if name != shell {
			options = append(options, name)
		}
	}
	m.Options.Hooks = options
}

// addDir records a created directory once.
func (m *Manifest) addDir(dir string) {
	m.Dirs = withPath(m.Dirs, dir)
}

// dropDir forgets a directory that was not created after all.
func (m *Manifest) dropDir(dir string) {
	m.Dirs = withoutPath(m.Dirs, dir)
}

// dropHookDir forgets a directory that was to be created for a shell's
// profile.
func (m *Manifest) dropHookDir(shell, dir string) {
	for i := range m.Hooks {
		if m.Hooks[i].Shell == shell {
			m.Hooks[i].DirsCreated = withoutPath(m.Hooks[i].DirsCreated, dir)
		}
	}
}

// withPath appends path to list unless it is there already, in whatever
// spelling (see pathKey).
func withPath(list []string, path string) []string {
	for _, existing := range list {
		if samePath(existing, path) {
			return list
		}
	}
	return append(list, path)
}

// withoutPath returns list without path, in a new slice.
func withoutPath(list []string, path string) []string {
	out := make([]string, 0, len(list))
	for _, existing := range list {
		if !samePath(existing, path) {
			out = append(out, existing)
		}
	}
	return out
}

// clone returns a deep copy, so that planning never changes the manifest it
// read.
func (m *Manifest) clone() *Manifest {
	c := &Manifest{
		Dirs:    append([]string{}, m.Dirs...),
		Options: Options{Hooks: append([]string{}, m.Options.Hooks...), Profile: m.Options.Profile, Shell: m.Options.Shell},
		Owned:   make(map[string]string, len(m.Owned)),
		Schema:  m.Schema,
		Version: m.Version,
	}
	for path, sum := range m.Owned {
		c.Owned[path] = sum
	}
	for _, hook := range m.Hooks {
		hook.DirsCreated = append([]string{}, hook.DirsCreated...)
		hook.Inserted = append([]byte{}, hook.Inserted...)
		c.Hooks = append(c.Hooks, hook)
	}
	c.normalize()
	return c
}
