//go:build darwin || linux

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/pilgrim13/rulesend/internal/core"
)

type previewFileObservation struct {
	Mode    os.FileMode
	ModTime int64
	Hash    string
}

type previewObservation struct {
	Files  map[string]previewFileObservation
	Config string
	State  string
}

func previewTestAgent(t *testing.T) (*Agent, Target) {
	t.Helper()
	a, target := applyTestAgent(t)
	a.Config.DeviceID = "preview-device"
	a.Config.DeviceToken = strings.Repeat("a", 64) // Synthetic fixture, never printed.
	a.Config.Targets = []Target{target}
	a.State.LastContact = "2026-01-01T00:00:00Z"
	a.State.Targets[target.ID] = TargetState{LastAttempt: "2026-01-01T00:00:00Z", LastTrigger: 7, PolicyInitialized: true}
	if err := a.saveConfig(); err != nil {
		t.Fatal(err)
	}
	if err := a.saveState(); err != nil {
		t.Fatal(err)
	}
	return a, target
}

// Observe content, modes, timestamps, directory entries and in-memory values.
// Reads may update access times; access times are deliberately excluded.
func previewObserve(t *testing.T, a *Agent) previewObservation {
	t.Helper()
	base := filepath.Dir(a.Dir)
	observed := previewObservation{Files: map[string]previewFileObservation{}}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		entry := previewFileObservation{Mode: info.Mode(), ModTime: info.ModTime().UnixNano()}
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry.Hash = core.Hash(content)
		} else if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry.Hash = core.Hash([]byte(link))
		}
		observed.Files[rel] = entry
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(a.State)
	if err != nil {
		t.Fatal(err)
	}
	observed.Config, observed.State = string(config), string(state)
	return observed
}

func previewAssertUnchanged(t *testing.T, a *Agent, before previewObservation) {
	t.Helper()
	if !reflect.DeepEqual(before, previewObserve(t, a)) {
		t.Fatal("preview changed local file content, metadata, directory entries, configuration, or Agent state")
	}
}

func previewPlan(release *core.Release) core.Plan {
	return core.Plan{Pack: core.Pack{ID: "rules", Mode: "auto", IntervalSeconds: 60, Trigger: 9}, Release: release}
}

func TestPreviewReadyPredictsActualApplyWithoutLocalWrites(t *testing.T) {
	a, target := previewTestAgent(t)
	original := []byte("# Local rules\r\nPreserve whitespace  ")
	if err := os.WriteFile(target.Path, original, 0640); err != nil {
		t.Fatal(err)
	}
	release := fsTestRelease("rules", 1, "A synthetic published rule.\n")
	before := previewObserve(t, a)
	result := a.previewTarget(target, previewPlan(&release), true)
	previewAssertUnchanged(t, a, before)
	if result.Status != "ready" || result.CreatesFile || result.BeforeHash != core.Hash(original) || result.AfterHash == "" {
		t.Fatal("preview did not describe the ready existing-file change")
	}
	if result.RecordedVersion != 0 || result.PublishedVersion != 1 || !strings.Contains(result.Diff, "+A synthetic published rule.") {
		t.Fatal("preview omitted expected version information or added rule")
	}
	if _, err := a.apply(target, release); err != nil {
		t.Fatal(err)
	}
	actual := applyTestRead(t, target.Path)
	if core.Hash(actual) != result.AfterHash || !bytes.HasPrefix(actual, original) {
		t.Fatal("preview did not predict the subsequently applied whole-file content")
	}
}

func TestPreviewMissingFileIsOnlyProposed(t *testing.T) {
	a, target := previewTestAgent(t)
	release := fsTestRelease("rules", 1, "new file content")
	before := previewObserve(t, a)
	result := a.previewTarget(target, previewPlan(&release), true)
	previewAssertUnchanged(t, a, before)
	if result.Status != "ready" || !result.CreatesFile || result.BeforeHash != core.Hash(nil) || result.AfterHash == "" {
		t.Fatal("missing file preview did not describe a proposed creation")
	}
	if _, err := os.Stat(target.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview created the proposed target")
	}
}

func TestPreviewCurrentAndPausedDoNotPersistObservations(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "paused change"}[paused], func(t *testing.T) {
			a, target := previewTestAgent(t)
			first := fsTestRelease("rules", 1, "current content")
			if _, err := a.apply(target, first); err != nil {
				t.Fatal(err)
			}
			release := first
			if paused {
				release = fsTestRelease("rules", 2, "future paused content")
			}
			plan := previewPlan(&release)
			plan.Pack.Paused = paused
			before := previewObserve(t, a)
			result := a.previewTarget(target, plan, true)
			previewAssertUnchanged(t, a, before)
			if paused {
				if result.Status != "paused" || result.RecordedVersion != 1 || result.PublishedVersion != 2 || result.Diff == "" {
					t.Fatal("paused change was not reviewable without application")
				}
			} else if result.Status != "current" || result.Diff != "" || result.BeforeHash != result.AfterHash {
				t.Fatal("unchanged file was not reported as current")
			}
		})
	}
}

func TestPreviewPendingDoesNotInventAnInstallation(t *testing.T) {
	a, target := previewTestAgent(t)
	before := previewObserve(t, a)
	result := a.previewTarget(target, previewPlan(nil), true)
	previewAssertUnchanged(t, a, before)
	if result.Status != "pending" || result.PublishedVersion != 0 || result.AfterHash != "" || result.Diff != "" || result.CreatesFile {
		t.Fatal("unpublished pack produced an installation proposal")
	}
}

func TestPreviewPreservesLocalConflicts(t *testing.T) {
	for _, scenario := range []string{"edited body", "removed block", "malformed marker", "untracked valid block"} {
		t.Run(scenario, func(t *testing.T) {
			a, target := previewTestAgent(t)
			first := fsTestRelease("rules", 1, "accepted body")
			if _, err := a.apply(target, first); err != nil {
				t.Fatal(err)
			}
			local := applyTestRead(t, target.Path)
			switch scenario {
			case "edited body":
				local = bytes.Replace(local, []byte("accepted body"), []byte("preserve my local body"), 1)
			case "removed block":
				local = []byte("preserve remaining local rules")
			case "malformed marker":
				local = bytes.Replace(local, []byte("<!-- rulesend:end -->"), []byte("<!-- rulesend:broken -->"), 1)
			case "untracked valid block":
				a.State.Targets[target.ID] = TargetState{}
			}
			if err := os.WriteFile(target.Path, local, 0600); err != nil {
				t.Fatal(err)
			}
			before := previewObserve(t, a)
			next := fsTestRelease("rules", 2, "new remote content")
			result := a.previewTarget(target, previewPlan(&next), true)
			previewAssertUnchanged(t, a, before)
			if result.Status != "conflict" || result.AfterHash != "" || result.Diff != "" || result.CreatesFile {
				t.Fatal("local conflict produced a proposed replacement")
			}
		})
	}
}

func TestPreviewNeverReconcilesOrDeletesRecoveryJournal(t *testing.T) {
	for _, scenario := range []string{"valid post-image", "malformed journal"} {
		t.Run(scenario, func(t *testing.T) {
			a, target := previewTestAgent(t)
			release := fsTestRelease("rules", 1, "already replaced but unrecorded")
			post := append(block(release), '\n')
			if err := os.WriteFile(target.Path, post, 0600); err != nil {
				t.Fatal(err)
			}
			journal := Journal{TargetID: target.ID, PreHash: core.Hash(nil), PostHash: core.Hash(post), Next: fsTestState(t, release)}
			if err := writeJSON(a.journalPath(target), journal); err != nil {
				t.Fatal(err)
			}
			if scenario == "malformed journal" {
				if err := os.WriteFile(a.journalPath(target), []byte("preserve incomplete recovery evidence"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := previewObserve(t, a)
			result := a.previewTarget(target, previewPlan(&release), true)
			previewAssertUnchanged(t, a, before)
			if result.Status != "blocked" || !strings.Contains(result.Reason, "recovery journal") || result.Diff != "" {
				t.Fatal("preview reconciled or offered application despite pending recovery")
			}
		})
	}
}

func TestPreviewBlocksUnsafePathsAndInvalidPublishedContent(t *testing.T) {
	for _, scenario := range []string{"target symlink", "missing parent", "directory", "wrong pack", "wrong hash", "zero version", "reserved content", "oversized final", "outside scope"} {
		t.Run(scenario, func(t *testing.T) {
			a, target := previewTestAgent(t)
			release := fsTestRelease("rules", 1, "synthetic published content")
			allowed := true
			switch scenario {
			case "target symlink":
				outside := filepath.Join(filepath.Dir(target.Path), "outside.md")
				if err := os.WriteFile(outside, []byte("preserve outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target.Path); err != nil {
					t.Fatal(err)
				}
			case "missing parent":
				target.Path = filepath.Join(filepath.Dir(target.Path), "unavailable", "AGENTS.md")
			case "directory":
				if err := os.Mkdir(target.Path, 0700); err != nil {
					t.Fatal(err)
				}
			case "wrong pack":
				release.PackID = "different-pack"
			case "wrong hash":
				release.Hash = strings.Repeat("0", 64)
			case "zero version":
				release.Version = 0
			case "reserved content":
				release = fsTestRelease("rules", 1, "<!-- rulesend:end -->")
			case "oversized final":
				release = fsTestRelease("rules", 1, strings.Repeat("a", 32*1024))
			case "outside scope":
				allowed = false
			}
			before := previewObserve(t, a)
			result := a.previewTarget(target, previewPlan(&release), allowed)
			previewAssertUnchanged(t, a, before)
			if result.Status != "blocked" || result.AfterHash != "" || result.Diff != "" || result.CreatesFile {
				t.Fatal("unsafe or invalid input produced a proposed write")
			}
		})
	}
}

func TestPreviewPollsOnlyAndFiltersLocalTargets(t *testing.T) {
	a, target := previewTestAgent(t)
	other := target
	other.ID, other.Path = "second-target", filepath.Join(filepath.Dir(target.Path), "second.md")
	a.Config.Targets = append(a.Config.Targets, other)
	release := fsTestRelease("rules", 1, "safe remote example")
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/api/agent/poll" || r.Header.Get("Authorization") != "Bearer "+a.Config.DeviceToken {
			t.Error("preview sent an unexpected request")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(core.PollResponse{Device: core.Device{ID: a.Config.DeviceID, PackIDs: []string{"rules"}}, Plans: []core.Plan{previewPlan(&release)}})
	}))
	defer server.Close()
	a.Config.Hub = server.URL
	if err := a.saveConfig(); err != nil {
		t.Fatal(err)
	}
	before := previewObserve(t, a)
	results, err := a.Preview(context.Background(), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	previewAssertUnchanged(t, a, before)
	if len(results) != 1 || results[0].TargetID != other.ID || results[0].Status != "ready" {
		t.Fatal("target selection was not respected")
	}
	for _, id := range []string{"unknown-target", "../invalid"} {
		if _, err := a.Preview(context.Background(), id); err == nil {
			t.Fatal("unknown target was accepted")
		}
	}
	previewAssertUnchanged(t, a, before)
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(requests, []string{"/api/agent/poll"}) {
		t.Fatal("preview sent a receipt or contacted the Hub for an unknown target")
	}
}

func TestPreviewRejectsInvalidHubIdentityAndDuplicatePlans(t *testing.T) {
	for _, scenario := range []string{"wrong identity", "revoked", "duplicate packs", "malformed release", "missing scope"} {
		t.Run(scenario, func(t *testing.T) {
			a, target := previewTestAgent(t)
			release := fsTestRelease("rules", 1, "synthetic")
			response := core.PollResponse{Device: core.Device{ID: a.Config.DeviceID}, Plans: []core.Plan{previewPlan(&release)}}
			switch scenario {
			case "wrong identity":
				response.Device.ID = "another-device"
			case "revoked":
				response.Device.Revoked = true
			case "duplicate packs":
				response.Plans = append(response.Plans, response.Plans[0])
			case "malformed release":
				release.Hash = strings.Repeat("0", 64)
			case "missing scope":
				response.Plans = nil
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			a.Config.Hub = server.URL
			before := previewObserve(t, a)
			results, err := a.Preview(context.Background(), target.ID)
			previewAssertUnchanged(t, a, before)
			if err == nil {
				t.Fatal("invalid Hub response did not fail preview")
			}
			for _, result := range results {
				if result.Diff != "" || result.AfterHash != "" {
					t.Fatal("invalid Hub response produced a proposed write")
				}
			}
			if scenario == "malformed release" || scenario == "missing scope" {
				if !errors.Is(err, ErrPreviewBlocked) || len(results) != 1 || results[0].Status != "blocked" {
					t.Fatal("target failure did not preserve its blocked result")
				}
			}
		})
	}
}

func TestPreviewEscapesTerminalControlsWithoutChangingContent(t *testing.T) {
	a, target := previewTestAgent(t)
	old := []byte("local\x1b[2J\r\t\u202e\u2066\u0085\u2028\n")
	if err := os.WriteFile(target.Path, old, 0600); err != nil {
		t.Fatal(err)
	}
	target.Label = "Synthetic\x1b[31m\u202e"
	release := fsTestRelease("rules", 1, "published\x1b[2J\r\u202e content")
	before := previewObserve(t, a)
	result := a.previewTarget(target, previewPlan(&release), true)
	previewAssertUnchanged(t, a, before)
	if result.Status != "ready" || result.Diff == "" {
		t.Fatal("control-character content was not available for safe review")
	}
	text := PreviewText([]PreviewResult{result})
	for _, r := range text {
		if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			t.Fatal("preview output contains an executable or misleading terminal control")
		}
	}
	if !strings.Contains(text, "\\u001b") || !strings.Contains(text, "\\u202e") {
		t.Fatal("control characters were silently dropped instead of visibly escaped")
	}
}
