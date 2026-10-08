package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/joaooliveira10/serena-terminal/internal/engine"
	"github.com/joaooliveira10/serena-terminal/internal/mode"
	"github.com/joaooliveira10/serena-terminal/internal/palette"
	"github.com/joaooliveira10/serena-terminal/internal/platform"
	"github.com/joaooliveira10/serena-terminal/internal/render"
	"github.com/joaooliveira10/serena-terminal/internal/target/shells"
	"github.com/joaooliveira10/serena-terminal/internal/target/winterminal"
)

// site is the installation a command works on: sereno's folder, what the
// manifest records about it and the settings of the mode file.
type site struct {
	root     platform.Root
	manifest *engine.Manifest
	// applied is true when ROOT has a manifest: after an apply and before an
	// uninstall.
	applied  bool
	modePath string
	// settings is the content of the mode file, or the defaults of R11 when
	// the file is missing or malformed, which is how the shells read it too.
	settings mode.Settings
}

// site finds ROOT and reads the manifest and the mode file. It writes
// nothing. When that fails, the reason has been printed and ok is false: the
// command ends with exit code 1.
func (r *run) site() (s *site, ok bool) {
	root, err := r.env.Host.Root()
	if err != nil {
		r.warn(Program + ": " + r.T("error.root", err))
		return nil, false
	}
	manifest, applied, err := engine.LoadManifest(root.Path)
	if err != nil {
		r.warn(Program + ": " + r.T("error.manifest", filepath.Join(root.Path, engine.ManifestName), err))
		var schema *engine.SchemaError
		if errors.As(err, &schema) && schema.Got > engine.Schema {
			// Not a damaged file: restoring or deleting it is the wrong advice.
			r.warn(r.T("error.manifest.newer"))
		} else {
			r.warn(r.T("error.manifest.next"))
		}
		return nil, false
	}
	s = &site{root: root, manifest: manifest, applied: applied, modePath: filepath.Join(root.Path, engine.ModeName)}
	s.settings = readSettings(s.modePath)
	return s, true
}

// readSettings reads a mode file; anything but a well-formed one means the
// defaults (R11).
func readSettings(path string) mode.Settings {
	data, err := os.ReadFile(path)
	if err != nil {
		return mode.Defaults()
	}
	settings, err := mode.Parse(data)
	if err != nil {
		return mode.Defaults()
	}
	return settings
}

// samePath compares two paths the way the host's file system does: without
// regard to letter case on Windows.
func (r *run) samePath(a, b string) bool {
	if r.env.Host.IsWindows() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// owns reports whether the manifest records a path, and under which
// spelling.
func (r *run) owns(manifest *engine.Manifest, path string) (string, bool) {
	for recorded := range manifest.Owned {
		if r.samePath(recorded, path) {
			return recorded, true
		}
	}
	return "", false
}

// profileInstalled reports whether the fragment sereno installed carries the
// Serena profile. The file decides, not the option the last apply recorded:
// after an apply that could not replace the fragment the two differ, and what
// Windows Terminal shows is the file. The option stands in only when the
// file cannot be read as a fragment.
func (r *run) profileInstalled(s *site, fragmentPath string) bool {
	if data, err := os.ReadFile(fragmentPath); err == nil {
		var fragment struct {
			Profiles []json.RawMessage `json:"profiles"`
		}
		if json.Unmarshal(data, &fragment) == nil {
			return len(fragment.Profiles) > 0
		}
	}
	return s.manifest.Options.Profile
}

// terminalSite is what a command knows about Windows Terminal on this host.
type terminalSite struct {
	// fragmentPath is the one file sereno writes for Windows Terminal (R16).
	fragmentPath string
	// installation says whether Windows Terminal is there and where its
	// settings are.
	installation winterminal.Installation
}

// windowsTerminal looks for Windows Terminal (R16). ok is false on macOS and
// Linux, where there is none to look for.
func (r *run) windowsTerminal() (t terminalSite, ok bool, err error) {
	if !r.env.Host.IsWindows() {
		return terminalSite{}, false, nil
	}
	local, err := r.env.Host.LocalAppData()
	if err != nil {
		return terminalSite{}, false, err
	}
	return terminalSite{fragmentPath: winterminal.FragmentPath(local), installation: winterminal.Detect(local)}, true, nil
}

// profileShell resolves `--shell` (R18): powershell and pwsh as named, auto to
// PowerShell 7 when pwsh.exe is on PATH at this moment and to Windows
// PowerShell 5.1 otherwise. found is false when PowerShell 7 was named and
// is not there.
func (r *run) profileShell(option string) (shell shells.Shell, found bool) {
	_, onPath := r.env.Host.LookPath(shells.Pwsh.Executable(r.env.Host))
	switch {
	case option == string(shells.Pwsh):
		return shells.Pwsh, onPath
	case option == "auto" && onPath:
		return shells.Pwsh, true
	}
	return shells.PowerShell, true
}

// renderFragment renders the fragment and turns the one mistake that is the
// user's to solve into a refusal: a folder of sereno's that the command line
// of the Serena profile cannot name (R18).
func (r *run) renderFragment(pal *palette.Palette, options render.FragmentOptions) ([]byte, *engine.Refusal, error) {
	data, err := render.Fragment(pal, options)
	if errors.Is(err, render.ErrRootInCommandLine) {
		return nil, &engine.Refusal{
			Path:   options.Root.Path,
			Reason: engine.NewNote("refusal.root-in-command-line"),
			Advice: engine.NewNote("refusal.advice.no-profile"),
		}, nil
	}
	return data, nil, err
}
