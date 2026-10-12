// Package core defines the wire format shared by the Hub and local Agent.
package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const Version = "0.1.0-alpha.1"
const MaxContentBytes = 128 * 1024

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Token() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func ValidID(s string) bool { return idPattern.MatchString(s) }
func ValidateContent(s string) error {
	if len(s) > MaxContentBytes {
		return errors.New("rule content exceeds 128 KiB")
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return errors.New("rule content must be UTF-8 text without NUL bytes")
	}
	if strings.Contains(s, "<!-- rulesend:") {
		return errors.New("rule content contains a reserved RuleSend marker")
	}
	return nil
}

type Pack struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Description      string      `json:"description"`
	Content          string      `json:"content"`
	DraftRevision    int64       `json:"draftRevision"`
	PublishedVersion int64       `json:"publishedVersion"`
	PublishedHash    string      `json:"publishedHash"`
	Mode             string      `json:"mode"`
	IntervalSeconds  int         `json:"intervalSeconds"`
	Paused           bool        `json:"paused"`
	Trigger          int64       `json:"trigger"`
	UpdatedAt        string      `json:"updatedAt"`
	Source           *PackSource `json:"source,omitempty"`
}

// SourceProof identifies a reviewed external content release, independently of
// the monotonically increasing RuleSend pack version.
type SourceProof struct {
	Commit    string `json:"commit"`
	ReleaseID string `json:"releaseId"`
	Version   string `json:"version"`
	Hash      string `json:"hash"`
}
type PackSource struct {
	Label   string      `json:"label"`
	Proof   SourceProof `json:"proof"`
	Pending string      `json:"pending"`
	Status  string      `json:"status"`
	Error   string      `json:"error"`
}
type SourcePublication struct {
	ID                   string       `json:"id"`
	PackID               string       `json:"packId"`
	Content              string       `json:"content,omitempty"`
	Hash                 string       `json:"hash"`
	Message              string       `json:"message"`
	BaseDraftRevision    int64        `json:"baseDraftRevision"`
	BaseSourceProof      SourceProof  `json:"baseSourceProof"`
	BasePublishedVersion int64        `json:"basePublishedVersion"`
	BasePublishedHash    string       `json:"basePublishedHash"`
	Status               string       `json:"status"`
	Error                string       `json:"error"`
	CreatedAt            string       `json:"createdAt"`
	CompletedAt          string       `json:"completedAt,omitempty"`
	ReleaseVersion       int64        `json:"releaseVersion,omitempty"`
	Proof                *SourceProof `json:"proof,omitempty"`
}
type Release struct {
	PackID    string       `json:"packId"`
	Version   int64        `json:"version"`
	Content   string       `json:"content"`
	Hash      string       `json:"hash"`
	Message   string       `json:"message"`
	CreatedAt string       `json:"createdAt"`
	Source    *SourceProof `json:"source,omitempty"`
}
type Device struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	PackIDs  []string `json:"packIds"`
	Revoked  bool     `json:"revoked"`
	LastSeen string   `json:"lastSeen"`
}
type Receipt struct {
	DeviceID         string `json:"deviceId"`
	PackID           string `json:"packId"`
	TargetID         string `json:"targetId"`
	Adapter          string `json:"adapter"`
	TargetLabel      string `json:"targetLabel"`
	Version          int64  `json:"version"`
	Hash             string `json:"hash"`
	Status           string `json:"status"`
	Error            string `json:"error"`
	CheckedAt        string `json:"checkedAt"`
	AppLoaded        string `json:"appLoaded"`
	BehaviorVerified string `json:"behaviorVerified"`
}
type Event struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Actor     string `json:"actor"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}
type Snapshot struct {
	Packs        []Pack              `json:"packs"`
	Devices      []Device            `json:"devices"`
	Receipts     []Receipt           `json:"receipts"`
	Events       []Event             `json:"events"`
	Publications []SourcePublication `json:"publications"`
}
type Plan struct {
	Pack    Pack     `json:"pack"`
	Release *Release `json:"release"`
}
type PollResponse struct {
	Device Device `json:"device"`
	Plans  []Plan `json:"plans"`
}
