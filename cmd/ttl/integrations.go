// Integrations CLI commands.

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/client"
)

// integrationsCmd groups integration subcommands.
var integrationsCmd = &cobra.Command{
	Use:   "integrations",
	Short: "Manage external issue integrations (GitHub, Linear, ...)",
}

var integrationsAddCmd = &cobra.Command{
	Use:   "add <provider>",
	Short: "Add a new integration for a provider (github|linear)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := strings.ToLower(args[0])
		switch provider {
		case "github", "linear":
		default:
			return fmt.Errorf("unknown provider %q (supported: github, linear)", provider)
		}
		label, _ := cmd.Flags().GetString("label")
		if label == "" {
			label = provider
		}
		token, err := promptSecret(cmd.OutOrStdout(), "Personal Access Token (or API key): ")
		if err != nil {
			return err
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return fmt.Errorf("token is required")
		}
		cfg := map[string]string{"token": token}
		if provider == "github" {
			fmt.Fprint(cmd.OutOrStdout(), "GitHub login (optional, press Enter to skip): ")
			reader := bufio.NewReader(cmd.InOrStdin())
			login, _ := reader.ReadString('\n')
			login = strings.TrimSpace(login)
			if login != "" {
				cfg["login"] = login
			}
		}
		secret, err := promptSecret(cmd.OutOrStdout(), "Webhook secret (Enter to skip): ")
		if err != nil {
			return err
		}
		secret = strings.TrimSpace(secret)
		if secret != "" {
			cfg["webhook_secret"] = secret
		}
		c := mustClient()
		it, err := c.CreateIntegration(context.Background(), provider, label, cfg)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"created integration %s (id=%s)\nwebhook URL: %s/api/v1/webhooks/%s\n  X-Ttl-Integration: %s\n",
			it.Label, it.ID, mustServer(), provider, it.ID)
		return nil
	},
}

var integrationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured integrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		its, err := c.ListIntegrations(context.Background())
		if err != nil {
			return err
		}
		if len(its) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "(no integrations configured)")
			return nil
		}
		for _, it := range its {
			extra := ""
			if it.LastSyncedAt != nil {
				extra = "  last sync: " + it.LastSyncedAt.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %-6s  %s  (id=%s)%s\n",
				shortID(&it.ID), it.Provider, it.Label, it.ID, extra)
		}
		return nil
	},
}

var integrationsSyncCmd = &cobra.Command{
	Use:   "sync <id>",
	Short: "Pull assigned issues from a provider and reconcile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		stats, err := c.SyncIntegration(context.Background(), args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"sync done: created=%d updated=%d closed=%d unchanged=%d\n",
			stats.Created, stats.Updated, stats.Closed, stats.Unchanged)
		return nil
	},
}

var integrationsRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Delete an integration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := mustClient()
		if err := c.DeleteIntegration(context.Background(), args[0]); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "removed")
		return nil
	},
}

// mustServer returns the configured server URL.
func mustServer() string {
	return useConfigServer()
}

func init() {
	integrationsAddCmd.Flags().String("label", "", "human-readable label (defaults to provider name)")
	integrationsCmd.AddCommand(integrationsAddCmd, integrationsListCmd, integrationsSyncCmd, integrationsRemoveCmd)
	cliCmd.AddCommand(integrationsCmd)
}

var _ = os.Stderr
var _ = client.New
