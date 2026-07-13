package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/anirudh-777/ttl/internal/db"
	"github.com/anirudh-777/ttl/internal/tenant"
)

func TestInviteAndScopedExpiringKey(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	owner, err := Signup(context.Background(), d, "Team", "owner@test.local", "password")
	if err != nil {
		t.Fatal(err)
	}
	tc := &tenant.Context{TenantID: owner.TenantID, UserID: owner.ID, Role: "owner"}
	token, _, err := CreateInvite(context.Background(), d, tc, "member", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	member, err := JoinWithInvite(context.Background(), d, token, "member@test.local", "password")
	if err != nil {
		t.Fatal(err)
	}
	if member.TenantID != owner.TenantID || member.Role != "member" {
		t.Fatalf("member=%+v", member)
	}
	if _, err := JoinWithInvite(context.Background(), d, token, "again@test.local", "password"); err == nil {
		t.Fatal("invite reused")
	}
	exp := time.Now().Add(time.Hour)
	plain, key, err := IssueAPIKeyWithOptions(context.Background(), d, member, "agent", []string{"tasks:read"}, &exp)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := LookupAPIKey(context.Background(), d, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.HasScope("tasks:read") || resolved.HasScope("tasks:write") {
		t.Fatalf("scopes=%v", resolved.Scopes)
	}
	memberTC := &tenant.Context{TenantID: member.TenantID, UserID: member.ID}
	if err := RenameAPIKey(context.Background(), d, memberTC, key.ID, "renamed agent"); err != nil {
		t.Fatal(err)
	}
	rotated, replacement, err := RotateAPIKey(context.Background(), d, memberTC, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LookupAPIKey(context.Background(), d, plain); err == nil {
		t.Fatal("rotated old key accepted")
	}
	if _, err := LookupAPIKey(context.Background(), d, rotated); err != nil {
		t.Fatalf("replacement rejected: %v", err)
	}
	if replacement.Name != "renamed agent" {
		t.Fatalf("replacement name=%q", replacement.Name)
	}
	if err := RevokeAPIKey(context.Background(), d, memberTC, replacement.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := LookupAPIKey(context.Background(), d, rotated); err == nil {
		t.Fatal("revoked replacement accepted")
	}
}

func TestEmailIdentityIsGloballyUnambiguous(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "email.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	owner, err := Signup(ctx, d, "First", "same@test.local", "password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Signup(ctx, d, "Second", "same@test.local", "password"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate workspace signup err=%v", err)
	}
	tc := &tenant.Context{TenantID: owner.TenantID, UserID: owner.ID, Role: owner.Role}
	token, _, err := CreateInvite(ctx, d, tc, "member", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := JoinWithInvite(ctx, d, token, "same@test.local", "password"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate invited email err=%v", err)
	}
	if _, err := Login(ctx, d, "same@test.local", "password"); err != nil {
		t.Fatalf("unique login failed: %v", err)
	}
}

func TestBootstrapSignupCanOnlyCreateOneWorkspace(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "bootstrap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	if _, err := SignupBootstrap(ctx, d, "First", "first@test.local", "password"); err != nil {
		t.Fatal(err)
	}
	if _, err := SignupBootstrap(ctx, d, "Second", "second@test.local", "password"); !errors.Is(err, ErrBootstrapDone) {
		t.Fatalf("second bootstrap err=%v", err)
	}
	var tenants int
	if err := d.QueryRow(`SELECT COUNT(*) FROM tenants`).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if tenants != 1 {
		t.Fatalf("bootstrap left %d tenants", tenants)
	}
}
