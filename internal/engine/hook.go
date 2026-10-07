package engine

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"
	"unicode/utf16"
)

// HookMarker ends every hook line (R21). It is how sereno recognises a line
// as its own when the exact bytes it recorded are no longer in the file.
const HookMarker = "# sereno:hook"

// MaxProfileSize is the largest profile sereno edits (R22): 1 MiB.
const MaxProfileSize = 1 << 20

// Byte order marks.
var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
	bomUTF32LE = []byte{0xFF, 0xFE, 0x00, 0x00}
)

// textKind is how the text of a profile is stored.
type textKind int

const (
	// textBytes covers every encoding in which ASCII is one byte per
	// character: ASCII, UTF-8 with or without a mark, the ANSI code pages.
	textBytes textKind = iota
	// textUTF16LE is UTF-16 little endian with its byte order mark, which is
	// what Windows PowerShell 5.1 writes with Out-File.
	textUTF16LE
)

// unit is the size of one code unit in bytes.
func (k textKind) unit() int {
	if k == textUTF16LE {
		return 2
	}
	return 1
}

// encode converts ASCII text to the bytes of the encoding.
func (k textKind) encode(text string) []byte {
	if k != textUTF16LE {
		return []byte(text)
	}
	out := make([]byte, 0, 2*len(text))
	for i := 0; i < len(text); i++ {
		out = append(out, text[i], 0)
	}
	return out
}

// decode returns the text of a run of whole code units.
func (k textKind) decode(data []byte) string {
	if k != textUTF16LE {
		return string(data)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	return string(utf16.Decode(units))
}

// at returns the code unit that starts at byte offset i.
func (k textKind) at(data []byte, i int) uint16 {
	if k == textUTF16LE {
		return uint16(data[i]) | uint16(data[i+1])<<8
	}
	return uint16(data[i])
}

// markLen is the length of the byte order mark at the start of data.
func (k textKind) markLen(data []byte) int {
	switch {
	case k == textUTF16LE:
		return len(bomUTF16LE)
	case bytes.HasPrefix(data, bomUTF8):
		return len(bomUTF8)
	}
	return 0
}

// index finds sub in data at an offset that is a whole number of code units,
// or returns -1.
func (k textKind) index(data, sub []byte) int {
	if len(sub) == 0 {
		return -1
	}
	for from := 0; from <= len(data); {
		i := bytes.Index(data[from:], sub)
		if i < 0 {
			return -1
		}
		i += from
		if i%k.unit() == 0 {
			return i
		}
		from = i + 1
	}
	return -1
}

// line is one line of a profile: its text is data[start:end] and the line
// with its terminator (LF or CRLF, absent on an unterminated last line) is
// data[start:next].
type line struct{ start, end, next int }

// lines splits the text after the byte order mark.
func (k textKind) lines(data []byte) []line {
	unit := k.unit()
	var out []line
	start := k.markLen(data)
	for i := start; i+unit <= len(data); i += unit {
		if k.at(data, i) != '\n' {
			continue
		}
		end := i
		if end-unit >= start && k.at(data, end-unit) == '\r' {
			end -= unit
		}
		out = append(out, line{start, end, i + unit})
		start = i + unit
	}
	if start < len(data) {
		out = append(out, line{start, len(data), len(data)})
	}
	return out
}

// profile is what planning learns about a hook target.
type profile struct {
	exists bool
	data   []byte
	perm   fs.FileMode
	kind   textKind
}

// readProfile inspects a hook target without changing it. A non-empty Note is
// the reason the file must not be edited (R22): the caller turns it into a
// refusal that shows the line, so the user can add it by hand.
func readProfile(path string) (profile, Note) {
	info, err := os.Lstat(path)
	if isMissing(err) {
		return profile{}, Note{}
	}
	if err != nil {
		return profile{}, NewNote(ReasonUnreadable, cause(err))
	}
	switch mode := info.Mode(); {
	case mode&fs.ModeSymlink != 0:
		return profile{}, NewNote(ReasonSymlink)
	case !mode.IsRegular():
		return profile{}, NewNote(ReasonNotRegular)
	case info.Size() > MaxProfileSize:
		return profile{}, NewNote(ReasonTooLarge)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return profile{}, NewNote(ReasonUnreadable, cause(err))
	}
	if len(data) > MaxProfileSize {
		return profile{}, NewNote(ReasonTooLarge)
	}
	kind, reason := classify(data)
	if reason != "" {
		return profile{}, NewNote(reason)
	}
	return profile{exists: true, data: data, perm: info.Mode().Perm(), kind: kind}, Note{}
}

// classify decides how the text of a profile is stored, or returns the key of
// the reason it cannot be edited.
func classify(data []byte) (textKind, string) {
	switch {
	case bytes.HasPrefix(data, bomUTF16BE):
		return 0, ReasonUTF16BE
	case bytes.HasPrefix(data, bomUTF32LE), bytes.HasPrefix(data, bomUTF16LE) && len(data)%2 != 0:
		// UTF-32, or UTF-16 cut in the middle of a code unit: appending
		// UTF-16 text would damage either.
		return 0, ReasonMalformed
	case bytes.HasPrefix(data, bomUTF16LE):
		return textUTF16LE, ""
	case bytes.IndexByte(data, 0) >= 0:
		return 0, ReasonBinary
	}
	return textBytes, ""
}

// cause returns the system's own words for an I/O error, without the path
// that sereno prints itself.
func cause(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

// hookAction says what has to happen to a profile.
type hookAction int

const (
	hookKeep    hookAction = iota // the line is there, nothing to write
	hookCreate                    // the file does not exist: create it with the line
	hookAppend                    // add the line at the end
	hookReplace                   // a different sereno line is there: replace it in place
)

// hookEdit is the outcome of editHook.
type hookEdit struct {
	action   hookAction
	after    []byte // the new content of the file (not set for hookKeep)
	inserted []byte // the bytes the manifest must record from now on
}

// editHook computes the edit that puts the hook line text into a profile
// (R22). recorded holds the bytes a previous apply inserted, if any. The
// function is pure: it reads nothing and writes nothing.
func editHook(p profile, recorded []byte, text string, newFileCRLF bool) hookEdit {
	if !p.exists {
		eol := "\n"
		if newFileCRLF {
			eol = "\r\n"
		}
		content := []byte(text + eol)
		return hookEdit{action: hookCreate, after: content, inserted: content}
	}
	k, data := p.kind, p.data

	// 1. The bytes sereno recorded are still there: they are replaced as a
	// whole, so the line ending sereno added in front of them (when the file
	// did not end with one) stays accounted for.
	if i := k.index(data, recorded); i >= 0 {
		if lead, term, ok := insertionShape(k.decode(recorded)); ok {
			want := k.encode(lead + text + term)
			if bytes.Equal(want, recorded) {
				return hookEdit{action: hookKeep, inserted: recorded}
			}
			return hookEdit{action: hookReplace, after: splice(data, i, i+len(recorded), want), inserted: want}
		}
	}

	// 2. A line with the marker exists: identical means nothing to do,
	// otherwise the first one is replaced in place, keeping its terminator.
	lines := k.lines(data)
	first := -1
	for n, l := range lines {
		current := k.decode(data[l.start:l.end])
		if !strings.Contains(current, HookMarker) {
			continue
		}
		if current == text {
			return hookEdit{action: hookKeep, inserted: recorded}
		}
		if first < 0 {
			first = n
		}
	}
	if first >= 0 {
		l := lines[first]
		want := append(k.encode(text), data[l.end:l.next]...)
		return hookEdit{action: hookReplace, after: splice(data, l.start, l.next, want), inserted: want}
	}

	// 3. Append at the end: a line ending first when the text does not end
	// with one, then the line, then one line ending.
	eol := "\n"
	for _, l := range lines {
		if l.next-l.end == 2*k.unit() {
			eol = "\r\n"
			break
		}
	}
	lead := ""
	if body := data[k.markLen(data):]; len(body) > 0 && k.at(body, len(body)-k.unit()) != '\n' {
		lead = eol
	}
	inserted := k.encode(lead + text + eol)
	return hookEdit{action: hookAppend, after: splice(data, len(data), len(data), inserted), inserted: inserted}
}

// insertionShape takes apart the text sereno recorded for an insertion: an
// optional leading line ending, one line with the marker, an optional
// terminator. ok is false when the text is anything else.
func insertionShape(recorded string) (lead, term string, ok bool) {
	body := recorded
	for _, eol := range []string{"\r\n", "\n"} {
		if strings.HasPrefix(body, eol) {
			lead, body = eol, body[len(eol):]
			break
		}
	}
	for _, eol := range []string{"\r\n", "\n"} {
		if strings.HasSuffix(body, eol) {
			term, body = eol, body[:len(body)-len(eol)]
			break
		}
	}
	return lead, term, isHookLine(body)
}

// isHookLine reports whether text is exactly one line that carries the marker.
func isHookLine(text string) bool {
	return strings.Contains(text, HookMarker) && !strings.ContainsAny(text, "\r\n")
}

// insertedText returns the line inside the bytes sereno recorded for an
// insertion, whichever of the two encodings they are in.
func insertedText(inserted []byte) string {
	kind := textBytes
	if bytes.IndexByte(inserted, 0) >= 0 {
		kind = textUTF16LE
	}
	return strings.Trim(kind.decode(inserted), "\r\n")
}

// splice returns data with data[from:to] replaced by with, in a new slice.
func splice(data []byte, from, to int, with []byte) []byte {
	out := make([]byte, 0, len(data)-(to-from)+len(with))
	out = append(out, data[:from]...)
	out = append(out, with...)
	return append(out, data[to:]...)
}

// hookRemoval is the outcome of removeHook.
type hookRemoval struct {
	after   []byte   // the content of the file without the hook
	removed []string // the lines that go away, for the plan
	// restored is true when after is the backup because an older hook line
	// that sereno had replaced was put back.
	restored bool
}

// removeHook computes the content of a profile without its hook line (R39).
// If the bytes sereno recorded occur in the file, exactly those are removed;
// otherwise every line containing the marker is. When a backup is available
// and the file differs from it only where sereno's bytes stand in place of an
// older hook line, the backup's content is the result, so a line that an
// earlier installation owned comes back byte for byte.
func removeHook(p profile, recorded []byte, backup []byte, haveBackup bool) hookRemoval {
	k, data := p.kind, p.data
	if len(recorded) == 0 {
		// The line was there before sereno: it is not sereno's to remove.
		return hookRemoval{after: data}
	}
	if i := k.index(data, recorded); i >= 0 {
		prefix, suffix := data[:i], data[i+len(recorded):]
		result := hookRemoval{
			after:   splice(data, i, i+len(recorded), nil),
			removed: []string{strings.Trim(k.decode(recorded), "\r\n")},
		}
		if haveBackup && len(backup) >= len(prefix)+len(suffix) &&
			bytes.HasPrefix(backup, prefix) && bytes.HasSuffix(backup, suffix) {
			replaced := backup[len(prefix) : len(backup)-len(suffix)]
			if len(replaced)%k.unit() == 0 && isHookLine(strings.TrimSuffix(strings.TrimSuffix(k.decode(replaced), "\n"), "\r")) {
				result.after, result.restored = backup, true
			}
		}
		return result
	}
	result := hookRemoval{}
	kept := 0
	for _, l := range k.lines(data) {
		if current := k.decode(data[l.start:l.end]); strings.Contains(current, HookMarker) {
			result.after = append(result.after, data[kept:l.start]...)
			result.removed = append(result.removed, current)
			kept = l.next
		}
	}
	result.after = append(result.after, data[kept:]...)
	return result
}
