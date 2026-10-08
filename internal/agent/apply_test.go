//go:build darwin || linux

package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pilgrim13/rulesend/internal/core"
)

func applyTestAgent(t *testing.T) (*Agent, Target) {
	t.Helper()
	base := fsTestPhysicalDir(t)
	dir := filepath.Join(base, "agent-data")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := Target{ID: "target-test", PackID: "rules", Adapter: "codex", Label: "Synthetic target", Path: filepath.Join(base, "AGENTS.md")}
	a := &Agent{Dir: dir, Config: Config{PackIDs: []string{"rules"}}, State: State{Targets: map[string]TargetState{}}}
	return a, target
}

func applyTestRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestApplyPreservesUserBytesPermissionsAndBackups(t *testing.T) {
	a, target := applyTestAgent(t)
	original := []byte("# Existing user rules\r\nNo normalization  ")
	if err := os.WriteFile(target.Path, original, 0640); err != nil {
		t.Fatal(err)
	}
	first := fsTestRelease("rules", 1, "First published content")
	state, err := a.apply(target, first)
	if err != nil {
		t.Fatal(err)
	}
	firstFile := applyTestRead(t, target.Path)
	if !bytes.HasPrefix(firstFile, original) {
		t.Fatal("initial apply lost user bytes")
	}
	if state.Version != 1 || state.Hash != first.Hash || state.BlockHash == "" {
		t.Fatalf("invalid installed state: %+v", state)
	}
	st, err := os.Stat(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0640 {
		t.Fatalf("changed original permissions to %o", st.Mode().Perm())
	}
	var persisted State
	if err := readJSON(filepath.Join(a.Dir, "state.json"), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Targets[target.ID].BlockHash != state.BlockHash {
		t.Fatal("installation state was not persisted")
	}
	entries, err := os.ReadDir(filepath.Join(a.Dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("want one pre-image backup, got %d", len(entries))
	}
	backup := filepath.Join(a.Dir, "backups", entries[0].Name())
	if !bytes.Equal(applyTestRead(t, backup), original) {
		t.Fatal("backup does not contain the previous whole file")
	}
	st, err = os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("backup not private: %o", st.Mode().Perm())
	}
	if _, err := os.Stat(a.journalPath(target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("successful apply left a journal: %v", err)
	}

	// A user edit outside the owned block is legitimate and must survive updates.
	suffix := []byte("\r\n# New local preference\r\nkeep trailing spaces  ")
	withSuffix := append(append([]byte(nil), firstFile...), suffix...)
	if err := os.WriteFile(target.Path, withSuffix, 0640); err != nil {
		t.Fatal(err)
	}
	second := fsTestRelease("rules", 2, "Second published content")
	if _, err := a.apply(target, second); err != nil {
		t.Fatal(err)
	}
	updated := applyTestRead(t, target.Path)
	if !bytes.HasPrefix(updated, original) || !bytes.HasSuffix(updated, suffix) {
		t.Fatal("update changed content outside managed block")
	}
	entries, err = os.ReadDir(filepath.Join(a.Dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	beforeCount := len(entries)
	if _, err := a.apply(target, second); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(filepath.Join(a.Dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != beforeCount {
		t.Fatal("unchanged apply created another backup")
	}
}

func TestApplyConflictNeverOverwritesLocalContent(t *testing.T) {
	a, target := applyTestAgent(t)
	first := fsTestRelease("rules", 1, "published text")
	if _, err := a.apply(target, first); err != nil {
		t.Fatal(err)
	}
	local := bytes.Replace(applyTestRead(t, target.Path), []byte("published text"), []byte("local unsent text"), 1)
	if err := os.WriteFile(target.Path, local, 0600); err != nil {
		t.Fatal(err)
	}
	before := a.State.Targets[target.ID]
	if _, err := a.apply(target, fsTestRelease("rules", 2, "new remote text")); !errors.Is(err, ErrConflict) {
		t.Fatalf("wanted conflict, got %v", err)
	}
	if !bytes.Equal(applyTestRead(t, target.Path), local) {
		t.Fatal("conflict lost local content")
	}
	if a.State.Targets[target.ID].BlockHash != before.BlockHash {
		t.Fatal("conflicting content was recorded as installed")
	}
	if err := a.verify(target); !errors.Is(err, ErrConflict) {
		t.Fatalf("verification missed local drift: %v", err)
	}
}

func TestApplyRefusesSymlinkWithoutChangingDestination(t *testing.T) {
	a, target := applyTestAgent(t)
	outside := filepath.Join(filepath.Dir(target.Path), "outside.md")
	if err := os.WriteFile(outside, []byte("preserve outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.apply(target, fsTestRelease("rules", 1, "new")); err == nil {
		t.Fatal("applied through symlink")
	}
	if string(applyTestRead(t, outside)) != "preserve outside" {
		t.Fatal("changed symlink destination")
	}
	if len(a.State.Targets) != 0 {
		t.Fatal("recorded rejected target as installed")
	}
}

func TestReconcileInterruptedApply(t *testing.T) {
	for _, scenario := range []string{"before replacement", "after replacement", "concurrent local change", "new target not created"} {
		t.Run(scenario, func(t *testing.T) {
			a, target := applyTestAgent(t)
			old := []byte("# Preserve original\r\n")
			preExists := scenario != "new target not created"
			if !preExists {
				old = []byte{}
			}
			release := fsTestRelease("rules", 1, "published after recovery")
			post, err := compose(old, target, TargetState{}, release)
			if err != nil {
				t.Fatal(err)
			}
			next := fsTestState(t, release)
			j := Journal{TargetID: target.ID, PreHash: core.Hash(old), PreExists: preExists, PostHash: core.Hash(post), Backup: "synthetic-backup.md", Next: next}
			backupDir := filepath.Join(a.Dir, "backups")
			if err := os.Mkdir(backupDir, 0700); err != nil {
				t.Fatal(err)
			}
			backup := filepath.Join(backupDir, j.Backup)
			if err := writePrivate(backup, old); err != nil {
				t.Fatal(err)
			}
			if err := writeJSON(a.journalPath(target), j); err != nil {
				t.Fatal(err)
			}
			actual := old
			if scenario == "after replacement" {
				actual = post
			}
			if scenario == "concurrent local change" {
				actual = []byte("edited after interrupted replacement")
			}
			if preExists {
				if err := os.WriteFile(target.Path, actual, 0600); err != nil {
					t.Fatal(err)
				}
			}
			root, name, err := openTarget(target.Path)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			err = a.reconcile(target, root, name)
			if scenario == "concurrent local change" {
				if err == nil {
					t.Fatal("ambiguous recovery was accepted")
				}
				if _, err := os.Stat(a.journalPath(target)); err != nil {
					t.Fatal("ambiguous recovery discarded journal")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(a.journalPath(target)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("reconciled journal not cleared: %v", err)
				}
			}
			if scenario == "after replacement" {
				if a.State.Targets[target.ID].BlockHash != next.BlockHash {
					t.Fatal("exact installed post-image was not recovered")
				}
				var saved State
				if err := readJSON(filepath.Join(a.Dir, "state.json"), &saved); err != nil {
					t.Fatal(err)
				}
				if saved.Targets[target.ID].Version != release.Version {
					t.Fatal("recovered installation not persisted")
				}
			} else if a.State.Targets[target.ID].Version != 0 {
				t.Fatal("pre-image or ambiguous content falsely marked installed")
			}
			if preExists && !bytes.Equal(applyTestRead(t, target.Path), actual) {
				t.Fatal("recovery modified observed content")
			}
			if !bytes.Equal(applyTestRead(t, backup), old) {
				t.Fatal("recovery changed backup")
			}
		})
	}
}

func TestApplyRecoversAfterStatePersistenceFailure(t *testing.T) {
	a, target := applyTestAgent(t)
	old := []byte("original user text\n")
	if err := os.WriteFile(target.Path, old, 0600); err != nil {
		t.Fatal(err)
	}
	// A directory at the state-file path deterministically fails the state save
	// after replacement, without adding production-only fault-injection hooks.
	statePath := filepath.Join(a.Dir, "state.json")
	if err := os.Mkdir(statePath, 0700); err != nil {
		t.Fatal(err)
	}
	r := fsTestRelease("rules", 1, "recover this installation")
	if _, err := a.apply(target, r); err == nil {
		t.Fatal("state persistence failure reported success")
	}
	installed := applyTestRead(t, target.Path)
	if !bytes.Contains(installed, []byte(r.Content)) {
		t.Fatal("fixture did not reach post-replacement failure")
	}
	if _, err := os.Stat(a.journalPath(target)); err != nil {
		t.Fatal("failure did not preserve recovery journal")
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	restarted := &Agent{Dir: a.Dir, State: State{Targets: map[string]TargetState{}}}
	root, name, err := openTarget(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := restarted.reconcile(target, root, name); err != nil {
		t.Fatal(err)
	}
	if restarted.State.Targets[target.ID].Version != r.Version {
		t.Fatal("restart failed to recover installed version")
	}
	if !bytes.Equal(applyTestRead(t, target.Path), installed) {
		t.Fatal("recovery rewrote already verified post-image")
	}
	if _, err := os.Stat(a.journalPath(target)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal not cleaned after recovery: %v", err)
	}
}

func TestVerifyReportsRemovedBlockButAllowsOutsideChanges(t *testing.T) {
	a, target := applyTestAgent(t)
	if _, err := a.apply(target, fsTestRelease("rules", 1, "managed")); err != nil {
		t.Fatal(err)
	}
	content := append(applyTestRead(t, target.Path), []byte("\nlocal suffix")...)
	if err := os.WriteFile(target.Path, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.verify(target); err != nil {
		t.Fatalf("unrelated edits are not managed drift: %v", err)
	}
	if err := os.WriteFile(target.Path, []byte("only local content remains"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.verify(target); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed block not reported: %v", err)
	}
	if err := os.Remove(target.Path); err != nil {
		t.Fatal(err)
	}
	if err := a.verify(target); !errors.Is(err, ErrConflict) {
		t.Fatalf("removed file not reported: %v", err)
	}
}

func TestBindRejectsUnsupportedAdapterAndOutOfScopePack(t *testing.T) {
	a, target := applyTestAgent(t)
	for _, adapter := range []string{"", "unknown", "shell", "CODEX"} {
		if _, err := a.Bind("rules", adapter, target.Path, "Synthetic"); err == nil {
			t.Errorf("accepted unsupported adapter %q", adapter)
		}
	}
	if _, err := a.Bind("unauthorized", "codex", target.Path, "Synthetic"); err == nil {
		t.Fatal("accepted pack outside enrollment scope")
	}
	if len(a.Config.Targets) != 0 {
		t.Fatal("invalid Bind changed configuration")
	}
	if _, err := os.Stat(target.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Bind validation created a target file: %v", err)
	}
}

func TestVerifyParentDetectsDirectoryReplacement(t *testing.T) {
	for _, replacement := range []string{"new directory", "symlink to held directory"} {
		t.Run(replacement, func(t *testing.T) {
			base := fsTestPhysicalDir(t)
			parent := filepath.Join(base, "approved")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "AGENTS.md")
			root, _, err := openTarget(path)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := verifyParent(root, path); err != nil {
				t.Fatalf("unchanged parent rejected: %v", err)
			}
			moved := filepath.Join(base, "moved")
			if err := os.Rename(parent, moved); err != nil {
				t.Fatal(err)
			}
			if replacement == "new directory" {
				err = os.Mkdir(parent, 0700)
			} else {
				err = os.Symlink(moved, parent)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyParent(root, path); err == nil {
				t.Fatal("held directory is no longer the approved pathname, but verification passed")
			}
		})
	}
}
