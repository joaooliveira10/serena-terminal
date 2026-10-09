package winterminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Settings is what sereno reads from Windows Terminal's settings.json for
// `sereno status` (R37). The file is never written.
type Settings struct {
	// Theme is the value of the top-level "theme" key when it is a name;
	// empty when the key is absent (Windows Terminal then uses "dark").
	Theme string
	// ThemePair is true when "theme" is an object that names one theme for
	// light and one for dark, which makes Windows Terminal follow Windows.
	ThemePair bool
	// DefaultsOverride lists which of colorScheme, font and environment are
	// set in profiles.defaults, in that order: they win over the fragment.
	DefaultsOverride []string

	profiles []string          // GUIDs of the entries of the profile list
	themes   map[string]string // custom theme name -> window.applicationTheme
}

// HasProfile reports whether the profile list has an entry with the GUID. For
// the Serena profile that entry is the stub Windows Terminal writes itself.
func (s Settings) HasProfile(guid string) bool {
	for _, known := range s.profiles {
		if sameGUID(known, guid) {
			return true
		}
	}
	return false
}

// FollowsSystem reports whether Windows Terminal's own choice between light
// and dark follows the Windows setting. Only then does a light/dark scheme
// pair in a profile follow the system without the in-band route.
//
// The rules for a theme of the user are those of the source code of Windows
// Terminal 1.24, which differs from its documentation page in the first one:
// a theme that names no window.applicationTheme follows Windows (the default
// of the key is the system's theme, not "dark"), and a theme that takes the
// name of a built-in one is ignored.
func (s Settings) FollowsSystem() bool {
	switch s.Theme {
	case "":
		return s.ThemePair
	case "system", "legacySystem":
		return true
	case "dark", "light", "legacyDark", "legacyLight":
		return false
	}
	application, defined := s.themes[s.Theme]
	return defined && (application == "" || application == "system")
}

// State is what sereno reads from Windows Terminal's state.json.
type State struct {
	// GeneratedProfiles are the GUIDs of the generated and fragment profiles
	// Windows Terminal has seen. A GUID listed here whose entry is missing
	// from settings.json belongs to a profile the user deleted, and Windows
	// Terminal keeps such a profile hidden.
	GeneratedProfiles []string
}

// Knows reports whether Windows Terminal has seen the profile before.
func (s State) Knows(guid string) bool {
	for _, known := range s.GeneratedProfiles {
		if sameGUID(known, guid) {
			return true
		}
	}
	return false
}

// ReadSettings reads and parses a settings.json. An error means the file is
// unreadable or not understood; for status that is a warning, never a failure.
func ReadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}
	return ParseSettings(data)
}

// ReadState reads and parses a state.json.
func ReadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	return ParseState(data)
}

// ParseSettings extracts the facts of Settings from the content of a
// settings.json. Comments and trailing commas are tolerated; keys and value
// types sereno does not know are ignored.
func ParseSettings(data []byte) (Settings, error) {
	root, err := parseObject(data)
	if err != nil {
		return Settings{}, fmt.Errorf("settings.json: %w", err)
	}
	var s Settings
	switch theme := root["theme"].(type) {
	case string:
		s.Theme = theme
	case map[string]any:
		s.ThemePair = true
	}

	// "profiles" is an object with defaults and list, or, in files written
	// by old versions, the list itself.
	var list []any
	switch profiles := root["profiles"].(type) {
	case map[string]any:
		list, _ = profiles["list"].([]any)
		if defaults, ok := profiles["defaults"].(map[string]any); ok {
			for _, key := range []string{"colorScheme", "font", "environment"} {
				if _, set := defaults[key]; set {
					s.DefaultsOverride = append(s.DefaultsOverride, key)
				}
			}
		}
	case []any:
		list = profiles
	}
	for _, entry := range list {
		if profile, ok := entry.(map[string]any); ok {
			if guid, ok := profile["guid"].(string); ok {
				s.profiles = append(s.profiles, guid)
			}
		}
	}

	themes, _ := root["themes"].([]any)
	for _, entry := range themes {
		theme, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := theme["name"].(string)
		window, _ := theme["window"].(map[string]any)
		application, _ := window["applicationTheme"].(string)
		if name != "" {
			if s.themes == nil {
				s.themes = map[string]string{}
			}
			s.themes[name] = application
		}
	}
	return s, nil
}

// ParseState extracts the facts of State from the content of a state.json.
func ParseState(data []byte) (State, error) {
	root, err := parseObject(data)
	if err != nil {
		return State{}, fmt.Errorf("state.json: %w", err)
	}
	var s State
	generated, _ := root["generatedProfiles"].([]any)
	for _, entry := range generated {
		if guid, ok := entry.(string); ok {
			s.GeneratedProfiles = append(s.GeneratedProfiles, guid)
		}
	}
	return s, nil
}

// parseObject decodes a JSON object that may carry comments and trailing
// commas, as Windows Terminal's own parser accepts.
func parseObject(data []byte) (map[string]any, error) {
	var root map[string]any
	if err := json.Unmarshal(stripJSONC(data), &root); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("the file does not hold a JSON object")
	}
	return root, nil
}

// stripJSONC turns "JSON with comments" into plain JSON: it drops a UTF-8
// byte order mark, // and /* */ comments outside strings, and commas that are
// followed only by white space before a closing bracket. Line breaks are
// kept, so error positions still point at the right line.
func stripJSONC(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	out := make([]byte, 0, len(data))
	lastSignificant := -1 // index in out of the last byte that is not white space
	inString, escaped := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
				lastSignificant = len(out) - 1
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			i-- // the loop's increment lands on the line break, which is kept
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			end := bytes.Index(data[i+2:], []byte("*/"))
			if end < 0 {
				return append(out, data[i:]...) // unterminated: let the JSON parser report it
			}
			for _, skipped := range data[i : i+2+end+2] {
				if skipped == '\n' {
					out = append(out, '\n')
				}
			}
			i += 2 + end + 1
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			out = append(out, c)
		default:
			if (c == '}' || c == ']') && lastSignificant >= 0 && out[lastSignificant] == ',' {
				out[lastSignificant] = ' '
			}
			out = append(out, c)
			lastSignificant = len(out) - 1
		}
	}
	return out
}
