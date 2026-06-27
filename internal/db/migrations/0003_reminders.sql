-- 0003_reminders.sql -- per-task reminders.
--
-- A reminder is a one-shot notification at a specific time. The server
-- scans for "fired" reminders every minute and pushes them via the
-- WebSocket hub. Status: pending -> sent -> (optional) acknowledged.

CREATE TABLE IF NOT EXISTS reminders (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    task_id         TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fire_at         INTEGER NOT NULL,           -- unix ms
    status          TEXT NOT NULL DEFAULT 'pending', -- pending | sent | ack
    created_at      INTEGER NOT NULL,
    sent_at         INTEGER
);

CREATE INDEX IF NOT EXISTS idx_reminders_pending
    ON reminders(tenant_id, fire_at)
    WHERE status = 'pending';
