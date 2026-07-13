// Package auth handles password hashing, sessions, and API keys.
//
// Sessions are browser cookies (web UI). API keys are long-lived tokens
// passed in the X-API-Key header (CLI and MCP clients). Both are bound
// to a tenant_id at issuance time and cannot cross tenants.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/tenant"
)

// Errors callers can check.
var (
	ErrEmailTaken     = errors.New("email already in use")
	ErrInvalidLogin   = errors.New("invalid email or password")
	ErrSessionExpired = errors.New("session expired")
	ErrInvalidAPIKey  = errors.New("invalid api key")
	ErrAmbiguousEmail = errors.New("email belongs to multiple workspaces")
	ErrBootstrapDone  = errors.New("initial workspace has already been created")
)

var FullScopes = []string{
	"tasks:read", "tasks:write", "tasks:delete", "productivity:read", "productivity:write",
	"workspace:read", "workspace:write", "integrations:manage", "admin",
}

// HashPassword returns a bcrypt hash of the plaintext password.
func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// VerifyPassword reports whether plain matches the bcrypt hash.
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// Signup creates a new tenant, an owner user, and returns the user.
// The tenant name and user email are trimmed; password must be non-empty.
func Signup(ctx context.Context, d *sql.DB, tenantName, email, password string) (*model.User, error) {
	return signup(ctx, d, tenantName, email, password, false)
}

// SignupBootstrap creates the initial workspace only if the installation has
// no users. The check and insert share one write transaction, so concurrent
// first-run requests cannot both bootstrap separate workspaces.
func SignupBootstrap(ctx context.Context, d *sql.DB, tenantName, email, password string) (*model.User, error) {
	return signup(ctx, d, tenantName, email, password, true)
}

func signup(ctx context.Context, d *sql.DB, tenantName, email, password string, bootstrapOnly bool) (*model.User, error) {
	tenantName = strings.TrimSpace(tenantName)
	email = strings.TrimSpace(strings.ToLower(email))
	if tenantName == "" || email == "" || password == "" {
		return nil, errors.New("tenant name, email and password are required")
	}
	if len(password) < 6 {
		return nil, errors.New("password must be at least 6 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	tenantID := uuid.NewString()
	userID := uuid.NewString()
	now := time.Now().UnixMilli()

	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tenants(id, name, created_at) VALUES (?, ?, ?)`,
		tenantID, tenantName, now,
	); err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}

	condition := `NOT EXISTS (SELECT 1 FROM users WHERE email = ?)`
	if bootstrapOnly {
		condition += ` AND NOT EXISTS (SELECT 1 FROM users)`
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO users(id, tenant_id, email, password_hash, role, created_at)
		 SELECT ?, ?, ?, ?, 'owner', ?
		 WHERE `+condition,
		userID, tenantID, email, hash, now,
		email,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		if bootstrapOnly {
			var users int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users); err == nil && users > 0 {
				return nil, ErrBootstrapDone
			}
		}
		return nil, ErrEmailTaken
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &model.User{
		ID: userID, TenantID: tenantID, Email: email,
		Role: "owner", CreatedAt: time.UnixMilli(now),
	}, nil
}

// Login verifies credentials and returns the user.
func Login(ctx context.Context, d *sql.DB, email, password string) (*model.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	rows, err := d.QueryContext(ctx,
		`SELECT id, tenant_id, email, password_hash, role, created_at
		 FROM users WHERE email = ? LIMIT 2`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type candidate struct {
		u         model.User
		hash      string
		createdAt int64
	}
	var found []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.u.ID, &c.u.TenantID, &c.u.Email, &c.hash, &c.u.Role, &c.createdAt); err != nil {
			return nil, err
		}
		found = append(found, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrInvalidLogin
	}
	if len(found) > 1 {
		return nil, ErrAmbiguousEmail
	}
	c := found[0]
	if !VerifyPassword(c.hash, password) {
		return nil, ErrInvalidLogin
	}
	c.u.CreatedAt = time.UnixMilli(c.createdAt)
	return &c.u, nil
}

// CreateSession returns a session token (random, base64) valid for 30 days.
func CreateSession(ctx context.Context, d *sql.DB, user *model.User) (string, time.Time, error) {
	tok, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(30 * 24 * time.Hour)
	_, err = d.ExecContext(ctx,
		`INSERT INTO sessions(id, user_id, tenant_id, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		tok, user.ID, user.TenantID, expires.UnixMilli(), time.Now().UnixMilli(),
	)
	if err != nil {
		return "", time.Time{}, err
	}
	return tok, expires, nil
}

// LookupSession returns the tenant context for a session token, or an
// error if the token is unknown or expired.
func LookupSession(ctx context.Context, d *sql.DB, token string) (*tenant.Context, error) {
	var userID, tenantID string
	var expiresAt int64
	err := d.QueryRowContext(ctx,
		`SELECT user_id, tenant_id, expires_at FROM sessions WHERE id = ?`,
		token,
	).Scan(&userID, &tenantID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionExpired
	}
	if err != nil {
		return nil, err
	}
	if time.UnixMilli(expiresAt).Before(time.Now()) {
		return nil, ErrSessionExpired
	}
	var role string
	if err := d.QueryRowContext(ctx,
		`SELECT role FROM users WHERE id = ?`, userID,
	).Scan(&role); err != nil {
		return nil, err
	}
	return &tenant.Context{
		TenantID: tenantID, UserID: userID, Role: role,
	}, nil
}

// DestroySession deletes a session token (logout).
func DestroySession(ctx context.Context, d *sql.DB, token string) error {
	_, err := d.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, token)
	return err
}

// IssueAPIKey returns the plaintext key (only shown once) and the
// stored model. The plaintext form is "ttk_" + 32 random base64 chars.
func IssueAPIKey(ctx context.Context, d *sql.DB, user *model.User, name string) (string, *model.APIKey, error) {
	return IssueAPIKeyWithOptions(ctx, d, user, name, FullScopes, nil)
}

func IssueAPIKeyWithOptions(ctx context.Context, d *sql.DB, user *model.User, name string, scopes []string, expiresAt *time.Time) (string, *model.APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "cli"
	}
	raw, err := randomToken(32)
	if err != nil {
		return "", nil, err
	}
	plain := "ttk_" + raw
	id := uuid.NewString()
	now := time.Now().UnixMilli()
	hash := hashKey(plain)
	if len(scopes) == 0 {
		scopes = []string{"tasks:read", "tasks:write"}
	}
	scopeJSON, _ := json.Marshal(scopes)
	if _, err := d.ExecContext(ctx,
		`INSERT INTO api_keys(id, user_id, tenant_id, key_hash, name, created_at, scopes_json, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, user.ID, user.TenantID, hash, name, now, string(scopeJSON), nullableAuthTime(expiresAt),
	); err != nil {
		return "", nil, err
	}
	k := &model.APIKey{
		ID: id, UserID: user.ID, TenantID: user.TenantID,
		Name: name, CreatedAt: time.UnixMilli(now), Scopes: scopes, ExpiresAt: expiresAt,
	}
	_, _ = d.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now, id,
	)
	return plain, k, nil
}

// LookupAPIKey returns the tenant context for an API key, or an error.
func LookupAPIKey(ctx context.Context, d *sql.DB, plain string) (*tenant.Context, error) {
	if !strings.HasPrefix(plain, "ttk_") {
		return nil, ErrInvalidAPIKey
	}
	hash := hashKey(plain)
	var userID, tenantID, role, scopesJSON string
	var keyID string
	var expiresAt sql.NullInt64
	err := d.QueryRowContext(ctx,
		`SELECT ak.id, ak.user_id, ak.tenant_id, u.role, ak.scopes_json, ak.expires_at
		 FROM api_keys ak JOIN users u ON u.id = ak.user_id
		 WHERE ak.key_hash = ?`, hash,
	).Scan(&keyID, &userID, &tenantID, &role, &scopesJSON, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidAPIKey
	}
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid && time.UnixMilli(expiresAt.Int64).Before(time.Now()) {
		return nil, ErrInvalidAPIKey
	}
	var scopes []string
	if err := json.Unmarshal([]byte(scopesJSON), &scopes); err != nil {
		return nil, ErrInvalidAPIKey
	}
	_, _ = d.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		time.Now().UnixMilli(), keyID,
	)
	return &tenant.Context{
		TenantID: tenantID, UserID: userID, Role: role, Scopes: scopes,
	}, nil
}

func ListAPIKeys(ctx context.Context, d *sql.DB, tc *tenant.Context) ([]model.APIKey, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id, user_id, tenant_id, name, last_used_at, created_at, scopes_json, expires_at
		 FROM api_keys WHERE tenant_id = ? AND user_id = ? ORDER BY created_at DESC`, tc.TenantID, tc.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.APIKey
	for rows.Next() {
		var k model.APIKey
		var last, exp sql.NullInt64
		var created int64
		var scopes string
		if err := rows.Scan(&k.ID, &k.UserID, &k.TenantID, &k.Name, &last, &created, &scopes, &exp); err != nil {
			return nil, err
		}
		k.CreatedAt = time.UnixMilli(created)
		_ = json.Unmarshal([]byte(scopes), &k.Scopes)
		if last.Valid {
			v := time.UnixMilli(last.Int64)
			k.LastUsedAt = &v
		}
		if exp.Valid {
			v := time.UnixMilli(exp.Int64)
			k.ExpiresAt = &v
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func RevokeAPIKey(ctx context.Context, d *sql.DB, tc *tenant.Context, id string) error {
	res, err := d.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND tenant_id = ? AND user_id = ?`, id, tc.TenantID, tc.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrInvalidAPIKey
	}
	return nil
}

func RenameAPIKey(ctx context.Context, d *sql.DB, tc *tenant.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name is required")
	}
	res, err := d.ExecContext(ctx, `UPDATE api_keys SET name = ? WHERE id = ? AND tenant_id = ? AND user_id = ?`, name, id, tc.TenantID, tc.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrInvalidAPIKey
	}
	return nil
}

// RotateAPIKey replaces a key while preserving its name, scopes and expiry.
// The old credential is revoked only after the replacement is stored.
func RotateAPIKey(ctx context.Context, d *sql.DB, tc *tenant.Context, id string) (string, *model.APIKey, error) {
	var name, scopesJSON string
	var expires sql.NullInt64
	err := d.QueryRowContext(ctx, `SELECT name, scopes_json, expires_at FROM api_keys WHERE id = ? AND tenant_id = ? AND user_id = ?`, id, tc.TenantID, tc.UserID).Scan(&name, &scopesJSON, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrInvalidAPIKey
	}
	if err != nil {
		return "", nil, err
	}
	var scopes []string
	if err := json.Unmarshal([]byte(scopesJSON), &scopes); err != nil {
		return "", nil, err
	}
	var expiry *time.Time
	if expires.Valid {
		v := time.UnixMilli(expires.Int64)
		expiry = &v
	}
	raw, replacement, err := IssueAPIKeyWithOptions(ctx, d, &model.User{ID: tc.UserID, TenantID: tc.TenantID}, name, scopes, expiry)
	if err != nil {
		return "", nil, err
	}
	if err := RevokeAPIKey(ctx, d, tc, id); err != nil {
		_ = RevokeAPIKey(ctx, d, tc, replacement.ID)
		return "", nil, err
	}
	return raw, replacement, nil
}

func nullableAuthTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// HashAPIKey exposes the sha256 hash of an API key plaintext so other
// packages (e.g. the WebSocket upgrade handler) can match a key the
// client provides against the stored hash.
func HashAPIKey(plain string) string { return hashKey(plain) }
