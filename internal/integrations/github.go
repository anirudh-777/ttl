// GitHub provider. Uses the REST API with a Personal Access Token
// (classic or fine-grained). Scopes needed: repo (read), write:issues
// if you want close to round-trip.

package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GitHub implements Provider.
type GitHub struct{}

// NewGitHub returns a fresh GitHub provider. Stateless, safe to share.
func NewGitHub() *GitHub { return &GitHub{} }

func (GitHub) Name() string { return "github" }

// GitHub API types.
type ghIssue struct {
	ID        int64     `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	State     string    `json:"state"`
	UpdatedAt string    `json:"updated_at"`
	Labels    []ghLabel `json:"labels"`
}

type ghLabel struct {
	Name string `json:"name"`
}

func (g GitHub) FetchAssigned(ctx context.Context, cfg map[string]string) ([]ExternalIssue, error) {
	token, ok := cfg["token"]
	if !ok || token == "" {
		return nil, errors.New("github: config.token is required")
	}
	login := cfg["login"] // optional; defaults to /user via Authorization header
	endpoint := "https://api.github.com/issues?state=open&filter=assigned&per_page=100"
	if login != "" {
		// query=assignee:me OR assignee:login depending on whether
		// login matches the authenticated user. We just request
		// assigned-to-me which is the common case.
		endpoint = fmt.Sprintf("https://api.github.com/issues?state=open&assignee=%s&per_page=100", login)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := httpClient(0).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, newUpstreamErr("github", resp)
	}
	var raw []ghIssue
	if err := readJSON(resp.Body, &raw); err != nil {
		return nil, err
	}
	out := make([]ExternalIssue, 0, len(raw))
	for _, it := range raw {
		labels := make([]string, 0, len(it.Labels))
		for _, l := range it.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, ExternalIssue{
			ID:        fmt.Sprintf("%d", it.Number),
			Title:     it.Title,
			Body:      it.Body,
			URL:       it.HTMLURL,
			State:     it.State,
			Labels:    labels,
			UpdatedAt: parseTime(it.UpdatedAt),
		})
	}
	return out, nil
}

func (g GitHub) CloseIssue(ctx context.Context, cfg map[string]string, externalID string) error {
	token, ok := cfg["token"]
	if !ok || token == "" {
		return errors.New("github: config.token is required")
	}
	repo := cfg["repo"] // owner/repo
	if repo == "" {
		return errors.New("github: config.repo (owner/repo) is required to close")
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/issues/%s", repo, externalID)
	body := bytes.NewReader([]byte(`{"state":"closed"}`))
	req, err := http.NewRequestWithContext(ctx, "PATCH", endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient(0).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		// Drain body so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode == 404 {
			return ErrNotFound
		}
		return newUpstreamErr("github", resp)
	}
	return nil
}

// VerifyWebhook checks the X-Hub-Signature-256 HMAC header on the
// incoming request and converts the GitHub event into our normalised
// WebhookEvent shape.
func (g GitHub) VerifyWebhook(r *http.Request, secret string) (*WebhookEvent, error) {
	if secret == "" {
		return nil, errors.New("github: webhook secret not configured")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if !verifyHMACSHA256(body, r.Header.Get("X-Hub-Signature-256"), secret) {
		return nil, errors.New("github: signature mismatch")
	}
	event := r.Header.Get("X-GitHub-Event")
	delivery := r.Header.Get("X-GitHub-Delivery")
	_ = delivery

	switch event {
	case "issues":
		var payload struct {
			Action string  `json:"action"`
			Issue  ghIssue `json:"issue"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("github: parse issues payload: %w", err)
		}
		labels := make([]string, 0, len(payload.Issue.Labels))
		for _, l := range payload.Issue.Labels {
			labels = append(labels, l.Name)
		}
		state := strings.ToLower(payload.Issue.State)
		return &WebhookEvent{
			Provider:   "github",
			Action:     payload.Action,
			ExternalID: fmt.Sprintf("%d", payload.Issue.Number),
			Title:      payload.Issue.Title,
			URL:        payload.Issue.HTMLURL,
			State:      state,
			Labels:     labels,
			UpdatedAt:  parseTime(payload.Issue.UpdatedAt),
		}, nil
	default:
		return nil, fmt.Errorf("github: ignoring event %q", event)
	}
}
