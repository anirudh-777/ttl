// Package config persists CLI/TUI settings (server URL, API key).
//
// Storage location:
//   - $TTL_CONFIG_DIR/config.json if set
//   - ~/.config/ttl/config.json otherwise
//
// File mode 0600 because it contains the API key.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the on-disk shape.
type Config struct {
	ServerURL string `json:"server_url"`
	APIKey    string `json:"api_key"`
	Email     string `json:"email,omitempty"`
}

// Path returns the absolute path to the config file.
func Path() (string, error) {
	if d := os.Getenv("TTL_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ttl", "config.json"), nil
}

// PathHint returns a friendlier form of Path() suitable for messages.
func PathHint() string {
	p, err := Path()
	if err != nil {
		return "(unknown)"
	}
	return p
}

// Load reads the config file. Returns an empty Config (not an error)
// if the file does not exist yet. TTL_SERVER_URL env var overrides
// the on-disk server URL — useful for one-off CLI calls against a
// different host without rewriting config.
//
// Migration: if the on-disk server_url still ends in :8080 (the old
// default before v0.4.1), it is silently rewritten to :8093 so users
// upgrading from earlier versions don't hit "can't reach server" until
// they manually run `ttl login` again.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if v := os.Getenv("TTL_SERVER_URL"); v != "" {
		c.ServerURL = v
		return &c, nil
	}
	if migrated, changed := migrateLegacyPort(&c); changed {
		_ = Save(migrated) // best-effort
		fmt.Fprintf(os.Stderr,
			"note: config server_url auto-migrated from :8080 to :8093 (use TTL_SERVER_URL to override)\n")
		return migrated, nil
	}
	return &c, nil
}

// migrateLegacyPort bumps a server_url ending in the old :8080
// default to the new :8093 default. Returns the (possibly modified)
// config and whether anything changed.
func migrateLegacyPort(c *Config) (*Config, bool) {
	if c.ServerURL == "" {
		return c, false
	}
	if strings.HasSuffix(c.ServerURL, ":8080") || strings.HasSuffix(c.ServerURL, ":8080/") {
		new := strings.Replace(c.ServerURL, ":8080", ":8093", 1)
		c.ServerURL = new
		return c, true
	}
	return c, false
}

// Save writes the config to disk with mode 0600.
func Save(c *Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Clear deletes the config file (logout).
func Clear() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
