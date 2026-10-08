package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilgrim13/rulesend/internal/core"
	"github.com/pilgrim13/rulesend/internal/hub"
)

func TestTwoAgentsPublishConflictPolicyAndRevocation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := hub.Open(filepath.Join(root, "hub"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler, err := hub.NewServer(store, "")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	secret, err := os.ReadFile(filepath.Join(root, "hub", "admin-token"))
	if err != nil {
		t.Fatal(err)
	}
	admin := strings.TrimSpace(string(secret))
	call := func(method, path string, in, out any) {
		t.Helper()
		b, _ := json.Marshal(in)
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+admin)
		req.Header.Set("Content-Type", "application/json")
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			data, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, data)
		}
		if out != nil {
			if e = json.NewDecoder(resp.Body).Decode(out); e != nil {
				t.Fatal(e)
			}
		}
	}
	var pack core.Pack
	call("POST", "/api/packs", map[string]string{"name": "Shared rules", "description": "Synthetic integration fixture", "content": "Keep answers concise.\n"}, &pack)
	publish := func(content string) core.Release {
		t.Helper()
		var detail struct {
			Pack core.Pack `json:"pack"`
		}
		call("GET", "/api/packs/"+pack.ID, nil, &detail)
		var updated core.Pack
		call("PUT", "/api/packs/"+pack.ID+"/draft", map[string]any{"baseRevision": detail.Pack.DraftRevision, "content": content}, &updated)
		var release core.Release
		call("POST", "/api/packs/"+pack.ID+"/publish", map[string]any{"baseRevision": updated.DraftRevision, "message": "Integration test"}, &release)
		return release
	}
	v1 := publish(pack.Content)
	newAgent := func(name string) *Agent {
		t.Helper()
		var invitation struct {
			Token string `json:"token"`
		}
		call("POST", "/api/enrollment", map[string]any{"packIds": []string{pack.ID}, "expiresMinutes": 15}, &invitation)
		_, err := Enroll(context.Background(), filepath.Join(root, name), server.URL, name, invitation.Token)
		if err != nil {
			t.Fatal(err)
		}
		a, err := Open(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(a.Close)
		return a
	}
	first, second := newAgent("first"), newAgent("second")
	path1, path2 := filepath.Join(root, "AGENTS.md"), filepath.Join(root, "CLAUDE.md")
	prefix := []byte("# Existing local rules\r\nPreserve this exact text.\r\n")
	if err = os.WriteFile(path1, prefix, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err = first.Bind(pack.ID, "codex", path1, "Project Codex"); err != nil {
		t.Fatal(err)
	}
	if _, err = second.Bind(pack.ID, "claude", path2, "Project Claude"); err != nil {
		t.Fatal(err)
	}
	for _, a := range []*Agent{first, second} {
		rs, e := a.Step(context.Background(), true)
		if e != nil || len(rs) != 1 || rs[0].Status != "applied" || rs[0].Version != v1.Version {
			t.Fatalf("initial apply: %v %+v", e, rs)
		}
	}
	b, _ := os.ReadFile(path1)
	if !bytes.HasPrefix(b, prefix) {
		t.Fatal("outside prefix changed")
	}
	info, _ := os.Stat(path1)
	if info.Mode().Perm() != 0640 {
		t.Fatal("file mode not preserved")
	}
	var snapshot core.Snapshot
	call("GET", "/api/state", nil, &snapshot)
	if len(snapshot.Receipts) != 2 {
		t.Fatalf("want two real receipts, got %d", len(snapshot.Receipts))
	}
	for _, r := range snapshot.Receipts {
		if r.AppLoaded != "unverified" || r.BehaviorVerified != "unverified" {
			t.Fatal("false application evidence")
		}
	}
	old, _ := os.ReadFile(path2)
	local := bytes.Replace(old, []byte("Keep answers concise."), []byte("A local edit that must survive."), 1)
	_ = os.WriteFile(path2, local, 0600)
	v2 := publish("Use clear examples.\n")
	if _, err = first.Step(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	rs, err := second.Step(context.Background(), true)
	if err == nil || len(rs) != 1 || rs[0].Status != "conflict" {
		t.Fatalf("expected conflict: %v %+v", err, rs)
	}
	after, _ := os.ReadFile(path2)
	if !bytes.Equal(after, local) {
		t.Fatal("conflicting edit overwritten")
	}
	call("GET", "/api/state", nil, &snapshot)
	for _, r := range snapshot.Receipts {
		if r.DeviceID == first.Config.DeviceID && r.Version != v2.Version {
			t.Fatal("missing latest receipt")
		}
	}
	call("PATCH", "/api/packs/"+pack.ID+"/policy", map[string]any{"mode": "manual", "intervalSeconds": 60, "paused": true}, nil)
	v3 := publish("Respect local project rules.\n")
	rs, err = first.Step(context.Background(), true)
	if err != nil || rs[0].Version == v3.Version || rs[0].Status != "pending" {
		t.Fatalf("pause bypassed: %v %+v", err, rs)
	}
	call("PATCH", "/api/packs/"+pack.ID+"/policy", map[string]any{"mode": "manual", "intervalSeconds": 60, "paused": false}, nil)
	rs, err = first.Step(context.Background(), false)
	if err != nil || rs[0].Version == v3.Version {
		t.Fatalf("manual auto-applied: %v %+v", err, rs)
	}
	call("POST", "/api/packs/"+pack.ID+"/sync-now", map[string]any{}, nil)
	rs, err = first.Step(context.Background(), false)
	if err != nil || rs[0].Version != v3.Version {
		t.Fatalf("manual trigger failed: %v %+v", err, rs)
	}
	// Restart with persisted schedule and binding. Repeat is idempotent.
	first.Close()
	first, err = Open(filepath.Join(root, "first"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	before, _ := os.ReadFile(path1)
	if _, err = first.Step(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path1)
	if !bytes.Equal(before, after) {
		t.Fatal("restart changed an already installed block")
	}
	call("POST", "/api/devices/"+second.Config.DeviceID+"/revoke", map[string]any{}, nil)
	if _, err = second.Step(context.Background(), true); err == nil {
		t.Fatal("revoked device retained access")
	}
	third := newAgent("third")
	if _, err = third.Bind(pack.ID, "markdown", filepath.Join(root, "RULES.md"), "Manual first install"); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/packs/"+pack.ID+"/sync-now", map[string]any{}, nil)
	rs, err = third.Step(context.Background(), false)
	if err != nil || len(rs) != 1 || rs[0].Status != "applied" || rs[0].Version != v3.Version {
		t.Fatalf("trigger between bind and first poll lost: %v %+v", err, rs)
	}
}

func TestTransportRejectsUnsafeHubURL(t *testing.T) {
	for _, s := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/path", "https://example.com?token=x", "file:///tmp/x"} {
		if _, err := HubURL(s); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
	for _, s := range []string{"http://127.0.0.1:8787", "http://[::1]:8787", "https://rules.example.com"} {
		if _, err := HubURL(s); err != nil {
			t.Error(err)
		}
	}
}

func TestTransportAcceptsAuthorizedAggregateLargerThanEightMiB(t *testing.T) {
	var fixture core.PollResponse
	content := strings.Repeat("x", core.MaxContentBytes)
	for i := 0; i < 65; i++ {
		id := fmt.Sprintf("pack-%d", i)
		fixture.Plans = append(fixture.Plans, core.Plan{Pack: core.Pack{ID: id}, Release: &core.Release{PackID: id, Version: 1, Content: content, Hash: core.Hash([]byte(content))}})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fixture)
	}))
	defer server.Close()
	var got core.PollResponse
	if err := request(context.Background(), server.URL, "", "/api/agent/poll", struct{}{}, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Plans) != 65 || got.Plans[64].Release.Content != content {
		t.Fatal("valid scoped content was truncated")
	}
}
