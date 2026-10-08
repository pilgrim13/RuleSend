package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/pilgrim13/rulesend/internal/core"
)

type Journal struct {
	TargetID  string      `json:"targetId"`
	PreHash   string      `json:"preHash"`
	PreExists bool        `json:"preExists"`
	PostHash  string      `json:"postHash"`
	Backup    string      `json:"backup"`
	Next      TargetState `json:"next"`
}

func (a *Agent) journalPath(t Target) string { return filepath.Join(a.Dir, "journal-"+t.ID+".json") }

// reconcile never guesses: only the exact pre-image or post-image is recoverable.
func (a *Agent) reconcile(t Target, root *os.Root, name string) error {
	var j Journal
	err := readJSON(a.journalPath(t), &j)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("cannot read recovery journal")
	}
	if j.TargetID != t.ID || j.Next.BlockHash == "" {
		return errors.New("invalid recovery journal")
	}
	b, _, exists, err := readTarget(root, name, targetLimit(t.Adapter))
	if err != nil {
		return err
	}
	if exists && core.Hash(b) == j.PostHash {
		m, err := parseBlock(b)
		if err != nil || m == nil || m.PackID != t.PackID || m.BlockHash != j.Next.BlockHash {
			return ErrConflict
		}
		a.State.Targets[t.ID] = j.Next
		if err = a.saveState(); err != nil {
			return err
		}
	} else if exists != j.PreExists || core.Hash(b) != j.PreHash {
		return errors.New("interrupted write needs review; recovery backup preserved")
	}
	if err = os.Remove(a.journalPath(t)); err != nil {
		return err
	}
	return syncDir(a.Dir)
}

func (a *Agent) apply(t Target, r core.Release) (TargetState, error) {
	previous := a.State.Targets[t.ID]
	root, name, err := openTarget(t.Path)
	if err != nil {
		return previous, err
	}
	defer root.Close()
	if err = a.reconcile(t, root, name); err != nil {
		return previous, err
	}
	previous = a.State.Targets[t.ID]
	old, mode, exists, err := readTarget(root, name, targetLimit(t.Adapter))
	if err != nil {
		return previous, err
	}
	next, err := compose(old, t, previous, r)
	if err != nil {
		return previous, err
	}
	m, err := parseBlock(next)
	if err != nil || m == nil {
		return previous, errors.New("invalid generated block")
	}
	result := previous
	result.Version = r.Version
	result.Hash = r.Hash
	result.BlockHash = m.BlockHash
	if bytes.Equal(old, next) {
		if err = verifyParent(root, t.Path); err != nil {
			return previous, err
		}
		return result, nil
	}

	nonce, err := core.Token()
	if err != nil {
		return previous, err
	}
	backupDir := filepath.Join(a.Dir, "backups")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		return previous, errors.New("cannot create backup directory")
	}
	backup := t.ID + "-" + time.Now().UTC().Format("20060102T150405") + "-" + nonce[:12] + ".md"
	if err = writePrivate(filepath.Join(backupDir, backup), old); err != nil {
		return previous, errors.New("cannot save recovery backup")
	}
	j := Journal{t.ID, core.Hash(old), exists, core.Hash(next), backup, result}
	if err = writeJSON(a.journalPath(t), j); err != nil {
		return previous, errors.New("cannot persist recovery journal")
	}
	tmp := ".rulesend-" + nonce[:24] + ".tmp"
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return previous, errors.New("cannot create replacement file")
	}
	defer root.Remove(tmp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(next)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return previous, errors.New("cannot persist replacement file")
	}
	fresh, freshMode, freshExists, err := readTarget(root, name, targetLimit(t.Adapter))
	if err != nil {
		return previous, err
	}
	if freshExists != exists || freshMode != mode || !bytes.Equal(fresh, old) {
		return previous, ErrConflict
	}
	if err = root.Rename(tmp, name); err != nil {
		return previous, errors.New("cannot replace target file")
	}
	// Sync the same held directory, not a potentially replaced pathname.
	d, err := root.Open(".")
	if err != nil {
		return previous, errors.New("cannot sync target directory")
	}
	err = d.Sync()
	_ = d.Close()
	if err != nil {
		return previous, errors.New("cannot sync target directory")
	}
	actual, _, present, err := readTarget(root, name, targetLimit(t.Adapter))
	if err != nil || !present || !bytes.Equal(actual, next) {
		return previous, errors.New("target changed during verification; backup preserved")
	}
	if err = verifyParent(root, t.Path); err != nil {
		return previous, err
	}
	a.State.Targets[t.ID] = result
	if err = a.saveState(); err != nil {
		return previous, errors.New("file written; recovery journal retained because state save failed")
	}
	if err = os.Remove(a.journalPath(t)); err != nil {
		return result, errors.New("file written; recovery journal cleanup pending")
	}
	if err = syncDir(a.Dir); err != nil {
		return result, errors.New("cannot persist journal cleanup")
	}
	return result, nil
}

// verify observes local drift even while a pack is paused or waits for a schedule.
func (a *Agent) verify(t Target) error {
	root, name, err := openTarget(t.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = a.reconcile(t, root, name); err != nil {
		return err
	}
	s := a.State.Targets[t.ID]
	b, _, exists, err := readTarget(root, name, targetLimit(t.Adapter))
	if err != nil {
		return err
	}
	m, err := parseBlock(b)
	if err != nil {
		return err
	}
	if s.BlockHash == "" {
		if m != nil {
			return ErrConflict
		}
		return verifyParent(root, t.Path)
	}
	if !exists || m == nil || m.PackID != t.PackID || m.BlockHash != s.BlockHash {
		return ErrConflict
	}
	return verifyParent(root, t.Path)
}

func verifyParent(root *os.Root, path string) error {
	held, err := root.Stat(".")
	if err != nil {
		return errors.New("cannot verify target parent")
	}
	current, err := os.Lstat(filepath.Dir(path))
	if err != nil || !current.IsDir() || !os.SameFile(held, current) {
		return errors.New("target parent changed during observation")
	}
	return nil
}
