// Timer, pomodoro, and worklog CLI subcommands.

package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/client"
	"github.com/anirudh-777/ttl/internal/model"
)

// startCmd begins a timer on a task (or with no task for a "blank" timer).
var startCmd = &cobra.Command{
	Use:   "start [task-id]",
	Short: "Start a timer on a task",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		kind, _ := cmd.Flags().GetString("kind")
		note, _ := cmd.Flags().GetString("note")
		var ref string
		if len(args) > 0 {
			ref = args[0]
			id, err := resolveTaskID(c, ref)
			if err != nil {
				return err
			}
			ref = id
		}
		e, err := c.StartTimer(cmd.Context(), ref, kind, note)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "timer started: %s (%s)\n",
			timerLabel(e), shortID(e.TaskID))
		return nil
	},
}

// stopCmd ends the active timer.
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the active timer",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		note, _ := cmd.Flags().GetString("note")
		e, err := c.StopTimer(cmd.Context(), note)
		if err != nil {
			return err
		}
		d := time.Duration(e.DurationMs) * time.Millisecond
		fmt.Fprintf(cmd.OutOrStdout(), "timer stopped: %s ran for %s\n",
			timerLabel(e), formatDur(d))
		return nil
	},
}

// timerCmd groups timer/pomodoro subcommands.
var timerCmd = &cobra.Command{
	Use:   "timer",
	Short: "Manage timers (start, stop, pomodoro, log)",
}
var timerStartCmd = startCmd // expose under `timer start` too
var timerStopCmd = stopCmd

// pomodoroCmd runs a 25-minute focus session.
var pomodoroCmd = &cobra.Command{
	Use:   "pomodoro [task-id]",
	Short: "Run a pomodoro session on a task",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		mins, _ := cmd.Flags().GetInt("minutes")
		if mins <= 0 {
			mins = 25
		}
		var ref string
		if len(args) > 0 {
			id, err := resolveTaskID(c, args[0])
			if err != nil {
				return err
			}
			ref = id
		}
		e, err := c.StartTimerPlanned(cmd.Context(), ref, "pomodoro", fmt.Sprintf("pomodoro %dm", mins), mins)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"pomodoro started (%dm). run `ttl stop` or Ctrl-C the server when done.\n",
			mins)
		fmt.Fprintf(cmd.OutOrStdout(), "task: %s\n", shortID(e.TaskID))
		return nil
	},
}

// logCmd prints today's work-log (aggregated time per task + total).
var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show today's work log (time per task, plus active timer)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		summary, active, err := c.WorklogToday(cmd.Context(), "")
		if err != nil {
			return err
		}
		printWorklog(cmd.OutOrStdout(), summary, active)
		return nil
	},
}

// printWorklog renders the daily summary as a small table.
func printWorklog(w io.Writer, summary *client.DailySummary, active *model.TimeEntry) {
	fmt.Fprintf(w, "\nWork log for %s\n", summary.Day.Format("2006-01-02 (Mon)"))
	fmt.Fprintln(w, strings.Repeat("-", 50))
	total := time.Duration(summary.TotalMs) * time.Millisecond
	fmt.Fprintf(w, "Total tracked: %s\n", formatDur(total))
	if active != nil {
		elapsed := time.Duration(active.DurationMs) * time.Millisecond
		fmt.Fprintf(w, "Active now:    %s  %s\n",
			timerLabel(active), formatDur(elapsed))
	}
	fmt.Fprintln(w)
	if len(summary.PerTask) == 0 {
		fmt.Fprintln(w, "(no completed entries today)")
		return
	}
	fmt.Fprintf(w, "  %-8s  %-7s  %s\n", "TIME", "ENTRIES", "TASK")
	for _, p := range summary.PerTask {
		fmt.Fprintf(w, "  %-8s  %-7d  %s\n",
			formatDur(time.Duration(p.TotalMs)*time.Millisecond),
			p.Count, p.TaskTitle)
	}
}

func timerLabel(e *model.TimeEntry) string {
	title := e.TaskTitle
	if title == "" {
		title = "(no task)"
	}
	if e.Kind == "pomodoro" {
		return "pomodoro: " + title
	}
	return "working: " + title
}

func shortID(s *string) string {
	if s == nil || *s == "" {
		return "—"
	}
	id := *s
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

// formatDur is exposed for use elsewhere.
func formatDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func init() {
	startCmd.Flags().String("kind", "work", "entry kind: work|pomodoro")
	startCmd.Flags().String("note", "", "optional note")

	stopCmd.Flags().String("note", "", "append a note on stop")

	pomodoroCmd.Flags().Int("minutes", 25, "pomodoro length in minutes")

	timerCmd.AddCommand(timerStartCmd, timerStopCmd)
}
