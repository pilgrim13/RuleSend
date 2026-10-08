// Package agent applies published rule packs to explicitly approved local files.
package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pilgrim13/rulesend/internal/core"
)

type Target struct {
	ID      string `json:"id"`
	PackID  string `json:"packId"`
	Adapter string `json:"adapter"`
	Label   string `json:"label"`
	Path    string `json:"path"`
}
type Config struct {
	Hub         string   `json:"hub"`
	DeviceID    string   `json:"deviceId"`
	DeviceToken string   `json:"deviceToken"`
	Name        string   `json:"name"`
	PackIDs     []string `json:"packIds"`
	Targets     []Target `json:"targets"`
}
type TargetState struct {
	Version           int64        `json:"version"`
	Hash              string       `json:"hash"`
	BlockHash         string       `json:"blockHash"`
	LastAttempt       string       `json:"lastAttempt"`
	LastTrigger       int64        `json:"lastTrigger"`
	PolicyInitialized bool         `json:"policyInitialized"`
	Receipt           core.Receipt `json:"receipt"`
}
type State struct {
	Targets     map[string]TargetState `json:"targets"`
	LastContact string                 `json:"lastContact"`
}
type Agent struct {
	Dir    string
	Config Config
	State  State
	unlock func()
}

func Open(dataDir string) (*Agent, error) {
	dir, err := privateDir(dataDir)
	if err != nil {
		return nil, err
	}
	unlock, err := acquireLock(filepath.Join(dir, "agent.lock"))
	if err != nil {
		return nil, err
	}
	a := &Agent{Dir: dir, unlock: unlock, State: State{Targets: map[string]TargetState{}}}
	if err = readJSON(filepath.Join(dir, "config.json"), &a.Config); err != nil {
		unlock()
		return nil, errors.New("cannot read private Agent configuration; enroll first")
	}
	if _, err = HubURL(a.Config.Hub); err != nil {
		unlock()
		return nil, err
	}
	if !core.ValidID(a.Config.DeviceID) || len(a.Config.DeviceToken) != 64 {
		unlock()
		return nil, errors.New("invalid Agent configuration")
	}
	if err = readJSON(filepath.Join(dir, "state.json"), &a.State); err != nil && !errors.Is(err, os.ErrNotExist) {
		unlock()
		return nil, errors.New("cannot read Agent state; do not discard recovery files")
	}
	if a.State.Targets == nil {
		a.State.Targets = map[string]TargetState{}
	}
	for _, t := range a.Config.Targets {
		if !core.ValidID(t.ID) || !core.ValidID(t.PackID) || (t.Adapter != "codex" && t.Adapter != "claude" && t.Adapter != "markdown") {
			unlock()
			return nil, errors.New("invalid target configuration")
		}
	}
	return a, nil
}
func (a *Agent) Close() {
	if a.unlock != nil {
		a.unlock()
		a.unlock = nil
	}
}
func (a *Agent) saveState() error  { return writeJSON(filepath.Join(a.Dir, "state.json"), a.State) }
func (a *Agent) saveConfig() error { return writeJSON(filepath.Join(a.Dir, "config.json"), a.Config) }

func Enroll(ctx context.Context, dataDir, hub, name, token string) (string, error) {
	base, err := HubURL(hub)
	if err != nil {
		return "", err
	}
	if len(name) < 1 || len(name) > 100 {
		return "", errors.New("device name must be 1–100 bytes")
	}
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 256 {
		return "", errors.New("invalid enrollment token file")
	}
	dir, err := privateDir(dataDir)
	if err != nil {
		return "", err
	}
	unlock, err := acquireLock(filepath.Join(dir, "agent.lock"))
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err = os.Lstat(filepath.Join(dir, "config.json")); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("this data directory is already enrolled or unavailable")
	}
	var res struct {
		DeviceID    string   `json:"deviceId"`
		DeviceToken string   `json:"deviceToken"`
		PackIDs     []string `json:"packIds"`
	}
	if err = request(ctx, base, "", "/api/enroll", map[string]string{"token": token, "name": name, "platform": runtime.GOOS + "/" + runtime.GOARCH}, &res); err != nil {
		return "", err
	}
	if !core.ValidID(res.DeviceID) || len(res.DeviceToken) != 64 {
		return "", errors.New("Hub enrollment response is invalid")
	}
	cfg := Config{Hub: base, DeviceID: res.DeviceID, DeviceToken: res.DeviceToken, Name: name, PackIDs: res.PackIDs, Targets: []Target{}}
	if err = writeJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
		return "", errors.New("registration succeeded but private configuration could not be saved; revoke that device and enroll again")
	}
	if err = writeJSON(filepath.Join(dir, "state.json"), State{Targets: map[string]TargetState{}}); err != nil {
		return "", errors.New("could not initialize local state")
	}
	return res.DeviceID, nil
}

func (a *Agent) Bind(packID, adapter, path, label string) (Target, error) {
	if len(a.Config.Targets) >= 100 {
		return Target{}, errors.New("alpha supports at most 100 targets per device")
	}
	if adapter != "codex" && adapter != "claude" && adapter != "markdown" {
		return Target{}, errors.New("adapter must be codex, claude, or markdown")
	}
	allowed := false
	for _, id := range a.Config.PackIDs {
		if id == packID {
			allowed = true
		}
	}
	if !core.ValidID(packID) || !allowed {
		return Target{}, errors.New("pack is outside this device's enrollment scope")
	}
	path, err := canonicalTarget(path, a.Dir)
	if err != nil {
		return Target{}, err
	}
	for _, t := range a.Config.Targets {
		if t.Path == path {
			return Target{}, errors.New("this file already has a binding")
		}
	}
	root, name, err := openTarget(path)
	if err != nil {
		return Target{}, err
	}
	defer root.Close()
	b, _, _, err := readTarget(root, name, targetLimit(adapter))
	if err != nil {
		return Target{}, err
	}
	m, err := parseBlock(b)
	if err != nil {
		return Target{}, err
	}
	if m != nil {
		return Target{}, errors.New("existing RuleSend block requires its original Agent state; automatic adoption is disabled")
	}
	if label == "" {
		label = adapter + " rules"
	}
	if len(label) > 100 || strings.ContainsAny(label, "\r\n\x00/\\") {
		return Target{}, errors.New("target label must be a short single line")
	}
	id, err := core.Token()
	if err != nil {
		return Target{}, err
	}
	t := Target{ID: id[:24], PackID: packID, Adapter: adapter, Label: label, Path: path}
	// Capture the policy generation at local approval, so a Sync-now issued
	// between bind and the first run is not accidentally consumed as a baseline.
	var poll core.PollResponse
	if err = request(context.Background(), a.Config.Hub, a.Config.DeviceToken, "/api/agent/poll", struct{}{}, &poll); err != nil {
		return Target{}, err
	}
	if poll.Device.ID != a.Config.DeviceID || poll.Device.Revoked {
		return Target{}, errors.New("invalid device identity")
	}
	found := false
	for _, p := range poll.Plans {
		if p.Pack.ID == packID {
			a.State.Targets[t.ID] = TargetState{LastTrigger: p.Pack.Trigger, PolicyInitialized: true}
			found = true
			break
		}
	}
	if !found {
		return Target{}, errors.New("pack is no longer in this device's scope")
	}
	if err = a.saveState(); err != nil {
		return Target{}, errors.New("cannot persist binding policy baseline")
	}
	a.Config.Targets = append(a.Config.Targets, t)
	if err = a.saveConfig(); err != nil {
		return Target{}, errors.New("cannot save local binding")
	}
	return t, nil
}

func (a *Agent) receipt(t Target, s TargetState, status, message string) core.Receipt {
	return core.Receipt{DeviceID: a.Config.DeviceID, PackID: t.PackID, TargetID: t.ID, Adapter: t.Adapter, TargetLabel: t.Label, Version: s.Version, Hash: s.Hash, Status: status, Error: message, CheckedAt: time.Now().UTC().Format(time.RFC3339), AppLoaded: "unverified", BehaviorVerified: "unverified"}
}

// Step refreshes observations every poll; apply is separately controlled by policy.
func (a *Agent) Step(ctx context.Context, force bool) ([]core.Receipt, error) {
	var poll core.PollResponse
	if err := request(ctx, a.Config.Hub, a.Config.DeviceToken, "/api/agent/poll", struct{}{}, &poll); err != nil {
		return nil, err
	}
	if poll.Device.ID != a.Config.DeviceID || poll.Device.Revoked {
		return nil, errors.New("Hub returned an invalid device identity")
	}
	plans := map[string]core.Plan{}
	for _, p := range poll.Plans {
		if core.ValidID(p.Pack.ID) {
			plans[p.Pack.ID] = p
		}
	}
	a.State.LastContact = time.Now().UTC().Format(time.RFC3339)
	receipts := make([]core.Receipt, 0, len(a.Config.Targets))
	hadFailure := false
	for _, t := range a.Config.Targets {
		s := a.State.Targets[t.ID]
		p, ok := plans[t.PackID]
		if !ok {
			continue
		} // Never report outside current server scope.
		status, message := "pending", "Waiting for a published version"
		err := a.verify(t)
		s = a.State.Targets[t.ID]
		if !s.PolicyInitialized {
			s.LastTrigger = p.Pack.Trigger
			s.PolicyInitialized = true
		}
		a.State.Targets[t.ID] = s
		if err == nil && p.Release != nil {
			if p.Release.PackID != t.PackID || p.Release.Version < 1 || p.Release.Hash != core.Hash([]byte(p.Release.Content)) {
				err = errors.New("invalid published release")
			} else {
				last, _ := time.Parse(time.RFC3339, s.LastAttempt)
				interval := p.Pack.IntervalSeconds
				if interval < 15 {
					interval = 15
				}
				if interval > 86400 {
					interval = 86400
				}
				due := force || p.Pack.Trigger > s.LastTrigger || (p.Pack.Mode == "auto" && time.Since(last) >= time.Duration(interval)*time.Second)
				if !p.Pack.Paused && due {
					s, err = a.apply(t, *p.Release)
					s.LastAttempt = time.Now().UTC().Format(time.RFC3339)
					if err == nil {
						s.LastTrigger = p.Pack.Trigger
					}
				}
				if s.Version == p.Release.Version && s.Hash == p.Release.Hash && s.BlockHash != "" {
					status, message = "applied", ""
				} else if p.Pack.Paused {
					message = "Pack is paused"
				} else if p.Pack.Mode == "manual" {
					message = "Waiting for a manual sync request"
				} else {
					message = "Waiting for the next scheduled apply"
				}
			}
		}
		if err != nil {
			hadFailure = true
			status = "error"
			if errors.Is(err, ErrConflict) {
				status = "conflict"
			}
			message = err.Error()
		}
		s.Receipt = a.receipt(t, s, status, message)
		a.State.Targets[t.ID] = s
		receipts = append(receipts, s.Receipt)
	}
	if err := a.saveState(); err != nil {
		return receipts, errors.New("cannot persist Agent observations")
	}
	if len(receipts) > 0 {
		if err := request(ctx, a.Config.Hub, a.Config.DeviceToken, "/api/agent/receipts", map[string]any{"receipts": receipts}, nil); err != nil {
			return receipts, err
		}
	}
	if hadFailure {
		return receipts, errors.New("one or more targets need review; run agent status")
	}
	return receipts, nil
}

func (a *Agent) Run(ctx context.Context, report func([]core.Receipt, error)) error {
	for {
		receipts, err := a.Step(ctx, false)
		if ctx.Err() != nil {
			return nil
		}
		report(receipts, err)
		timer := time.NewTimer(15 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (a *Agent) Status() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Device: %s (%s)\nHub: %s\nLast Hub contact: %s\n", a.Config.Name, a.Config.DeviceID, a.Config.Hub, a.State.LastContact)
	for _, t := range a.Config.Targets {
		s := a.State.Targets[t.ID]
		status := s.Receipt.Status
		if status == "" {
			status = "pending"
		}
		fmt.Fprintf(&b, "\n%s · %s · %s\n  target: %s\n  pack: %s · version: %d · status: %s\n  checked: %s\n", t.Label, t.Adapter, t.ID, t.Path, t.PackID, s.Version, status, s.Receipt.CheckedAt)
		if s.Receipt.Error != "" {
			fmt.Fprintf(&b, "  detail: %s\n", s.Receipt.Error)
		}
	}
	b.WriteString("\nApp loading and AI behavior: unverified. Status is the last saved observation.\n")
	return b.String()
}

// ReadStatus is a historical, read-only snapshot and can run beside the daemon.
func ReadStatus(dataDir string) (string, error) {
	a := &Agent{State: State{Targets: map[string]TargetState{}}}
	if err := readJSON(filepath.Join(dataDir, "config.json"), &a.Config); err != nil {
		return "", errors.New("cannot read private Agent configuration")
	}
	if err := readJSON(filepath.Join(dataDir, "state.json"), &a.State); err != nil {
		return "", errors.New("cannot read Agent observations")
	}
	return a.Status(), nil
}
