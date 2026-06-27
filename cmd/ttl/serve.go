package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/api"
	"github.com/anirudh-777/ttl/internal/client"
	"github.com/anirudh-777/ttl/internal/config"
	"github.com/anirudh-777/ttl/internal/db"
	"github.com/anirudh-777/ttl/internal/events"
	"github.com/anirudh-777/ttl/internal/store"
	"github.com/anirudh-777/ttl/internal/tui"
)

// todayCmd opens the today TUI.
var todayCmd = &cobra.Command{
	Use:   "today",
	Short: "Open the today TUI",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		return tui.Run(c, tui.ViewToday)
	},
}

// inboxCmd opens the inbox TUI.
var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Open the inbox TUI",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		return tui.Run(c, tui.ViewInbox)
	},
}

// serveCmd runs the HTTP server + embedded web UI.
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the ttl server (web UI + REST API)",
	RunE: func(cmd *cobra.Command, args []string) error {
		addr, _ := cmd.Flags().GetString("addr")
		dbPath, _ := cmd.Flags().GetString("db")
		reminderEvery, _ := cmd.Flags().GetDuration("reminder-interval")
		if dbPath == "" {
			dataDir := defaultDataDir()
			if err := os.MkdirAll(dataDir, 0o700); err != nil {
				return err
			}
			dbPath = filepath.Join(dataDir, "ttl.db")
		}
		d, err := db.Open(dbPath)
		if err != nil {
			return err
		}
		defer d.Close()
		st := store.New(d)
		hub := events.New()

		mux := http.NewServeMux()
		// /api/ subtree -> chi router. /health is also handled by chi.
		apiHandler := api.New(d, st, hub)
		mux.Handle("/api/", apiHandler)
		mux.Handle("/health", apiHandler)
		mux.Handle("/version", versionHandler())
		mux.Handle("/", webHandler())

		srv := &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}

		// Graceful shutdown on SIGINT/SIGTERM.
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		// Reminder ticker: default 60s, configurable via --reminder-interval.
		go runReminderTicker(ctx, d, st, hub, reminderEvery)

		go func() {
			<-ctx.Done()
			shutdownCtx, c2 := context.WithTimeout(context.Background(), 5*time.Second)
			defer c2()
			_ = srv.Shutdown(shutdownCtx)
		}()

		fmt.Fprintf(cmd.OutOrStdout(),
			"ttl server %s (commit %s, built %s) listening on %s (db=%s)\n",
			version, commit, date, addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	},
}

// runReminderTicker scans the DB every `every` for due reminders and
// publishes them on the hub. Local OS notification is best-effort.
func runReminderTicker(ctx context.Context, d *sql.DB, st *store.Store, hub *events.Hub, every time.Duration) {
	if every <= 0 {
		every = 60 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	// First scan immediately.
	scanAndPublish(ctx, st, hub)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			scanAndPublish(ctx, st, hub)
		}
	}
}

func scanAndPublish(ctx context.Context, st *store.Store, hub *events.Hub) {
	due, err := st.FetchDueReminders(ctx, time.Now())
	if err != nil {
		return
	}
	for _, rm := range due {
		hub.Publish(events.Event{
			Kind:     events.KindReminderFired,
			TenantID: rm.TenantID,
			UserID:   rm.UserID,
			Payload: map[string]any{
				"id":         rm.ID,
				"task_id":    rm.TaskID,
				"task_title": rm.TaskTitle,
				"fire_at":    rm.FireAt,
			},
		})
		// Best-effort OS notification.
		go notifyOS("ttl reminder", rm.TaskTitle)
	}
}

// notifyOS shows a desktop notification. Supports macOS, Linux
// (notify-send), and Windows (msg). No-ops if no tool is available.
func notifyOS(title, body string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// -title sets the banner; osascript is in /usr/bin on macOS.
		script := fmt.Sprintf(`display notification %q with title %q`, body, title)
		cmd = exec.Command("osascript", "-e", script)
	case "linux":
		cmd = exec.Command("notify-send", title, body)
	case "windows":
		cmd = exec.Command("msg", "*", title+": "+body)
	default:
		return
	}
	_ = cmd.Run()
}

func defaultDataDir() string {
	if d := os.Getenv("TTL_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ttl")
}

// useConfigServer is a small helper used by commands that need the
// persisted server URL even when the API key is empty (e.g. login).
func useConfigServer() string {
	if flagServer != "" {
		return flagServer
	}
	if v := os.Getenv("TTL_SERVER_URL"); v != "" {
		return v
	}
	cfg, _ := config.Load()
	if cfg != nil && cfg.ServerURL != "" {
		return cfg.ServerURL
	}
	return "http://localhost:8093"
}

// loadConfigClient is a tiny shortcut used by login/signup commands.
func loadConfigClient() *client.Client {
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = &config.Config{}
	}
	return client.New(useConfigServer(), "")
}

// versionHandler returns build metadata so a user can verify which
// binary their browser is talking to (essential when troubleshooting
// stale caches).
func versionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, `{"version":%q,"built":%q,"go":%q}`+"\n",
			version, date, runtime.Version())
	})
}
