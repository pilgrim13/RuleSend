package hub

import (
	"bytes"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pilgrim13/rulesend/internal/core"
)

//go:embed web/*
var webAssets embed.FS

type inputError string

func (e inputError) Error() string  { return string(e) }
func badInput(message string) error { return inputError(message) }

type attempt struct {
	start time.Time
	count int
}
type server struct {
	store        *Store
	publicOrigin string
	secure       bool
	static       http.Handler
	mu           sync.Mutex
	attempts     map[string]attempt
	sourceHash   string
}

// NewServer supports plaintext only on loopback. Behind a trusted TLS proxy,
// configure publicURL and keep the backend listening on loopback.
func NewServer(store *Store, publicURL string) (http.Handler, error) {
	s := &server{store: store, attempts: make(map[string]attempt)}
	if publicURL != "" {
		u, err := url.Parse(publicURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, errors.New("public URL must be an HTTP(S) origin")
		}
		if u.Scheme != "https" && !loopbackHost(u.Host) {
			return nil, errors.New("HTTPS is required outside loopback")
		}
		s.publicOrigin = u.Scheme + "://" + u.Host
		s.secure = u.Scheme == "https"
	}
	assets, err := fs.Sub(webAssets, "web")
	if err != nil {
		return nil, errors.New("web assets unavailable")
	}
	s.static = http.FileServer(http.FS(assets))
	return s, nil
}
func loopbackHost(host string) bool {
	if strings.ContainsAny(host, "/\\@ \t\r\n") {
		return false
	}
	h := host
	if split, _, err := net.SplitHostPort(host); err == nil {
		h = split
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
func (s *server) origin(r *http.Request) string {
	if s.publicOrigin != "" {
		return s.publicOrigin
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
func (s *server) sameOrigin(r *http.Request, required bool) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Sec-Fetch-Site") == "same-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return !required
	}
	return origin == s.origin(r)
}
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
func (s *server) admin(r *http.Request, write bool) bool {
	if r.Header.Get("Authorization") != "" {
		if !s.sameOrigin(r, false) {
			return false
		}
		t := bearer(r)
		return validToken(t) && subtle.ConstantTimeCompare([]byte(core.Hash([]byte(t))), []byte(s.store.adminHash)) == 1
	}
	if !s.sameOrigin(r, write) {
		return false
	}
	c, err := r.Cookie("rulesend_session")
	if err != nil || !validToken(c.Value) {
		return false
	}
	var expires int64
	err = s.store.db.QueryRow(`SELECT expires FROM sessions WHERE token_hash=?`, core.Hash([]byte(c.Value))).Scan(&expires)
	return err == nil && expires > time.Now().Unix()
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}
func storeError(w http.ResponseWriter, err error) {
	var input inputError
	switch {
	case errors.Is(err, errMissing):
		apiError(w, 404, "not found")
	case errors.Is(err, errConflict):
		apiError(w, 409, errConflict.Error())
	case errors.Is(err, errUnauthorized):
		apiError(w, 401, "authentication required")
	case errors.Is(err, errForbidden):
		apiError(w, 403, "access denied")
	case errors.As(err, &input):
		apiError(w, 400, string(input))
	default:
		apiError(w, 500, "the Hub could not complete this operation")
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		apiError(w, 400, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			apiError(w, 413, "request is too large")
		} else {
			apiError(w, 400, "cannot read JSON request")
		}
		return false
	}
	if !utf8.Valid(data) {
		apiError(w, 400, "JSON request must contain valid UTF-8")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			apiError(w, 413, "request is too large")
		} else {
			apiError(w, 400, "invalid JSON request")
		}
		return false
	}
	var trailing any
	if err = dec.Decode(&trailing); err != io.EOF {
		apiError(w, 400, "request must contain one JSON object")
		return false
	}
	return true
}
func textField(s string, max int, required bool) bool {
	if len(s) > max || !utf8.ValidString(s) || required && strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (s *server) allowedAttempt(r *http.Request, kind string, max int) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	key := kind + ":" + host
	s.mu.Lock()
	defer s.mu.Unlock()
	t := time.Now()
	if len(s.attempts) > 4096 {
		for k, v := range s.attempts {
			if t.Sub(v.start) > time.Minute {
				delete(s.attempts, k)
			}
		}
		if len(s.attempts) > 4096 {
			return false
		}
	}
	v := s.attempts[key]
	if t.Sub(v.start) >= time.Minute {
		v = attempt{start: t}
	}
	v.count++
	s.attempts[key] = v
	return v.count <= max
}
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if s.publicOrigin == "" && !loopbackHost(r.Host) {
		apiError(w, 403, "configure an HTTPS public URL for remote access")
		return
	}
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			apiError(w, 404, "not found")
			return
		}
		jsonResponse(w, 200, map[string]string{"version": core.Version})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			apiError(w, 404, "not found")
			return
		}
		s.static.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodOptions {
		apiError(w, 403, "cross-origin access is not supported")
		return
	}
	switch r.URL.Path {
	case "/api/login":
		s.login(w, r)
		return
	case "/api/enroll":
		s.enroll(w, r)
		return
	case "/api/agent/poll", "/api/agent/receipts":
		s.agent(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/source/") {
		s.sourceAPI(w, r)
		return
	}
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	if !s.admin(r, write) {
		apiError(w, 401, "authentication required")
		return
	}
	if r.URL.Path == "/api/logout" {
		if r.Method != http.MethodPost {
			apiError(w, 404, "not found")
			return
		}
		var req struct{}
		if !decode(w, r, &req) {
			return
		}
		if c, err := r.Cookie("rulesend_session"); err == nil {
			if _, err = s.store.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, core.Hash([]byte(c.Value))); err != nil {
				storeError(w, err)
				return
			}
		}
		s.sessionCookie(w, r, "", -1)
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.URL.Path == "/api/state" && r.Method == http.MethodGet {
		v, err := s.store.snapshot()
		if err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, v)
		return
	}
	if r.URL.Path == "/api/packs" && r.Method == http.MethodPost {
		s.createPack(w, r)
		return
	}
	if r.URL.Path == "/api/enrollment" && r.Method == http.MethodPost {
		s.enrollment(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	if len(parts) >= 2 && parts[0] == "packs" && core.ValidID(parts[1]) {
		s.pack(w, r, parts)
		return
	}
	if len(parts) == 3 && parts[0] == "devices" && parts[2] == "revoke" && core.ValidID(parts[1]) && r.Method == http.MethodPost {
		var req struct{}
		if !decode(w, r, &req) {
			return
		}
		if err := s.store.revoke(parts[1]); err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	apiError(w, 404, "not found")
}
func (s *server) sessionCookie(w http.ResponseWriter, r *http.Request, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "rulesend_session", Value: value, Path: "/", HttpOnly: true, Secure: s.secure || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: age})
}
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, 404, "not found")
		return
	}
	// Login itself needs CSRF protection, even before a session exists.
	if !s.sameOrigin(r, true) {
		apiError(w, 403, "same-origin request required")
		return
	}
	if !s.allowedAttempt(r, "login", 10) {
		w.Header().Set("Retry-After", "60")
		apiError(w, 429, "too many login attempts; retry in a minute")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validToken(req.Token) || subtle.ConstantTimeCompare([]byte(core.Hash([]byte(req.Token))), []byte(s.store.adminHash)) != 1 {
		apiError(w, 401, "authentication required")
		return
	}
	token, err := core.Token()
	if err != nil {
		storeError(w, err)
		return
	}
	tx, err := s.store.db.Begin()
	if err != nil {
		storeError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM sessions WHERE expires<=?`, time.Now().Unix()); err != nil {
		storeError(w, err)
		return
	}
	if c, e := r.Cookie("rulesend_session"); e == nil {
		if _, err = tx.Exec(`DELETE FROM sessions WHERE token_hash=?`, core.Hash([]byte(c.Value))); err != nil {
			storeError(w, err)
			return
		}
	}
	if _, err = tx.Exec(`INSERT INTO sessions(token_hash,expires) VALUES(?,?)`, core.Hash([]byte(token)), time.Now().Add(12*time.Hour).Unix()); err != nil {
		storeError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		storeError(w, err)
		return
	}
	s.sessionCookie(w, r, token, 12*60*60)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *server) createPack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !textField(req.Name, 120, true) || !textField(req.Description, 1000, false) {
		apiError(w, 400, "invalid name or description")
		return
	}
	if err := core.ValidateContent(req.Content); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	p, err := s.store.createPack(req.Name, req.Description, req.Content)
	if err != nil {
		storeError(w, err)
		return
	}
	jsonResponse(w, 201, p)
}
func (s *server) pack(w http.ResponseWriter, r *http.Request, parts []string) {
	id := parts[1]
	if len(parts) == 2 && r.Method == http.MethodGet {
		p, err := loadPack(s.store.db, id)
		if err != nil {
			storeError(w, err)
			return
		}
		releases, err := s.store.releaseList(id)
		if err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"pack": p, "releases": releases})
		return
	}
	if len(parts) != 3 {
		apiError(w, 404, "not found")
		return
	}
	switch {
	case parts[2] == "draft" && r.Method == http.MethodPut:
		var req struct {
			BaseRevision int64  `json:"baseRevision"`
			Content      string `json:"content"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.BaseRevision < 1 {
			apiError(w, 400, "baseRevision is required")
			return
		}
		if err := core.ValidateContent(req.Content); err != nil {
			apiError(w, 400, err.Error())
			return
		}
		p, err := s.store.draft(id, req.BaseRevision, req.Content)
		if err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, p)
	case parts[2] == "publish" && r.Method == http.MethodPost:
		var req struct {
			BaseRevision int64  `json:"baseRevision"`
			Message      string `json:"message"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.BaseRevision < 1 || !textField(req.Message, 1000, false) {
			apiError(w, 400, "invalid revision or release message")
			return
		}
		p, err := loadPack(s.store.db, id)
		if err != nil {
			storeError(w, err)
			return
		}
		if p.Source != nil {
			job, err := s.store.queueSource(id, req.BaseRevision, req.Message)
			if err != nil {
				sourceError(w, err)
				return
			}
			jsonResponse(w, 202, map[string]any{"publication": job})
			return
		}
		release, err := s.store.publish(id, req.BaseRevision, req.Message)
		if err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, release)
	case parts[2] == "restore" && r.Method == http.MethodPost:
		var req struct {
			BaseRevision int64 `json:"baseRevision"`
			Version      int64 `json:"version"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.BaseRevision < 1 || req.Version < 1 {
			apiError(w, 400, "revision and version are required")
			return
		}
		p, err := s.store.restore(id, req.BaseRevision, req.Version)
		if err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, p)
	case parts[2] == "policy" && r.Method == http.MethodPatch:
		var req struct {
			Mode            string `json:"mode"`
			IntervalSeconds int    `json:"intervalSeconds"`
			Paused          *bool  `json:"paused"`
		}
		if !decode(w, r, &req) {
			return
		}
		if (req.Mode != "auto" && req.Mode != "manual") || req.IntervalSeconds < 15 || req.IntervalSeconds > 86400 || req.Paused == nil {
			apiError(w, 400, "policy requires auto/manual mode, 15–86400 seconds, and paused")
			return
		}
		p, err := s.store.changePack(id, func(p *core.Pack, _ *sql.Tx) error {
			p.Mode = req.Mode
			p.IntervalSeconds = req.IntervalSeconds
			p.Paused = *req.Paused
			return nil
		}, "policy.updated")
		if err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, p)
	case parts[2] == "sync-now" && r.Method == http.MethodPost:
		var req struct{}
		if !decode(w, r, &req) {
			return
		}
		p, err := s.store.changePack(id, func(p *core.Pack, _ *sql.Tx) error { p.Trigger++; return nil }, "sync.requested")
		if err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, p)
	default:
		apiError(w, 404, "not found")
	}
}
func (s *server) enrollment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PackIDs        []string `json:"packIds"`
		ExpiresMinutes int      `json:"expiresMinutes"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.PackIDs) < 1 || len(req.PackIDs) > 100 || req.ExpiresMinutes < 1 || req.ExpiresMinutes > 60 {
		apiError(w, 400, "choose 1–100 packs and an expiry of 1–60 minutes")
		return
	}
	seen := map[string]bool{}
	for _, id := range req.PackIDs {
		if !core.ValidID(id) || seen[id] {
			apiError(w, 400, "invalid or duplicate pack ID")
			return
		}
		seen[id] = true
	}
	token, expires, err := s.store.enrollment(req.PackIDs, req.ExpiresMinutes)
	if err != nil {
		storeError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"token": token, "expiresAt": expires})
}
func (s *server) enroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, 404, "not found")
		return
	}
	if r.Header.Get("Origin") != "" && !s.sameOrigin(r, false) {
		apiError(w, 403, "cross-origin access is not supported")
		return
	}
	if !s.allowedAttempt(r, "enroll", 20) {
		w.Header().Set("Retry-After", "60")
		apiError(w, 429, "too many enrollment attempts; retry in a minute")
		return
	}
	var req struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validToken(req.Token) {
		apiError(w, 401, "authentication required")
		return
	}
	if !textField(req.Name, 120, true) || !textField(req.Platform, 64, true) {
		apiError(w, 400, "invalid device name or platform")
		return
	}
	d, token, err := s.store.enroll(req.Token, req.Name, req.Platform)
	if err != nil {
		storeError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"deviceId": d.ID, "deviceToken": token, "packIds": d.PackIDs})
}
func (s *server) agent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, 404, "not found")
		return
	}
	if r.Header.Get("Origin") != "" && !s.sameOrigin(r, false) {
		apiError(w, 403, "cross-origin access is not supported")
		return
	}
	token := bearer(r)
	if !validToken(token) {
		apiError(w, 401, "authentication required")
		return
	}
	if r.URL.Path == "/api/agent/poll" {
		var req struct{}
		if !decode(w, r, &req) {
			return
		}
		out, err := s.store.poll(token)
		if err != nil {
			storeError(w, err)
			return
		}
		jsonResponse(w, 200, out)
		return
	}
	var req struct {
		Receipts []core.Receipt `json:"receipts"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Receipts) < 1 || len(req.Receipts) > 100 {
		apiError(w, 400, "send 1–100 receipts")
		return
	}
	seen := map[string]bool{}
	for i := range req.Receipts {
		v := &req.Receipts[i]
		if !core.ValidID(v.PackID) || !core.ValidID(v.TargetID) || seen[v.TargetID] || !textField(v.TargetLabel, 120, true) || !textField(v.Error, 512, false) || v.Version < 0 || (v.Hash != "" && !validToken(v.Hash)) || (v.Adapter != "codex" && v.Adapter != "claude" && v.Adapter != "claude-code" && v.Adapter != "custom" && v.Adapter != "markdown") || (v.Status != "applied" && v.Status != "conflict" && v.Status != "error" && v.Status != "pending") {
			apiError(w, 400, "invalid receipt")
			return
		}
		seen[v.TargetID] = true
		if len(v.DeviceID) > 64 || len(v.CheckedAt) > 64 || len(v.AppLoaded) > 32 || len(v.BehaviorVerified) > 32 || strings.ContainsAny(v.TargetLabel, "/\\") {
			apiError(w, 400, "receipt label must not contain a path")
			return
		}
		// Keep local errors local: user-controlled errors can include credentials,
		// full paths or rule content. Store only a safe status-specific description.
		switch v.Status {
		case "applied":
			v.Error = ""
		case "conflict":
			v.Error = "Local changes require review on the device"
		case "error":
			v.Error = "Local operation failed; inspect the device"
		case "pending":
			v.Error = "Waiting for an eligible published release"
		}
	}
	if err := s.store.receipts(token, req.Receipts); err != nil {
		storeError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
