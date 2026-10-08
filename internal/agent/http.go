package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Up to 100 authorized 128 KiB packs, including JSON's worst-case six-byte
// escaping and bounded policy metadata. The transport remains size-limited.
const maxHubResponse = 96 * 1024 * 1024

func HubURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("Hub URL must be an origin without a path, credentials, query, or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", errors.New("HTTPS is required except for a loopback Hub")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

func request(ctx context.Context, base, token, path string, input, output any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(b))
	if err != nil {
		return errors.New("cannot create Hub request")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Hub connection failed; check address, network, and TLS")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxHubResponse+1))
	if err != nil || len(data) > maxHubResponse {
		return errors.New("Hub response could not be read within limits")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do not reflect server body content into local logs or receipts.
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return errors.New("Hub rejected authentication or scope; check enrollment and revocation")
		}
		return fmt.Errorf("Hub request failed (HTTP %d)", resp.StatusCode)
	}
	if output != nil {
		if err = json.Unmarshal(data, output); err != nil {
			return errors.New("Hub returned invalid JSON")
		}
	}
	return nil
}
