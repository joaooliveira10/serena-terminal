// Package winterminal knows where Windows Terminal keeps its files and how to
// read them. sereno writes exactly one file there, the fragment (R16); the
// settings and state files are only ever read (R2, R37).
//
// Every function takes %LOCALAPPDATA% or a file path as an argument and works
// on any system, so the rules are tested everywhere; only Windows calls them
// with a real folder.
package winterminal

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Names sereno uses in Windows Terminal.
const (
	// FragmentApp is the folder of the fragment and the application name
	// its profile GUID is derived from. Windows Terminal shows it as the
	// "source" of the profile.
	FragmentApp = "SerenaTerminal"
	// FragmentFile is the one file sereno writes.
	FragmentFile = "sereno.json"
	// ProfileName is the name of the profile in the new-tab menu.
	ProfileName = "Serena"
)

// Package family folders of the Store builds, below %LOCALAPPDATA%\Packages.
const (
	stablePackage  = "Microsoft.WindowsTerminal_8wekyb3d8bbwe"
	previewPackage = "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe"
)

// fragmentNamespace is the namespace GUID Windows Terminal documents for
// profiles that come from fragments: {f65ddb7e-706b-4499-8a50-40313caf510a}.
var fragmentNamespace = [16]byte{
	0xf6, 0x5d, 0xdb, 0x7e, 0x70, 0x6b, 0x44, 0x99, 0x8a, 0x50, 0x40, 0x31, 0x3c, 0xaf, 0x51, 0x0a,
}

// FragmentGUID returns the GUID Windows Terminal assigns to a profile that a
// fragment of the given application defines without a GUID of its own, in the
// "{xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}" form. It is the documented
// derivation: a UUIDv5 of the profile name in the namespace that is itself the
// UUIDv5 of the application name in Windows Terminal's fragment namespace,
// both names encoded as UTF-16LE.
func FragmentGUID(app, profile string) string {
	guid := uuid5(uuid5(fragmentNamespace, utf16LE(app)), utf16LE(profile))
	return fmt.Sprintf("{%x-%x-%x-%x-%x}", guid[0:4], guid[4:6], guid[6:8], guid[8:10], guid[10:16])
}

// ProfileGUID returns the GUID of the Serena profile (R18).
func ProfileGUID() string { return FragmentGUID(FragmentApp, ProfileName) }

// uuid5 computes a name-based UUID with SHA-1 (RFC 4122, version 5).
func uuid5(namespace [16]byte, name []byte) [16]byte {
	hash := sha1.New()
	hash.Write(namespace[:])
	hash.Write(name)
	var guid [16]byte
	copy(guid[:], hash.Sum(nil))
	guid[6] = guid[6]&0x0f | 0x50 // version 5
	guid[8] = guid[8]&0x3f | 0x80 // RFC 4122 variant
	return guid
}

func utf16LE(text string) []byte {
	var out []byte
	for _, unit := range utf16.Encode([]rune(text)) {
		out = binary.LittleEndian.AppendUint16(out, unit)
	}
	return out
}

// Installation describes the Windows Terminal folders found below
// %LOCALAPPDATA% (R16).
type Installation struct {
	// Present is true when at least one of the three folders exists.
	Present bool
	// Dirs lists the folders that exist, in the order stable, preview,
	// unpackaged.
	Dirs []string
	// SettingsPath is settings.json in the first of those folders that has
	// one; empty when none does.
	SettingsPath string
	// StatePath is state.json next to SettingsPath; empty when missing.
	StatePath string
}

// candidateDirs returns the three folders whose existence means "Windows
// Terminal is here", in the order of R16.
func candidateDirs(localAppData string) []string {
	return []string{
		filepath.Join(localAppData, "Packages", stablePackage, "LocalState"),
		filepath.Join(localAppData, "Packages", previewPackage, "LocalState"),
		filepath.Join(localAppData, "Microsoft", "Windows Terminal"),
	}
}

// Detect looks for Windows Terminal below %LOCALAPPDATA%. It only checks that
// folders and files exist; nothing is opened.
func Detect(localAppData string) Installation {
	var found Installation
	for _, dir := range candidateDirs(localAppData) {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		found.Present = true
		found.Dirs = append(found.Dirs, dir)
		if found.SettingsPath != "" {
			continue
		}
		settings := filepath.Join(dir, "settings.json")
		if isFile(settings) {
			found.SettingsPath = settings
			if state := filepath.Join(dir, "state.json"); isFile(state) {
				found.StatePath = state
			}
		}
	}
	return found
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// FragmentDir returns the folder of sereno's fragment:
// %LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\SerenaTerminal.
func FragmentDir(localAppData string) string {
	return filepath.Join(localAppData, "Microsoft", "Windows Terminal", "Fragments", FragmentApp)
}

// FragmentPath returns the one file sereno writes for Windows Terminal (R16).
func FragmentPath(localAppData string) string {
	return filepath.Join(FragmentDir(localAppData), FragmentFile)
}

// sameGUID compares two GUID strings ignoring case and braces.
func sameGUID(a, b string) bool {
	return strings.EqualFold(strings.Trim(a, "{}"), strings.Trim(b, "{}"))
}
