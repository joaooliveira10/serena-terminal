package engine

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// sampleManifest builds a manifest that uses every field, with paths inside
// the temporary home and lists deliberately out of order.
func sampleManifest(home *testenv.Home) *Manifest {
	root := filepath.Join(home.LocalAppData, "sereno")
	return &Manifest{
		Dirs: []string{root, filepath.Join(root, "shell"), filepath.Join(home.Dir, "R&D <team>")},
		Hooks: []HookRecord{
			{Shell: "pwsh", Path: home.PwshProfile, FileCreated: true, DirsCreated: []string{filepath.Dir(home.PwshProfile)}, Inserted: []byte("line # sereno:hook\r\n")},
			{Shell: "bash", Path: filepath.Join(home.Dir, ".bashrc"), BeforeSHA256: SHA256([]byte("before")), Backup: filepath.Join(root, BackupsDir, "bash-0000000000000000.bak"), Inserted: []byte("\nline <&> # sereno:hook\n")},
		},
		Options: Options{Hooks: []string{"pwsh", "bash", "pwsh"}, Profile: true, Shell: "auto"},
		Owned: map[string]string{
			filepath.Join(root, "shell", "init.ps1"): SHA256([]byte("ps1")),
			filepath.Join(root, ModeName):            SHA256([]byte("mode")),
		},
		Schema:  Schema,
		Version: "0.1.0",
	}
}

// TestManifestRoundTrip is the R12 requirement: encoding is deterministic,
// carries no timestamp, and decoding gives back what was encoded.
func TestManifestRoundTrip(t *testing.T) {
	home := testenv.New(t)
	first, err := sampleManifest(home).Encode()
	if err != nil {
		t.Fatal(err)
	}
	second, err := sampleManifest(home).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("two encodings of the same manifest differ")
	}

	parsed, err := ParseManifest(first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := parsed.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, again) {
		t.Errorf("decode and encode changed the bytes:\n%s\n---\n%s", first, again)
	}

	if parsed.Hooks[0].Shell != "bash" || parsed.Hooks[1].Shell != "pwsh" {
		t.Errorf("hooks are not sorted by shell: %s, %s", parsed.Hooks[0].Shell, parsed.Hooks[1].Shell)
	}
	if got := strings.Join(parsed.Options.Hooks, ","); got != "bash,pwsh" {
		t.Errorf("option hooks = %s, want bash,pwsh (sorted, unique)", got)
	}
	if string(parsed.Hooks[0].Inserted) != "\nline <&> # sereno:hook\n" {
		t.Errorf("inserted bytes did not survive: %q", parsed.Hooks[0].Inserted)
	}
	record, ok := parsed.Hook("pwsh")
	if !ok || !record.FileCreated || record.Path != home.PwshProfile {
		t.Errorf("Hook(pwsh) = %+v, %v", record, ok)
	}
	if _, ok := parsed.Hook("zsh"); ok {
		t.Error("Hook found a shell that has no record")
	}
}

func TestManifestEncodingFormat(t *testing.T) {
	home := testenv.New(t)
	data, err := sampleManifest(home).Encode()
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "\r") || !strings.HasSuffix(text, "}\n") || strings.HasSuffix(text, "\n\n") {
		t.Error("the manifest must use LF and end with exactly one newline")
	}
	if !strings.HasPrefix(text, "{\n  \"dirs\": [\n    \"") {
		t.Errorf("the manifest is not indented with two spaces: %q", text[:40])
	}
	inserted := base64.StdEncoding.EncodeToString([]byte("\nline <&> # sereno:hook\n"))
	if !strings.Contains(text, `"inserted": "`+inserted+`"`) {
		t.Errorf("inserted is not the standard base64 of the bytes:\n%s", text)
	}
	if !strings.Contains(text, "R&D <team>") {
		t.Errorf("a path with & and <> is escaped, which makes the manifest hard to read:\n%s", text)
	}
	for _, word := range []string{"time", "date", "stamp"} {
		if strings.Contains(strings.ToLower(text), word) {
			t.Errorf("the manifest mentions %q; it must carry no timestamp", word)
		}
	}
	assertSortedKeys(t, data)
}

// assertSortedKeys decodes JSON generically and checks that the keys of every
// object appear in sorted order in the encoded bytes.
func assertSortedKeys(t *testing.T, data []byte) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func(path string)
	walk = func(path string) {
		token, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return
		}
		switch delim {
		case '{':
			var keys []string
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					t.Fatal(err)
				}
				key := keyToken.(string)
				keys = append(keys, key)
				walk(path + "." + key)
			}
			if !sort.StringsAreSorted(keys) {
				t.Errorf("keys of %s are not sorted: %v", path, keys)
			}
		case '[':
			for dec.More() {
				walk(path + "[]")
			}
		}
		if _, err := dec.Token(); err != nil { // the closing delimiter
			t.Fatal(err)
		}
	}
	walk("manifest")
}

// TestEmptyManifestHasNoNulls checks the canonical form of a fresh manifest:
// empty lists and objects, never null.
func TestEmptyManifestHasNoNulls(t *testing.T) {
	data, err := NewManifest().Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "dirs": [],
  "hooks": [],
  "options": {
    "hooks": [],
    "profile": true,
    "shell": "auto"
  },
  "owned": {},
  "schema": 1,
  "version": ""
}
`
	if string(data) != want {
		t.Errorf("empty manifest:\n%s\nwant:\n%s", data, want)
	}
	withHook := NewManifest()
	withHook.Hooks = []HookRecord{{Shell: "bash", Path: "/x"}}
	data, err = withHook.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("a hook record with unset lists encodes null:\n%s", data)
	}
}

func TestParseManifestRejects(t *testing.T) {
	home := testenv.New(t)
	valid, err := sampleManifest(home).Encode()
	if err != nil {
		t.Fatal(err)
	}
	text := string(valid)
	tests := map[string]string{
		"not json":            "manifest",
		"empty":               "",
		"another schema":      strings.Replace(text, `"schema": 1`, `"schema": 2`, 1),
		"no schema":           strings.Replace(text, `  "schema": 1,`+"\n", "", 1),
		"unknown field":       strings.Replace(text, `"schema": 1`, `"schema": 1, "installedAt": "2026-10-07"`, 1),
		"trailing data":       text + "{}",
		"relative owned":      strings.Replace(text, `"owned": {`, `"owned": {"shell/init.ps1": "00",`, 1),
		"hook without a path": strings.Replace(text, `"shell": "bash"`, `"shell": "bash"}, {"shell": "zsh", "path": "relative"`, 1),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if input == text {
				t.Fatal("the edit did not change the manifest")
			}
			if _, err := ParseManifest([]byte(input)); err == nil {
				t.Errorf("ParseManifest accepted:\n%s", input)
			}
		})
	}
}

func TestLoadManifest(t *testing.T) {
	home := testenv.New(t)
	root := filepath.Join(home.Dir, "root")

	m, found, err := LoadManifest(root)
	if err != nil || found {
		t.Fatalf("missing manifest: found = %v, err = %v", found, err)
	}
	if !m.Options.Profile || m.Options.Shell != "auto" || len(m.Options.Hooks) != 0 || m.Schema != Schema {
		t.Errorf("a missing manifest must yield first-apply options, got %+v", m.Options)
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := sampleManifest(home).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	m, found, err = LoadManifest(root)
	if err != nil || !found || m.Version != "0.1.0" || len(m.Owned) != 2 {
		t.Errorf("LoadManifest = %+v, %v, %v", m, found, err)
	}

	if err := os.WriteFile(filepath.Join(root, ManifestName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadManifest(root); err == nil {
		t.Error("a corrupt manifest was accepted")
	}
}

// TestCloneIsDeep makes sure planning cannot change the manifest it read.
func TestCloneIsDeep(t *testing.T) {
	home := testenv.New(t)
	original := sampleManifest(home)
	before, err := original.Encode()
	if err != nil {
		t.Fatal(err)
	}
	clone := original.clone()
	clone.Dirs = append(clone.Dirs, "x")
	clone.Owned["x"] = "y"
	clone.Hooks[0].Inserted[0] = 'X'
	clone.Hooks[0].DirsCreated = append(clone.Hooks[0].DirsCreated, "x")
	clone.Options.Hooks = append(clone.Options.Hooks, "zsh")
	clone.removeHook("pwsh")
	clone.addDir("y")
	after, err := original.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("changing a clone changed the original")
	}
	if _, ok := clone.Hook("pwsh"); ok || strings.Contains(strings.Join(clone.Options.Hooks, ","), "pwsh") {
		t.Error("removeHook left the record or the option behind")
	}
}
