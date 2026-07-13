package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var keyCmd = &cobra.Command{Use: "key", Short: "Manage API keys"}
var keyListCmd = &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
	keys, err := mustClient().ListAPIKeys(cmd.Context())
	if err != nil {
		return err
	}
	if flagFormat == "json" || flagFormat == "ndjson" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(keys)
	}
	for _, k := range keys {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %-16s %s\n", shortReminderID(k.ID), k.Name, strings.Join(k.Scopes, ","))
	}
	return nil
}}
var keyCreateCmd = &cobra.Command{Use: "create <name>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	scopes, _ := cmd.Flags().GetStringSlice("scope")
	expires, _ := cmd.Flags().GetDuration("expires-in")
	var exp *time.Time
	if expires > 0 {
		v := time.Now().Add(expires)
		exp = &v
	}
	raw, key, err := mustClient().CreateAPIKey(cmd.Context(), args[0], scopes, exp)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "API key (shown once): %s\nid: %s\n", raw, key.ID)
	return nil
}}
var keyRevokeCmd = &cobra.Command{Use: "revoke <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().RevokeAPIKey(cmd.Context(), args[0])
}}
var keyRenameCmd = &cobra.Command{Use: "rename <id> <name>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().RenameAPIKey(cmd.Context(), args[0], args[1])
}}
var keyRotateCmd = &cobra.Command{Use: "rotate <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	raw, key, err := mustClient().RotateAPIKey(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "API key (shown once): %s\nid: %s\n", raw, key.ID)
	return nil
}}

var memberCmd = &cobra.Command{Use: "member", Short: "Manage workspace members"}
var memberListCmd = &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
	items, err := mustClient().ListMembers(cmd.Context())
	if err != nil {
		return err
	}
	if flagFormat == "json" || flagFormat == "ndjson" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
	}
	for _, m := range items {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %-8s %s\n", shortReminderID(m.ID), m.Role, m.Email)
	}
	return nil
}}
var memberInviteCmd = &cobra.Command{Use: "invite", RunE: func(cmd *cobra.Command, args []string) error {
	role, _ := cmd.Flags().GetString("role")
	expires, _ := cmd.Flags().GetDuration("expires-in")
	v := time.Now().Add(expires)
	token, invite, err := mustClient().CreateInvite(cmd.Context(), role, &v)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Invite token (shown once): %s\nexpires: %s\nid: %s\n", token, invite.ExpiresAt.Local().Format(time.RFC3339), invite.ID)
	return nil
}}
var memberRoleCmd = &cobra.Command{Use: "role <id> <member|admin|owner>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().SetMemberRole(cmd.Context(), args[0], args[1])
}}
var memberRemoveCmd = &cobra.Command{Use: "remove <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		return fmt.Errorf("member removal requires --yes")
	}
	return mustClient().RemoveMember(cmd.Context(), args[0])
}}

var notificationCmd = &cobra.Command{Use: "notification", Short: "Manage reminder webhook endpoints"}
var notificationListCmd = &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
	items, err := mustClient().ListNotificationEndpoints(cmd.Context())
	if err != nil {
		return err
	}
	if flagFormat == "json" || flagFormat == "ndjson" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
	}
	for _, e := range items {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %-5t %-16s %s\n", shortReminderID(e.ID), e.Enabled, e.Name, e.URL)
	}
	return nil
}}
var notificationAddCmd = &cobra.Command{Use: "add <name> <url>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
	secret, endpoint, err := mustClient().CreateNotificationEndpoint(cmd.Context(), args[0], args[1])
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Webhook secret (shown once): %s\nid: %s\n", secret, endpoint.ID)
	return nil
}}
var notificationEnableCmd = &cobra.Command{Use: "enable <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().SetNotificationEndpointEnabled(cmd.Context(), args[0], true)
}}
var notificationDisableCmd = &cobra.Command{Use: "disable <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().SetNotificationEndpointEnabled(cmd.Context(), args[0], false)
}}
var notificationRemoveCmd = &cobra.Command{Use: "remove <id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	return mustClient().DeleteNotificationEndpoint(cmd.Context(), args[0])
}}

func init() {
	keyCreateCmd.Flags().StringSlice("scope", []string{"tasks:read", "tasks:write"}, "credential scopes")
	keyCreateCmd.Flags().Duration("expires-in", 0, "optional lifetime, e.g. 720h")
	keyCmd.AddCommand(keyListCmd, keyCreateCmd, keyRenameCmd, keyRotateCmd, keyRevokeCmd)
	memberInviteCmd.Flags().String("role", "member", "member or admin")
	memberInviteCmd.Flags().Duration("expires-in", 7*24*time.Hour, "invite lifetime")
	memberRemoveCmd.Flags().Bool("yes", false, "confirm removal")
	memberCmd.AddCommand(memberListCmd, memberInviteCmd, memberRoleCmd, memberRemoveCmd)
	notificationCmd.AddCommand(notificationListCmd, notificationAddCmd, notificationEnableCmd, notificationDisableCmd, notificationRemoveCmd)
	cliCmd.AddCommand(keyCmd, memberCmd, notificationCmd)
}
