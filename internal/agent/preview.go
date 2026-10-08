package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/pilgrim13/rulesend/internal/core"
)

var ErrPreviewBlocked = errors.New("one or more preview targets need review; no application files changed")

// PreviewResult describes a local observation, never an installation receipt.
// Diff is for display: control characters are escaped, so it is not a patch file.
type PreviewResult struct {
	TargetID         string `json:"targetId"`
	TargetLabel      string `json:"targetLabel"`
	PackID           string `json:"packId"`
	Adapter          string `json:"adapter"`
	Path             string `json:"path"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	RecordedVersion  int64  `json:"recordedVersion"`
	PublishedVersion int64  `json:"publishedVersion"`
	CreatesFile      bool   `json:"createsFile"`
	BeforeHash       string `json:"beforeHash,omitempty"`
	AfterHash        string `json:"afterHash,omitempty"`
	Diff             string `json:"diff,omitempty"`
}

// Preview polls for published releases and composes changes in memory. It never
// writes targets, configuration, state, backups, journals, or receipts. Polling
// updates the Hub heartbeat. Open's exclusive lock prevents concurrent Agent work.
func (a *Agent) Preview(ctx context.Context, targetID string) ([]PreviewResult, error) {
	selected := make([]Target, 0, len(a.Config.Targets))
	for _, t := range a.Config.Targets {
		if targetID == "" || targetID == t.ID {
			selected = append(selected, t)
		}
	}
	if targetID != "" && (!core.ValidID(targetID) || len(selected) == 0) {
		return nil, errors.New("unknown local target; use agent status to find its ID")
	}
	if len(selected) == 0 {
		return nil, errors.New("no local targets; bind a file before previewing")
	}
	var poll core.PollResponse
	if err := request(ctx, a.Config.Hub, a.Config.DeviceToken, "/api/agent/poll", struct{}{}, &poll); err != nil {
		return nil, err
	}
	if poll.Device.ID != a.Config.DeviceID || poll.Device.Revoked {
		return nil, errors.New("Hub returned an invalid device identity")
	}
	plans := make(map[string]core.Plan, len(poll.Plans))
	for _, p := range poll.Plans {
		if _, duplicate := plans[p.Pack.ID]; !core.ValidID(p.Pack.ID) || duplicate {
			return nil, errors.New("Hub returned invalid or duplicate rule packs")
		}
		plans[p.Pack.ID] = p
	}
	results := make([]PreviewResult, 0, len(selected))
	var resultErr error
	for _, t := range selected {
		p, allowed := plans[t.PackID]
		r := a.previewTarget(t, p, allowed)
		if r.Status == "blocked" || r.Status == "conflict" {
			resultErr = ErrPreviewBlocked
		}
		results = append(results, r)
	}
	return results, resultErr
}

func (a *Agent) previewTarget(t Target, p core.Plan, allowed bool) PreviewResult {
	previous := a.State.Targets[t.ID]
	r := PreviewResult{TargetID: t.ID, TargetLabel: t.Label, PackID: t.PackID,
		Adapter: t.Adapter, Path: t.Path, RecordedVersion: previous.Version}
	fail := func(err error) PreviewResult {
		r.Status, r.Reason = "blocked", err.Error()
		if errors.Is(err, ErrConflict) {
			r.Status = "conflict"
		}
		// An unstable or invalid observation must never offer a proposed write.
		r.AfterHash, r.Diff, r.CreatesFile = "", "", false
		return r
	}
	if !allowed {
		return fail(errors.New("pack is outside the device's current Hub scope"))
	}
	if _, err := os.Lstat(a.journalPath(t)); err == nil {
		return fail(errors.New("pending recovery journal; preserve it and run agent once to reconcile before previewing"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail(errors.New("cannot inspect target recovery journal"))
	}
	root, name, err := openTarget(t.Path)
	if err != nil {
		return fail(err)
	}
	defer root.Close()
	before, mode, exists, err := readTarget(root, name, targetLimit(t.Adapter))
	if err != nil {
		return fail(err)
	}
	r.BeforeHash = core.Hash(before)
	m, err := parseBlock(before)
	if err != nil {
		return fail(err)
	}
	if (previous.BlockHash == "" && m != nil) ||
		(previous.BlockHash != "" && (m == nil || m.PackID != t.PackID || m.BlockHash != previous.BlockHash)) {
		return fail(ErrConflict)
	}
	after := before
	if p.Release != nil {
		after, err = compose(before, t, previous, *p.Release)
		if err != nil {
			return fail(err)
		}
		r.PublishedVersion = p.Release.Version
	}
	latest, latestMode, latestExists, err := readTarget(root, name, targetLimit(t.Adapter))
	if err != nil {
		return fail(err)
	}
	if latestExists != exists || latestMode != mode || !bytes.Equal(latest, before) {
		return fail(errors.New("target changed during preview; retry after local edits finish"))
	}
	if err := verifyParent(root, t.Path); err != nil {
		return fail(err)
	}
	if p.Release == nil {
		r.Status, r.Reason = "pending", "Waiting for a published version"
		return r
	}
	r.AfterHash, r.CreatesFile = core.Hash(after), !exists
	r.Diff = displayDiff(before, after)
	switch {
	case bytes.Equal(before, after):
		r.Status, r.Reason = "current", "File already matches the published version"
	case p.Pack.Paused:
		r.Status, r.Reason = "paused", "Proposed change only; resume the pack before applying"
	default:
		r.Status, r.Reason = "ready", "Ready for an explicit agent once; nothing has been applied"
	}
	return r
}

// PreviewText escapes untrusted labels, paths, and rule content at the terminal
// boundary. The snapshot can become outdated before the next Agent command.
func PreviewText(results []PreviewResult) string {
	var out strings.Builder
	out.WriteString("Preview only. No application files changed; no receipts sent.\nLocal file contents stay on this device. The Hub records a heartbeat.\n")
	for _, r := range results {
		fmt.Fprintf(&out, "\n%s · %s · %s\n  target: %s\n  pack: %s · status: %s\n  recorded: v%d · published: v%d\n  %s\n",
			displayText(r.TargetLabel), displayText(r.Adapter), displayText(r.TargetID),
			displayText(r.Path), displayText(r.PackID), displayText(r.Status),
			r.RecordedVersion, r.PublishedVersion, displayText(r.Reason))
		if r.CreatesFile {
			out.WriteString("  A new file would be created.\n")
		}
		if r.Diff != "" {
			out.WriteString("\n")
			out.WriteString(r.Diff)
		}
	}
	out.WriteString("\nThis is a snapshot, not a reservation. Apply rechecks the file and the latest release.\nApp loading and AI behavior: unverified.\n")
	return out.String()
}

func displayText(s string) string {
	var out strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			fmt.Fprintf(&out, "\\u%04x", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// displayDiff uses one hunk bounded by common prefix/suffix lines. Its work is
// linear in file size, even for many tiny lines. It favors predictable resource
// use over minimal diffs and is intentionally display-only.
func displayDiff(before, after []byte) string {
	if bytes.Equal(before, after) {
		return ""
	}
	lines := func(b []byte) []string {
		if len(b) == 0 {
			return nil
		}
		x := strings.SplitAfter(string(b), "\n")
		if x[len(x)-1] == "" {
			x = x[:len(x)-1]
		}
		return x
	}
	old, next := lines(before), lines(after)
	prefix := 0
	for prefix < len(old) && prefix < len(next) && old[prefix] == next[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(next)-prefix && old[len(old)-1-suffix] == next[len(next)-1-suffix] {
		suffix++
	}
	start := max(0, prefix-3)
	oldEnd, nextEnd := len(old)-suffix, len(next)-suffix
	contextEnd := min(3, suffix)
	oldCount, nextCount := oldEnd+contextEnd-start, nextEnd+contextEnd-start
	oldStart, nextStart := start+1, start+1
	if oldCount == 0 {
		oldStart = start
	}
	if nextCount == 0 {
		nextStart = start
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- current\n+++ published\n@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, nextStart, nextCount)
	emit := func(prefix byte, line string) {
		out.WriteByte(prefix)
		out.WriteString(displayText(strings.TrimSuffix(line, "\n")))
		out.WriteByte('\n')
		if !strings.HasSuffix(line, "\n") {
			out.WriteString("\\ No newline at end of file\n")
		}
	}
	for _, line := range old[start:prefix] {
		emit(' ', line)
	}
	for _, line := range old[prefix:oldEnd] {
		emit('-', line)
	}
	for _, line := range next[prefix:nextEnd] {
		emit('+', line)
	}
	for _, line := range old[oldEnd : oldEnd+contextEnd] {
		emit(' ', line)
	}
	return out.String()
}
