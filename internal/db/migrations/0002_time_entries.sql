-- 0002_time_entries.sql -- time tracking + pomodoro sessions.
--
-- A row per "started an activity" event. We don't write a separate
-- row on stop; an entry is "open" while ended_at IS NULL. Only one
-- open entry per user is allowed (enforced by partial UNIQUE index).

CREATE TABLE IF NOT EXISTS time_entries (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_id         TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    kind            TEXT NOT NULL DEFAULT 'work',  -- 'work' | 'pomodoro'
    started_at      INTEGER NOT NULL,              -- unix ms
    ended_at        INTEGER,                        -- NULL while running
    duration_ms     INTEGER NOT NULL DEFAULT 0,    -- cached; recomputed on stop
    note            TEXT NOT NULL DEFAULT ''
);

-- Composite indexes for the queries we actually run.
CREATE INDEX IF NOT EXISTS idx_time_entries_tenant_user_started
    ON time_entries(tenant_id, user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_time_entries_tenant_task
    ON time_entries(tenant_id, task_id);

-- At most one open (ended_at IS NULL) entry per user. NULL values are
-- considered distinct in SQLite, so multiple closed entries per user
-- are still allowed.
CREATE UNIQUE INDEX IF NOT EXISTS idx_time_entries_open_per_user
    ON time_entries(user_id)
    WHERE ended_at IS NULL;
