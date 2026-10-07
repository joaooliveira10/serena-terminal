// Package policy holds no code. Its tests enforce the hard limits of the
// specification (R1, R2, R5, R6) on the source tree itself: which packages may
// be imported, where files may be written from, where hex colours may appear,
// and that every message key used in the code exists in the catalogues.
//
// The tests read the repository with go/parser; they start no process.
package policy
