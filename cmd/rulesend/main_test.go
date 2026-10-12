package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListenRequiresRemoteTLSAndExactPublicOrigin(t *testing.T) {
	for _, tc := range []struct {
		addr, public, cert, key string
		valid                   bool
	}{
		{"127.0.0.1:8787", "", "", "", true},
		{"[::1]:8787", "https://rules.example.com", "", "", true},
		{"0.0.0.0:8787", "", "", "", false},
		{"0.0.0.0:8787", "https://rules.example.com", "", "", false},
		{"0.0.0.0:8787", "https://rules.example.com", "cert", "key", true},
		{"0.0.0.0:8787", "", "cert", "key", false},
		{"127.0.0.1:8787", "https://rules.example.com/path", "", "", false},
		{"127.0.0.1:8787", "http://rules.example.com", "", "", false},
		{"127.0.0.1:8787", "https://user:pass@rules.example.com", "", "", false},
		{"127.0.0.1:8787", "", "cert", "", false},
	} {
		if valid := validateListen(tc.addr, tc.public, tc.cert, tc.key) == nil; valid != tc.valid {
			t.Errorf("listen policy mismatch for %s %s", tc.addr, tc.public)
		}
	}
}

func TestSourceConfigurationIsPrivateStrictAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.json")
	valid := `{"pack_example":{"label":"Git-backed source","initialProof":{"commit":"` + strings.Repeat("a", 40) + `","releaseId":"release-1","version":"v1","hash":"` + strings.Repeat("b", 64) + `"}}}`
	for _, body := range []string{valid + " {}", strings.Replace(valid, `"label":`, `"unexpected":`, 1), `{}`, strings.Repeat(" ", 65*1024)} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readSourceConfig(path); err == nil {
			t.Fatal("invalid source configuration accepted")
		}
	}
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	if sources, err := readSourceConfig(path); err != nil || len(sources) != 1 {
		t.Fatal("private source configuration rejected")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSourceConfig(path); err == nil {
		t.Fatal("shared source configuration accepted")
	}
}

func TestTokenFileMustBePrivateRegularAndBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("synthetic-test-credential\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := readToken(path)
	if err != nil || token != "synthetic-test-credential" {
		t.Fatal("private credential file rejected")
	}
	_ = os.Chmod(path, 0644)
	if _, err = readToken(path); err == nil {
		t.Fatal("world-readable credential accepted")
	}
	_ = os.Chmod(path, 0600)
	link := filepath.Join(dir, "link")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = readToken(link); err == nil {
		t.Fatal("symlink credential accepted")
	}
	_ = os.WriteFile(path, make([]byte, 2048), 0600)
	if _, err = readToken(path); err == nil {
		t.Fatal("oversized credential accepted")
	}
}
