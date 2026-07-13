// ttl — main entrypoint.
//
// Without arguments, defaults to "cli" (the CLI dispatcher). The full
// command tree is:
//
//	ttl                -> cli (same as `ttl cli`)
//	ttl cli <args>     -> run a CLI subcommand
//	ttl serve          -> run the HTTP server + web UI
//	ttl today          -> open the TUI for today
//	ttl inbox          -> open the TUI for inbox
//	ttl version        -> print version
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/update"
)

var (
	version = "1.1.0"
	commit  = "none"
	date    = "unknown"
)

// versionCmd prints build info.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "ttl %s (commit %s, built %s)\n", version, commit, date)
	},
}

// serveFlagInit wires the serve command's local flags.
func init() {
	serveCmd.Flags().String("addr", ":8093", "listen address")
	serveCmd.Flags().String("db", "", "SQLite database path (default ~/.local/share/ttl/ttl.db)")
	serveCmd.Flags().Duration("reminder-interval", 60*time.Second, "how often to scan for due reminders")
	serveCmd.Flags().Duration("trash-retention", 30*24*time.Hour, "purge trashed tasks after this duration (0 disables)")

	rootCmd.AddCommand(versionCmd)

	// Default to "cli" subcommand when no args given.
	rootCmd.SetArgs(defaultArgs(os.Args[1:]))
	cobra.OnInitialize()
}

// defaultArgs injects "cli" when the user ran `ttl` with no subcommand
// or used a top-level shortcut like `ttl task add ...`.
func defaultArgs(argv []string) []string {
	if len(argv) == 0 {
		return []string{"cli"}
	}
	// Recognised top-level commands. Anything else (e.g. "task", "project")
	// is routed under "cli".
	switch argv[0] {
	case "cli", "serve", "today", "inbox", "view", "mcp", "agents", "version", "update", "help", "-h", "--help":
		return argv
	}
	return append([]string{"cli"}, argv...)
}

func main() {
	// Print the one-line update notice on stderr before doing anything
	// else, so it surfaces in CI logs and shell sessions alike.
	// Skip the notice when running `ttl update` itself (no point
	// telling the user about an update mid-update).
	if len(os.Args) < 2 || os.Args[1] != "update" {
		update.MaybeNotice(version, 0)
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ttl:", err)
		os.Exit(1)
	}
}
