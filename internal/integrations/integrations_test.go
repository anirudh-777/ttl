package integrations

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestVerifyHMACSHA256(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	secret := "topsecret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !verifyHMACSHA256(body, good, secret) {
		t.Error("expected good signature to verify")
	}
	if verifyHMACSHA256(body, "sha256="+strings.Repeat("0", 64), secret) {
		t.Error("expected bad signature to fail")
	}
	if verifyHMACSHA256(body, "no-prefix", secret) {
		t.Error("expected missing prefix to fail")
	}
	if verifyHMACSHA256([]byte("tampered"), good, secret) {
		t.Error("expected tampered body to fail")
	}
}

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"2026-06-27T18:00:00Z", true},
		{"2026-06-27T18:00:00.000Z", true},
		{"2026-06-27", true},
		{"not a date", false},
	}
	for _, c := range cases {
		got := parseTime(c.in)
		ok := !got.IsZero()
		if ok != c.want {
			t.Errorf("parseTime(%q) ok=%v want %v", c.in, ok, c.want)
		}
	}
}

func TestGitHubVerifyWebhookIssues(t *testing.T) {
	body := []byte(`{
		"action": "opened",
		"issue": {
			"id": 123,
			"number": 42,
			"title": "Bug in login",
			"body": "Steps to reproduce...",
			"html_url": "https://github.com/acme/widgets/issues/42",
			"state": "open",
			"updated_at": "2026-06-27T18:00:00Z",
			"labels": [{"name": "bug"}, {"name": "P1"}]
		}
	}`)
	secret := "githubsecret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	r, _ := http.NewRequest("POST", "/webhook", bytes.NewReader(body))
	r.Header.Set("X-GitHub-Event", "issues")
	r.Header.Set("X-Hub-Signature-256", sig)

	g := GitHub{}
	ev, err := g.VerifyWebhook(r, secret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ev.Provider != "github" || ev.ExternalID != "42" || ev.Title != "Bug in login" {
		t.Errorf("unexpected event: %+v", ev)
	}
	if ev.State != "open" || len(ev.Labels) != 2 {
		t.Errorf("labels/state wrong: %+v", ev)
	}
}

func TestGitHubVerifyWebhookRejectsBadSignature(t *testing.T) {
	body := []byte(`{"action":"opened","issue":{"number":1}}`)
	r, _ := http.NewRequest("POST", "/webhook", bytes.NewReader(body))
	r.Header.Set("X-GitHub-Event", "issues")
	r.Header.Set("X-Hub-Signature-256", "sha256=00")
	g := GitHub{}
	if _, err := g.VerifyWebhook(r, "secret"); err == nil {
		t.Error("expected error on bad signature")
	}
}

func TestGitHubVerifyWebhookIgnoresUnknownEvent(t *testing.T) {
	body := []byte(`{}`)
	r, _ := http.NewRequest("POST", "/webhook", bytes.NewReader(body))
	r.Header.Set("X-GitHub-Event", "push")
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	g := GitHub{}
	if _, err := g.VerifyWebhook(r, "secret"); err == nil {
		t.Error("expected error on unknown event type")
	}
}

// Linear webhook: same signature scheme, different header + payload.
func TestLinearVerifyWebhook(t *testing.T) {
	body := []byte(`{
		"action": "update",
		"type": "Issue",
		"data": {
			"id": "abc",
			"identifier": "ENG-99",
			"title": "Refactor auth",
			"url": "https://linear.app/acme/issue/ENG-99",
			"updatedAt": "2026-06-27T18:00:00.000Z",
			"state": {"name":"In Progress","type":"started"},
			"labels": {"nodes":[{"name":"backend"}]}
		}
	}`)
	secret := "linearsecret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	r, _ := http.NewRequest("POST", "/webhook", bytes.NewReader(body))
	r.Header.Set("Linear-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))

	ev, err := (Linear{}).VerifyWebhook(r, secret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if ev.ExternalID != "ENG-99" || ev.State != "started" {
		t.Errorf("unexpected event: %+v", ev)
	}
}

func TestErrUpstreamError(t *testing.T) {
	e := &errUpstream{Provider: "github", Status: 401, Body: "auth required"}
	if !strings.Contains(e.Error(), "github") || !strings.Contains(e.Error(), "401") {
		t.Errorf("error message missing fields: %s", e.Error())
	}
}

// Sanity: ExternalIssue is JSON-encodable.
func TestExternalIssueJSON(t *testing.T) {
	ei := ExternalIssue{
		ID:        "42",
		Title:     "Test",
		URL:       "https://example.com/42",
		State:     "open",
		UpdatedAt: time.Now(),
	}
	b, err := json.Marshal(ei)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"id":"42"`)) {
		t.Errorf("missing id in json: %s", string(b))
	}
	// Round-trip
	var got ExternalIssue
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != ei.ID || got.Title != ei.Title {
		t.Errorf("round-trip: got %+v", got)
	}
	_ = io.Discard
}
