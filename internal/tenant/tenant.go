// Package tenant provides the per-request tenant/user context.
//
// Every store method that reads or writes business data takes a *Context
// as its first argument. This makes it impossible for a handler to
// accidentally cross tenants — the helper fails loud if the context is
// missing.
package tenant

import (
	"context"
	"errors"
)

// Context is the authenticated tenant + user for the current request.
type Context struct {
	TenantID string
	UserID   string
	Role     string // owner | admin | member
}

type ctxKey struct{}

// With attaches a tenant Context to a Go context.Context.
func With(ctx context.Context, tc *Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, tc)
}

// From extracts the tenant Context. Returns an error if missing.
func From(ctx context.Context) (*Context, error) {
	tc, ok := ctx.Value(ctxKey{}).(*Context)
	if !ok || tc == nil {
		return nil, errors.New("tenant context missing")
	}
	if tc.TenantID == "" || tc.UserID == "" {
		return nil, errors.New("tenant context incomplete")
	}
	return tc, nil
}

// MustFrom extracts or panics. Use only when the request is guaranteed
// to have been authenticated (e.g. behind RequireAuth middleware).
func MustFrom(ctx context.Context) *Context {
	tc, err := From(ctx)
	if err != nil {
		panic(err)
	}
	return tc
}
