package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/api"
	"github.com/anirudh-777/ttl/internal/client"
	"github.com/anirudh-777/ttl/internal/config"
	"github.com/anirudh-777/ttl/internal/db"
	"github.com/anirudh-777/ttl/internal/events"
	"github.com/anirudh-777/ttl/internal/model"
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

var viewCmd = &cobra.Command{
	Use: "view <inbox|today|upcoming|overdue|next|done|trash>", Short: "Open a TUI task view", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		view := tui.View(args[0])
		valid := map[tui.View]bool{tui.ViewInbox: true, tui.ViewToday: true, tui.ViewUpcoming: true, tui.ViewOverdue: true, tui.ViewNext: true, tui.ViewProgress: true, tui.ViewDone: true, tui.ViewTrash: true}
		if !valid[view] {
			return fmt.Errorf("unknown view %q", args[0])
		}
		return tui.Run(mustClient(), view)
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
		trashRetention, _ := cmd.Flags().GetDuration("trash-retention")
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
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       90 * time.Second,
		}

		// Graceful shutdown on SIGINT/SIGTERM.
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		// Reminder ticker: default 60s, configurable via --reminder-interval.
		go runReminderTicker(ctx, d, st, hub, reminderEvery, trashRetention)

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
func runReminderTicker(ctx context.Context, d *sql.DB, st *store.Store, hub *events.Hub, every, trashRetention time.Duration) {
	if every <= 0 {
		every = 60 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	// First scan immediately.
	scanAndPublish(ctx, st, hub)
	if trashRetention > 0 {
		_, _ = st.PurgeTrashOlderThan(ctx, time.Now().Add(-trashRetention))
	}
	lastPurge := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			scanAndPublish(ctx, st, hub)
			if trashRetention > 0 && time.Since(lastPurge) >= time.Hour {
				_, _ = st.PurgeTrashOlderThan(ctx, time.Now().Add(-trashRetention))
				lastPurge = time.Now()
			}
		}
	}
}

func scanAndPublish(ctx context.Context, st *store.Store, hub *events.Hub) {
	expired, _ := st.StopExpiredTimers(ctx, time.Now())
	for _, entry := range expired {
		hub.Publish(events.Event{Kind: events.KindTimerStopped, TenantID: entry.TenantID, UserID: entry.UserID,
			Payload: map[string]any{"entry_id": entry.ID, "task_id": entry.TaskID, "automatic": true}})
		go notifyOS("ttl pomodoro", "Focus session complete")
	}
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
		if rm.EndpointID != nil {
			go deliverReminderWebhook(context.Background(), st, rm)
		}
	}
}

func deliverReminderWebhook(ctx context.Context, st *store.Store, rm model.Reminder) {
	rawURL, secret, enabled, err := st.NotificationDeliveryConfig(ctx, rm.TenantID, *rm.EndpointID)
	if err != nil || !enabled {
		return
	}
	body, _ := json.Marshal(map[string]any{"event": "reminder.fired", "reminder": rm})
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	client := webhookHTTPClient()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-TTL-Signature", sig)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			st.RecordReminderDelivery(ctx, rm.ID, "")
			return
		}
		if err == nil {
			lastErr = fmt.Errorf("webhook returned %s", resp.Status)
			resp.Body.Close()
		} else {
			lastErr = err
		}
		if attempt < 2 {
			time.Sleep(time.Duration(1<<attempt) * 250 * time.Millisecond)
		}
	}
	st.RecordReminderDelivery(ctx, rm.ID, lastErr.Error())
}

func webhookHTTPClient() *http.Client {
	allowPrivate := strings.EqualFold(strings.TrimSpace(os.Getenv("TTL_ALLOW_PRIVATE_WEBHOOKS")), "true")
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !allowPrivate && unsafeWebhookIP(ip) {
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, errors.New("webhook target resolves only to private or unavailable addresses")
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

func unsafeWebhookIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
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
