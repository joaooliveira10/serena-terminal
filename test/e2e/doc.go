// Package e2e holds no code. Its tests are the end-to-end tree tests of test
// plan T6: they run whole command lines of sereno, in the same process,
// against fixture homes copied into a temporary directory, and compare the
// complete tree of that home before and after.
//
// The machine is injected: every test runs for a Windows host and for hosts
// of macOS and Linux, whatever system the tests themselves run on, so the
// rules for Windows paths and for POSIX paths are checked on all three.
package e2e
