// Integration and issue-link store methods.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/tenant"
)

// CreateIntegration persists a new tenant-scoped integration.
// config_json is opaque to the store.
func (s *Store) CreateIntegration(ctx context.Context, tc *tenant.Context, provider, label string, config map[string]string) (*model.Integration, error) {
	if provider == "" || label == "" {
		return nil, errors.New("provider and label are required")
	}
	cfgJSON, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	now := time.Now()
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO integrations(id, tenant_id, provider, label, config_json, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, tc.TenantID, provider, label, string(cfgJSON), tc.UserID, now.UnixMilli(),
	)
	if err != nil {
		return nil, err
	}
	return s.GetIntegration(ctx, tc, id)
}

func (s *Store) GetIntegration(ctx context.Context, tc *tenant.Context, id string) (*model.Integration, error) {
	const sel = `
		SELECT id, tenant_id, provider, label, config_json,
		       created_by, created_at, last_synced_at
		FROM integrations WHERE id = ? AND tenant_id = ?`
	var (
		it         model.Integration
		cfgJSON    string
		createdBy  string
		createdAt  int64
		lastSynced sql.NullInt64
	)
	err := s.DB.QueryRowContext(ctx, sel, id, tc.TenantID).Scan(
		&it.ID, &it.TenantID, &it.Provider, &it.Label, &cfgJSON,
		&createdBy, &createdAt, &lastSynced,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(cfgJSON), &it.Config)
	it.CreatedBy = createdBy
	it.CreatedAt = time.UnixMilli(createdAt)
	if lastSynced.Valid {
		t := time.UnixMilli(lastSynced.Int64)
		it.LastSyncedAt = &t
	}
	return &it, nil
}

// ListIntegrations returns all integrations for the tenant. config is
// NOT included in the result (it's a secret).
func (s *Store) ListIntegrations(ctx context.Context, tc *tenant.Context) ([]model.Integration, error) {
	const sel = `
		SELECT id, tenant_id, provider, label, created_by, created_at, last_synced_at
		FROM integrations WHERE tenant_id = ?
		ORDER BY created_at DESC`
	rows, err := s.DB.QueryContext(ctx, sel, tc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Integration
	for rows.Next() {
		var (
			it         model.Integration
			createdBy  string
			createdAt  int64
			lastSynced sql.NullInt64
		)
		if err := rows.Scan(
			&it.ID, &it.TenantID, &it.Provider, &it.Label,
			&createdBy, &createdAt, &lastSynced,
		); err != nil {
			return nil, err
		}
		it.CreatedBy = createdBy
		it.CreatedAt = time.UnixMilli(createdAt)
		if lastSynced.Valid {
			t := time.UnixMilli(lastSynced.Int64)
			it.LastSyncedAt = &t
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DeleteIntegration removes an integration and cascades its issue_links.
func (s *Store) DeleteIntegration(ctx context.Context, tc *tenant.Context, id string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM integrations WHERE id = ? AND tenant_id = ?`,
		id, tc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchIntegration updates last_synced_at to "now".
func (s *Store) TouchIntegration(ctx context.Context, tc *tenant.Context, id string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE integrations SET last_synced_at = ? WHERE id = ? AND tenant_id = ?`,
		time.Now().UnixMilli(), id, tc.TenantID)
	return err
}

// UpsertIssueLink records that taskID is linked to externalID on the
// given integration, or refreshes the external_state if it already exists.
func (s *Store) UpsertIssueLink(ctx context.Context, tc *tenant.Context, integrationID, taskID, provider, externalID, url, state string) (*model.IssueLink, error) {
	now := time.Now().UnixMilli()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO issue_links(id, tenant_id, task_id, integration_id, provider, external_id, external_url, external_state, last_synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(integration_id, external_id) DO UPDATE SET
		   task_id = excluded.task_id,
		   external_state = excluded.external_state,
		   external_url = excluded.external_url,
		   last_synced_at = excluded.last_synced_at`,
		uuid.NewString(), tc.TenantID, taskID, integrationID, provider,
		externalID, url, state, now,
	)
	if err != nil {
		return nil, err
	}
	var l model.IssueLink
	err = s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, task_id, integration_id, provider, external_id, external_url, external_state, last_synced_at
		 FROM issue_links WHERE integration_id = ? AND external_id = ?`,
		integrationID, externalID,
	).Scan(&l.ID, &l.TenantID, &l.TaskID, &l.IntegrationID,
		&l.Provider, &l.ExternalID, &l.ExternalURL, &l.ExternalState, &l.LastSyncedAt)
	if err != nil {
		return nil, err
	}
	l.LastSyncedAt = time.UnixMilli(l.LastSyncedAt.UnixMilli())
	return &l, nil
}

// IssueLinkForTask returns the link for a task (at most one per task in
// the current data model).
func (s *Store) IssueLinkForTask(ctx context.Context, tc *tenant.Context, taskID string) (*model.IssueLink, error) {
	var l model.IssueLink
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, task_id, integration_id, provider, external_id, external_url, external_state, last_synced_at
		 FROM issue_links WHERE tenant_id = ? AND task_id = ?`,
		tc.TenantID, taskID,
	).Scan(&l.ID, &l.TenantID, &l.TaskID, &l.IntegrationID,
		&l.Provider, &l.ExternalID, &l.ExternalURL, &l.ExternalState, &l.LastSyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	l.LastSyncedAt = time.UnixMilli(l.LastSyncedAt.UnixMilli())
	return &l, nil
}

// IssueLinkForExternal returns the link for a specific external_id on
// an integration, or ErrNotFound.
func (s *Store) IssueLinkForExternal(ctx context.Context, tc *tenant.Context, integrationID, externalID string) (*model.IssueLink, error) {
	var l model.IssueLink
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, tenant_id, task_id, integration_id, provider, external_id, external_url, external_state, last_synced_at
		 FROM issue_links WHERE tenant_id = ? AND integration_id = ? AND external_id = ?`,
		tc.TenantID, integrationID, externalID,
	).Scan(&l.ID, &l.TenantID, &l.TaskID, &l.IntegrationID,
		&l.Provider, &l.ExternalID, &l.ExternalURL, &l.ExternalState, &l.LastSyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	l.LastSyncedAt = time.UnixMilli(l.LastSyncedAt.UnixMilli())
	return &l, nil
}
