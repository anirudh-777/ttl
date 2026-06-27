---
layout: default
title: Integrations
---
# Integrations

ttl can pull issues from external services (currently GitHub and Linear)
and mirror them as tasks. Each user task can be linked to one external
issue; closing either side cascades to the other.

## Provider model

```
internal/integrations/
  integrations.go    Provider interface + helpers
  github.go          GitHub provider (REST + Personal Access Token)
  linear.go          Linear provider (GraphQL + Personal API Key)
  syncer.go          Syncer turns ExternalIssues into ttl tasks
  *_test.go          HMAC + webhook parsing tests
```

A `Provider` is just four methods:

```go
type Provider interface {
    Name() string
    FetchAssigned(ctx, cfg) ([]ExternalIssue, error)
    CloseIssue(ctx, cfg, externalID) error
    VerifyWebhook(r, secret) (*WebhookEvent, error)
}
```

Adding a new provider means writing one struct and registering it in
`internal/api/integrations.go`:

```go
var providers = map[string]integrations.Provider{
    "github":   integrations.NewGitHub(),
    "linear":   integrations.NewLinear(),
    "jira":     integrations.NewJira(), // when you add it
}
```

## CLI

```bash
# Add a GitHub integration. You'll be prompted for the PAT, optional
# login (for queries that need it), and an optional webhook secret.
ttl integrations add github --label "My work"

# Same for Linear
ttl integrations add linear --label "My team"

# List configured integrations
ttl integrations list

# Sync now (pulls assigned issues; new ones become tasks, state changes mirror)
ttl integrations sync <id>

# Remove
ttl integrations remove <id>
```

## How sync works

1. `Syncer.Sync` calls `Provider.FetchAssigned` to pull open issues
   assigned to the authenticated user.
2. For each issue, look up `issue_links` by `(integration_id, external_id)`.
3. **No link** → create a new task in a project named after the
   integration label, attach an `issue_links` row.
4. **Link exists** → update `external_state` if it changed; if the
   upstream issue is now closed and the linked ttl task is open,
   complete the task.
5. Bump `integrations.last_synced_at`.

The result is a `SyncStats{ Created, Updated, Closed, Unchanged }`.

## Webhooks

For instant updates without polling, point the upstream service at:

```
POST /api/v1/webhooks/<provider>
Headers:
  X-Ttl-Integration: <integration-id>
  X-Hub-Signature-256: sha256=<hmac>      (GitHub)
  Linear-Signature:    sha256=<hmac>      (Linear)
Body: provider-native payload
```

Webhook URLs and the HMAC secret are printed by `ttl integrations add`.
The handler verifies the signature, normalises the event, and calls
`Syncer.HandleWebhook` — same code path as `Sync`, but for a single
issue at a time.

### Setting up GitHub webhooks

1. In GitHub, **Settings → Webhooks → Add webhook**
2. Payload URL: `https://your-ttl-host/api/v1/webhooks/github`
3. Content type: `application/json`
4. Secret: the value you passed to `ttl integrations add` (or skip if
   you omitted `--webhook-secret` — but then signature verification fails)
5. Events: "Issues" only
6. Save and copy the webhook ID. Set the `X-Ttl-Integration` header
   to your integration ID (from `ttl integrations list`).

### Setting up Linear webhooks

1. Linear → Settings → API → Webhooks → New webhook
2. URL: `https://your-ttl-host/api/v1/webhooks/linear`
3. Resource types: Issue
4. Secret: same as above

## Security notes

- The PAT / API key is stored in `integrations.config_json` in
  plaintext. Encrypting at rest is on the roadmap; until then,
  restrict file permissions on the SQLite database.
- Webhook signatures prevent arbitrary hosts from forging events.
  Always set a webhook secret.
- The webhook endpoint is the only public, unauthenticated route. It
  cannot list or modify data without a valid HMAC signature.

## Tests

- `internal/integrations/integrations_test.go` covers HMAC verification
  for both providers and a sample GitHub `issues` webhook round-trip.
- The full provider+syncer flow requires real upstream credentials and
  is covered manually against the GitHub/Linear sandboxes.
