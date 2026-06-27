-- 0004_integrations.sql -- Phase 4: external issue providers.
--
-- An integration is a tenant-scoped connection to one provider
-- (github, linear, jira). Credentials and provider-specific config
-- are stored in config_json as opaque text. For Phase 4 we accept
-- Personal Access Tokens; OAuth flows land in Phase 5.
--
-- issue_links ties a task to an external issue. When the issue
-- changes state upstream we mirror the change; when the user
-- completes the task, we POST the close back to the provider.

CREATE TABLE IF NOT EXISTS integrations (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL,                  -- 'github' | 'linear'
    label           TEXT NOT NULL,                  -- user-chosen name
    config_json     TEXT NOT NULL DEFAULT '{}',     -- token + provider opts
    created_by      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      INTEGER NOT NULL,
    last_synced_at  INTEGER
);

CREATE INDEX IF NOT EXISTS idx_integrations_tenant_provider
    ON integrations(tenant_id, provider);

CREATE TABLE IF NOT EXISTS issue_links (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    integration_id  TEXT NOT NULL REFERENCES integrations(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL,                  -- denormalised for fast filtering
    external_id     TEXT NOT NULL,                  -- e.g. '42' for a GitHub issue
    external_url    TEXT NOT NULL DEFAULT '',
    external_state  TEXT NOT NULL DEFAULT 'open',   -- 'open' | 'closed' | ...
    last_synced_at  INTEGER NOT NULL,
    UNIQUE (integration_id, external_id)
);

CREATE INDEX IF NOT EXISTS idx_issue_links_task
    ON issue_links(task_id);
CREATE INDEX IF NOT EXISTS idx_issue_links_tenant_provider
    ON issue_links(tenant_id, provider, external_state);
