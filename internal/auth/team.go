package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/tenant"
)

func HasUsers(ctx context.Context, d *sql.DB) (bool, error) {
	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func CreateInvite(ctx context.Context, d *sql.DB, tc *tenant.Context, role string, expiresAt time.Time) (string, *model.Invite, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = "member"
	}
	if role != "member" && role != "admin" {
		return "", nil, errors.New("role must be member or admin")
	}
	if role == "admin" && tc.Role != "owner" {
		return "", nil, errors.New("only owners can invite admins")
	}
	if !expiresAt.After(time.Now()) {
		return "", nil, errors.New("invite expiry must be in the future")
	}
	raw, err := randomToken(32)
	if err != nil {
		return "", nil, err
	}
	id, now := uuid.NewString(), time.Now()
	_, err = d.ExecContext(ctx, `INSERT INTO invites(id, tenant_id, token_hash, role, created_by, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, tc.TenantID, hashKey(raw), role, tc.UserID, expiresAt.UnixMilli(), now.UnixMilli())
	if err != nil {
		return "", nil, err
	}
	return raw, &model.Invite{ID: id, TenantID: tc.TenantID, Role: role, CreatedBy: tc.UserID, ExpiresAt: expiresAt, CreatedAt: now}, nil
}

func ListInvites(ctx context.Context, d *sql.DB, tc *tenant.Context) ([]model.Invite, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, tenant_id, role, created_by, expires_at, used_at, created_at FROM invites WHERE tenant_id = ? ORDER BY created_at DESC`, tc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Invite
	for rows.Next() {
		var v model.Invite
		var exp, created int64
		var used sql.NullInt64
		if err := rows.Scan(&v.ID, &v.TenantID, &v.Role, &v.CreatedBy, &exp, &used, &created); err != nil {
			return nil, err
		}
		v.ExpiresAt, v.CreatedAt = time.UnixMilli(exp), time.UnixMilli(created)
		if used.Valid {
			t := time.UnixMilli(used.Int64)
			v.UsedAt = &t
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func JoinWithInvite(ctx context.Context, d *sql.DB, token, email, password string) (*model.User, error) {
	email, token = strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(token)
	if token == "" || email == "" || password == "" {
		return nil, errors.New("invite token, email and password are required")
	}
	if len(password) < 6 {
		return nil, errors.New("password must be at least 6 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var inviteID, tenantID, role string
	var expires int64
	var used sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, tenant_id, role, expires_at, used_at FROM invites WHERE token_hash = ?`, hashKey(token)).Scan(&inviteID, &tenantID, &role, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("invalid invite")
	}
	if err != nil {
		return nil, err
	}
	if used.Valid || time.UnixMilli(expires).Before(time.Now()) {
		return nil, errors.New("invite expired or already used")
	}
	id, now := uuid.NewString(), time.Now()
	res, err := tx.ExecContext(ctx, `INSERT INTO users(id, tenant_id, email, password_hash, role, created_at)
		SELECT ?, ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM users WHERE email = ?)`, id, tenantID, email, hash, role, now.UnixMilli(), email)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return nil, ErrEmailTaken
	}
	res, err = tx.ExecContext(ctx, `UPDATE invites SET used_at = ? WHERE id = ? AND used_at IS NULL`, now.UnixMilli(), inviteID)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return nil, errors.New("invite expired or already used")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &model.User{ID: id, TenantID: tenantID, Email: email, Role: role, CreatedAt: now}, nil
}

func ListMembers(ctx context.Context, d *sql.DB, tc *tenant.Context) ([]model.User, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, tenant_id, email, role, created_at FROM users WHERE tenant_id = ? ORDER BY created_at`, tc.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		var created int64
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &created); err != nil {
			return nil, err
		}
		u.CreatedAt = time.UnixMilli(created)
		out = append(out, u)
	}
	return out, rows.Err()
}

func SetMemberRole(ctx context.Context, d *sql.DB, tc *tenant.Context, userID, role string) error {
	if tc.Role != "owner" {
		return errors.New("only owners can change roles")
	}
	if role != "member" && role != "admin" && role != "owner" {
		return errors.New("invalid role")
	}
	if role != "owner" {
		var current string
		if err := d.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ? AND tenant_id = ?`, userID, tc.TenantID).Scan(&current); err != nil {
			return err
		}
		if current == "owner" {
			var n int
			_ = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = ? AND role = 'owner'`, tc.TenantID).Scan(&n)
			if n <= 1 {
				return errors.New("cannot demote final owner")
			}
		}
	}
	res, err := d.ExecContext(ctx, `UPDATE users SET role = ? WHERE id = ? AND tenant_id = ?`, role, userID, tc.TenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("member not found")
	}
	return nil
}

func RemoveMember(ctx context.Context, d *sql.DB, tc *tenant.Context, userID string) error {
	if tc.Role != "owner" {
		return errors.New("only owners can remove members")
	}
	if userID == tc.UserID {
		return errors.New("cannot remove yourself")
	}
	var role string
	if err := d.QueryRowContext(ctx, `SELECT role FROM users WHERE id = ? AND tenant_id = ?`, userID, tc.TenantID).Scan(&role); err != nil {
		return err
	}
	if role == "owner" {
		var n int
		_ = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = ? AND role = 'owner'`, tc.TenantID).Scan(&n)
		if n <= 1 {
			return errors.New("cannot remove final owner")
		}
	}
	_, err := d.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND tenant_id = ?`, userID, tc.TenantID)
	return err
}
