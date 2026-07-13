// ttl — command-line interface to a ttl server.
//
// The CLI is a thin client: every command is one HTTP call. State
// (server URL, API key) lives in the local config file.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/client"
	"github.com/anirudh-777/ttl/internal/config"
	"github.com/anirudh-777/ttl/internal/fmtcmd"
	"github.com/anirudh-777/ttl/internal/recurrence"
)

var (
	flagFormat string
	flagServer string
)

// rootCmd is the entrypoint used when no subcommand is given.
var rootCmd = &cobra.Command{
	Use:           "ttl",
	Short:         "ttl — the agents-first task tracker",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// cliCmd is used when invoked as `ttl ...` from the terminal. It
// dispatches to either the interactive TUI or the explicit subcommands.
var cliCmd = &cobra.Command{
	Use:   "cli",
	Short: "Run a CLI command against the configured server",
}

// addCmd creates a new task.
var addCmd = &cobra.Command{
	Use:   "add <title>",
	Short: "Add a new task",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		title := strings.Join(args, " ")
		priority, _ := cmd.Flags().GetInt("priority")
		projectName, _ := cmd.Flags().GetString("project")
		tagList, _ := cmd.Flags().GetStringSlice("tag")
		dueStr, _ := cmd.Flags().GetString("due")
		notes, _ := cmd.Flags().GetString("notes")
		repeat, _ := cmd.Flags().GetString("repeat")

		var projectID string
		if projectName != "" {
			projects, err := c.ListProjects(context.Background())
			if err != nil {
				return err
			}
			for _, p := range projects {
				if strings.EqualFold(p.Name, projectName) {
					projectID = p.ID
					break
				}
			}
			if projectID == "" {
				// Create on demand.
				p, err := c.CreateProject(context.Background(), projectName, "")
				if err != nil {
					return err
				}
				projectID = p.ID
			}
		}

		var due *time.Time
		if dueStr != "" {
			t, err := parseDue(dueStr)
			if err != nil {
				return err
			}
			due = &t
		}

		rrule, err := recurrence.Normalize(repeat, time.Now())
		if err != nil {
			return err
		}
		created, err := c.CreateTask(context.Background(), client.CreateTaskOpts{
			Title: title, Notes: notes, Priority: priority,
			ProjectID: projectID, DueAt: due, Tags: tagList, RecurrenceRRule: rrule,
		})
		if err != nil {
			return err
		}
		return fmtcmd.PrintTask(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), created)
	},
}

// listCmd lists tasks.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		opts := client.ListOpts{}
		if cmd.Flags().Changed("all") {
			opts.Status = ""
		} else if cmd.Flags().Changed("done") {
			opts.Status = "done"
		} else {
			opts.Status = "open"
		}
		opts.Overdue = overdueFlag(cmd)
		opts.Search, _ = cmd.Flags().GetString("search")
		opts.View, _ = cmd.Flags().GetString("view")
		projectName, _ := cmd.Flags().GetString("project")
		if projectName != "" {
			projects, _ := c.ListProjects(context.Background())
			for _, p := range projects {
				if strings.EqualFold(p.Name, projectName) {
					opts.ProjectID = p.ID
					break
				}
			}
		}
		opts.Limit = 500
		tasks, err := c.ListTasks(context.Background(), opts)
		if err != nil {
			return err
		}
		return fmtcmd.PrintTasks(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), tasks)
	},
}

// showCmd shows a single task.
var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a task by id (or short prefix)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		id, err := resolveTaskID(c, args[0])
		if err != nil {
			return err
		}
		t, err := c.GetTask(context.Background(), id)
		if err != nil {
			return err
		}
		return fmtcmd.PrintTask(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), t)
	},
}

var moveCmd = &cobra.Command{
	Use: "move <id>", Short: "Move or reorder a task", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		id, err := resolveTaskID(c, args[0])
		if err != nil {
			return err
		}
		var projectID, parentID *string
		if cmd.Flags().Changed("project") {
			name, _ := cmd.Flags().GetString("project")
			resolved := ""
			if name != "" && !strings.EqualFold(name, "inbox") {
				items, err := c.ListProjects(cmd.Context())
				if err != nil {
					return err
				}
				for _, p := range items {
					if strings.EqualFold(p.Name, name) || strings.HasPrefix(p.ID, name) {
						resolved = p.ID
						break
					}
				}
				if resolved == "" {
					return fmt.Errorf("project %q not found", name)
				}
			}
			projectID = &resolved
		}
		if cmd.Flags().Changed("parent") {
			value, _ := cmd.Flags().GetString("parent")
			resolved := ""
			if value != "" && !strings.EqualFold(value, "none") {
				resolved, err = resolveTaskID(c, value)
				if err != nil {
					return err
				}
			}
			parentID = &resolved
		}
		before, _ := cmd.Flags().GetString("before")
		after, _ := cmd.Flags().GetString("after")
		if before != "" {
			before, err = resolveTaskID(c, before)
			if err != nil {
				return err
			}
		}
		if after != "" {
			after, err = resolveTaskID(c, after)
			if err != nil {
				return err
			}
		}
		if before != "" && after != "" {
			return fmt.Errorf("use only one of --before or --after")
		}
		moved, err := c.ReorderTask(cmd.Context(), id, projectID, parentID, before, after)
		if err != nil {
			return err
		}
		return fmtcmd.PrintTask(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), moved)
	},
}

// doneCmd marks a task as completed.
var doneCmd = &cobra.Command{
	Use:   "done <id>",
	Short: "Mark a task as done",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		id, err := resolveTaskID(c, args[0])
		if err != nil {
			return err
		}
		completed, next, err := c.CompleteTaskWithRecur(context.Background(), id)
		if err != nil {
			return err
		}
		return fmtcmd.PrintCompleted(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), completed, next)
	},
}

// editCmd updates fields on a task.
var editCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		id, err := resolveTaskID(c, args[0])
		if err != nil {
			return err
		}
		fields := map[string]any{}
		if cmd.Flags().Changed("title") {
			fields["title"], _ = cmd.Flags().GetString("title")
		}
		if cmd.Flags().Changed("notes") {
			fields["notes"], _ = cmd.Flags().GetString("notes")
		}
		if cmd.Flags().Changed("priority") {
			p, _ := cmd.Flags().GetInt("priority")
			fields["priority"] = p
		}
		if cmd.Flags().Changed("due") {
			dueStr, _ := cmd.Flags().GetString("due")
			if dueStr == "" || dueStr == "none" {
				fields["due_at"] = nil
			} else {
				t, err := parseDue(dueStr)
				if err != nil {
					return err
				}
				fields["due_at"] = t.UnixMilli()
			}
		}
		if cmd.Flags().Changed("project") {
			projectName, _ := cmd.Flags().GetString("project")
			if projectName == "" {
				fields["project_id"] = nil
			} else {
				projects, _ := c.ListProjects(context.Background())
				pid := ""
				for _, p := range projects {
					if strings.EqualFold(p.Name, projectName) {
						pid = p.ID
						break
					}
				}
				if pid == "" {
					p, err := c.CreateProject(context.Background(), projectName, "")
					if err != nil {
						return err
					}
					pid = p.ID
				}
				fields["project_id"] = pid
			}
		}
		if cmd.Flags().Changed("tag") {
			fields["tags"], _ = cmd.Flags().GetStringSlice("tag")
		}
		if cmd.Flags().Changed("repeat") {
			repeat, _ := cmd.Flags().GetString("repeat")
			rule, err := recurrence.Normalize(repeat, time.Now())
			if err != nil {
				return err
			}
			if rule == "" {
				fields["recurrence_rrule"] = nil
			} else {
				fields["recurrence_rrule"] = rule
			}
		}
		t, err := c.UpdateTask(context.Background(), id, fields)
		if err != nil {
			return err
		}
		return fmtcmd.PrintTask(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), t)
	},
}

// rmCmd deletes a task.
var rmCmd = &cobra.Command{
	Use:   "rm <id>",
	Short: "Delete a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		id, err := resolveTaskID(c, args[0])
		if err != nil {
			return err
		}
		if err := c.DeleteTask(context.Background(), id); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "moved to trash", id)
		return nil
	},
}

var restoreCmd = &cobra.Command{
	Use: "restore <id>", Short: "Restore a task from trash", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		id, err := resolveTrashedTaskID(c, args[0])
		if err != nil {
			return err
		}
		t, err := c.RestoreTask(cmd.Context(), id)
		if err != nil {
			return err
		}
		return fmtcmd.PrintTask(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), t)
	},
}

var purgeCmd = &cobra.Command{
	Use: "purge <id>", Short: "Permanently delete a trashed task", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			return errors.New("permanent deletion requires --yes")
		}
		c := mustClient()
		id, err := resolveTrashedTaskID(c, args[0])
		if err != nil {
			return err
		}
		if err := c.PurgeTask(cmd.Context(), id); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "permanently deleted", id)
		return nil
	},
}

// projectCmd groups project subcommands.
var projectCmd = &cobra.Command{Use: "project", Short: "Manage projects"}
var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		ps, err := c.ListProjects(context.Background())
		if err != nil {
			return err
		}
		return fmtcmd.PrintProjects(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), ps)
	},
}
var projectAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		color, _ := cmd.Flags().GetString("color")
		p, err := c.CreateProject(context.Background(), args[0], color)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "created project %s (%s)\n", p.Name, p.ID)
		return nil
	},
}
var projectEditCmd = &cobra.Command{Use: "edit <id> <name>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	c := mustClient()
	id := args[0]
	color := ""
	if cmd.Flags().Changed("color") {
		color, _ = cmd.Flags().GetString("color")
	} else {
		items, err := c.ListProjects(cmd.Context())
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == args[0] || strings.HasPrefix(item.ID, args[0]) {
				id = item.ID
				color = item.Color
				break
			}
		}
	}
	return c.UpdateProject(cmd.Context(), id, args[1], color)
}}
var projectArchiveCmd = &cobra.Command{Use: "archive <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().ArchiveProject(cmd.Context(), args[0])
}}
var projectRestoreCmd = &cobra.Command{Use: "restore <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().RestoreProject(cmd.Context(), args[0])
}}
var projectPurgeCmd = &cobra.Command{Use: "purge <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		return errors.New("project purge requires --yes")
	}
	return mustClient().PurgeProject(cmd.Context(), args[0])
}}

// tagCmd groups tag subcommands.
var tagCmd = &cobra.Command{Use: "tag", Short: "Manage tags"}
var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tags",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		ts, err := c.ListTags(context.Background())
		if err != nil {
			return err
		}
		return fmtcmd.PrintTags(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), ts)
	},
}
var tagAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a tag",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		color, _ := cmd.Flags().GetString("color")
		t, err := c.CreateTag(context.Background(), args[0], color)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "created tag %s (%s)\n", t.Name, t.ID)
		return nil
	},
}
var tagEditCmd = &cobra.Command{Use: "edit <id> <name>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	c := mustClient()
	id := args[0]
	color := ""
	if cmd.Flags().Changed("color") {
		color, _ = cmd.Flags().GetString("color")
	} else {
		items, err := c.ListTags(cmd.Context())
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.ID == args[0] || strings.HasPrefix(item.ID, args[0]) {
				id = item.ID
				color = item.Color
				break
			}
		}
	}
	return c.UpdateTag(cmd.Context(), id, args[1], color)
}}
var tagMergeCmd = &cobra.Command{Use: "merge <source-id> <target-id>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().MergeTag(cmd.Context(), args[0], args[1])
}}
var tagDeleteCmd = &cobra.Command{Use: "delete <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error { return mustClient().DeleteTag(cmd.Context(), args[0]) }}

// loginCmd prompts for email and password and stores an API key.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in (or sign up) and store credentials locally",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if flagServer != "" {
			cfg.ServerURL = flagServer
		}
		if cfg.ServerURL == "" {
			cfg.ServerURL = defaultServerURL()
		}
		fmt.Fprint(cmd.OutOrStdout(), "Email: ")
		email, _ := readLine(cmd.InOrStdin())
		email = strings.TrimSpace(email)
		pw, err := promptSecret(cmd.InOrStdin(), cmd.OutOrStdout(), "Password: ")
		if err != nil {
			return err
		}

		c := client.New(cfg.ServerURL, "")
		if err := c.Login(context.Background(), email, pw); err != nil {
			return err
		}
		key, err := c.IssueAPIKey(context.Background(), "cli")
		if err != nil {
			return err
		}
		cfg.APIKey = key
		cfg.Email = email
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "logged in. API key stored in", config.PathHint())
		return nil
	},
}

// signupCmd creates a new tenant + user and stores credentials.
var signupCmd = &cobra.Command{
	Use:   "signup",
	Short: "Create a new workspace and user",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if flagServer != "" {
			cfg.ServerURL = flagServer
		}
		if cfg.ServerURL == "" {
			cfg.ServerURL = defaultServerURL()
		}
		fmt.Fprint(cmd.OutOrStdout(), "Workspace name: ")
		tenantName, _ := readLine(cmd.InOrStdin())
		tenantName = strings.TrimSpace(tenantName)
		fmt.Fprint(cmd.OutOrStdout(), "Email: ")
		email, _ := readLine(cmd.InOrStdin())
		email = strings.TrimSpace(email)
		pw, err := promptSecret(cmd.InOrStdin(), cmd.OutOrStdout(), "Password (min 6 chars): ")
		if err != nil {
			return err
		}

		c := client.New(cfg.ServerURL, "")
		invite, _ := cmd.Flags().GetString("invite")
		u, err := c.SignupWithInvite(context.Background(), tenantName, email, pw, invite)
		if err != nil {
			return err
		}
		// Issue an API key for the CLI.
		key, err := c.IssueAPIKey(context.Background(), "cli")
		if err != nil {
			return err
		}
		cfg.APIKey = key
		cfg.Email = email
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "signed up %s (%s)\n", u.Email, u.TenantID)
		return nil
	},
}

// logoutCmd clears local credentials.
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear local credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		_ = config.Clear()
		fmt.Fprintln(cmd.OutOrStdout(), "logged out")
		return nil
	},
}

// configCmd shows/sets local config.
var configCmd = &cobra.Command{Use: "config", Short: "Manage CLI config"}
var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current config",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "config path: %s\n", config.PathHint())
		fmt.Fprintf(cmd.OutOrStdout(), "server_url:  %s\n", cfg.ServerURL)
		fmt.Fprintf(cmd.OutOrStdout(), "email:       %s\n", cfg.Email)
		fmt.Fprintf(cmd.OutOrStdout(), "api_key:     %s...%s\n", safePrefix(cfg.APIKey), safeSuffix(cfg.APIKey))
		return nil
	},
}
var configServerCmd = &cobra.Command{
	Use:   "server <url>",
	Short: "Set the server URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		cfg.ServerURL = args[0]
		return config.Save(cfg)
	},
}

// -------------------------- helpers --------------------------

func mustClient() *client.Client {
	cfg, err := config.Load()
	if err != nil {
		exitError("load config: %v", err)
	}
	if cfg.APIKey == "" {
		exitError("not logged in. Run `ttl login` or `ttl signup`.")
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = defaultServerURL()
	}
	if flagServer != "" {
		cfg.ServerURL = flagServer
	}
	return client.New(cfg.ServerURL, cfg.APIKey)
}

// defaultServerURL returns the TTL server URL. CLI flag and env var
// win; otherwise http://localhost:8093 (matches the crash-course
// quickstart).
func defaultServerURL() string {
	if v := os.Getenv("TTL_SERVER_URL"); v != "" {
		return v
	}
	return "http://localhost:8093"
}

// promptSecret prints prompt and reads a line of input with the
// terminal echo disabled. Falls back to plain stdin read when the
// input is not a TTY (e.g. piped from a script).
func promptSecret(in io.Reader, w io.Writer, prompt string) (string, error) {
	fmt.Fprint(w, prompt)
	if f, ok := in.(*os.File); ok && term.IsTerminal(f.Fd()) {
		fd := f.Fd()
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(w) // move past the hidden line
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	// Non-tty: read plainly but emit a warning so users don't
	// accidentally type secrets into piped scripts.
	fmt.Fprintln(os.Stderr, "(warning: stdin is not a TTY; reading secret in cleartext)")
	line, err := readLine(in)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readLine deliberately reads only through the newline. Unlike creating a
// fresh bufio.Reader for each prompt, it cannot consume bytes intended for a
// later prompt when credentials are piped into the CLI.
func readLine(in io.Reader) (string, error) {
	var b strings.Builder
	var one [1]byte
	for {
		n, err := in.Read(one[:])
		if n > 0 {
			if one[0] == '\n' {
				return b.String(), nil
			}
			b.WriteByte(one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && b.Len() > 0 {
				return b.String(), nil
			}
			return b.String(), err
		}
	}
}

// resolveTaskID accepts either a full UUID or a short prefix. It looks
// up tasks via the configured filter and matches the prefix. Returns
// the full ID or an error if ambiguous / not found.
func resolveTaskID(c *client.Client, ref string) (string, error) {
	return resolveTaskIDInView(c, ref, "")
}

func resolveTrashedTaskID(c *client.Client, ref string) (string, error) {
	return resolveTaskIDInView(c, ref, "trash")
}

func resolveTaskIDInView(c *client.Client, ref, view string) (string, error) {
	if len(ref) == 36 {
		return ref, nil
	}
	tasks, err := c.ListTasks(context.Background(), client.ListOpts{Status: "", View: view, Limit: 500})
	if err != nil {
		return "", err
	}
	var matches []string
	for _, t := range tasks {
		if strings.HasPrefix(t.ID, ref) {
			matches = append(matches, t.ID)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no task with id prefix %q", ref)
	default:
		return "", fmt.Errorf("ambiguous id prefix %q matches %d tasks", ref, len(matches))
	}
}

// parseDue accepts:
//
//	today, tomorrow, monday..sunday (next occurrence)
//	YYYY-MM-DD, YYYY-MM-DDTHH:MM
//	"" -> error so callers can disambiguate from "none"
func parseDue(s string) (time.Time, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return time.Time{}, errors.New("empty due")
	}
	now := time.Now()
	switch s {
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 0, 0, now.Location()), nil
	case "tomorrow":
		t := now.Add(24 * time.Hour)
		return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 0, 0, t.Location()), nil
	case "none":
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return t, nil
	}
	for i, name := range []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"} {
		if s == name {
			days := (i - int(now.Weekday()) + 7) % 7
			if days == 0 {
				days = 7
			}
			t := now.Add(time.Duration(days) * 24 * time.Hour)
			return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 0, 0, t.Location()), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised due date %q (try today, tomorrow, monday, YYYY-MM-DD)", s)
}

func overdueFlag(cmd *cobra.Command) bool {
	b, _ := cmd.Flags().GetBool("overdue")
	return b
}

func safePrefix(s string) string {
	if len(s) < 8 {
		return "***"
	}
	return s[:8]
}
func safeSuffix(s string) string {
	if len(s) < 4 {
		return "***"
	}
	return s[len(s)-4:]
}

// exitError prints to stderr and exits with a non-zero status.
// Used for fatal config errors where cobra cannot help.
func exitError(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ttl: "+format+"\n", args...)
	os.Exit(2)
}

// init wires all commands and persistent flags.
func init() {
	// Global flags on root.
	rootCmd.PersistentFlags().StringVarP(&flagFormat, "format", "o", "text", "output format: text|json|ndjson")
	rootCmd.PersistentFlags().StringVar(&flagServer, "server", "", "override server URL for this command")

	// Subcommand wiring.
	rootCmd.AddCommand(cliCmd)
	cliCmd.AddCommand(addCmd, listCmd, showCmd, doneCmd, editCmd, moveCmd, rmCmd, restoreCmd, purgeCmd)
	cliCmd.AddCommand(projectCmd, tagCmd, loginCmd, signupCmd, logoutCmd, configCmd)
	cliCmd.AddCommand(startCmd, stopCmd, pomodoroCmd, logCmd, timerCmd)
	rootCmd.AddCommand(todayCmd, inboxCmd, viewCmd, serveCmd)

	// add flags.
	addCmd.Flags().IntP("priority", "p", 0, "priority 0..3 (3=high)")
	addCmd.Flags().StringP("project", "P", "", "project name (created on demand)")
	addCmd.Flags().StringSliceP("tag", "t", nil, "tags (comma-separated)")
	addCmd.Flags().StringP("due", "d", "", "due: today|tomorrow|monday|YYYY-MM-DD")
	addCmd.Flags().StringP("notes", "n", "", "notes (markdown)")
	addCmd.Flags().String("repeat", "", "repeat: daily|weekdays|weekly|monthly|yearly|rrule:<rule>")

	// list flags.
	listCmd.Flags().Bool("all", false, "include done tasks")
	listCmd.Flags().Bool("done", false, "only done tasks")
	listCmd.Flags().Bool("overdue", false, "only overdue tasks")
	listCmd.Flags().String("search", "", "search title/notes")
	listCmd.Flags().StringP("project", "P", "", "filter by project name")
	listCmd.Flags().String("view", "", "view: inbox|today|upcoming|overdue|next|done|trash")
	moveCmd.Flags().String("project", "", "destination project name, ID prefix, or inbox")
	moveCmd.Flags().String("parent", "", "destination parent task ID or none")
	moveCmd.Flags().String("before", "", "place before task ID")
	moveCmd.Flags().String("after", "", "place after task ID")

	// edit flags.
	editCmd.Flags().StringP("title", "t", "", "new title")
	editCmd.Flags().StringP("notes", "n", "", "new notes")
	editCmd.Flags().IntP("priority", "p", 0, "new priority")
	editCmd.Flags().StringP("due", "d", "", "new due (or 'none')")
	editCmd.Flags().StringP("project", "P", "", "new project (or '' to clear)")
	editCmd.Flags().StringSlice("tag", nil, "replacement tags")
	editCmd.Flags().String("repeat", "", "repeat preset, rrule:<rule>, or none")
	purgeCmd.Flags().Bool("yes", false, "confirm permanent deletion")

	// project/tag subcommands.
	projectCmd.AddCommand(projectListCmd, projectAddCmd, projectEditCmd, projectArchiveCmd, projectRestoreCmd, projectPurgeCmd)
	projectAddCmd.Flags().String("color", "", "hex color, e.g. #ff8800")
	projectEditCmd.Flags().String("color", "", "new hex color (keeps current color when omitted)")
	projectPurgeCmd.Flags().Bool("yes", false, "confirm permanent deletion")
	tagCmd.AddCommand(tagListCmd, tagAddCmd, tagEditCmd, tagMergeCmd, tagDeleteCmd)
	tagAddCmd.Flags().String("color", "", "hex color")
	tagEditCmd.Flags().String("color", "", "new hex color (keeps current color when omitted)")

	// config subcommands.
	configCmd.AddCommand(configShowCmd, configServerCmd)
	signupCmd.Flags().String("invite", "", "single-use invite token for an existing workspace")
}
