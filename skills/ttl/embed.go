// Package ttlskill embeds the agent skill in release binaries.
package ttlskill

import _ "embed"

// Content is the canonical ttl agent skill.
//
//go:embed SKILL.md
var Content string
