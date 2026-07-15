// Package fmtcmd formats tasks, projects, and tags for CLI output.
//
// Three renderers:
//
//	Text     - human-friendly terminal table (default)
//	JSON     - one JSON object on stdout
//	NDJSON   - one JSON object per line, suitable for piping to jq, etc.
package fmtcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/anirudh-777/ttl/internal/model"
)

// Format selects the output renderer.
type Format string

const (
	FormatText   Format = "text"
	FormatJSON   Format = "json"
	FormatNDJSON Format = "ndjson"
)

// ResolveFormat parses --format flag value, defaulting to text.
func ResolveFormat(s string) Format {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	case "ndjson":
		return FormatNDJSON
	default:
		return FormatText
	}
}

// PrintTasks writes tasks in f to w.
func PrintTasks(w io.Writer, f Format, tasks []model.Task) error {
	switch f {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"tasks": tasks})
	case FormatNDJSON:
		enc := json.NewEncoder(w)
		for _, t := range tasks {
			if err := enc.Encode(t); err != nil {
				return err
			}
		}
		return nil
	default:
		return printTasksTable(w, tasks)
	}
}

// PrintTask writes a single task in f to w.
func PrintTask(w io.Writer, f Format, t *model.Task) error {
	switch f {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(t)
	case FormatNDJSON:
		return json.NewEncoder(w).Encode(t)
	default:
		_, err := fmt.Fprintln(w, taskOneLine(t))
		return err
	}
}

// PrintCompleted renders the result of a "done" command: the
// completed task plus an optional "next occurrence" line if the task
// had a recurrence_rrule.
func PrintCompleted(w io.Writer, f Format, completed *model.Task, next *model.Task) error {
	switch f {
	case FormatJSON:
		out := map[string]any{"task": completed}
		if next != nil {
			out["next_occurred"] = next
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	case FormatNDJSON:
		enc := json.NewEncoder(w)
		if err := enc.Encode(completed); err != nil {
			return err
		}
		if next != nil {
			if err := enc.Encode(next); err != nil {
				return err
			}
		}
		return nil
	default:
		if _, err := fmt.Fprintln(w, taskOneLine(completed)); err != nil {
			return err
		}
		if next != nil {
			fmt.Fprintf(w, "  spawned next occurrence: %s\n", taskOneLine(next))
		}
		return nil
	}
}

// PrintProjects writes projects in f to w.
func PrintProjects(w io.Writer, f Format, ps []model.Project) error {
	switch f {
	case FormatJSON:
		return json.NewEncoder(w).Encode(map[string]any{"projects": ps})
	case FormatNDJSON:
		enc := json.NewEncoder(w)
		for _, p := range ps {
			if err := enc.Encode(p); err != nil {
				return err
			}
		}
		return nil
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tCOLOR")
		for _, p := range ps {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", short(p.ID), p.Name, p.Color)
		}
		return tw.Flush()
	}
}

// PrintTags writes tags in f to w.
func PrintTags(w io.Writer, f Format, ts []model.Tag) error {
	switch f {
	case FormatJSON:
		return json.NewEncoder(w).Encode(map[string]any{"tags": ts})
	case FormatNDJSON:
		enc := json.NewEncoder(w)
		for _, t := range ts {
			if err := enc.Encode(t); err != nil {
				return err
			}
		}
		return nil
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tCOLOR")
		for _, t := range ts {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", short(t.ID), t.Name, t.Color)
		}
		return tw.Flush()
	}
}

// Messagef prints a human status line ("created task 42").
func Messagef(f Format, format string, args ...any) {
	if f == FormatText || f == "" {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

func printTasksTable(w io.Writer, tasks []model.Task) error {
	if len(tasks) == 0 {
		fmt.Fprintln(w, "(no tasks)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPRI\tDUE\tPROJECT\tTAGS\tTITLE")
	now := time.Now()
	for _, t := range tasks {
		priority := ""
		switch t.Priority {
		case 3:
			priority = "!!"
		case 2:
			priority = "!"
		case 1:
			priority = "-"
		}
		due := ""
		if t.DueAt != nil {
			due = humanDue(*t.DueAt, now)
		}
		project := ""
		if t.ProjectID != nil {
			project = short(*t.ProjectID)
		}
		tags := strings.Join(t.Tags, ",")
		status := ""
		if t.Status == "done" {
			status = "[x] "
		} else if t.Status == "in_progress" {
			status = "[>] "
		} else {
			status = "[ ] "
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s%s\n",
			short(t.ID), priority, due, project, tags, status, t.Title,
		)
	}
	return tw.Flush()
}

func taskOneLine(t *model.Task) string {
	mark := "[ ]"
	if t.Status == "done" {
		mark = "[x]"
	} else if t.Status == "in_progress" {
		mark = "[>]"
	}
	due := ""
	if t.DueAt != nil {
		due = "  due " + t.DueAt.Format("2006-01-02 15:04")
	}
	tags := ""
	if len(t.Tags) > 0 {
		tags = "  [" + strings.Join(t.Tags, ",") + "]"
	}
	return fmt.Sprintf("%s %s%s%s  (%s)", mark, t.Title, due, tags, short(t.ID))
}

func humanDue(t time.Time, now time.Time) string {
	today := now.Format("2006-01-02")
	tomorrow := now.Add(24 * time.Hour).Format("2006-01-02")
	switch t.Format("2006-01-02") {
	case today:
		return "today"
	case tomorrow:
		return "tomorrow"
	}
	if t.Before(now) {
		return "overdue " + t.Format("01-02")
	}
	return t.Format("01-02")
}

// short returns the first 8 characters of an ID. Useful for terminal
// output where UUIDs are noisy.
func short(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
