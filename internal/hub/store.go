// Package hub serves the single-administrator RuleSend control plane.
package hub

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pilgrim13/rulesend/internal/core"
	_ "modernc.org/sqlite"
)

var (
	errMissing      = errors.New("not found")
	errConflict     = errors.New("the draft changed; reload before trying again")
	errUnauthorized = errors.New("authentication required")
	errForbidden    = errors.New("access denied")
)

type Store struct {
	db        *sql.DB
	dir       string
	adminHash string
}

// Open creates a private database and initial administrator token. Losing an
// existing token file is an error; it never silently changes administrator identity.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, errors.New("cannot create Hub data directory")
	}
	info, err := os.Lstat(dataDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Hub data directory must be a real directory")
	}
	if err := os.Chmod(dataDir, 0700); err != nil {
		return nil, errors.New("cannot protect Hub data directory")
	}
	dbPath := filepath.Join(dataDir, "hub.db")
	f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		_ = f.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return nil, errors.New("cannot create Hub database")
	}
	info, err = os.Lstat(dbPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("Hub database must be a regular file")
	}
	if err := os.Chmod(dbPath, 0600); err != nil {
		return nil, errors.New("cannot protect Hub database")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, errors.New("cannot open Hub database")
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, dir: dataDir}
	ok := false
	defer func() {
		if !ok {
			_ = db.Close()
		}
	}()
	_, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=DELETE;
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS packs (id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS releases (pack_id TEXT NOT NULL REFERENCES packs(id), version INTEGER NOT NULL, data TEXT NOT NULL, PRIMARY KEY(pack_id,version));
CREATE TRIGGER IF NOT EXISTS immutable_release_update BEFORE UPDATE ON releases BEGIN SELECT RAISE(ABORT,'immutable release'); END;
CREATE TRIGGER IF NOT EXISTS immutable_release_delete BEFORE DELETE ON releases BEGIN SELECT RAISE(ABORT,'immutable release'); END;
CREATE TABLE IF NOT EXISTS devices (id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS enrollments (token_hash TEXT PRIMARY KEY, pack_ids TEXT NOT NULL, expires INTEGER NOT NULL, used INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS receipts (device_id TEXT NOT NULL REFERENCES devices(id), target_id TEXT NOT NULL, data TEXT NOT NULL, PRIMARY KEY(device_id,target_id));
CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, actor TEXT NOT NULL, message TEXT NOT NULL, created_at TEXT NOT NULL);`)
	if err != nil {
		return nil, errors.New("cannot initialize Hub database")
	}
	err = db.QueryRow(`SELECT value FROM settings WHERE key='admin_hash'`).Scan(&s.adminHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("cannot read Hub credentials")
	}
	initialized := err == nil
	tokenPath := s.AdminTokenPath()
	if info, err = os.Lstat(tokenPath); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("administrator token must be a regular file")
	}
	tokenBytes, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		if initialized || !errors.Is(readErr, os.ErrNotExist) {
			return nil, errors.New("administrator token unavailable; restore the original private token file")
		}
		token, err := core.Token()
		if err != nil {
			return nil, errors.New("cannot generate credentials")
		}
		f, err := os.OpenFile(tokenPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, errors.New("cannot create administrator token")
		}
		_, writeErr := f.WriteString(token + "\n")
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return nil, errors.New("cannot save administrator token")
		}
		tokenBytes = []byte(token)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if !validToken(token) {
		return nil, errors.New("invalid administrator token file")
	}
	if err := os.Chmod(tokenPath, 0600); err != nil {
		return nil, errors.New("cannot protect administrator token")
	}
	hash := core.Hash([]byte(token))
	if initialized {
		if subtle.ConstantTimeCompare([]byte(s.adminHash), []byte(hash)) != 1 {
			return nil, errors.New("administrator token does not match existing Hub identity")
		}
	} else {
		if _, err := db.Exec(`INSERT INTO settings(key,value) VALUES('admin_hash',?)`, hash); err != nil {
			return nil, errors.New("cannot initialize administrator identity")
		}
		s.adminHash = hash
	}
	ok = true
	return s, nil
}

func (s *Store) Close() error           { return s.db.Close() }
func (s *Store) AdminTokenPath() string { return filepath.Join(s.dir, "admin-token") }
func now() string                       { return time.Now().UTC().Format(time.RFC3339Nano) }
func validToken(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func newID(prefix string) (string, error) {
	t, e := core.Token()
	if e != nil {
		return "", e
	}
	return prefix + t[:24], nil
}
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }

type querier interface{ QueryRow(string, ...any) *sql.Row }

func loadPack(q querier, id string) (core.Pack, error) {
	var p core.Pack
	var data string
	err := q.QueryRow(`SELECT data FROM packs WHERE id=?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return p, errMissing
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(data), &p)
	return p, err
}
func loadRelease(q querier, id string, version int64) (core.Release, error) {
	var r core.Release
	var data string
	err := q.QueryRow(`SELECT data FROM releases WHERE pack_id=? AND version=?`, id, version).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errMissing
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(data), &r)
	return r, err
}
func audit(tx *sql.Tx, kind, actor, message string) error {
	_, err := tx.Exec(`INSERT INTO events(kind,actor,message,created_at) VALUES(?,?,?,?)`, kind, actor, message, now())
	if err != nil {
		return err
	}
	// Audit messages contain identifiers and fixed descriptions, never content or tokens.
	_, err = tx.Exec(`DELETE FROM events WHERE id <= (SELECT COALESCE(MAX(id),0)-1000 FROM events)`)
	return err
}
func (s *Store) createPack(name, description, content string) (core.Pack, error) {
	id, err := newID("pack_")
	if err != nil {
		return core.Pack{}, err
	}
	p := core.Pack{ID: id, Name: name, Description: description, Content: content, DraftRevision: 1, Mode: "manual", IntervalSeconds: 60, UpdatedAt: now()}
	tx, err := s.db.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO packs(id,data) VALUES(?,?)`, p.ID, encode(p)); err != nil {
		return p, err
	}
	if err = audit(tx, "pack.created", "admin", p.ID); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func (s *Store) changePack(id string, change func(*core.Pack, *sql.Tx) error, kind string) (core.Pack, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return core.Pack{}, err
	}
	defer tx.Rollback()
	p, err := loadPack(tx, id)
	if err != nil {
		return p, err
	}
	if err = change(&p, tx); err != nil {
		return p, err
	}
	p.UpdatedAt = now()
	if _, err = tx.Exec(`UPDATE packs SET data=? WHERE id=?`, encode(p), id); err != nil {
		return p, err
	}
	if err = audit(tx, kind, "admin", id); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func (s *Store) draft(id string, base int64, content string) (core.Pack, error) {
	return s.changePack(id, func(p *core.Pack, _ *sql.Tx) error {
		if p.DraftRevision != base {
			return errConflict
		}
		p.Content = content
		p.DraftRevision++
		return nil
	}, "draft.saved")
}
func (s *Store) publish(id string, base int64, message string) (core.Release, error) {
	var r core.Release
	_, err := s.changePack(id, func(p *core.Pack, tx *sql.Tx) error {
		if p.DraftRevision != base {
			return errConflict
		}
		r = core.Release{PackID: id, Version: p.PublishedVersion + 1, Content: p.Content, Hash: core.Hash([]byte(p.Content)), Message: message, CreatedAt: now()}
		if _, err := tx.Exec(`INSERT INTO releases(pack_id,version,data) VALUES(?,?,?)`, id, r.Version, encode(r)); err != nil {
			return err
		}
		p.PublishedVersion = r.Version
		p.PublishedHash = r.Hash
		p.DraftRevision++
		return nil
	}, "release.published")
	return r, err
}
func (s *Store) restore(id string, base, version int64) (core.Pack, error) {
	return s.changePack(id, func(p *core.Pack, tx *sql.Tx) error {
		if p.DraftRevision != base {
			return errConflict
		}
		r, err := loadRelease(tx, id, version)
		if err != nil {
			return err
		}
		p.Content = r.Content
		p.DraftRevision++
		return nil
	}, "draft.restored")
}
func (s *Store) releaseList(id string) ([]core.Release, error) {
	rows, err := s.db.Query(`SELECT data FROM releases WHERE pack_id=? ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []core.Release{}
	for rows.Next() {
		var data string
		var r core.Release
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) snapshot() (core.Snapshot, error) {
	out := core.Snapshot{Packs: []core.Pack{}, Devices: []core.Device{}, Receipts: []core.Receipt{}, Events: []core.Event{}}
	// A read transaction keeps the dashboard's versions and observations consistent.
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	for _, item := range []struct {
		query string
		add   func(string) error
	}{
		{`SELECT data FROM packs ORDER BY id`, func(data string) error {
			var v core.Pack
			e := json.Unmarshal([]byte(data), &v)
			out.Packs = append(out.Packs, v)
			return e
		}},
		{`SELECT data FROM devices ORDER BY id`, func(data string) error {
			var v core.Device
			e := json.Unmarshal([]byte(data), &v)
			out.Devices = append(out.Devices, v)
			return e
		}},
		{`SELECT data FROM receipts ORDER BY device_id,target_id`, func(data string) error {
			var v core.Receipt
			e := json.Unmarshal([]byte(data), &v)
			out.Receipts = append(out.Receipts, v)
			return e
		}},
	} {
		rows, e := tx.Query(item.query)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var data string
			if e = rows.Scan(&data); e != nil {
				break
			}
			if e = item.add(data); e != nil {
				break
			}
		}
		rowErr := rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if rowErr != nil {
			return out, rowErr
		}
	}
	rows, err := tx.Query(`SELECT id,kind,actor,message,created_at FROM events ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var e core.Event
		if err = rows.Scan(&e.ID, &e.Kind, &e.Actor, &e.Message, &e.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Events = append(out.Events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) enrollment(packIDs []string, minutes int) (string, string, error) {
	token, err := core.Token()
	if err != nil {
		return "", "", err
	}
	expires := time.Now().UTC().Add(time.Duration(minutes) * time.Minute)
	tx, err := s.db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	for _, id := range packIDs {
		if _, err = loadPack(tx, id); err != nil {
			return "", "", err
		}
	}
	_, err = tx.Exec(`INSERT INTO enrollments(token_hash,pack_ids,expires) VALUES(?,?,?)`, core.Hash([]byte(token)), encode(packIDs), expires.Unix())
	if err != nil {
		return "", "", err
	}
	if err = audit(tx, "enrollment.created", "admin", fmt.Sprintf("Scoped registration for %d pack(s)", len(packIDs))); err != nil {
		return "", "", err
	}
	// Used/expired capabilities are no longer needed after one day.
	if _, err = tx.Exec(`DELETE FROM enrollments WHERE expires<?`, time.Now().Add(-24*time.Hour).Unix()); err != nil {
		return "", "", err
	}
	err = tx.Commit()
	return token, expires.Format(time.RFC3339), err
}
func (s *Store) enroll(token, name, platform string) (core.Device, string, error) {
	var d core.Device
	deviceToken, err := core.Token()
	if err != nil {
		return d, "", err
	}
	id, err := newID("dev_")
	if err != nil {
		return d, "", err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return d, "", err
	}
	defer tx.Rollback()
	var packs string
	err = tx.QueryRow(`SELECT pack_ids FROM enrollments WHERE token_hash=? AND used=0 AND expires>?`, core.Hash([]byte(token)), time.Now().Unix()).Scan(&packs)
	if errors.Is(err, sql.ErrNoRows) {
		return d, "", errUnauthorized
	}
	if err != nil {
		return d, "", err
	}
	d = core.Device{ID: id, Name: name, Platform: platform, LastSeen: now()}
	if err = json.Unmarshal([]byte(packs), &d.PackIDs); err != nil {
		return d, "", err
	}
	result, err := tx.Exec(`UPDATE enrollments SET used=1 WHERE token_hash=? AND used=0 AND expires>?`, core.Hash([]byte(token)), time.Now().Unix())
	if err != nil {
		return d, "", err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return d, "", err
	}
	if count != 1 {
		return d, "", errUnauthorized
	}
	if _, err = tx.Exec(`INSERT INTO devices(id,token_hash,data) VALUES(?,?,?)`, d.ID, core.Hash([]byte(deviceToken)), encode(d)); err != nil {
		return d, "", err
	}
	if err = audit(tx, "device.enrolled", d.ID, "Device registered"); err != nil {
		return d, "", err
	}
	return d, deviceToken, tx.Commit()
}
func deviceFor(q querier, token string) (core.Device, error) {
	var d core.Device
	var data string
	err := q.QueryRow(`SELECT data FROM devices WHERE token_hash=?`, core.Hash([]byte(token))).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return d, errUnauthorized
	}
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal([]byte(data), &d); err != nil {
		return d, err
	}
	if d.Revoked {
		return d, errUnauthorized
	}
	return d, nil
}
func (s *Store) revoke(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var data string
	var d core.Device
	err = tx.QueryRow(`SELECT data FROM devices WHERE id=?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return errMissing
	}
	if err != nil {
		return err
	}
	if err = json.Unmarshal([]byte(data), &d); err != nil {
		return err
	}
	d.Revoked = true
	if _, err = tx.Exec(`UPDATE devices SET data=? WHERE id=?`, encode(d), id); err != nil {
		return err
	}
	if err = audit(tx, "device.revoked", "admin", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) poll(token string) (core.PollResponse, error) {
	out := core.PollResponse{Plans: []core.Plan{}}
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	d, err := deviceFor(tx, token)
	if err != nil {
		return out, err
	}
	d.LastSeen = now()
	out.Device = d
	for _, id := range d.PackIDs {
		p, err := loadPack(tx, id)
		if err != nil {
			return out, err
		}
		plan := core.Plan{Pack: core.Pack{ID: p.ID, Name: p.Name, Mode: p.Mode, IntervalSeconds: p.IntervalSeconds, Paused: p.Paused, Trigger: p.Trigger}}
		if p.PublishedVersion > 0 {
			r, err := loadRelease(tx, id, p.PublishedVersion)
			if err != nil {
				return out, err
			}
			plan.Release = &r
		}
		out.Plans = append(out.Plans, plan)
	}
	if _, err = tx.Exec(`UPDATE devices SET data=? WHERE id=?`, encode(d), d.ID); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func contains(list []string, value string) bool {
	for _, s := range list {
		if s == value {
			return true
		}
	}
	return false
}
func (s *Store) receipts(token string, receipts []core.Receipt) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	d, err := deviceFor(tx, token)
	if err != nil {
		return err
	}
	for _, r := range receipts {
		if !contains(d.PackIDs, r.PackID) {
			return errForbidden
		}
		if r.DeviceID != "" && r.DeviceID != d.ID {
			return errForbidden
		}
		if r.Version > 0 {
			rel, err := loadRelease(tx, r.PackID, r.Version)
			if err != nil {
				return badInput("receipt must refer to a published version")
			}
			if r.Hash != rel.Hash {
				return badInput("receipt hash does not match the published version")
			}
		} else if r.Status == "applied" || r.Hash != "" {
			return badInput("applied receipts require a published version and hash")
		}
		// A target's older retry must never replace a newer recorded observation.
		var oldData string
		err = tx.QueryRow(`SELECT data FROM receipts WHERE device_id=? AND target_id=?`, d.ID, r.TargetID).Scan(&oldData)
		if err == nil {
			var old core.Receipt
			if err = json.Unmarshal([]byte(oldData), &old); err != nil {
				return err
			}
			if old.PackID != r.PackID {
				return badInput("target is already associated with another pack")
			}
			if r.Version < old.Version {
				return badInput("receipt is older than the recorded version")
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		} else {
			var count int
			if err = tx.QueryRow(`SELECT COUNT(*) FROM receipts WHERE device_id=?`, d.ID).Scan(&count); err != nil {
				return err
			}
			if count >= 100 {
				return badInput("a device can report at most 100 targets")
			}
		}
		r.DeviceID = d.ID
		r.CheckedAt = now()
		r.AppLoaded = "unverified"
		r.BehaviorVerified = "unverified"
		if _, err = tx.Exec(`INSERT INTO receipts(device_id,target_id,data) VALUES(?,?,?) ON CONFLICT(device_id,target_id) DO UPDATE SET data=excluded.data`, d.ID, r.TargetID, encode(r)); err != nil {
			return err
		}
	}
	d.LastSeen = now()
	if _, err = tx.Exec(`UPDATE devices SET data=? WHERE id=?`, encode(d), d.ID); err != nil {
		return err
	}
	return tx.Commit()
}
