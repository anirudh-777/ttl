-- 0001_init.sql -- core schema for ttl
-- All business tables are scoped by tenant_id. Composite indexes put
-- tenant_id first so query plans never cross tenants.

CREATE TABLE IF NOT EXISTS tenants (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email           TEXT NOT NULL,
    password_hash   TEXT NOT NULL,
    role            TEXT NOT NULL DEFAULT 'member', -- 'owner' | 'admin' | 'member'
    created_at      INTEGER NOT NULL,
    UNIQUE (tenant_id, email)
);

CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);

CREATE TABLE IF NOT EXISTS api_keys (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key_hash        TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    last_used_at    INTEGER,
    created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    expires_at  INTEGER NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    color       TEXT NOT NULL DEFAULT '#888888',
    archived_at INTEGER,
    created_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_projects_tenant ON projects(tenant_id);

CREATE TABLE IF NOT EXISTS tags (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    color       TEXT NOT NULL DEFAULT '#888888',
    created_at  INTEGER NOT NULL,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_tags_tenant ON tags(tenant_id);

CREATE TABLE IF NOT EXISTS tasks (
    id                  TEXT PRIMARY KEY,
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          TEXT REFERENCES projects(id) ON DELETE SET NULL,
    parent_id           TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    title               TEXT NOT NULL,
    notes               TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'open', -- 'open' | 'done'
    priority            INTEGER NOT NULL DEFAULT 0,   -- 0 none, 1 low, 2 med, 3 high
    due_at              INTEGER,
    recurrence_rrule    TEXT,
    created_by          TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    completed_at        INTEGER
);

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_status_due
    ON tasks(tenant_id, status, due_at);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_project
    ON tasks(tenant_id, project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_parent
    ON tasks(tenant_id, parent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_updated
    ON tasks(tenant_id, updated_at);

CREATE TABLE IF NOT EXISTS task_tags (
    task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    tag_id      TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_task_tags_tenant_tag ON task_tags(tenant_id, tag_id);

-- Single-row metadata table used to record the current schema version.
CREATE TABLE IF NOT EXISTS schema_meta (
    id      INTEGER PRIMARY KEY CHECK (id = 1),
    version INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

INSERT OR IGNORE INTO schema_meta (id, version, updated_at)
VALUES (1, 1, strftime('%s','now') * 1000);
