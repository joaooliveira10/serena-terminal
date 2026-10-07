// Package platform answers the questions sereno asks about the machine it runs
// on: where its owned folder is, which programs are on PATH, which language
// the system speaks and what kind of terminal session it is in.
//
// Every answer is derived from a Host, which carries the operating system name
// and an environment lookup. Production code uses Current; tests build a Host
// by hand, so the rules for Windows, macOS and Linux are all tested on every
// system and no test depends on the developer's real environment.
package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Operating system names, as runtime.GOOS spells them.
const (
	Windows = "windows"
	Darwin  = "darwin"
	Linux   = "linux"
)

// Environment variables that move sereno's folders.
const (
	EnvHome     = "SERENO_HOME"   // absolute path that replaces the default ROOT (R10)
	EnvDataHome = "XDG_DATA_HOME" // base of the default ROOT on macOS and Linux
)

// Host is the machine as sereno sees it.
type Host struct {
	// OS is the operating system name in the spelling of runtime.GOOS.
	OS string
	// Getenv looks up an environment variable; empty means unset.
	Getenv func(string) string
	// UILanguageID returns the Windows user-interface language identifier
	// (GetUserDefaultUILanguage). It is consulted only when OS is "windows";
	// nil or a result of 0 means unknown.
	UILanguageID func() uint16
}

// Current returns the Host of the running process.
func Current() Host {
	return Host{OS: runtime.GOOS, Getenv: os.Getenv, UILanguageID: uiLanguageID}
}

func (h Host) env(key string) string {
	if h.Getenv == nil {
		return ""
	}
	return h.Getenv(key)
}

// IsWindows reports whether the host runs Windows.
func (h Host) IsWindows() bool { return h.OS == Windows }

// Root is the folder sereno owns (ROOT in the specification).
type Root struct {
	// Path is the absolute path of the folder.
	Path string
	// Default is false when SERENO_HOME moved the folder or, on macOS and
	// Linux, XDG_DATA_HOME did. Hook lines and the fragment then carry the
	// absolute path instead of the portable form (R18, R21).
	Default bool
}

// Root returns ROOT (R10): %LOCALAPPDATA%\sereno on Windows and
// ${XDG_DATA_HOME:-$HOME/.local/share}/sereno elsewhere, unless SERENO_HOME
// names another absolute path.
func (h Host) Root() (Root, error) {
	if home := h.env(EnvHome); home != "" {
		if !filepath.IsAbs(home) {
			return Root{}, fmt.Errorf("%s must be an absolute path, got %q", EnvHome, home)
		}
		return Root{Path: filepath.Clean(home)}, nil
	}
	if h.IsWindows() {
		local, err := h.LocalAppData()
		if err != nil {
			return Root{}, err
		}
		return Root{Path: filepath.Join(local, "sereno"), Default: true}, nil
	}
	// The XDG specification tells implementations to ignore relative values.
	if data := h.env(EnvDataHome); data != "" && filepath.IsAbs(data) {
		return Root{Path: filepath.Join(data, "sereno")}, nil
	}
	home, err := h.Home()
	if err != nil {
		return Root{}, err
	}
	return Root{Path: filepath.Join(home, ".local", "share", "sereno"), Default: true}, nil
}

// LocalAppData returns %LOCALAPPDATA%, the base of ROOT and of the Windows
// Terminal folders on Windows.
func (h Host) LocalAppData() (string, error) {
	local := h.env("LOCALAPPDATA")
	if local == "" || !filepath.IsAbs(local) {
		return "", errors.New("LOCALAPPDATA is not set to an absolute path")
	}
	return filepath.Clean(local), nil
}

// Home returns the user's home directory as the shells see it: HOME, and on
// Windows USERPROFILE when HOME is not set (Git Bash sets HOME itself, R20).
func (h Host) Home() (string, error) {
	home := h.env("HOME")
	if home == "" && h.IsWindows() {
		home = h.env("USERPROFILE")
	}
	if home == "" || !filepath.IsAbs(home) {
		return "", errors.New("the home directory is unknown: HOME is not set to an absolute path")
	}
	return filepath.Clean(home), nil
}

// LookPath searches PATH for a program and returns its full path. It does what
// the shell would do, without importing os/exec: on Windows it tries the
// extensions of PATHEXT, elsewhere it requires an executable bit. Names that
// already contain a path separator are not looked up.
func (h Host) LookPath(name string) (string, bool) {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", false
	}
	separator := ":"
	extensions := []string{""}
	if h.IsWindows() {
		separator = ";"
		extensions = h.pathExtensions(name)
	}
	for _, dir := range strings.Split(h.env("PATH"), separator) {
		dir = strings.Trim(dir, `"`)
		if dir == "" || !filepath.IsAbs(dir) {
			// An empty or relative entry means the current directory;
			// sereno never runs programs from there.
			continue
		}
		for _, ext := range extensions {
			candidate := filepath.Join(dir, name+ext)
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			if !h.IsWindows() && info.Mode().Perm()&0o111 == 0 {
				continue
			}
			return candidate, true
		}
	}
	return "", false
}

// pathExtensions returns the suffixes to try for a program name on Windows.
func (h Host) pathExtensions(name string) []string {
	if filepath.Ext(name) != "" {
		return []string{""}
	}
	list := h.env("PATHEXT")
	if list == "" {
		list = ".COM;.EXE;.BAT;.CMD"
	}
	var extensions []string
	for _, ext := range strings.Split(list, ";") {
		if ext = strings.TrimSpace(ext); strings.HasPrefix(ext, ".") {
			extensions = append(extensions, ext)
		}
	}
	return extensions
}

// Portuguese is the tag UILanguage returns for Brazilian Portuguese.
const Portuguese = "pt-BR"

// UILanguage returns "pt-BR" when the system's user interface is in
// Portuguese and "" otherwise (R5). On Windows the primary language of
// GetUserDefaultUILanguage decides (0x16 is Portuguese); elsewhere the first
// of LC_ALL, LC_MESSAGES and LANG that is set decides, as POSIX orders them.
func (h Host) UILanguage() string {
	if h.IsWindows() {
		if h.UILanguageID != nil && h.UILanguageID()&0x3ff == 0x16 {
			return Portuguese
		}
		return ""
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if value := h.env(key); value != "" {
			if strings.HasPrefix(strings.ToLower(value), "pt") {
				return Portuguese
			}
			return ""
		}
	}
	return ""
}
