package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var reminderCmd = &cobra.Command{Use: "reminder", Short: "Manage task reminders"}

var reminderAddCmd = &cobra.Command{
	Use: "add <task-id> --at <time>", Short: "Add a reminder", Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := resolveTaskID(mustClient(), args[0])
		if err != nil {
			return err
		}
		at, _ := cmd.Flags().GetString("at")
		when, err := parseReminderTime(at)
		if err != nil {
			return err
		}
		endpoint, _ := cmd.Flags().GetString("endpoint")
		r, err := mustClient().CreateReminderWithEndpoint(cmd.Context(), id, when, endpoint)
		if err != nil {
			return err
		}
		return printReminder(cmd, r)
	},
}

var reminderListCmd = &cobra.Command{
	Use: "list", Short: "List reminders",
	RunE: func(cmd *cobra.Command, args []string) error {
		status, _ := cmd.Flags().GetString("status")
		items, err := mustClient().ListReminders(cmd.Context(), status)
		if err != nil {
			return err
		}
		if flagFormat == "json" || flagFormat == "ndjson" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
		}
		for _, r := range items {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %-8s  %s  %s\n", shortReminderID(r.ID), r.Status, r.FireAt.Local().Format("2006-01-02 15:04"), r.TaskTitle)
		}
		return nil
	},
}

func reminderTimeCommand(use, short, action string) *cobra.Command {
	c := &cobra.Command{Use: use + " <id> --at <time>", Short: short, Args: cobra.ExactArgs(1)}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		at, _ := cmd.Flags().GetString("at")
		when, err := parseReminderTime(at)
		if err != nil {
			return err
		}
		var r any
		if action == "edit" {
			r, err = mustClient().UpdateReminder(cmd.Context(), args[0], when)
		} else {
			r, err = mustClient().SnoozeReminder(cmd.Context(), args[0], when)
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(r)
	}
	c.Flags().String("at", "", "time: RFC3339, YYYY-MM-DDTHH:MM, or +duration")
	return c
}

var reminderAckCmd = &cobra.Command{Use: "ack <id>", Short: "Acknowledge a reminder", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	r, err := mustClient().AcknowledgeReminder(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	return printReminder(cmd, r)
}}
var reminderDeleteCmd = &cobra.Command{Use: "delete <id>", Short: "Delete a reminder", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().DeleteReminder(cmd.Context(), args[0])
}}

func parseReminderTime(input string) (time.Time, error) {
	v := strings.TrimSpace(input)
	if v == "" {
		return time.Time{}, fmt.Errorf("--at is required")
	}
	if strings.HasPrefix(v, "+") {
		d, err := time.ParseDuration(strings.TrimPrefix(v, "+"))
		if err != nil {
			return time.Time{}, err
		}
		return time.Now().Add(d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid reminder time %q", input)
}

func printReminder(cmd *cobra.Command, r any) error {
	if flagFormat == "json" || flagFormat == "ndjson" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(r)
	}
	b, _ := json.Marshal(r)
	fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return nil
}
func shortReminderID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func init() {
	reminderAddCmd.Flags().String("at", "", "time: RFC3339, YYYY-MM-DDTHH:MM, or +duration")
	reminderAddCmd.Flags().String("endpoint", "", "notification endpoint ID")
	reminderListCmd.Flags().String("status", "", "pending|sent|ack")
	edit := reminderTimeCommand("edit", "Reschedule a reminder", "edit")
	snooze := reminderTimeCommand("snooze", "Snooze a reminder", "snooze")
	reminderCmd.AddCommand(reminderAddCmd, reminderListCmd, edit, reminderAckCmd, snooze, reminderDeleteCmd)
	cliCmd.AddCommand(reminderCmd)
}
