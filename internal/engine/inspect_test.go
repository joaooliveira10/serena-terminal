package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/joaooliveira10/serena-terminal/internal/testenv"
)

// TestInspect follows an installation through the states `sereno status`
// reports (R37): applied, owned files modified or missing, hook lines
// present or not. Inspect itself must never change anything.
func TestInspect(t *testing.T) {
	s, target, req := profileSite(t, "bash", []byte("export A=1\n"))
	req.Hooks = append(req.Hooks, Hook{Shell: "powershell", Path: s.home.PSProfile, Line: psLine, NewFileCRLF: true})

	inspect := func(t *testing.T) *Inspection {
		t.Helper()
		before := testenv.Snapshot(t, s.home.Dir)
		result, err := Inspect(s.root)
		if err != nil {
			t.Fatal(err)
		}
		sameTree(t, before, s.home.Dir, "Inspect")
		return result
	}
	states := func(result *Inspection) map[string]OwnedState {
		out := map[string]OwnedState{}
		for _, file := range result.Owned {
			out[file.Path] = file.State
		}
		return out
	}
	present := func(result *Inspection) map[string]bool {
		out := map[string]bool{}
		for _, hook := range result.Hooks {
			out[hook.Shell] = hook.Present
		}
		return out
	}

	// Before any apply.
	fresh := inspect(t)
	if fresh.Applied || fresh.Manifest == nil || len(fresh.Owned) != 0 || len(fresh.Hooks) != 0 || fresh.Count(OwnedIntact) != 0 {
		t.Errorf("before apply: %+v", fresh)
	}
	if fresh.Owned == nil || fresh.Hooks == nil {
		t.Error("empty lists must not be nil: they become JSON arrays")
	}

	// Right after apply everything is as recorded.
	mustApply(t, req)
	clean := inspect(t)
	if !clean.Applied || clean.Manifest.Version != "0.1.0" {
		t.Errorf("after apply: applied %v, version %q", clean.Applied, clean.Manifest.Version)
	}
	want := map[string]OwnedState{s.init: OwnedIntact, s.mode: OwnedIntact, s.outside: OwnedIntact}
	if !reflect.DeepEqual(states(clean), want) || clean.Count(OwnedIntact) != 3 || clean.Count(OwnedModified) != 0 || clean.Count(OwnedMissing) != 0 {
		t.Errorf("owned after apply = %v", clean.Owned)
	}
	if len(clean.Owned) != 3 || clean.Owned[0].Path > clean.Owned[1].Path || clean.Owned[1].Path > clean.Owned[2].Path {
		t.Errorf("owned is not sorted by path: %v", clean.Owned)
	}
	if !reflect.DeepEqual(clean.Hooks, []HookState{
		{Shell: "bash", Path: target, Present: true},
		{Shell: "powershell", Path: s.home.PSProfile, Present: true},
	}) {
		t.Errorf("hooks after apply = %+v", clean.Hooks)
	}

	// The user edits one owned file, deletes another and makes a third a
	// directory.
	put(t, s.init, "my own init\n")
	if err := os.Remove(s.mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.outside, 0o755); err != nil {
		t.Fatal(err)
	}
	edited := inspect(t)
	want = map[string]OwnedState{s.init: OwnedModified, s.mode: OwnedMissing, s.outside: OwnedModified}
	if !reflect.DeepEqual(states(edited), want) || edited.Count(OwnedModified) != 2 || edited.Count(OwnedMissing) != 1 {
		t.Errorf("owned after the edits = %v", edited.Owned)
	}

	// Hook lines: commented out (still a line that ends with the marker),
	// deleted, and a profile that is gone altogether.
	put(t, target, "export A=1\n# "+bashLine+"\n")
	if got := present(inspect(t)); !got["bash"] || !got["powershell"] {
		t.Errorf("a commented hook line counts as present for apply and uninstall, Inspect says %v", got)
	}
	put(t, target, "export A=1\nalias h='grep \"# sereno:hook\" f'\n")
	if got := present(inspect(t)); got["bash"] {
		t.Error("a line that only mentions the marker was taken for the hook line")
	}
	if err := os.Remove(s.home.PSProfile); err != nil {
		t.Fatal(err)
	}
	if got := present(inspect(t)); got["bash"] || got["powershell"] {
		t.Errorf("hooks after the lines were removed = %v", got)
	}

	// A profile sereno would not edit is reported with the reason.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	blocked := inspect(t)
	if hook := blocked.Hooks[0]; hook.Present || hook.Problem.Key != ReasonNotRegular {
		t.Errorf("a directory in place of the profile: %+v", hook)
	}
}

// TestInspectSeesALineThatWasThereBefore: the record of a line sereno did not
// insert has no bytes; the line still counts as present.
func TestInspectSeesALineThatWasThereBefore(t *testing.T) {
	s, _, req := profileSite(t, "bash", []byte("export A=1\n"+bashLine+"\n"))
	mustApply(t, req)
	result, err := Inspect(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hooks) != 1 || !result.Hooks[0].Present {
		t.Errorf("hooks = %+v", result.Hooks)
	}
}

func TestInspectNeedsAnAbsoluteRoot(t *testing.T) {
	testenv.New(t)
	if _, err := Inspect(filepath.Join("relative", "sereno")); err == nil {
		t.Error("Inspect accepted a relative root")
	}
}
