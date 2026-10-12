package hub

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pilgrim13/rulesend/internal/core"
)

// SourceConfig is a local opt-in. It names no command, path, or credential that
// the Hub could ask a receiving Agent to execute or access.
type SourceConfig struct {
	Label        string           `json:"label"`
	InitialProof core.SourceProof `json:"initialProof"`
}

var (
	sourceCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sourceHashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sourceLabelPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,119}$`)
	errSourcePending    = errors.New("a source publication is already queued for this pack")
	errSourceConflict   = errors.New("source ownership changed; review before publishing")
)

func validSourceProof(p core.SourceProof) bool {
	return sourceCommitPattern.MatchString(p.Commit) && sourceHashPattern.MatchString(p.Hash) &&
		sourceLabelPattern.MatchString(p.ReleaseID) && sourceLabelPattern.MatchString(p.Version)
}

// NewServerWithSources retains the original reviewed anchor on every restart.
// Completed jobs advance the current proof in the database, never the anchor.
func NewServerWithSources(store *Store, publicURL string, sources map[string]SourceConfig) (http.Handler, error) {
	h, err := NewServer(store, publicURL)
	if err != nil {
		return h, err
	}
	if len(sources) == 0 {
		// Omitting an enabled local integration must not silently start a Hub
		// that accepts approvals but can no longer authenticate its publisher.
		var exists, owners int
		if err = store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='source_anchors'`).Scan(&exists); err != nil {
			return nil, errors.New("cannot verify source ownership")
		}
		if exists != 0 {
			if err = store.db.QueryRow(`SELECT COUNT(*) FROM source_anchors`).Scan(&owners); err != nil {
				return nil, errors.New("cannot verify source ownership")
			}
		}
		if owners != 0 {
			return nil, errors.New("source configuration is required for existing source-managed packs")
		}
		return h, nil
	}
	if err = store.configureSources(sources); err != nil {
		return nil, err
	}
	hash, err := store.sourceCredential()
	if err != nil {
		return nil, err
	}
	h.(*server).sourceHash = hash
	return h, nil
}

func (s *Store) SourceTokenPath() string { return filepath.Join(s.dir, "source-token") }

func (s *Store) sourceCredential() (string, error) {
	var expected string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key='source_hash'`).Scan(&expected)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("cannot read publisher identity")
	}
	initialized := err == nil
	path := s.SourceTokenPath()
	if info, statErr := os.Lstat(path); statErr == nil && !info.Mode().IsRegular() {
		return "", errors.New("publisher token must be a regular file")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if initialized || !errors.Is(readErr, os.ErrNotExist) {
			return "", errors.New("publisher token unavailable; restore the original private file")
		}
		token, genErr := core.Token()
		if genErr != nil {
			return "", errors.New("cannot generate publisher credentials")
		}
		f, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			return "", errors.New("cannot create publisher token")
		}
		_, writeErr := f.WriteString(token + "\n")
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return "", errors.New("cannot save publisher token")
		}
		data = []byte(token)
	}
	token := strings.TrimSpace(string(data))
	if !validToken(token) {
		return "", errors.New("invalid publisher token file")
	}
	if err = os.Chmod(path, 0600); err != nil {
		return "", errors.New("cannot protect publisher token")
	}
	hash := core.Hash([]byte(token))
	if initialized {
		if subtle.ConstantTimeCompare([]byte(expected), []byte(hash)) != 1 {
			return "", errors.New("publisher token does not match its recorded identity")
		}
	} else if _, err = s.db.Exec(`INSERT INTO settings(key,value) VALUES('source_hash',?)`, hash); err != nil {
		return "", errors.New("cannot initialize publisher identity")
	}
	return hash, nil
}

func (s *Store) configureSources(sources map[string]SourceConfig) error {
	for id, config := range sources {
		if !core.ValidID(id) || !textField(config.Label, 120, true) || !validSourceProof(config.InitialProof) {
			return errors.New("invalid source configuration")
		}
	}
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS source_anchors (pack_id TEXT PRIMARY KEY REFERENCES packs(id), data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS source_jobs (id TEXT PRIMARY KEY, pack_id TEXT NOT NULL REFERENCES packs(id), status TEXT NOT NULL, data TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS source_one_pending ON source_jobs(pack_id) WHERE status IN ('pending','failed');`)
	if err != nil {
		return errors.New("cannot initialize source publications")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT pack_id FROM source_anchors`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		if _, exists := sources[id]; !exists {
			err = errors.New("source configuration omitted an existing owner")
			break
		}
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	for id, config := range sources {
		p, e := loadPack(tx, id)
		if e != nil {
			return errors.New("source pack is unavailable")
		}
		var anchor string
		e = tx.QueryRow(`SELECT data FROM source_anchors WHERE pack_id=?`, id).Scan(&anchor)
		if e == nil {
			if anchor != encode(config) || p.Source == nil || !validSourceProof(p.Source.Proof) || p.PublishedHash != p.Source.Proof.Hash {
				return errors.New("source anchor or ownership differs; review the configuration")
			}
			continue
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if p.Source != nil || p.PublishedVersion < 1 || p.PublishedHash != config.InitialProof.Hash {
			return errors.New("initial source proof must match an existing published pack")
		}
		r, e := loadRelease(tx, id, p.PublishedVersion)
		if e != nil || core.Hash([]byte(r.Content)) != config.InitialProof.Hash {
			return errors.New("initial published content differs from source proof")
		}
		p.Source = &core.PackSource{Label: config.Label, Proof: config.InitialProof, Status: "idle"}
		p.UpdatedAt = now()
		if _, e = tx.Exec(`INSERT INTO source_anchors(pack_id,data) VALUES(?,?)`, id, encode(config)); e != nil {
			return e
		}
		if _, e = tx.Exec(`UPDATE packs SET data=? WHERE id=?`, encode(p), id); e != nil {
			return e
		}
		if e = audit(tx, "source.configured", "operator", id); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func loadSourceJob(q querier, id string) (core.SourcePublication, error) {
	var job core.SourcePublication
	var data string
	err := q.QueryRow(`SELECT data FROM source_jobs WHERE id=?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return job, errMissing
	}
	if err != nil {
		return job, err
	}
	err = json.Unmarshal([]byte(data), &job)
	return job, err
}

func saveSourcePack(tx *sql.Tx, p core.Pack, job core.SourcePublication, event string) error {
	p.UpdatedAt = now()
	if _, err := tx.Exec(`UPDATE packs SET data=? WHERE id=?`, encode(p), p.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE source_jobs SET status=?,data=? WHERE id=?`, job.Status, encode(job), job.ID); err != nil {
		return err
	}
	return audit(tx, event, "publisher", job.ID)
}

func (s *Store) queueSource(id string, base int64, message string) (core.SourcePublication, error) {
	var job core.SourcePublication
	tx, err := s.db.Begin()
	if err != nil {
		return job, err
	}
	defer tx.Rollback()
	p, err := loadPack(tx, id)
	if err != nil {
		return job, err
	}
	if p.Source == nil {
		return job, errForbidden
	}
	if p.Source.Pending != "" {
		return job, errSourcePending
	}
	if p.DraftRevision != base {
		return job, errConflict
	}
	jobID, err := newID("pub_")
	if err != nil {
		return job, err
	}
	job = core.SourcePublication{ID: jobID, PackID: id, Content: p.Content, Hash: core.Hash([]byte(p.Content)), Message: message,
		BaseDraftRevision: base, BaseSourceProof: p.Source.Proof, BasePublishedVersion: p.PublishedVersion,
		BasePublishedHash: p.PublishedHash, Status: "pending", CreatedAt: now()}
	p.Source.Pending = job.ID
	p.Source.Status = "pending"
	p.Source.Error = ""
	if _, err = tx.Exec(`INSERT INTO source_jobs(id,pack_id,status,data) VALUES(?,?,?,?)`, job.ID, id, job.Status, encode(job)); err != nil {
		return job, err
	}
	if err = saveSourcePack(tx, p, job, "source.requested"); err != nil {
		return job, err
	}
	return job, tx.Commit()
}

func sourceJobMatches(p core.Pack, job core.SourcePublication) bool {
	return p.Source != nil && p.Source.Pending == job.ID && p.Source.Proof == job.BaseSourceProof &&
		p.PublishedVersion == job.BasePublishedVersion && p.PublishedHash == job.BasePublishedHash &&
		core.Hash([]byte(job.Content)) == job.Hash
}

func (s *Store) completeSource(id string, proof core.SourceProof, parent string) (core.Release, error) {
	var release core.Release
	if !validSourceProof(proof) || !sourceCommitPattern.MatchString(parent) {
		return release, badInput("invalid source proof")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return release, err
	}
	defer tx.Rollback()
	job, err := loadSourceJob(tx, id)
	if err != nil {
		return release, err
	}
	if parent != job.BaseSourceProof.Commit || proof.Hash != job.Hash {
		return release, errSourceConflict
	}
	if job.Status == "completed" {
		if job.Proof == nil || *job.Proof != proof {
			return release, errSourceConflict
		}
		return loadRelease(tx, job.PackID, job.ReleaseVersion)
	}
	if job.Status != "pending" && job.Status != "failed" {
		return release, errSourceConflict
	}
	p, err := loadPack(tx, job.PackID)
	if err != nil {
		return release, err
	}
	if !sourceJobMatches(p, job) || (proof.Commit == job.BaseSourceProof.Commit && proof != job.BaseSourceProof) {
		return release, errSourceConflict
	}
	release = core.Release{PackID: p.ID, Version: p.PublishedVersion + 1, Content: job.Content, Hash: job.Hash, Message: job.Message, CreatedAt: now(), Source: &proof}
	if _, err = tx.Exec(`INSERT INTO releases(pack_id,version,data) VALUES(?,?,?)`, p.ID, release.Version, encode(release)); err != nil {
		return release, err
	}
	p.PublishedVersion = release.Version
	p.PublishedHash = release.Hash
	p.DraftRevision++
	p.Source.Proof = proof
	p.Source.Pending = ""
	p.Source.Status = "idle"
	p.Source.Error = ""
	job.Status = "completed"
	job.Error = ""
	job.CompletedAt = now()
	job.ReleaseVersion = release.Version
	job.Proof = &proof
	if err = saveSourcePack(tx, p, job, "source.published"); err != nil {
		return release, err
	}
	return release, tx.Commit()
}

func (s *Store) failSource(id, reason string) (core.SourcePublication, error) {
	var job core.SourcePublication
	if reason != "source_publish_failed" && reason != "source_content_rejected" {
		return job, badInput("invalid publication failure reason")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return job, err
	}
	defer tx.Rollback()
	job, err = loadSourceJob(tx, id)
	if err != nil {
		return job, err
	}
	if job.Status == "rejected" && job.Error == reason {
		return job, nil
	}
	if job.Status == "completed" || job.Status == "rejected" {
		return job, errSourceConflict
	}
	p, err := loadPack(tx, job.PackID)
	if err != nil {
		return job, err
	}
	if !sourceJobMatches(p, job) {
		return job, errSourceConflict
	}
	job.Status = "failed"
	job.Error = reason
	p.Source.Status = "failed"
	p.Source.Error = reason
	if reason == "source_content_rejected" {
		job.Status = "rejected"
		job.CompletedAt = now()
		p.Source.Pending = ""
	}
	if err = saveSourcePack(tx, p, job, "source.failed"); err != nil {
		return job, err
	}
	return job, tx.Commit()
}

// Metadata snapshots omit frozen text. Only the dedicated publisher can fetch
// pending job bodies; the browser already has the editable draft and releases.
func readSourcePublications(tx *sql.Tx, metadata bool) ([]core.SourcePublication, error) {
	jobs := []core.SourcePublication{}
	query := `SELECT data FROM source_jobs WHERE status IN ('pending','failed') ORDER BY rowid`
	if metadata {
		query = `SELECT data FROM source_jobs ORDER BY rowid DESC LIMIT 100`
	}
	rows, err := tx.Query(query)
	if err != nil {
		return jobs, err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var job core.SourcePublication
		if err = rows.Scan(&data); err != nil {
			return jobs, err
		}
		if err = json.Unmarshal([]byte(data), &job); err != nil {
			return jobs, err
		}
		if metadata {
			job.Content = ""
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) pendingSourceJobs() ([]core.SourcePublication, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	jobs, err := readSourcePublications(tx, false)
	if err != nil {
		return nil, err
	}
	return jobs, tx.Commit()
}

func sourceError(w http.ResponseWriter, err error) {
	if errors.Is(err, errSourcePending) || errors.Is(err, errSourceConflict) {
		apiError(w, 409, err.Error())
		return
	}
	storeError(w, err)
}

func (s *server) sourceAPI(w http.ResponseWriter, r *http.Request) {
	// No cookies or browser Origin are accepted for this separate machine role.
	token := bearer(r)
	if s.sourceHash == "" || r.Header.Get("Origin") != "" || !s.sameOrigin(r, false) || !validToken(token) ||
		subtle.ConstantTimeCompare([]byte(core.Hash([]byte(token))), []byte(s.sourceHash)) != 1 {
		apiError(w, 401, "publisher authentication required")
		return
	}
	if r.URL.Path == "/api/source/jobs" && r.Method == http.MethodGet {
		jobs, err := s.store.pendingSourceJobs()
		if err != nil {
			sourceError(w, err)
			return
		}
		// Keep an explicit content field even for an approved empty draft. Metadata
		// snapshots omit it, but a publisher must receive the exact frozen bytes.
		type frozenJob struct {
			core.SourcePublication
			Content string `json:"content"`
		}
		frozen := make([]frozenJob, 0, len(jobs))
		for _, job := range jobs {
			frozen = append(frozen, frozenJob{job, job.Content})
		}
		jsonResponse(w, 200, map[string]any{"jobs": frozen})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/source/"), "/")
	if len(parts) != 3 || parts[0] != "jobs" || !core.ValidID(parts[1]) || r.Method != http.MethodPost {
		apiError(w, 404, "not found")
		return
	}
	switch parts[2] {
	case "complete":
		var req struct {
			Proof        core.SourceProof `json:"proof"`
			ParentCommit string           `json:"parentCommit"`
		}
		if !decode(w, r, &req) {
			return
		}
		release, err := s.store.completeSource(parts[1], req.Proof, req.ParentCommit)
		if err != nil {
			sourceError(w, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"release": release})
	case "fail":
		var req struct {
			Reason string `json:"reason"`
		}
		if !decode(w, r, &req) {
			return
		}
		job, err := s.store.failSource(parts[1], req.Reason)
		if err != nil {
			sourceError(w, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"publication": job})
	default:
		apiError(w, 404, "not found")
	}
}
