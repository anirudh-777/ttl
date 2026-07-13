package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/tenant"
)

func (s *Store) CreateNotificationEndpoint(ctx context.Context, tc *tenant.Context, name, rawURL string) (string, *model.NotificationEndpoint, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, errors.New("endpoint name required")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", nil, errors.New("endpoint must be an http(s) URL without credentials")
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", nil, err
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	sum := sha256.Sum256([]byte(secret))
	id, now := uuid.NewString(), time.Now()
	_, err = s.DB.ExecContext(ctx, `INSERT INTO notification_endpoints(id, tenant_id, name, url, secret_hash, secret_enc, enabled, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)`, id, tc.TenantID, name, u.String(), hex.EncodeToString(sum[:]), secret, tc.UserID, now.UnixMilli())
	if err != nil {
		return "", nil, err
	}
	return secret, &model.NotificationEndpoint{ID: id, TenantID: tc.TenantID, Name: name, URL: u.String(), Enabled: true, CreatedBy: tc.UserID, CreatedAt: now}, nil
}

func (s *Store) ListNotificationEndpoints(ctx context.Context, tc *tenant.Context) ([]model.NotificationEndpoint, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, tenant_id, name, url, enabled, created_by, created_at FROM notification_endpoints WHERE tenant_id = ? ORDER BY name`, tc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.NotificationEndpoint
	for rows.Next() {
		var e model.NotificationEndpoint
		var enabled int
		var created int64
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Name, &e.URL, &enabled, &e.CreatedBy, &created); err != nil {
			return nil, err
		}
		e.Enabled = enabled != 0
		e.CreatedAt = time.UnixMilli(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetNotificationEndpointEnabled(ctx context.Context, tc *tenant.Context, id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE notification_endpoints SET enabled = ? WHERE id = ? AND tenant_id = ?`, v, id, tc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteNotificationEndpoint(ctx context.Context, tc *tenant.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM notification_endpoints WHERE id = ? AND tenant_id = ?`, id, tc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) NotificationDeliveryConfig(ctx context.Context, tenantID, id string) (rawURL, secret string, enabled bool, err error) {
	var on int
	err = s.DB.QueryRowContext(ctx, `SELECT url, secret_enc, enabled FROM notification_endpoints WHERE id = ? AND tenant_id = ?`, id, tenantID).Scan(&rawURL, &secret, &on)
	if err != nil {
		return "", "", false, err
	}
	return rawURL, secret, on != 0, nil
}

func (s *Store) RecordReminderDelivery(ctx context.Context, id string, errText string) {
	status := "delivered"
	var value any
	if errText != "" {
		status, value = "failed", errText
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE reminders SET delivery_status = ?, delivery_error = ? WHERE id = ?`, status, value, id)
}
