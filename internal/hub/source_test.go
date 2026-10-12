package hub

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/pilgrim13/rulesend/internal/core"
)

func sourceFixture(t *testing.T) (*fixture, core.Pack, SourceConfig, string) {
	t.Helper()
	f := setup(t)
	p := f.pack("# Reviewed source\n")
	release := f.publish(p)
	config := SourceConfig{Label: "Reviewed Git source", InitialProof: core.SourceProof{
		Commit: strings.Repeat("a", 40), ReleaseID: "common-v1", Version: "v1", Hash: release.Hash,
	}}
	h, err := NewServerWithSources(f.store, "", map[string]SourceConfig{p.ID: config})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = h
	data, err := os.ReadFile(f.store.SourceTokenPath())
	if err != nil {
		t.Fatal("publisher credential unavailable")
	}
	p, err = loadPack(f.store.db, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f, p, config, strings.TrimSpace(string(data))
}

func sourceDraft(t *testing.T, f *fixture, p core.Pack, content string) core.Pack {
	t.Helper()
	w := f.request("PUT", "/api/packs/"+p.ID+"/draft", f.token, "", map[string]any{"baseRevision": p.DraftRevision, "content": content}, nil)
	status(t, w, 200)
	return result[core.Pack](t, w)
}

func sourceQueue(t *testing.T, f *fixture, p core.Pack) core.SourcePublication {
	t.Helper()
	w := f.request("POST", "/api/packs/"+p.ID+"/publish", f.token, "", map[string]any{"baseRevision": p.DraftRevision, "message": "reviewed publication"}, nil)
	status(t, w, 202)
	return result[struct {
		Publication core.SourcePublication `json:"publication"`
	}](t, w).Publication
}

func successor(job core.SourcePublication) core.SourceProof {
	return core.SourceProof{Commit: strings.Repeat("b", 40), ReleaseID: "common-v2", Version: "v2", Hash: job.Hash}
}

func TestSourcePublicationFreezesApprovalPreservesLaterDraftAndPublishesProof(t *testing.T) {
	f, p, config, publisher := sourceFixture(t)
	p = sourceDraft(t, f, p, "# Approved new body\n")
	job := sourceQueue(t, f, p)
	if job.BaseSourceProof != config.InitialProof || job.Hash != core.Hash([]byte(p.Content)) || job.BasePublishedVersion != 1 {
		t.Fatal("approval did not freeze the source and prior release")
	}
	// Queueing must not publish or deliver anything yet.
	before, _ := loadPack(f.store.db, p.ID)
	if before.PublishedVersion != 1 || before.Source.Pending != job.ID {
		t.Fatal("queue incorrectly advanced release")
	}
	later := sourceDraft(t, f, before, "# Later unrelated draft\n")
	proof := successor(job)
	w := f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": proof, "parentCommit": config.InitialProof.Commit}, nil)
	status(t, w, 200)
	release := result[struct {
		Release core.Release `json:"release"`
	}](t, w).Release
	if release.Content != job.Content || release.Version != 2 || release.Source == nil || *release.Source != proof {
		t.Fatal("release differs from the approved frozen body or proof")
	}
	after, _ := loadPack(f.store.db, p.ID)
	if after.Content != later.Content || after.DraftRevision != later.DraftRevision+1 || after.Source.Proof != proof || after.Source.Pending != "" {
		t.Fatal("completion replaced newer text or lost source ownership")
	}
	snapshot := result[core.Snapshot](t, f.request("GET", "/api/state", f.token, "", nil, nil))
	if len(snapshot.Publications) != 1 || snapshot.Publications[0].Content != "" || snapshot.Publications[0].Status != "completed" {
		t.Fatal("dashboard publication metadata is incorrect")
	}
	_, deviceToken := f.enroll([]string{p.ID})
	poll := result[core.PollResponse](t, f.request("POST", "/api/agent/poll", deviceToken, "", map[string]any{}, nil))
	if len(poll.Plans) != 1 || poll.Plans[0].Release.Source == nil || *poll.Plans[0].Release.Source != proof {
		t.Fatal("scoped device did not receive the immutable release proof")
	}
	plan := poll.Plans[0]
	if plan.Pack.PublishedVersion != release.Version || plan.Pack.PublishedHash != release.Hash {
		t.Fatal("scoped published identity differs from the immutable release")
	}
	if plan.Pack.Content != "" || plan.Pack.DraftRevision != 0 || plan.Pack.Source != nil {
		t.Fatal("scoped poll revealed the unpublished draft or source queue")
	}
}

func TestSourcePublisherRoleIsSeparateAndCannotBeUsedByBrowser(t *testing.T) {
	f, p, _, publisher := sourceFixture(t)
	job := sourceQueue(t, f, p)
	_, deviceToken := f.enroll([]string{p.ID})
	login := f.request("POST", "/api/login", "", "http://127.0.0.1:8080", map[string]any{"token": f.token}, nil)
	status(t, login, 200)
	cookie := login.Result().Cookies()[0]
	for name, token := range map[string]string{"none": "", "admin": f.token, "device": deviceToken} {
		t.Run(name, func(t *testing.T) { status(t, f.request("GET", "/api/source/jobs", token, "", nil, nil), 401) })
	}
	status(t, f.request("GET", "/api/source/jobs", "", "", nil, cookie), 401)
	status(t, f.request("GET", "/api/source/jobs", publisher, "http://127.0.0.1:8080", nil, nil), 401)
	status(t, f.request("GET", "/api/source/jobs", publisher, "https://foreign.invalid", nil, nil), 401)
	status(t, f.request("GET", "/api/state", publisher, "", nil, nil), 401)
	status(t, f.request("POST", "/api/agent/poll", publisher, "", map[string]any{}, nil), 401)
	w := f.request("GET", "/api/source/jobs", publisher, "", nil, nil)
	status(t, w, 200)
	jobs := result[struct {
		Jobs []core.SourcePublication `json:"jobs"`
	}](t, w).Jobs
	if len(jobs) != 1 || jobs[0].Content != job.Content {
		t.Fatal("publisher did not receive its frozen work")
	}
	if strings.Contains(w.Body.String(), publisher) || strings.Contains(w.Body.String(), f.token) {
		t.Fatal("credential in publisher response")
	}
	// A cookie can queue an approved draft; it cannot complete a source release.
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/complete", "", "http://127.0.0.1:8080", map[string]any{"proof": successor(job), "parentCommit": job.BaseSourceProof.Commit}, cookie), 401)
}

func TestSourceQueueConcurrencyAndCompletionIdempotence(t *testing.T) {
	f, p, _, publisher := sourceFixture(t)
	p = sourceDraft(t, f, p, "# Concurrent approval\n")
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted, conflicts := 0, 0
	var job core.SourcePublication
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := f.request("POST", "/api/packs/"+p.ID+"/publish", f.token, "", map[string]any{"baseRevision": p.DraftRevision}, nil)
			mu.Lock()
			defer mu.Unlock()
			switch w.Code {
			case 202:
				accepted++
				job = result[struct {
					Publication core.SourcePublication `json:"publication"`
				}](t, w).Publication
			case 409:
				conflicts++
			default:
				t.Error("unexpected concurrent queue result")
			}
		}()
	}
	wg.Wait()
	if accepted != 1 || conflicts != 11 {
		t.Fatal("more than one approval was queued")
	}
	proof := successor(job)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": proof, "parentCommit": job.BaseSourceProof.Commit}, nil)
			if w.Code != 200 {
				t.Error("exact completion retry failed")
			}
		}()
	}
	wg.Wait()
	after, _ := loadPack(f.store.db, p.ID)
	releases, _ := f.store.releaseList(p.ID)
	if after.PublishedVersion != 2 || len(releases) != 2 {
		t.Fatal("completion retry created extra releases")
	}
	wrong := proof
	wrong.Commit = strings.Repeat("c", 40)
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": wrong, "parentCommit": job.BaseSourceProof.Commit}, nil), 409)
}

func TestSourceProofValidationAndTerminalRejection(t *testing.T) {
	f, p, config, publisher := sourceFixture(t)
	p = sourceDraft(t, f, p, "# Rejected proposal\n")
	job := sourceQueue(t, f, p)
	proof := successor(job)
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": proof, "parentCommit": strings.Repeat("c", 40)}, nil), 409)
	wrongHash := proof
	wrongHash.Hash = config.InitialProof.Hash
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": wrongHash, "parentCommit": config.InitialProof.Commit}, nil), 409)
	bad := proof
	bad.Commit = "not-a-git-pin"
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": bad, "parentCommit": config.InitialProof.Commit}, nil), 400)
	secretReason := "private-contents-must-not-be-reflected"
	w := f.request("POST", "/api/source/jobs/"+job.ID+"/fail", publisher, "", map[string]any{"reason": secretReason}, nil)
	status(t, w, 400)
	if strings.Contains(w.Body.String(), secretReason) {
		t.Fatal("failure echoed private input")
	}
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/fail", publisher, "", map[string]any{"reason": "source_publish_failed"}, nil), 200)
	status(t, f.request("POST", "/api/packs/"+p.ID+"/publish", f.token, "", map[string]any{"baseRevision": p.DraftRevision}, nil), 409)
	jobs := result[struct {
		Jobs []core.SourcePublication `json:"jobs"`
	}](t, f.request("GET", "/api/source/jobs", publisher, "", nil, nil)).Jobs
	if len(jobs) != 1 || jobs[0].Content != job.Content || jobs[0].Status != "failed" {
		t.Fatal("retry lost frozen approval")
	}
	for i := 0; i < 2; i++ {
		status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/fail", publisher, "", map[string]any{"reason": "source_content_rejected"}, nil), 200)
	}
	after, _ := loadPack(f.store.db, p.ID)
	if after.Source.Pending != "" || after.Source.Proof != config.InitialProof || after.PublishedVersion != 1 {
		t.Fatal("rejection changed release or ownership")
	}
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": proof, "parentCommit": config.InitialProof.Commit}, nil), 409)
	after = sourceDraft(t, f, after, "# Corrected proposal\n")
	second := sourceQueue(t, f, after)
	if second.ID == job.ID || second.Content == job.Content {
		t.Fatal("rejection prevented corrected approval")
	}
}

func TestSourceIdentityAnchorAndFrozenJobSurviveRestart(t *testing.T) {
	f, p, config, publisher := sourceFixture(t)
	p = sourceDraft(t, f, p, "# Frozen across restart\n")
	job := sourceQueue(t, f, p)
	dir := f.store.dir
	info, err := os.Stat(f.store.SourceTokenPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("publisher credential is not private")
	}
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.store = reopened
	t.Cleanup(func() { reopened.Close() })
	h, err := NewServerWithSources(reopened, "", map[string]SourceConfig{p.ID: config})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = h
	data, _ := os.ReadFile(reopened.SourceTokenPath())
	if strings.TrimSpace(string(data)) != publisher {
		t.Fatal("publisher identity changed")
	}
	proof := successor(job)
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": proof, "parentCommit": config.InitialProof.Commit}, nil), 200)
	// The original config remains the restart anchor after a successor is published.
	if _, err = NewServerWithSources(reopened, "", map[string]SourceConfig{p.ID: config}); err != nil {
		t.Fatal("restart did not retain current proof")
	}
	after, _ := loadPack(reopened.db, p.ID)
	if after.Source.Proof != proof {
		t.Fatal("restart reset advanced proof")
	}
	changed := config
	changed.InitialProof = proof
	if _, err = NewServerWithSources(reopened, "", map[string]SourceConfig{p.ID: changed}); err == nil {
		t.Fatal("changed anchor was accepted")
	}
	if err = os.Remove(reopened.SourceTokenPath()); err != nil {
		t.Fatal(err)
	}
	if _, err = NewServerWithSources(reopened, "", map[string]SourceConfig{p.ID: config}); err == nil {
		t.Fatal("lost token silently replaced")
	}
}

func TestSourceInitialProofAndRepeatedIdenticalPublication(t *testing.T) {
	f := setup(t)
	p := f.pack("# Initial review\n")
	r := f.publish(p)
	badConfig := SourceConfig{Label: "Reviewed source", InitialProof: core.SourceProof{Commit: strings.Repeat("a", 40), ReleaseID: "common-v1", Version: "v1", Hash: strings.Repeat("0", 64)}}
	if _, err := NewServerWithSources(f.store, "", map[string]SourceConfig{p.ID: badConfig}); err == nil {
		t.Fatal("unrelated source anchor adopted")
	}
	current, _ := loadPack(f.store.db, p.ID)
	if current.Source != nil {
		t.Fatal("bad anchor changed ownership")
	}
	config := badConfig
	config.InitialProof.Hash = r.Hash
	h, err := NewServerWithSources(f.store, "", map[string]SourceConfig{p.ID: config})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = h
	token, _ := os.ReadFile(f.store.SourceTokenPath())
	publisher := strings.TrimSpace(string(token))
	p, _ = loadPack(f.store.db, p.ID)
	job := sourceQueue(t, f, p)
	status(t, f.request("POST", "/api/source/jobs/"+job.ID+"/complete", publisher, "", map[string]any{"proof": config.InitialProof, "parentCommit": config.InitialProof.Commit}, nil), 200)
	after, _ := loadPack(f.store.db, p.ID)
	if after.PublishedVersion != 2 || after.Source.Proof != config.InitialProof {
		t.Fatal("same canonical source was not preserved")
	}
	// A normal administrator cannot bypass the managed queue with its Bearer token.
	current = sourceDraft(t, f, after, "# A second approval\n")
	next := sourceQueue(t, f, current)
	if next.Status != "pending" {
		t.Fatal("managed publication bypassed queue")
	}
	ordinary := f.pack("# Ordinary Hub pack\n")
	plain := f.publish(ordinary)
	if plain.Source != nil {
		t.Fatal("ordinary release has an invented source proof")
	}
	encoded, _ := json.Marshal(after)
	if strings.Contains(string(encoded), publisher) {
		t.Fatal("publisher token in pack metadata")
	}
}

func TestSourceConfigurationCannotBeSilentlyOmitted(t *testing.T) {
	plain := setup(t)
	if _, err := NewServerWithSources(plain.store, "", nil); err != nil {
		t.Fatal("ordinary Hub required a source integration")
	}
	f, p, config, _ := sourceFixture(t)
	for _, missing := range []map[string]SourceConfig{nil, {}} {
		if _, err := NewServerWithSources(f.store, "", missing); err == nil {
			t.Fatal("enabled source owner silently lost its publisher")
		}
	}
	if _, err := NewServerWithSources(f.store, "", map[string]SourceConfig{p.ID: config}); err != nil {
		t.Fatal("unchanged reviewed source configuration could not restart")
	}
}
