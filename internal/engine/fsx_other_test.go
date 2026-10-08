//go:build !windows

package engine

// The spellings that only Windows has (drive letters, devices, short names):
// none here.

func windowsOutsideSpellings(sandbox, other string) map[string]string { return nil }

func windowsFarAway(sandbox, other string) map[string]string { return nil }
