// Package integrations defines the external-issue provider interface
// and ships built-in implementations for GitHub and Linear.
//
// A Provider is a single-purpose client for one external service.
// It pulls assigned issues, can close one, and can verify a webhook
// signature. The store layer manages integrations and issue_links;
// providers operate only on their own data structures.
package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ExternalIssue is the provider-agnostic shape we map all upstream
// issues to before persisting them as tasks.
type ExternalIssue struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
	Labels    []string  `json:"labels,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Provider is implemented by every external service ttl integrates
// with. All methods must be safe for concurrent use; providers
// internally create a fresh http.Client per call.
type Provider interface {
	// Name returns the short provider id used in URLs and config:
	// "github", "linear", etc.
	Name() string

	// FetchAssigned pulls issues assigned to the user identified by
	// cfg. cfg is the unmarshalled config_json for the integration
	// (typically contains a token).
	FetchAssigned(ctx context.Context, cfg map[string]string) ([]ExternalIssue, error)

	// CloseIssue marks an upstream issue as closed. Used when the
	// user marks the linked ttl task as done.
	CloseIssue(ctx context.Context, cfg map[string]string, externalID string) error

	// VerifyWebhook checks the signature on an inbound webhook
	// request and returns the parsed event payload if valid. The
	// payload format is provider-specific; we expose a normalised
	// shape below.
	VerifyWebhook(r *http.Request, secret string) (*WebhookEvent, error)
}

// WebhookEvent is the normalised shape providers convert their
// native webhooks to.
type WebhookEvent struct {
	Provider   string    `json:"provider"`
	Action     string    `json:"action"` // 'opened' | 'closed' | 'updated' | 'reopened' | 'labeled' | 'unlabeled'
	ExternalID string    `json:"external_id"`
	Title      string    `json:"title,omitempty"`
	URL        string    `json:"url,omitempty"`
	State      string    `json:"state,omitempty"`
	Labels     []string  `json:"labels,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// -------------------------- helpers --------------------------

// httpClient returns a client with a sensible timeout. Each Provider
// gets its own (cheap) client per call so we don't share state.
func httpClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// readJSON reads and decodes r into v.
func readJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// verifyHMACSHA256 checks that the body matches the signature in the
// X-Hub-Signature-256 header (format: "sha256=<hex>"). Used by GitHub.
func verifyHMACSHA256(body []byte, header, secret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := mac.Sum(nil)
	return hmac.Equal(got, want)
}

// errUpstream wraps a non-2xx response from an external API.
type errUpstream struct {
	Provider string
	Status   int
	Body     string
}

func (e *errUpstream) Error() string {
	return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.Status, e.Body)
}

func newUpstreamErr(provider string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return &errUpstream{Provider: provider, Status: resp.StatusCode, Body: string(body)}
}

var ErrNotFound = errors.New("not found in upstream")

// parseTime tries several common formats providers use.
func parseTime(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05.000-07:00",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
