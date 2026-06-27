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
)

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
	tenantName = strings.TrimSpace(tenantName)
	email = strings.TrimSpace(strings.ToLower(email))
	if tenantName == "" || email == "" || password == "" {
		return nil, errors.New("tenant name, email and password are required")
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

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users(id, tenant_id, email, password_hash, role, created_at)
		 VALUES (?, ?, ?, ?, 'owner', ?)`,
		userID, tenantID, email, hash, now,
	); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
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
	var u model.User
	var hash string
	var createdAt int64
	err := d.QueryRowContext(ctx,
		`SELECT id, tenant_id, email, password_hash, role, created_at
		 FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.TenantID, &u.Email, &hash, &u.Role, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidLogin
	}
	if err != nil {
		return nil, err
	}
	if !VerifyPassword(hash, password) {
		return nil, ErrInvalidLogin
	}
	u.CreatedAt = time.UnixMilli(createdAt)
	return &u, nil
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
	if _, err := d.ExecContext(ctx,
		`INSERT INTO api_keys(id, user_id, tenant_id, key_hash, name, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, user.ID, user.TenantID, hash, name, now,
	); err != nil {
		return "", nil, err
	}
	k := &model.APIKey{
		ID: id, UserID: user.ID, TenantID: user.TenantID,
		Name: name, CreatedAt: time.UnixMilli(now),
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
	var userID, tenantID, role string
	var keyID string
	err := d.QueryRowContext(ctx,
		`SELECT ak.id, ak.user_id, ak.tenant_id, u.role
		 FROM api_keys ak JOIN users u ON u.id = ak.user_id
		 WHERE ak.key_hash = ?`, hash,
	).Scan(&keyID, &userID, &tenantID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidAPIKey
	}
	if err != nil {
		return nil, err
	}
	_, _ = d.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		time.Now().UnixMilli(), keyID,
	)
	return &tenant.Context{
		TenantID: tenantID, UserID: userID, Role: role,
	}, nil
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
