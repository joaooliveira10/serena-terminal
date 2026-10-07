package platform

import "strings"

// Emulator classifies the terminal a session runs in (R37).
type Emulator string

// The emulators sereno tells apart.
const (
	WindowsTerminal Emulator = "windows-terminal"
	VSCode          Emulator = "vscode"
	OtherEmulator   Emulator = "other"
)

// Obstacle names what forbids writing the palette to the terminal (R36).
type Obstacle string

// The obstacles to in-band painting that the environment reveals. Whether
// stdout is a terminal at all is a separate question (IsTerminal).
const (
	NoObstacle          Obstacle = ""
	ObstacleVSCode      Obstacle = "vscode"      // Serena Theme owns the palette there
	ObstacleMultiplexer Obstacle = "multiplexer" // tmux or screen sits in between
)

// Session describes the terminal session, read from the environment only.
type Session struct {
	// Emulator is vscode when TERM_PROGRAM says so, else windows-terminal
	// when WT_SESSION is set, else other.
	Emulator Emulator
	// SerenaProfile is true in a session started from the Serena profile of
	// Windows Terminal: SERENO_SESSION is wt-profile, or WT_PROFILE_ID is the
	// profile's GUID (R30).
	SerenaProfile bool
	// Multiplexer is true inside tmux or screen: TMUX is set, or TERM starts
	// with "screen" or "tmux" (R36).
	Multiplexer bool
}

// Session reads the session facts from the environment. profileGUID is the
// GUID of the Serena profile (R18), compared with WT_PROFILE_ID ignoring case.
func (h Host) Session(profileGUID string) Session {
	s := Session{Emulator: OtherEmulator}
	switch {
	case h.env("TERM_PROGRAM") == "vscode":
		s.Emulator = VSCode
	case h.env("WT_SESSION") != "":
		s.Emulator = WindowsTerminal
	}
	s.SerenaProfile = h.env("SERENO_SESSION") == "wt-profile" ||
		(profileGUID != "" && strings.EqualFold(h.env("WT_PROFILE_ID"), profileGUID))
	term := h.env("TERM")
	s.Multiplexer = h.env("TMUX") != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux")
	return s
}

// PaintObstacle returns what forbids painting the palette in this session, or
// NoObstacle.
func (s Session) PaintObstacle() Obstacle {
	switch {
	case s.Emulator == VSCode:
		return ObstacleVSCode
	case s.Multiplexer:
		return ObstacleMultiplexer
	}
	return NoObstacle
}

// NoColor reports whether NO_COLOR asks for uncoloured output (R5).
func (h Host) NoColor() bool { return h.env("NO_COLOR") != "" }
