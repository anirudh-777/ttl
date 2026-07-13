package agentsetup

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Target struct {
	Name       string
	Label      string
	SkillPath  string
	DetectPath string
	Command    string
	MCP        string
}

type Record struct {
	SkillPath string `json:"skill_path"`
	SkillHash string `json:"skill_hash"`
	MCP       bool   `json:"mcp"`
}

type Manifest struct {
	Agents map[string]Record `json:"agents"`
}

type Options struct {
	Home       string
	ConfigDir  string
	Binary     string
	Skill      string
	Runner     func(string, ...string) error
	Probe      func(string, ...string) error
	Lookup     func(string) (string, error)
	SkillsOnly bool
	DryRun     bool
	Output     func(string, ...any)
}

func Targets(home string) []Target {
	return []Target{
		{Name: "claude", Label: "Claude Code", SkillPath: filepath.Join(home, ".claude", "skills", "ttl", "SKILL.md"), DetectPath: filepath.Join(home, ".claude"), Command: "claude", MCP: "claude"},
		{Name: "codex", Label: "OpenAI Codex", SkillPath: filepath.Join(home, ".codex", "skills", "ttl", "SKILL.md"), DetectPath: filepath.Join(home, ".codex"), Command: "codex", MCP: "codex"},
		{Name: "cursor", Label: "Cursor", SkillPath: filepath.Join(home, ".cursor", "rules", "ttl.md"), DetectPath: filepath.Join(home, ".cursor"), Command: "cursor", MCP: "cursor"},
		{Name: "continue", Label: "Continue.dev", SkillPath: filepath.Join(home, ".continue", "rules", "ttl.md"), DetectPath: filepath.Join(home, ".continue"), Command: "continue"},
		{Name: "cline", Label: "Cline / Roo-Cline", SkillPath: filepath.Join(home, ".clinerules", "ttl.md"), DetectPath: filepath.Join(home, ".clinerules")},
		{Name: "roo", Label: "Roo Code", SkillPath: filepath.Join(home, ".roo", "rules", "ttl.md"), DetectPath: filepath.Join(home, ".roo")},
	}
}

func Select(all []Target, names []string, includeAll bool, lookup func(string) (string, error)) ([]Target, error) {
	byName := make(map[string]Target, len(all))
	for _, target := range all {
		byName[target.Name] = target
	}
	if includeAll {
		return all, nil
	}
	if len(names) > 0 {
		selected := make([]Target, 0, len(names))
		seen := map[string]bool{}
		for _, name := range names {
			name = strings.ToLower(strings.TrimSpace(name))
			target, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("unknown agent %q", name)
			}
			if !seen[name] {
				selected = append(selected, target)
				seen[name] = true
			}
		}
		return selected, nil
	}
	var selected []Target
	for _, target := range all {
		_, commandErr := lookup(target.Command)
		_, pathErr := os.Stat(target.DetectPath)
		if commandErr == nil || pathErr == nil {
			selected = append(selected, target)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("no supported coding agents detected; use --agent NAME or --all")
	}
	return selected, nil
}

func Install(targets []Target, opts Options) error {
	manifest, err := loadManifest(opts.ConfigDir)
	if err != nil {
		return err
	}
	for _, target := range targets {
		record := manifest.Agents[target.Name]
		if err := installSkill(target, record, opts); err != nil {
			return err
		}
		record.SkillPath = target.SkillPath
		record.SkillHash = contentHash([]byte(opts.Skill))
		manifest.Agents[target.Name] = record
		if !opts.DryRun {
			if err := saveManifest(opts.ConfigDir, manifest); err != nil {
				return err
			}
		}
		if !opts.SkillsOnly && target.MCP != "" {
			owned, err := installMCP(target, opts)
			if err != nil {
				return err
			}
			record.MCP = record.MCP || owned
		}
		manifest.Agents[target.Name] = record
		if !opts.DryRun {
			if err := saveManifest(opts.ConfigDir, manifest); err != nil {
				return err
			}
		}
	}
	return nil
}

func Uninstall(targets []Target, opts Options) error {
	manifest, err := loadManifest(opts.ConfigDir)
	if err != nil {
		return err
	}
	for _, target := range targets {
		record, owned := manifest.Agents[target.Name]
		if !owned {
			opts.Output("skip %s: not installed by ttl\n", target.Label)
			continue
		}
		if record.SkillPath != "" {
			if opts.DryRun {
				opts.Output("would remove skill: %s\n", record.SkillPath)
			} else if err := removeOwnedSkill(record.SkillPath, record.SkillHash); err != nil {
				return err
			} else {
				opts.Output("removed skill: %s\n", record.SkillPath)
			}
			record.SkillPath = ""
			record.SkillHash = ""
		}
		if record.MCP && !opts.SkillsOnly {
			if err := removeMCP(target, opts); err != nil {
				return err
			}
			record.MCP = false
		}
		if record.MCP || record.SkillPath != "" {
			manifest.Agents[target.Name] = record
		} else {
			delete(manifest.Agents, target.Name)
		}
		if !opts.DryRun {
			if err := saveManifest(opts.ConfigDir, manifest); err != nil {
				return err
			}
		}
	}
	return nil
}

func Status(targets []Target, opts Options) error {
	manifest, err := loadManifest(opts.ConfigDir)
	if err != nil {
		return err
	}
	for _, target := range targets {
		record, owned := manifest.Agents[target.Name]
		skill := "missing"
		if sameFile(target.SkillPath, opts.Skill) {
			skill = "installed"
		} else if _, err := os.Stat(target.SkillPath); err == nil {
			skill = "different file"
		}
		mcp := "not managed"
		if record.MCP {
			mcp = "installed"
		}
		if !owned && skill == "installed" {
			mcp = "external"
		}
		opts.Output("%-14s skill: %-14s mcp: %s\n", target.Label, skill, mcp)
	}
	return nil
}

func installSkill(target Target, record Record, opts Options) error {
	if data, err := os.ReadFile(target.SkillPath); err == nil && string(data) != opts.Skill {
		managed := record.SkillPath == target.SkillPath && record.SkillHash != "" && record.SkillHash == contentHash(data)
		if !managed {
			return fmt.Errorf("refusing to overwrite unrelated or modified file: %s", target.SkillPath)
		}
	}
	if opts.DryRun {
		opts.Output("would install skill: %s\n", target.SkillPath)
		return nil
	}
	if err := atomicWrite(target.SkillPath, []byte(opts.Skill), 0o644); err != nil {
		return err
	}
	opts.Output("installed skill: %s\n", target.SkillPath)
	return nil
}

func installMCP(target Target, opts Options) (bool, error) {
	if target.MCP == "cursor" {
		return installCursorMCP(opts)
	}
	if _, err := opts.Lookup(target.Command); err != nil {
		opts.Output("skip MCP for %s: %s command not found\n", target.Label, target.Command)
		return false, nil
	}
	var check []string
	var add []string
	switch target.MCP {
	case "codex":
		check = []string{"mcp", "get", "ttl"}
		add = []string{"mcp", "add", "ttl", "--", opts.Binary, "mcp"}
	case "claude":
		check = []string{"mcp", "get", "ttl"}
		add = []string{"mcp", "add", "--scope", "user", "ttl", "--", opts.Binary, "mcp"}
	}
	if err := opts.Probe(target.Command, check...); err == nil {
		opts.Output("kept existing MCP server: %s\n", target.Label)
		return false, nil
	}
	if opts.DryRun {
		opts.Output("would register MCP: %s\n", target.Label)
		return true, nil
	}
	if err := opts.Runner(target.Command, add...); err != nil {
		return false, fmt.Errorf("register %s MCP: %w", target.Label, err)
	}
	opts.Output("registered MCP: %s\n", target.Label)
	return true, nil
}

func removeMCP(target Target, opts Options) error {
	if target.MCP == "cursor" {
		if opts.DryRun {
			opts.Output("would remove MCP: %s\n", target.Label)
			return nil
		}
		return removeCursorMCP(opts)
	}
	if _, err := opts.Lookup(target.Command); err != nil {
		opts.Output("skip MCP removal for %s: command not found\n", target.Label)
		return nil
	}
	if opts.DryRun {
		opts.Output("would remove MCP: %s\n", target.Label)
		return nil
	}
	args := []string{"mcp", "remove", "ttl"}
	if target.MCP == "claude" {
		args = []string{"mcp", "remove", "--scope", "user", "ttl"}
	}
	if err := opts.Runner(target.Command, args...); err != nil {
		return fmt.Errorf("remove %s MCP: %w", target.Label, err)
	}
	opts.Output("removed MCP: %s\n", target.Label)
	return nil
}

func installCursorMCP(opts Options) (bool, error) {
	path := filepath.Join(opts.Home, ".cursor", "mcp.json")
	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return false, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}
	if _, exists := servers["ttl"]; exists {
		opts.Output("kept existing MCP server: Cursor\n")
		return false, nil
	}
	if opts.DryRun {
		opts.Output("would register MCP: Cursor\n")
		return true, nil
	}
	servers["ttl"] = map[string]any{"command": opts.Binary, "args": []string{"mcp"}}
	data, _ := json.MarshalIndent(root, "", "  ")
	if err := atomicWrite(path, append(data, '\n'), 0o600); err != nil {
		return false, err
	}
	opts.Output("registered MCP: Cursor\n")
	return true, nil
}

func removeCursorMCP(opts Options) error {
	path := filepath.Join(opts.Home, ".cursor", "mcp.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	root := map[string]any{}
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	servers, _ := root["mcpServers"].(map[string]any)
	delete(servers, "ttl")
	data, _ = json.MarshalIndent(root, "", "  ")
	if err := atomicWrite(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	opts.Output("removed MCP: Cursor\n")
	return nil
}

func DefaultOptions(home, configDir, binary, skill string, output func(string, ...any)) Options {
	return Options{
		Home: home, ConfigDir: configDir, Binary: binary, Skill: skill,
		Runner: func(name string, args ...string) error {
			cmd := exec.Command(name, args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		Probe: func(name string, args ...string) error {
			cmd := exec.Command(name, args...)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			return cmd.Run()
		},
		Lookup: exec.LookPath,
		Output: output,
	}
}

func ManifestPath(configDir string) string { return filepath.Join(configDir, "agents.json") }

func loadManifest(configDir string) (Manifest, error) {
	manifest := Manifest{Agents: map[string]Record{}}
	data, err := os.ReadFile(ManifestPath(configDir))
	if errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	if manifest.Agents == nil {
		manifest.Agents = map[string]Record{}
	}
	return manifest, nil
}

func saveManifest(configDir string, manifest Manifest) error {
	if len(manifest.Agents) == 0 {
		if err := os.Remove(ManifestPath(configDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	return atomicWrite(ManifestPath(configDir), append(data, '\n'), 0o600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ttl-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func removeOwnedSkill(path, expectedHash string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if expectedHash == "" || contentHash(data) != expectedHash {
		return fmt.Errorf("refusing to remove modified file: %s", path)
	}
	return os.Remove(path)
}

func sameFile(path, skill string) bool {
	data, err := os.ReadFile(path)
	return err == nil && string(data) == skill
}

func contentHash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func Names(targets []Target) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	sort.Strings(names)
	return names
}
