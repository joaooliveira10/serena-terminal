//go:build windows

package mode

import (
	"strings"
	"testing"
)

// These tests read the real registry of the machine and never write it, which
// is the one exception test-plan rule T1 makes.

func TestRegistryReadsTheWindowColour(t *testing.T) {
	value, ok := currentUser{}.String(keyColors, "Window")
	if !ok {
		t.Skip("Control Panel\\Colors\\Window is not readable on this machine")
	}
	if len(strings.Fields(value)) != 3 {
		t.Errorf("Window = %q, want three numbers", value)
	}
	if strings.ContainsRune(value, 0) {
		t.Errorf("Window = %q still carries its terminator", value)
	}
}

func TestRegistryMissingValues(t *testing.T) {
	reg := currentUser{}
	if _, ok := reg.String(`Software\sereno-test\no-such-key`, "value"); ok {
		t.Error("a missing key was read as a string")
	}
	if _, ok := reg.Integer(`Software\sereno-test\no-such-key`, "value"); ok {
		t.Error("a missing key was read as a number")
	}
	if _, ok := reg.String(keyColors, "sereno-no-such-value"); ok {
		t.Error("a missing value was read")
	}
	// A value of the other type is not converted.
	if _, ok := reg.Integer(keyColors, "Window"); ok {
		t.Error("a text value was read as a number")
	}
}

func TestSystemReaderOnWindows(t *testing.T) {
	signals, known := System().Signals()
	if !known {
		t.Fatal("Windows must report known signals")
	}
	if signals != FromRegistry(currentUser{}) {
		t.Error("System disagrees with FromRegistry over the same registry")
	}
}
