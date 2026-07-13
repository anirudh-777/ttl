package agentsetup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testSkill = "---\nname: ttl\n---\n# ttl skill\n"

func testOptions(t *testing.T) Options {
	t.Helper()
	home := t.TempDir()
	return Options{
		Home: home, ConfigDir: filepath.Join(home, ".config", "ttl"),
		Binary: "/usr/local/bin/ttl", Skill: testSkill,
		Lookup: func(string) (string, error) { return "", errors.New("missing") },
		Runner: func(string, ...string) error { return errors.New("unexpected runner call") },
		Probe:  func(string, ...string) error { return errors.New("unexpected probe call") },
		Output: func(string, ...any) {},
	}
}

func targetNamed(t *testing.T, home, name string) Target {
	t.Helper()
	for _, target := range Targets(home) {
		if target.Name == name {
			return target
		}
	}
	t.Fatalf("target %q not found", name)
	return Target{}
}

func TestSelectDetectsInstalledAgents(t *testing.T) {
	all := Targets("/tmp/home")
	selected, err := Select(all, nil, false, func(name string) (string, error) {
		if name == "codex" || name == "cursor" {
			return "/bin/" + name, nil
		}
		return "", errors.New("missing")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Names(selected), []string{"codex", "cursor"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestSelectDetectsEditorConfigWithoutCLI(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o700); err != nil {
		t.Fatal(err)
	}
	selected, err := Select(Targets(home), nil, false, func(string) (string, error) {
		return "", errors.New("missing")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Names(selected), []string{"cursor"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
}

func TestInstallAndUninstallSkillsOnly(t *testing.T) {
	opts := testOptions(t)
	opts.SkillsOnly = true
	targets := []Target{targetNamed(t, opts.Home, "codex"), targetNamed(t, opts.Home, "claude")}

	if err := Install(targets, opts); err != nil {
		t.Fatal(err)
	}
	if err := Install(targets, opts); err != nil {
		t.Fatalf("idempotent install: %v", err)
	}
	for _, target := range targets {
		data, err := os.ReadFile(target.SkillPath)
		if err != nil || string(data) != testSkill {
			t.Fatalf("skill %s: data=%q err=%v", target.Name, data, err)
		}
	}
	if err := Uninstall(targets, opts); err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if _, err := os.Stat(target.SkillPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("skill still exists: %s", target.SkillPath)
		}
	}
	if _, err := os.Stat(ManifestPath(opts.ConfigDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest still exists")
	}
}

func TestInstallRefusesUnrelatedSkill(t *testing.T) {
	opts := testOptions(t)
	opts.SkillsOnly = true
	target := targetNamed(t, opts.Home, "codex")
	if err := os.MkdirAll(filepath.Dir(target.SkillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.SkillPath, []byte("user content"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Install([]Target{target}, opts)
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error = %v", err)
	}
}

func TestUpdateReplacesUnmodifiedManagedSkill(t *testing.T) {
	opts := testOptions(t)
	opts.SkillsOnly = true
	target := targetNamed(t, opts.Home, "codex")
	if err := Install([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	opts.Skill = testSkill + "updated\n"
	if err := Install([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target.SkillPath)
	if err != nil || string(data) != opts.Skill {
		t.Fatalf("updated skill = %q, err=%v", data, err)
	}
}

func TestUninstallRefusesModifiedManagedSkill(t *testing.T) {
	opts := testOptions(t)
	opts.SkillsOnly = true
	target := targetNamed(t, opts.Home, "codex")
	if err := Install([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.SkillPath, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Uninstall([]Target{target}, opts)
	if err == nil || !strings.Contains(err.Error(), "refusing to remove modified") {
		t.Fatalf("error = %v", err)
	}
}

func TestCursorMCPPreservesExistingServers(t *testing.T) {
	opts := testOptions(t)
	opts.Lookup = func(name string) (string, error) { return "/bin/" + name, nil }
	target := targetNamed(t, opts.Home, "cursor")
	path := filepath.Join(opts.Home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	initial := `{"theme":"dark","mcpServers":{"other":{"command":"other"}}}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Install([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	assertCursorServers(t, path, true, true)
	if err := Uninstall([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	assertCursorServers(t, path, false, true)
}

func TestExistingMCPIsNotOwnedOrRemoved(t *testing.T) {
	opts := testOptions(t)
	var calls []string
	opts.Lookup = func(name string) (string, error) { return "/bin/" + name, nil }
	opts.Probe = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil // `mcp get ttl` succeeds: pre-existing configuration.
	}
	target := targetNamed(t, opts.Home, "codex")
	if err := Install([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	if got, want := calls, []string{"codex mcp get ttl"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestManagedMCPIsRemoved(t *testing.T) {
	opts := testOptions(t)
	var calls []string
	opts.Lookup = func(name string) (string, error) { return "/bin/" + name, nil }
	opts.Probe = func(name string, args ...string) error {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		return errors.New("not found")
	}
	opts.Runner = func(name string, args ...string) error {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		return nil
	}
	target := targetNamed(t, opts.Home, "codex")
	if err := Install([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"codex mcp get ttl",
		"codex mcp add ttl -- /usr/local/bin/ttl mcp",
		"codex mcp remove ttl",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestSkillsOnlyUninstallRetainsMCPOwnership(t *testing.T) {
	opts := testOptions(t)
	opts.Lookup = func(name string) (string, error) { return "/bin/" + name, nil }
	opts.Probe = func(string, ...string) error { return errors.New("not found") }
	opts.Runner = func(string, ...string) error { return nil }
	target := targetNamed(t, opts.Home, "codex")
	if err := Install([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	opts.SkillsOnly = true
	if err := Uninstall([]Target{target}, opts); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadManifest(opts.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	record := manifest.Agents["codex"]
	if !record.MCP || record.SkillPath != "" {
		t.Fatalf("record = %+v", record)
	}
}

func TestMCPFailureLeavesInstalledSkillManaged(t *testing.T) {
	opts := testOptions(t)
	opts.Lookup = func(name string) (string, error) { return "/bin/" + name, nil }
	opts.Probe = func(string, ...string) error { return errors.New("not found") }
	opts.Runner = func(string, ...string) error { return errors.New("add failed") }
	target := targetNamed(t, opts.Home, "codex")
	if err := Install([]Target{target}, opts); err == nil {
		t.Fatal("expected MCP error")
	}
	manifest, err := loadManifest(opts.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	record := manifest.Agents["codex"]
	if record.SkillPath != target.SkillPath || record.SkillHash == "" || record.MCP {
		t.Fatalf("record = %+v", record)
	}
}

func assertCursorServers(t *testing.T, path string, ttl, other bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["ttl"]; ok != ttl {
		t.Fatalf("ttl present = %v, want %v", ok, ttl)
	}
	if _, ok := servers["other"]; ok != other {
		t.Fatalf("other present = %v, want %v; root=%s", ok, other, fmt.Sprint(root))
	}
}
