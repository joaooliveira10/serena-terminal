package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// host builds a Host for an operating system from a map of variables.
func host(goos string, vars map[string]string) Host {
	return Host{OS: goos, Getenv: testenv.Getenv(vars)}
}

func TestRoot(t *testing.T) {
	// Any absolute directory will do: nothing is created.
	base := testenv.TempDir(t)
	local := filepath.Join(base, "AppData", "Local")
	home := filepath.Join(base, "home")
	data := filepath.Join(base, "data")
	custom := filepath.Join(base, "custom", "sereno-root")

	tests := []struct {
		name    string
		os      string
		vars    map[string]string
		want    Root
		wantErr bool
	}{
		{"windows default", Windows, map[string]string{"LOCALAPPDATA": local, "HOME": home},
			Root{filepath.Join(local, "sereno"), true}, false},
		{"windows ignores XDG_DATA_HOME", Windows, map[string]string{"LOCALAPPDATA": local, "XDG_DATA_HOME": data},
			Root{filepath.Join(local, "sereno"), true}, false},
		{"windows with SERENO_HOME", Windows, map[string]string{"LOCALAPPDATA": local, "SERENO_HOME": custom},
			Root{custom, false}, false},
		{"windows without LOCALAPPDATA", Windows, map[string]string{"HOME": home}, Root{}, true},
		{"windows with a relative LOCALAPPDATA", Windows, map[string]string{"LOCALAPPDATA": "AppData"}, Root{}, true},
		{"linux default", Linux, map[string]string{"HOME": home},
			Root{filepath.Join(home, ".local", "share", "sereno"), true}, false},
		{"macOS default", Darwin, map[string]string{"HOME": home, "LOCALAPPDATA": local},
			Root{filepath.Join(home, ".local", "share", "sereno"), true}, false},
		{"linux with XDG_DATA_HOME", Linux, map[string]string{"HOME": home, "XDG_DATA_HOME": data},
			Root{filepath.Join(data, "sereno"), false}, false},
		{"linux ignores a relative XDG_DATA_HOME", Linux, map[string]string{"HOME": home, "XDG_DATA_HOME": "data"},
			Root{filepath.Join(home, ".local", "share", "sereno"), true}, false},
		{"linux with SERENO_HOME wins over XDG_DATA_HOME", Linux, map[string]string{"HOME": home, "XDG_DATA_HOME": data, "SERENO_HOME": custom},
			Root{custom, false}, false},
		{"SERENO_HOME needs no HOME", Linux, map[string]string{"SERENO_HOME": custom}, Root{custom, false}, false},
		{"relative SERENO_HOME", Linux, map[string]string{"HOME": home, "SERENO_HOME": "sereno"}, Root{}, true},
		{"linux without HOME", Linux, map[string]string{}, Root{}, true},
		{"linux does not fall back to USERPROFILE", Linux, map[string]string{"USERPROFILE": home}, Root{}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := host(test.os, test.vars).Root()
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, want error %v", err, test.wantErr)
			}
			if got != test.want {
				t.Errorf("Root = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestHome(t *testing.T) {
	base := testenv.TempDir(t)
	home, profile := filepath.Join(base, "home"), filepath.Join(base, "profile")
	tests := []struct {
		name string
		os   string
		vars map[string]string
		want string
	}{
		{"HOME", Linux, map[string]string{"HOME": home}, home},
		{"windows prefers HOME", Windows, map[string]string{"HOME": home, "USERPROFILE": profile}, home},
		{"windows falls back to USERPROFILE", Windows, map[string]string{"USERPROFILE": profile}, profile},
		{"linux has no fallback", Linux, map[string]string{"USERPROFILE": profile}, ""},
		{"relative HOME", Darwin, map[string]string{"HOME": "home"}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := host(test.os, test.vars).Home()
			if (err != nil) != (test.want == "") || got != test.want {
				t.Errorf("Home = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

// TestLookPathWindowsRules spells the fixture files with the upper-case
// extensions of the default PATHEXT, so the test also passes where file names
// are case-sensitive.
func TestLookPathWindowsRules(t *testing.T) {
	home := testenv.New(t)
	bin := filepath.Join(home.Dir, "bin")
	other := filepath.Join(home.Dir, "other")
	for _, dir := range []string{bin, other, filepath.Join(other, "tool.EXE")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(bin, "pwsh.EXE"), filepath.Join(bin, "script.CMD"), filepath.Join(bin, "notes.txt")} {
		if err := os.WriteFile(file, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := host(Windows, map[string]string{"PATH": `relative;;"` + other + `";` + bin})

	tests := []struct {
		name string
		want string
	}{
		{"pwsh", filepath.Join(bin, "pwsh.EXE")},
		{"pwsh.EXE", filepath.Join(bin, "pwsh.EXE")},
		{"script", filepath.Join(bin, "script.CMD")},
		{"notes", ""},      // .txt is not in PATHEXT
		{"tool", ""},       // a directory named tool.exe is not a program
		{"powershell", ""}, // not there
		{"", ""},           //
		{`bin\pwsh`, ""},   // paths are not looked up
		{"./pwsh", ""},     //
		{"notes.txt", filepath.Join(bin, "notes.txt")}, // an explicit extension is taken as given
	}
	for _, test := range tests {
		got, ok := h.LookPath(test.name)
		if got != test.want || ok != (test.want != "") {
			t.Errorf("LookPath(%q) = %q, %v; want %q", test.name, got, ok, test.want)
		}
	}

	custom := host(Windows, map[string]string{"PATH": bin, "PATHEXT": ".bat; .txt"})
	if got, ok := custom.LookPath("notes"); !ok || got != filepath.Join(bin, "notes.txt") {
		t.Errorf("LookPath with PATHEXT=.bat; .txt = %q, %v", got, ok)
	}
	if _, ok := host(Windows, nil).LookPath("pwsh"); ok {
		t.Error("LookPath found a program with an empty PATH")
	}
}

func TestLookPathUnixRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("files have no executable bit on Windows")
	}
	home := testenv.New(t)
	bin := filepath.Join(home.Dir, "bin")
	if err := os.MkdirAll(filepath.Join(bin, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "pwsh"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "data"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h := host(Linux, map[string]string{"PATH": "relative::" + bin})
	if got, ok := h.LookPath("pwsh"); !ok || got != filepath.Join(bin, "pwsh") {
		t.Errorf("LookPath(pwsh) = %q, %v", got, ok)
	}
	for _, name := range []string{"data", "dir", "zsh", "pwsh.exe"} {
		if got, ok := h.LookPath(name); ok {
			t.Errorf("LookPath(%q) = %q, want nothing", name, got)
		}
	}
}

func TestUILanguage(t *testing.T) {
	id := func(value uint16) func() uint16 { return func() uint16 { return value } }
	tests := []struct {
		name string
		host Host
		want string
	}{
		{"windows pt-BR", Host{OS: Windows, UILanguageID: id(0x0416)}, Portuguese},
		{"windows pt-PT has the same primary language", Host{OS: Windows, UILanguageID: id(0x0816)}, Portuguese},
		{"windows en-US", Host{OS: Windows, UILanguageID: id(0x0409)}, ""},
		{"windows unknown", Host{OS: Windows, UILanguageID: id(0)}, ""},
		{"windows without the call", Host{OS: Windows}, ""},
		{"windows ignores LANG", Host{OS: Windows, UILanguageID: id(0x0409), Getenv: testenv.Getenv(map[string]string{"LANG": "pt_BR.UTF-8"})}, ""},
		{"LANG", host(Linux, map[string]string{"LANG": "pt_BR.UTF-8"}), Portuguese},
		{"LANG pt_PT", host(Darwin, map[string]string{"LANG": "pt_PT"}), Portuguese},
		{"LC_MESSAGES before LANG", host(Linux, map[string]string{"LC_MESSAGES": "pt_BR", "LANG": "en_US.UTF-8"}), Portuguese},
		{"LC_ALL before the rest", host(Linux, map[string]string{"LC_ALL": "en_US.UTF-8", "LC_MESSAGES": "pt_BR", "LANG": "pt_BR"}), ""},
		{"LC_ALL pt", host(Linux, map[string]string{"LC_ALL": "pt_BR.UTF-8", "LANG": "en_US"}), Portuguese},
		{"English", host(Linux, map[string]string{"LANG": "en_US.UTF-8"}), ""},
		{"C locale", host(Linux, map[string]string{"LANG": "C"}), ""},
		{"nothing set", host(Linux, nil), ""},
		{"unix ignores the Windows call", Host{OS: Linux, UILanguageID: id(0x0416)}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.host.UILanguage(); got != test.want {
				t.Errorf("UILanguage = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSession(t *testing.T) {
	const guid = "{6b2d8b49-31d3-5dea-9087-93c26e1a033e}"
	tests := []struct {
		name     string
		vars     map[string]string
		want     Session
		obstacle Obstacle
	}{
		{"plain console", nil, Session{Emulator: OtherEmulator}, NoObstacle},
		{"windows terminal", map[string]string{"WT_SESSION": "abc"},
			Session{Emulator: WindowsTerminal}, NoObstacle},
		{"serena profile by environment", map[string]string{"WT_SESSION": "abc", "SERENO_SESSION": "wt-profile"},
			Session{Emulator: WindowsTerminal, SerenaProfile: true}, NoObstacle},
		{"serena profile by GUID in upper case", map[string]string{"WT_SESSION": "abc", "WT_PROFILE_ID": "{6B2D8B49-31D3-5DEA-9087-93C26E1A033E}"},
			Session{Emulator: WindowsTerminal, SerenaProfile: true}, NoObstacle},
		{"another profile", map[string]string{"WT_SESSION": "abc", "WT_PROFILE_ID": "{2ece5bfe-50ed-5f3a-ab87-5cd4baafed2b}", "SERENO_SESSION": "other"},
			Session{Emulator: WindowsTerminal}, NoObstacle},
		{"vscode wins over an inherited WT_SESSION", map[string]string{"WT_SESSION": "abc", "TERM_PROGRAM": "vscode"},
			Session{Emulator: VSCode}, ObstacleVSCode},
		{"tmux", map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"},
			Session{Emulator: OtherEmulator, Multiplexer: true}, ObstacleMultiplexer},
		{"screen by TERM", map[string]string{"TERM": "screen-256color"},
			Session{Emulator: OtherEmulator, Multiplexer: true}, ObstacleMultiplexer},
		{"tmux by TERM inside windows terminal", map[string]string{"WT_SESSION": "abc", "TERM": "tmux-256color"},
			Session{Emulator: WindowsTerminal, Multiplexer: true}, ObstacleMultiplexer},
		{"xterm", map[string]string{"TERM": "xterm-256color"}, Session{Emulator: OtherEmulator}, NoObstacle},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := host(Windows, test.vars).Session(guid)
			if got != test.want {
				t.Errorf("Session = %+v, want %+v", got, test.want)
			}
			if got.PaintObstacle() != test.obstacle {
				t.Errorf("PaintObstacle = %q, want %q", got.PaintObstacle(), test.obstacle)
			}
		})
	}
	// Without a GUID an empty WT_PROFILE_ID must not count as a match.
	if host(Windows, nil).Session("").SerenaProfile {
		t.Error("an empty GUID matched an empty WT_PROFILE_ID")
	}
}

func TestNoColor(t *testing.T) {
	if host(Linux, nil).NoColor() || !host(Linux, map[string]string{"NO_COLOR": "1"}).NoColor() {
		t.Error("NoColor does not follow NO_COLOR")
	}
}

func TestCurrent(t *testing.T) {
	home := testenv.New(t)
	h := Current()
	if h.OS != runtime.GOOS {
		t.Errorf("OS = %q", h.OS)
	}
	root, err := h.Root()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home.DataHome, "sereno")
	if runtime.GOOS == "windows" {
		want = filepath.Join(home.LocalAppData, "sereno")
	}
	if root.Path != want {
		t.Errorf("Root = %q, want %q inside the temporary home", root.Path, want)
	}
	// Reads the real UI language on Windows; any answer is fine, it must
	// only be one of the two.
	if lang := h.UILanguage(); lang != "" && lang != Portuguese {
		t.Errorf("UILanguage = %q", lang)
	}
}

// TestIsTerminal uses the three things a test can open that are certainly not
// terminals. A real console cannot be opened from a test run.
func TestIsTerminal(t *testing.T) {
	home := testenv.New(t)
	path := filepath.Join(home.Dir, "plain.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	for name, f := range map[string]*os.File{"file": file, "null device": null, "pipe": writer} {
		if IsTerminal(f) {
			t.Errorf("IsTerminal(%s) = true", name)
		}
		restore, ok := EnableVT(f)
		if ok {
			t.Errorf("EnableVT(%s) reported a console", name)
		}
		restore()
	}
}
