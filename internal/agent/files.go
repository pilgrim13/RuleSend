package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pilgrim13/rulesend/internal/core"
)

var ErrConflict = errors.New("local rule conflict; preserve and review the local file")
var marker = regexp.MustCompile(`^<!-- rulesend:begin pack=([a-zA-Z0-9][a-zA-Z0-9_-]{0,63}) version=([1-9][0-9]*) hash=([a-f0-9]{64}) -->\n`)

const endMarker = "\n<!-- rulesend:end -->"

type managed struct {
	Start, End      int
	PackID          string
	Version         int64
	Hash, BlockHash string
}

func parseBlock(b []byte) (*managed, error) {
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return nil, errors.New("target is not UTF-8 text")
	}
	count := bytes.Count(b, []byte("<!-- rulesend:"))
	if count == 0 {
		return nil, nil
	}
	if count != 2 {
		return nil, ErrConflict
	}
	start := bytes.Index(b, []byte("<!-- rulesend:begin"))
	if start < 0 {
		return nil, ErrConflict
	}
	match := marker.FindSubmatch(b[start:])
	if match == nil {
		return nil, ErrConflict
	}
	bodyStart := start + len(match[0])
	tail := bytes.Index(b[bodyStart:], []byte(endMarker))
	if tail < 0 {
		return nil, ErrConflict
	}
	end := bodyStart + tail + len(endMarker)
	version, err := strconv.ParseInt(string(match[2]), 10, 64)
	if err != nil {
		return nil, ErrConflict
	}
	if core.Hash(b[bodyStart:bodyStart+tail]) != string(match[3]) {
		return nil, ErrConflict
	}
	return &managed{start, end, string(match[1]), version, string(match[3]), core.Hash(b[start:end])}, nil
}

func block(r core.Release) []byte {
	return []byte(fmt.Sprintf("<!-- rulesend:begin pack=%s version=%d hash=%s -->\n%s%s", r.PackID, r.Version, r.Hash, r.Content, endMarker))
}

func targetLimit(adapter string) int {
	if adapter == "codex" {
		return 32 * 1024
	}
	return core.MaxContentBytes
}

// compose only replaces the verified owned span. Unrelated bytes are never normalized.
func compose(old []byte, t Target, previous TargetState, r core.Release) ([]byte, error) {
	if !core.ValidID(r.PackID) || r.PackID != t.PackID || r.Version < 1 || core.Hash([]byte(r.Content)) != r.Hash {
		return nil, errors.New("invalid published release")
	}
	if err := core.ValidateContent(r.Content); err != nil {
		return nil, err
	}
	m, err := parseBlock(old)
	if err != nil {
		return nil, err
	}
	replacement := block(r)
	var next []byte
	if m == nil {
		if previous.BlockHash != "" {
			return nil, ErrConflict
		}
		next = append(next, old...)
		if len(old) > 0 && old[len(old)-1] != '\n' {
			next = append(next, '\n')
		}
		next = append(next, replacement...)
		next = append(next, '\n')
	} else {
		if m.PackID != t.PackID || previous.BlockHash == "" || m.BlockHash != previous.BlockHash {
			return nil, ErrConflict
		}
		if r.Version < previous.Version || (r.Version == previous.Version && previous.Hash != "" && r.Hash != previous.Hash) {
			return nil, errors.New("Hub history differs from the locally verified version")
		}
		next = append(next, old[:m.Start]...)
		next = append(next, replacement...)
		next = append(next, old[m.End:]...)
	}
	if len(next) > targetLimit(t.Adapter) {
		return nil, errors.New("result exceeds this adapter's file size limit")
	}
	return next, nil
}

// Root confines all accesses to the approved physical parent. No-follow rejects
// target symlinks instead of adopting the file to which they happen to point.
func openTarget(path string) (*os.Root, string, error) {
	if !filepath.IsAbs(path) {
		return nil, "", errors.New("target path must be absolute")
	}
	parent := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, "", errors.New("target parent is unavailable")
	}
	if resolved != parent {
		return nil, "", errors.New("target parent changed or contains a symlink; rebind the physical path")
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, "", errors.New("cannot open target parent")
	}
	return root, filepath.Base(path), nil
}

func readTarget(root *os.Root, name string, limit int) ([]byte, os.FileMode, bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return []byte{}, 0600, false, nil
	}
	if err != nil {
		return nil, 0, false, errors.New("cannot inspect target")
	}
	if !info.Mode().IsRegular() {
		return nil, 0, false, errors.New("target must be a regular file, not a symlink or special file")
	}
	f, err := openNoFollow(root, name)
	if err != nil {
		return nil, 0, false, errors.New("cannot safely open target")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, 0, false, ErrConflict
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit+1)))
	if err != nil {
		return nil, 0, false, errors.New("cannot read target")
	}
	if len(b) > limit {
		return nil, 0, false, errors.New("target exceeds adapter size limit")
	}
	return b, info.Mode().Perm(), true, nil
}

func privateDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return "", errors.New("cannot create private data directory")
	}
	st, err := os.Lstat(abs)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("data directory must be a real directory")
	}
	if err = os.Chmod(abs, 0700); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func writePrivate(path string, b []byte) error {
	if st, err := os.Lstat(path); err == nil && (!st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0) {
		return errors.New("private state is not a regular file")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".rulesend-state-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, append(b, '\n'))
}
func readJSON(path string, value any) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return errors.New("private state must be a regular owner-only file")
	}
	if st.Size() > 4*1024*1024 {
		return errors.New("private state is too large")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func canonicalTarget(path, dataDir string) (string, error) {
	if strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
		return "", errors.New("approve an absolute target path")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return "", errors.New("parent traversal is not allowed")
		}
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", errors.New("create the target's parent directory before binding")
	}
	clean := filepath.Join(parent, filepath.Base(path))
	rel, err := filepath.Rel(dataDir, clean)
	if err != nil || rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		return "", errors.New("agent state cannot be an application target")
	}
	return clean, nil
}
