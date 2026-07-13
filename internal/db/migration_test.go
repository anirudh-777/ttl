package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestV04DatabaseMigratesToV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	d, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0001_init.sql", "0002_time_entries.sql", "0003_reminders.sql", "0004_integrations.sql"} {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := d.Exec(`INSERT INTO schema_meta(id, version, updated_at) VALUES (1, ?, 0) ON CONFLICT(id) DO UPDATE SET version=excluded.version`, versionFromName(name)); err != nil {
			t.Fatal(err)
		}
	}
	// Seed representative v0.4 data so the test proves migration preserves
	// existing user credentials and task/reminder records, not just columns.
	for _, stmt := range []string{
		`INSERT INTO tenants(id, name, created_at) VALUES ('tenant-old', 'Old workspace', 1)`,
		`INSERT INTO users(id, tenant_id, email, password_hash, role, created_at) VALUES ('user-old', 'tenant-old', 'old@example.com', 'hash', 'owner', 1)`,
		`INSERT INTO api_keys(id, user_id, tenant_id, key_hash, name, created_at) VALUES ('key-old', 'user-old', 'tenant-old', 'key-hash', 'legacy cli', 1)`,
		`INSERT INTO tasks(id, tenant_id, title, created_by, created_at, updated_at) VALUES ('task-old', 'tenant-old', 'Keep me', 'user-old', 1, 1)`,
		`INSERT INTO reminders(id, tenant_id, task_id, user_id, fire_at, created_at) VALUES ('reminder-old', 'tenant-old', 'task-old', 'user-old', 1000, 1)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.Close()
	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	for _, check := range []string{
		`SELECT deleted_at, position FROM tasks LIMIT 0`,
		`SELECT scopes_json, expires_at FROM api_keys LIMIT 0`,
		`SELECT planned_duration_ms, target_end_at FROM time_entries LIMIT 0`,
		`SELECT endpoint_id, delivery_status FROM reminders LIMIT 0`,
		`SELECT id FROM invites LIMIT 0`, `SELECT id FROM notification_endpoints LIMIT 0`,
	} {
		if _, err := upgraded.Exec(check); err != nil {
			t.Errorf("missing v1 schema for %q: %v", check, err)
		}
	}
	var title, scopes, delivery string
	var position int
	if err := upgraded.QueryRow(`SELECT title, position FROM tasks WHERE id = 'task-old'`).Scan(&title, &position); err != nil {
		t.Fatal(err)
	}
	if title != "Keep me" || position != 0 {
		t.Fatalf("migrated task = (%q, %d)", title, position)
	}
	if err := upgraded.QueryRow(`SELECT scopes_json FROM api_keys WHERE id = 'key-old'`).Scan(&scopes); err != nil {
		t.Fatal(err)
	}
	if scopes == "" {
		t.Fatal("legacy API key has no compatibility scopes")
	}
	if err := upgraded.QueryRow(`SELECT delivery_status FROM reminders WHERE id = 'reminder-old'`).Scan(&delivery); err != nil {
		t.Fatal(err)
	}
	if delivery != "pending" {
		t.Fatalf("legacy reminder delivery status = %q", delivery)
	}
}
