package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestDefaultArgsPreservesShortcuts(t *testing.T) {
	got := defaultArgs([]string{"add", "hello"})
	if len(got) != 3 || got[0] != "cli" || got[1] != "add" {
		t.Fatalf("got %v", got)
	}
	got = defaultArgs([]string{"serve"})
	if len(got) != 1 || got[0] != "serve" {
		t.Fatalf("got %v", got)
	}
}

func TestPinnedUpdateDoesNotRequireLatestReleaseLookup(t *testing.T) {
	oldTag, oldYes, oldCheck, oldTo := updateTag, updateYes, updateCheck, updateTo
	defer func() { updateTag, updateYes, updateCheck, updateTo = oldTag, oldYes, oldCheck, oldTo }()
	updateTag, updateYes, updateCheck, updateTo = "1.0.0-rc1", false, false, "/tmp/ttl-test-update"
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("n\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runUpdate(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-> v1.0.0-rc1") || !strings.Contains(out.String(), "aborted") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestPromptSecretReadsCommandInput(t *testing.T) {
	in := strings.NewReader("user@example.com\nsecret from pipe\n")
	var out bytes.Buffer
	first, err := readLine(in)
	if err != nil || first != "user@example.com" {
		t.Fatalf("first prompt = %q, %v", first, err)
	}
	got, err := promptSecret(in, &out, "Password: ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret from pipe" {
		t.Fatalf("got %q", got)
	}
}

func TestParseReminderTime(t *testing.T) {
	before := time.Now()
	got, err := parseReminderTime("+30m")
	if err != nil {
		t.Fatal(err)
	}
	if got.Before(before.Add(29*time.Minute)) || got.After(before.Add(31*time.Minute)) {
		t.Fatalf("got %v", got)
	}
	if _, err := parseReminderTime(""); err == nil {
		t.Fatal("expected missing time error")
	}
}
