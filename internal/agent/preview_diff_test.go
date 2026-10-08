package agent

import (
	"strings"
	"testing"
)

func TestDisplayDiff(t *testing.T) {
	tests := []struct{ name, before, after, want string }{
		{"unchanged", "same\n", "same\n", ""},
		{"new file", "", "new\n", "--- current\n+++ published\n@@ -0,0 +1,1 @@\n+new\n"},
		{"empty file", "old\n", "", "--- current\n+++ published\n@@ -1,1 +0,0 @@\n-old\n"},
		{"replacement", "start\nold\nend\n", "start\nnew\nend\n", "--- current\n+++ published\n@@ -1,3 +1,3 @@\n start\n-old\n+new\n end\n"},
		{"final newline", "same", "same\n", "--- current\n+++ published\n@@ -1,1 +1,1 @@\n-same\n\\ No newline at end of file\n+same\n"},
		{"context", "0\n1\n2\n3\nold\n5\n6\n7\n8\n", "0\n1\n2\n3\nnew\n5\n6\n7\n8\n", "--- current\n+++ published\n@@ -2,7 +2,7 @@\n 1\n 2\n 3\n-old\n+new\n 5\n 6\n 7\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := displayDiff([]byte(tt.before), []byte(tt.after)); got != tt.want {
				t.Fatalf("diff = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestDisplayDiffManyShortLines(t *testing.T) {
	// A quadratic line comparison would require billions of cells here.
	before := strings.Repeat("a\n", 64*1024)
	after := strings.Repeat("b\n", 64*1024)
	got := displayDiff([]byte(before), []byte(after))
	if !strings.Contains(got, "@@ -1,65536 +1,65536 @@") || strings.Count(got, "-a\n") != 65536 || strings.Count(got, "+b\n") != 65536 {
		t.Fatal("large diff lost changed lines")
	}
}

func TestPreviewTextEscapesUntrustedFields(t *testing.T) {
	unsafe := "text\x1b[2J\r\t\u0085\u202e\u2028"
	results := []PreviewResult{{TargetLabel: unsafe, Adapter: unsafe, TargetID: unsafe, Path: unsafe, PackID: unsafe, Status: unsafe, Reason: unsafe,
		Diff: displayDiff(nil, []byte(unsafe+"\n"))}}
	out := PreviewText(results)
	for _, control := range []string{"\x1b", "\r", "\t", "\u0085", "\u202e", "\u2028"} {
		if strings.Contains(out, control) {
			t.Fatalf("terminal control %q leaked", control)
		}
	}
	if !strings.Contains(out, `\u001b[2J\u000d\u0009\u0085\u202e\u2028`) || !strings.Contains(out, "snapshot, not a reservation") {
		t.Fatal("missing escaped content or snapshot caveat")
	}
}
