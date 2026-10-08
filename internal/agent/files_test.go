//go:build darwin || linux

package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/pilgrim13/rulesend/internal/core"
)

func fsTestRelease(pack string, version int64, content string) core.Release {
	return core.Release{PackID: pack, Version: version, Content: content, Hash: core.Hash([]byte(content))}
}

func fsTestState(t *testing.T, r core.Release) TargetState {
	t.Helper()
	m, err := parseBlock(block(r))
	if err != nil || m == nil {
		t.Fatalf("parse generated fixture: %v", err)
	}
	return TargetState{Version: r.Version, Hash: r.Hash, BlockHash: m.BlockHash}
}

func fsTestPhysicalDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestComposePreservesUnrelatedBytes(t *testing.T) {
	first := fsTestRelease("rules", 1, "첫 번째 규칙\n")
	second := fsTestRelease("rules", 2, "두 번째 규칙\nKeep user changes.")
	prefix := []byte("# User rules\r\n  keep spaces  \r\n")
	suffix := []byte("\r\n# Local additions\nno final newline")
	old := bytes.Join([][]byte{prefix, block(first), suffix}, nil)
	before := append([]byte(nil), old...)
	got, err := compose(old, Target{PackID: "rules", Adapter: "codex"}, fsTestState(t, first), second)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Join([][]byte{prefix, block(second), suffix}, nil)
	if !bytes.Equal(got, want) {
		t.Fatal("replacement changed bytes outside the owned span")
	}
	if !bytes.Equal(old, before) {
		t.Fatal("compose mutated its input")
	}
}

func TestComposeInitialAppendPreservesExistingPrefix(t *testing.T) {
	r := fsTestRelease("rules", 1, "Use explicit evidence.")
	for _, original := range []string{"", "# Local\n", "# Local without newline", "# Local\r\n"} {
		t.Run(original, func(t *testing.T) {
			got, err := compose([]byte(original), Target{PackID: "rules", Adapter: "markdown"}, TargetState{}, r)
			if err != nil {
				t.Fatal(err)
			}
			prefix := original
			if prefix != "" && !strings.HasSuffix(prefix, "\n") {
				prefix += "\n"
			}
			want := prefix + string(block(r)) + "\n"
			if string(got) != want {
				t.Fatalf("initial append changed user bytes: got %q want %q", got, want)
			}
		})
	}
}

func TestComposePreservesLocalConflicts(t *testing.T) {
	first := fsTestRelease("rules", 1, "accepted content")
	next := fsTestRelease("rules", 2, "next content")
	previous := fsTestState(t, first)
	other := fsTestRelease("other", 1, "other pack")
	cases := []struct {
		name  string
		old   []byte
		state TargetState
	}{
		{"locally edited body", bytes.Replace(block(first), []byte("accepted content"), []byte("local content"), 1), previous},
		{"locally removed block", []byte("my remaining rules\n"), previous},
		{"untracked existing block", block(first), TargetState{}},
		{"different pack", block(other), fsTestState(t, other)},
		{"locally rewritten valid block", block(fsTestRelease("rules", 1, "rehash local edit")), previous},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]byte(nil), tc.old...)
			if _, err := compose(tc.old, Target{PackID: "rules", Adapter: "markdown"}, tc.state, next); !errors.Is(err, ErrConflict) {
				t.Fatalf("expected a conflict, got %v", err)
			}
			if !bytes.Equal(tc.old, before) {
				t.Fatal("local content was modified")
			}
		})
	}
}

func TestMalformedManagedBlocksAreRejected(t *testing.T) {
	valid := block(fsTestRelease("rules", 1, "body"))
	cases := map[string][]byte{
		"missing end":      valid[:len(valid)-len(endMarker)],
		"duplicate":        bytes.Join([][]byte{valid, valid}, []byte("\n")),
		"nested marker":    bytes.Replace(valid, []byte("body"), []byte("<!-- rulesend:begin body"), 1),
		"zero version":     bytes.Replace(valid, []byte("version=1"), []byte("version=0"), 1),
		"overflow version": bytes.Replace(valid, []byte("version=1"), []byte("version=99999999999999999999999"), 1),
		"unknown marker":   []byte("<!-- rulesend:unknown -->"),
		"mismatched body":  bytes.Replace(valid, []byte("body"), []byte("edit"), 1),
		"invalid UTF-8":    {0xff},
		"NUL":              []byte("plain\x00text"),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseBlock(content); err == nil {
				t.Fatal("accepted malformed or unsupported target contents")
			}
		})
	}
}

func TestComposeChecksFinalFileLimit(t *testing.T) {
	for _, adapter := range []string{"codex", "claude", "markdown"} {
		t.Run(adapter, func(t *testing.T) {
			target := Target{PackID: "rules", Adapter: adapter}
			overhead := len(block(fsTestRelease("rules", 1, ""))) + 1
			content := strings.Repeat("a", targetLimit(adapter)-overhead)
			got, err := compose(nil, target, TargetState{}, fsTestRelease("rules", 1, content))
			if err != nil || len(got) != targetLimit(adapter) {
				t.Fatalf("exact limit: size %d, error %v", len(got), err)
			}
			if _, err := compose(nil, target, TargetState{}, fsTestRelease("rules", 1, content+"x")); err == nil {
				t.Fatal("accepted oversized generated file")
			}
			if _, err := compose([]byte("existing user content\n"), target, TargetState{}, fsTestRelease("rules", 1, content)); err == nil {
				t.Fatal("did not count existing user bytes toward final file limit")
			}
		})
	}
}

func TestComposeRejectsInvalidAndRolledBackReleases(t *testing.T) {
	current := fsTestRelease("rules", 2, "current")
	state := fsTestState(t, current)
	cases := map[string]core.Release{
		"wrong pack":                     fsTestRelease("different", 3, "next"),
		"invalid ID":                     fsTestRelease("../rules", 3, "next"),
		"zero version":                   fsTestRelease("rules", 0, "next"),
		"rollback":                       fsTestRelease("rules", 1, "old"),
		"same version different content": fsTestRelease("rules", 2, "different"),
		"reserved marker":                fsTestRelease("rules", 3, "<!-- rulesend:end -->"),
		"wrong hash":                     {PackID: "rules", Version: 3, Content: "next", Hash: strings.Repeat("0", 64)},
	}
	for name, release := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := compose(block(current), Target{PackID: "rules", Adapter: "markdown"}, state, release); err == nil {
				t.Fatal("accepted invalid or inconsistent published history")
			}
		})
	}
}

func TestReadTargetRejectsUnsupportedObjects(t *testing.T) {
	dir := fsTestPhysicalDir(t)
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, name := range []string{"link", "directory", "fifo"} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := readTarget(root, name, 1024); err == nil {
				t.Fatal("accepted unsupported target")
			}
		})
	}
	if _, _, _, err := readTarget(root, "real", 3); err == nil {
		t.Fatal("accepted target larger than read limit")
	}
	b, mode, exists, err := readTarget(root, "missing", 1024)
	if err != nil || exists || len(b) != 0 || mode != 0600 {
		t.Fatalf("missing target: %q %v %v %v", b, mode, exists, err)
	}
}

func TestTargetPathRestrictions(t *testing.T) {
	dir := fsTestPhysicalDir(t)
	data := filepath.Join(dir, "private")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"relative.md", filepath.Join(data, "state.json"), dir + "/../escape.md", filepath.Join(dir, "absent", "rules.md")} {
		if _, err := canonicalTarget(name, data); err == nil {
			t.Errorf("accepted forbidden target %q", name)
		}
	}
	if _, _, err := openTarget("relative.md"); err == nil {
		t.Fatal("openTarget accepted relative path")
	}
	physical := filepath.Join(dir, "physical")
	if err := os.Mkdir(physical, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(physical, link); err != nil {
		t.Fatal(err)
	}
	if root, _, err := openTarget(filepath.Join(link, "rules.md")); err == nil {
		root.Close()
		t.Fatal("accepted symlink parent without rebinding physical path")
	}
	canonical, err := canonicalTarget(filepath.Join(link, "rules.md"), data)
	if err != nil || canonical != filepath.Join(physical, "rules.md") {
		t.Fatalf("explicit local canonical registration: %q %v", canonical, err)
	}
}

func TestPrivateStateAndSingleAgentLock(t *testing.T) {
	dir := fsTestPhysicalDir(t)
	path := filepath.Join(dir, "state.json")
	if err := writeJSON(path, map[string]string{"test": "synthetic"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("state permissions: %o", st.Mode().Perm())
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := readJSON(path, &value); err == nil {
		t.Fatal("accepted group/world-readable state")
	}
	lock := filepath.Join(dir, "agent.lock")
	release, err := acquireLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireLock(lock)
	if err == nil {
		second()
		release()
		t.Fatal("two agent commands acquired one state directory")
	}
	release()
	again, err := acquireLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	again()
}
