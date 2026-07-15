ALTER TABLE tasks ADD COLUMN started_at INTEGER;

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_status_started
    ON tasks(tenant_id, status, started_at);
