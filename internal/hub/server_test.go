package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pilgrim13/rulesend/internal/core"
)

type fixture struct {
	t       *testing.T
	store   *Store
	handler http.Handler
	token   string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h, err := NewServer(s, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(s.AdminTokenPath())
	if err != nil {
		t.Fatal("cannot read test credentials")
	}
	return &fixture{t: t, store: s, handler: h, token: strings.TrimSpace(string(b))}
}
func (f *fixture) request(method, path, token, origin string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()
	var data []byte
	switch v := body.(type) {
	case nil:
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func status(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("HTTP status: got %d, want %d", w.Code, want)
	}
}
func result[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal("response is not valid expected JSON")
	}
	return v
}
func (f *fixture) pack(content string) core.Pack {
	f.t.Helper()
	w := f.request("POST", "/api/packs", f.token, "", map[string]any{"name": "Synthetic rules", "description": "test fixture", "content": content}, nil)
	status(f.t, w, 201)
	return result[core.Pack](f.t, w)
}
func (f *fixture) publish(p core.Pack) core.Release {
	f.t.Helper()
	w := f.request("POST", "/api/packs/"+p.ID+"/publish", f.token, "", map[string]any{"baseRevision": p.DraftRevision, "message": "test publication"}, nil)
	status(f.t, w, 200)
	return result[core.Release](f.t, w)
}
func (f *fixture) enroll(ids []string) (core.Device, string) {
	f.t.Helper()
	token, _, err := f.store.enrollment(ids, 15)
	if err != nil {
		f.t.Fatal(err)
	}
	d, secret, err := f.store.enroll(token, "Synthetic device", "test-os")
	if err != nil {
		f.t.Fatal(err)
	}
	return d, secret
}

func TestPrivateStorageAndStableIdentity(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	hash := s.adminHash
	path := s.AdminTokenPath()
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{dir: 0700, path: 0600, filepath.Join(dir, "hub.db"): 0600} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != want {
				t.Fatal("private storage mode mismatch")
			}
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.adminHash != hash {
		t.Fatal("administrator identity changed on reopen")
	}
	s.Close()
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if s, err = Open(dir); err == nil {
		s.Close()
		t.Fatal("missing administrator token silently reset identity")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing credentials were recreated")
	}
}
func TestStorageRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges")
	}
	for _, name := range []string{"hub.db", "admin-token"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "unrelated")
			if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			if s, err := Open(dir); err == nil {
				s.Close()
				t.Fatal("accepted symlink")
			}
			b, _ := os.ReadFile(outside)
			if string(b) != "untouched" {
				t.Fatal("modified symlink target")
			}
		})
	}
}
func TestSessionOriginAndRoleSeparation(t *testing.T) {
	f := setup(t)
	status(t, f.request("GET", "/api/state", "", "", nil, nil), 401)
	body := map[string]string{"token": f.token}
	status(t, f.request("POST", "/api/login", "", "", body, nil), 403)
	status(t, f.request("POST", "/api/login", "", "https://unrelated.example", body, nil), 403)
	w := f.request("POST", "/api/login", "", "http://127.0.0.1:8080", body, nil)
	status(t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	c := cookies[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.MaxAge <= 0 {
		t.Fatal("session cookie is insufficiently constrained")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("auth response must not be cached")
	}
	status(t, f.request("GET", "/api/state", "", "", nil, c), 200)
	status(t, f.request("GET", "/api/state", "", "https://unrelated.example", nil, c), 401)
	create := map[string]string{"name": "test", "content": "rules"}
	status(t, f.request("POST", "/api/packs", "", "", create, c), 401)
	status(t, f.request("POST", "/api/packs", "", "http://127.0.0.1:8080", create, c), 201)
	status(t, f.request("POST", "/api/packs", f.token, "https://unrelated.example", create, nil), 401)
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/state", nil)
	r.AddCookie(c)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	cross := httptest.NewRecorder()
	f.handler.ServeHTTP(cross, r)
	status(t, cross, 401)
	p := f.pack("scope")
	_, deviceToken := f.enroll([]string{p.ID})
	status(t, f.request("GET", "/api/state", deviceToken, "", nil, nil), 401)
	status(t, f.request("POST", "/api/packs", deviceToken, "", create, nil), 401)
	status(t, f.request("POST", "/api/agent/poll", f.token, "", map[string]any{}, nil), 401)
	status(t, f.request("POST", "/api/logout", "", "http://127.0.0.1:8080", map[string]any{}, c), 200)
	status(t, f.request("GET", "/api/state", "", "", nil, c), 401)
}
func TestSessionExpiryAndRateLimit(t *testing.T) {
	f := setup(t)
	w := f.request("POST", "/api/login", "", "http://127.0.0.1:8080", map[string]string{"token": f.token}, nil)
	status(t, w, 200)
	c := w.Result().Cookies()[0]
	if _, err := f.store.db.Exec(`UPDATE sessions SET expires=?`, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	status(t, f.request("GET", "/api/state", "", "", nil, c), 401)
	for i := 0; i < 9; i++ {
		status(t, f.request("POST", "/api/login", "", "http://127.0.0.1:8080", map[string]string{"token": "invalid"}, nil), 401)
	}
	status(t, f.request("POST", "/api/login", "", "http://127.0.0.1:8080", map[string]string{"token": "invalid"}, nil), 429)
}
func TestRemoteOriginConfiguration(t *testing.T) {
	f := setup(t)
	for _, u := range []string{"http://rules.example", "https://user:password@rules.example", "https://rules.example/path", "https://rules.example?token=abc", "file:///tmp/test"} {
		if _, err := NewServer(f.store, u); err == nil {
			t.Fatalf("accepted invalid public URL %q", u)
		}
	}
	r := httptest.NewRequest("GET", "http://rebinding.example/api/state", nil)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	status(t, w, 403)
	h, err := NewServer(f.store, "https://rules.example")
	if err != nil {
		t.Fatal(err)
	}
	f.handler = h
	w = f.request("POST", "/api/login", "", "https://rules.example", map[string]string{"token": f.token}, nil)
	status(t, w, 200)
	if !w.Result().Cookies()[0].Secure {
		t.Fatal("remote HTTPS cookie missing Secure")
	}
	status(t, f.request("OPTIONS", "/api/state", "", "https://other.example", nil, nil), 403)
}
func TestDraftCASPublishAndRestore(t *testing.T) {
	f := setup(t)
	p := f.pack("first")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, content := range []string{"second A", "second B"} {
		wg.Add(1)
		go func(content string) {
			defer wg.Done()
			w := f.request("PUT", "/api/packs/"+p.ID+"/draft", f.token, "", map[string]any{"baseRevision": p.DraftRevision, "content": content}, nil)
			codes <- w.Code
		}(content)
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for c := range codes {
		counts[c]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("concurrent draft edit did not preserve CAS")
	}
	status(t, f.request("POST", "/api/packs/"+p.ID+"/publish", f.token, "", map[string]any{"baseRevision": 1}, nil), 409)
	p, err := loadPack(f.store.db, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	r1 := f.publish(p)
	status(t, f.request("POST", "/api/packs/"+p.ID+"/publish", f.token, "", map[string]any{"baseRevision": p.DraftRevision}, nil), 409)
	p, _ = loadPack(f.store.db, p.ID)
	r2 := f.publish(p)
	if r2.Version != r1.Version+1 || r2.Hash != r1.Hash {
		t.Fatal("explicit identical-content publication must make a new version")
	}
	p, _ = loadPack(f.store.db, p.ID)
	w := f.request("PUT", "/api/packs/"+p.ID+"/draft", f.token, "", map[string]any{"baseRevision": p.DraftRevision, "content": "unpublished edit"}, nil)
	status(t, w, 200)
	p = result[core.Pack](t, w)
	w = f.request("POST", "/api/packs/"+p.ID+"/restore", f.token, "", map[string]any{"baseRevision": p.DraftRevision, "version": r1.Version}, nil)
	status(t, w, 200)
	restored := result[core.Pack](t, w)
	if restored.Content != r1.Content || restored.PublishedVersion != r2.Version || restored.DraftRevision != p.DraftRevision+1 {
		t.Fatal("restore must copy historical content to a new draft only")
	}
	stored, err := loadRelease(f.store.db, p.ID, r1.Version)
	if err != nil || stored != r1 {
		t.Fatal("historical release changed")
	}
	if _, err = f.store.db.Exec(`UPDATE releases SET data='{}' WHERE pack_id=?`, p.ID); err == nil {
		t.Fatal("database permitted release mutation")
	}
	if _, err = f.store.db.Exec(`DELETE FROM releases WHERE pack_id=?`, p.ID); err == nil {
		t.Fatal("database permitted release deletion")
	}
}
func TestPolicyAndSyncKeepPause(t *testing.T) {
	f := setup(t)
	p := f.pack("rules")
	if p.Mode != "manual" {
		t.Fatal("new packs must be manual")
	}
	w := f.request("PATCH", "/api/packs/"+p.ID+"/policy", f.token, "", map[string]any{"mode": "auto", "intervalSeconds": 30, "paused": true}, nil)
	status(t, w, 200)
	w = f.request("POST", "/api/packs/"+p.ID+"/sync-now", f.token, "", map[string]any{}, nil)
	status(t, w, 200)
	p = result[core.Pack](t, w)
	if !p.Paused || p.Trigger != 1 || p.Mode != "auto" {
		t.Fatal("sync-now unpaused or changed policy")
	}
	status(t, f.request("PATCH", "/api/packs/"+p.ID+"/policy", f.token, "", map[string]any{"mode": "auto", "intervalSeconds": 1, "paused": false}, nil), 400)
}
func TestEnrollmentSingleUseExpiryAndHashAtRest(t *testing.T) {
	f := setup(t)
	p := f.pack("enrollment scope")
	token, _, err := f.store.enrollment([]string{p.ID}, 15)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = f.store.db.QueryRow(`SELECT token_hash FROM enrollments`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token || stored != core.Hash([]byte(token)) {
		t.Fatal("enrollment credential is not hashed")
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := f.request("POST", "/api/enroll", "", "", map[string]string{"token": token, "name": "race device", "platform": "test-os"}, nil)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	count := map[int]int{}
	for c := range codes {
		count[c]++
	}
	if count[200] != 1 || count[401] != 1 {
		t.Fatal("single-use enrollment race allowed multiple registrations")
	}
	token, _, err = f.store.enrollment([]string{p.ID}, 15)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.db.Exec(`UPDATE enrollments SET expires=? WHERE token_hash=?`, time.Now().Add(-time.Second).Unix(), core.Hash([]byte(token))); err != nil {
		t.Fatal(err)
	}
	status(t, f.request("POST", "/api/enroll", "", "", map[string]string{"token": token, "name": "expired", "platform": "test-os"}, nil), 401)
	for _, body := range []any{map[string]any{"packIds": []string{}, "expiresMinutes": 15}, map[string]any{"packIds": []string{p.ID, p.ID}, "expiresMinutes": 15}, map[string]any{"packIds": []string{p.ID}, "expiresMinutes": 61}} {
		status(t, f.request("POST", "/api/enrollment", f.token, "", body, nil), 400)
	}
	status(t, f.request("POST", "/api/enrollment", f.token, "", map[string]any{"packIds": []string{"not_found"}, "expiresMinutes": 15}, nil), 404)
}
func TestScopedPollAndRevocation(t *testing.T) {
	f := setup(t)
	p := f.pack("published visible text")
	release := f.publish(p)
	private := f.pack("other device only")
	f.publish(private)
	p, _ = loadPack(f.store.db, p.ID)
	p, err := f.store.draft(p.ID, p.DraftRevision, "DRAFT-MUST-NOT-LEAVE-HUB")
	if err != nil {
		t.Fatal(err)
	}
	d, token := f.enroll([]string{p.ID})
	w := f.request("POST", "/api/agent/poll", token, "", map[string]any{}, nil)
	status(t, w, 200)
	poll := result[core.PollResponse](t, w)
	if len(poll.Plans) != 1 || poll.Plans[0].Pack.ID != p.ID || poll.Plans[0].Release == nil || poll.Plans[0].Release.Content != release.Content || poll.Plans[0].Pack.Content != "" || strings.Contains(w.Body.String(), "DRAFT-MUST-NOT-LEAVE-HUB") || strings.Contains(w.Body.String(), private.ID) {
		t.Fatal("poll leaked draft or unauthorized pack")
	}
	if poll.Device.LastSeen == "" {
		t.Fatal("heartbeat missing")
	}
	snap, err := f.store.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Receipts) != 0 {
		t.Fatal("poll invented installation receipts")
	}
	status(t, f.request("POST", "/api/devices/"+d.ID+"/revoke", f.token, "", map[string]any{}, nil), 200)
	status(t, f.request("POST", "/api/agent/poll", token, "", map[string]any{}, nil), 401)
	r := core.Receipt{PackID: p.ID, TargetID: "target1", TargetLabel: "Synthetic target", Adapter: "markdown", Version: release.Version, Hash: release.Hash, Status: "applied"}
	status(t, f.request("POST", "/api/agent/receipts", token, "", map[string]any{"receipts": []core.Receipt{r}}, nil), 401)
}
func TestReceiptScopeIntegrityPrivacyAndMonotonicity(t *testing.T) {
	f := setup(t)
	p := f.pack("content")
	r1 := f.publish(p)
	other := f.pack("other")
	rOther := f.publish(other)
	d, token := f.enroll([]string{p.ID})
	receipt := core.Receipt{DeviceID: d.ID, PackID: p.ID, TargetID: "target1", Adapter: "codex", TargetLabel: "Project rules", Version: r1.Version, Hash: r1.Hash, Status: "applied", AppLoaded: "verified", BehaviorVerified: "verified", CheckedAt: "1900-01-01T00:00:00Z"}
	send := func(v []core.Receipt) *httptest.ResponseRecorder {
		return f.request("POST", "/api/agent/receipts", token, "", map[string]any{"receipts": v}, nil)
	}
	for _, modify := range []func(*core.Receipt){func(r *core.Receipt) { r.DeviceID = "another_device" }, func(r *core.Receipt) { r.PackID = other.ID; r.Hash = rOther.Hash }} {
		r := receipt
		modify(&r)
		status(t, send([]core.Receipt{r}), 403)
	}
	for _, modify := range []func(*core.Receipt){func(r *core.Receipt) { r.Hash = strings.Repeat("0", 64) }, func(r *core.Receipt) { r.Version = 999 }, func(r *core.Receipt) { r.Version = 0; r.Hash = "" }, func(r *core.Receipt) { r.TargetLabel = "/private/target.md" }, func(r *core.Receipt) { r.Status = "verified" }} {
		r := receipt
		modify(&r)
		status(t, send([]core.Receipt{r}), 400)
	}
	bad := receipt
	bad.Hash = strings.Repeat("0", 64)
	bad.TargetID = "target2"
	status(t, send([]core.Receipt{receipt, bad}), 400)
	snap, _ := f.store.snapshot()
	if len(snap.Receipts) != 0 {
		t.Fatal("invalid batch partially persisted")
	}
	status(t, send([]core.Receipt{receipt}), 200)
	snap, _ = f.store.snapshot()
	got := snap.Receipts[0]
	if got.AppLoaded != "unverified" || got.BehaviorVerified != "unverified" || got.CheckedAt == receipt.CheckedAt || got.DeviceID != d.ID {
		t.Fatal("server accepted forged verification or timestamp")
	}
	conflict := receipt
	conflict.Status = "conflict"
	conflict.Error = "synthetic credential and confidential rule excerpt"
	status(t, send([]core.Receipt{conflict}), 200)
	snap, _ = f.store.snapshot()
	if strings.Contains(snap.Receipts[0].Error, "credential") {
		t.Fatal("unsafe local error reached Hub")
	}
	p, _ = loadPack(f.store.db, p.ID)
	r2 := f.publish(p)
	receipt.Version = r2.Version
	receipt.Hash = r2.Hash
	status(t, send([]core.Receipt{receipt}), 200)
	status(t, send([]core.Receipt{conflict}), 400)
	// A failure before any release is a valid truthful observation.
	draft := f.pack("unpublished")
	_, draftToken := f.enroll([]string{draft.ID})
	pending := core.Receipt{PackID: draft.ID, TargetID: "pending1", Adapter: "markdown", TargetLabel: "Pending target", Status: "pending"}
	status(t, f.request("POST", "/api/agent/receipts", draftToken, "", map[string]any{"receipts": []core.Receipt{pending}}, nil), 200)
}
func TestInputLimitsAndSafeErrors(t *testing.T) {
	f := setup(t)
	for _, content := range []string{"<!-- rulesend:begin -->", "has\x00NUL", strings.Repeat("a", core.MaxContentBytes+1)} {
		status(t, f.request("POST", "/api/packs", f.token, "", map[string]string{"name": "limits", "content": content}, nil), 400)
	}
	status(t, f.request("POST", "/api/packs", f.token, "", `{"name":"ok","content":"ok","arbitraryPath":"bad"}`, nil), 400)
	status(t, f.request("POST", "/api/packs", f.token, "", `{"name":"ok","content":"ok"} {}`, nil), 400)
	status(t, f.request("POST", "/api/packs", f.token, "", "{\"name\":\"ok\",\"content\":\"\xff\"}", nil), 400)
	status(t, f.request("POST", "/api/packs", f.token, "", `{"name":"ok","content":"`+strings.Repeat("a", 1024*1024)+`"}`, nil), 413)
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/packs", strings.NewReader(`{"name":"ok"}`))
	r.Header.Set("Authorization", "Bearer "+f.token)
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	status(t, w, 400)
	f.store.Close()
	w = f.request("GET", "/api/state", f.token, "", nil, nil)
	status(t, w, 500)
	body, _ := io.ReadAll(w.Result().Body)
	if strings.Contains(string(body), "sql") || strings.Contains(string(body), f.store.dir) {
		t.Fatal("internal database details leaked")
	}
}

func TestDeviceTargetLimitAndPersistence(t *testing.T) {
	f := setup(t)
	p := f.pack("bounded observations")
	release := f.publish(p)
	_, token := f.enroll([]string{p.ID})
	list := make([]core.Receipt, 100)
	for i := range list {
		list[i] = core.Receipt{PackID: p.ID, TargetID: fmt.Sprintf("target%d", i), TargetLabel: "Synthetic target", Adapter: "markdown", Version: release.Version, Hash: release.Hash, Status: "applied"}
	}
	status(t, f.request("POST", "/api/agent/receipts", token, "", map[string]any{"receipts": list}, nil), 200)
	extra := list[0]
	extra.TargetID = "target101"
	status(t, f.request("POST", "/api/agent/receipts", token, "", map[string]any{"receipts": []core.Receipt{extra}}, nil), 400)
	// Updating existing targets remains possible at the capacity boundary.
	status(t, f.request("POST", "/api/agent/receipts", token, "", map[string]any{"receipts": list[:1]}, nil), 200)
	dir := f.store.dir
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	snap, err := reopened.snapshot()
	if err != nil || len(snap.Receipts) != 100 || len(snap.Packs) != 1 || snap.Packs[0].PublishedVersion != release.Version {
		t.Fatal("Hub observations or releases did not survive restart")
	}
	if _, err := reopened.poll(token); err != nil {
		t.Fatal("device credential did not survive restart")
	}
}
