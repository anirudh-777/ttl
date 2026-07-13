package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/client"
	"github.com/anirudh-777/ttl/internal/fmtcmd"
)

var subtaskCmd = &cobra.Command{Use: "subtask", Short: "Manage subtasks"}
var subtaskAddCmd = &cobra.Command{Use: "add <parent-id> <title>", Short: "Add a subtask", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	c := mustClient()
	parent, err := resolveTaskID(c, args[0])
	if err != nil {
		return err
	}
	t, err := c.CreateTask(cmd.Context(), client.CreateTaskOpts{ParentID: parent, Title: args[1]})
	if err != nil {
		return err
	}
	return fmtcmd.PrintTask(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), t)
}}
var subtaskListCmd = &cobra.Command{Use: "list <parent-id>", Short: "List subtasks", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	c := mustClient()
	parent, err := resolveTaskID(c, args[0])
	if err != nil {
		return err
	}
	t, err := c.GetTask(context.Background(), parent)
	if err != nil {
		return err
	}
	return fmtcmd.PrintTasks(cmd.OutOrStdout(), fmtcmd.ResolveFormat(flagFormat), t.Subtasks)
}}

func init() { subtaskCmd.AddCommand(subtaskAddCmd, subtaskListCmd); cliCmd.AddCommand(subtaskCmd) }
