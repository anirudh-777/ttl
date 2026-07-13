-- v1 core-product readiness: recoverable task deletion, stable ordering,
-- bounded pomodoros, scoped credentials, team invites, and reminder webhooks.

ALTER TABLE tasks ADD COLUMN deleted_at INTEGER;
ALTER TABLE tasks ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_deleted
    ON tasks(tenant_id, deleted_at, status);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_project_position
    ON tasks(tenant_id, project_id, parent_id, position);

ALTER TABLE time_entries ADD COLUMN planned_duration_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE time_entries ADD COLUMN target_end_at INTEGER;

ALTER TABLE api_keys ADD COLUMN scopes_json TEXT NOT NULL DEFAULT '["tasks:read","tasks:write","tasks:delete","productivity:read","productivity:write","workspace:read","workspace:write","integrations:manage","admin"]';
ALTER TABLE api_keys ADD COLUMN expires_at INTEGER;

CREATE TABLE IF NOT EXISTS invites (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    role        TEXT NOT NULL DEFAULT 'member',
    created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  INTEGER NOT NULL,
    used_at     INTEGER,
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_invites_tenant ON invites(tenant_id, expires_at);

CREATE TABLE IF NOT EXISTS notification_endpoints (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    url         TEXT NOT NULL,
    secret_hash TEXT NOT NULL,
    secret_enc  TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_by  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, name)
);

ALTER TABLE reminders ADD COLUMN endpoint_id TEXT REFERENCES notification_endpoints(id) ON DELETE SET NULL;
ALTER TABLE reminders ADD COLUMN delivery_status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE reminders ADD COLUMN delivery_error TEXT;
