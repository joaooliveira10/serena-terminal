//go:build !windows

package engine

// patient is false where a rename or a removal does not fail because another
// program has the file open, and nothing puts files next to sereno's.
const patient = false

// transient reports whether an error is worth retrying: never, here.
func transient(error) bool { return false }
