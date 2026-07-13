package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/agentsetup"
	ttlskill "github.com/anirudh-777/ttl/skills/ttl"
)

var (
	agentNames       []string
	agentsAll        bool
	agentsSkillsOnly bool
	agentsDryRun     bool
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Install ttl skills and MCP tools for coding agents",
}

var agentsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Detect coding agents and install the ttl skill and MCP interface",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		targets, opts, err := agentCommandContext(cmd, false)
		if err != nil {
			return err
		}
		if err := agentsetup.Install(targets, opts); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "restart installed agents, then ask: use ttl to show my open tasks")
		return nil
	},
}

var agentsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh ttl-managed agent skills and MCP registrations",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		targets, opts, err := agentCommandContext(cmd, false)
		if err != nil {
			return err
		}
		return agentsetup.Install(targets, opts)
	},
}

var agentsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show ttl skill and MCP installation status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		targets, opts, err := agentCommandContext(cmd, true)
		if err != nil {
			return err
		}
		return agentsetup.Status(targets, opts)
	},
}

var agentsUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove only skills and MCP entries installed by ttl",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		targets, opts, err := agentCommandContext(cmd, true)
		if err != nil {
			return err
		}
		return agentsetup.Uninstall(targets, opts)
	},
}

func agentCommandContext(cmd *cobra.Command, defaultAll bool) ([]agentsetup.Target, agentsetup.Options, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, agentsetup.Options{}, err
	}
	binary, err := os.Executable()
	if err != nil {
		return nil, agentsetup.Options{}, err
	}
	binary, _ = filepath.Abs(binary)
	configDir := os.Getenv("TTL_CONFIG_DIR")
	if configDir == "" {
		configDir = filepath.Join(home, ".config", "ttl")
	}
	opts := agentsetup.DefaultOptions(home, configDir, binary, ttlskill.Content,
		func(format string, values ...any) { fmt.Fprintf(cmd.OutOrStdout(), format, values...) })
	opts.SkillsOnly = agentsSkillsOnly
	opts.DryRun = agentsDryRun
	targets, err := agentsetup.Select(agentsetup.Targets(home), agentNames, agentsAll || defaultAll, opts.Lookup)
	return targets, opts, err
}

func init() {
	for _, command := range []*cobra.Command{agentsInstallCmd, agentsUpdateCmd, agentsStatusCmd, agentsUninstallCmd} {
		command.Flags().StringSliceVar(&agentNames, "agent", nil, "agent to configure: claude, codex, cursor, continue, cline, roo (repeatable)")
		command.Flags().BoolVar(&agentsAll, "all", false, "configure every supported agent")
		command.Flags().BoolVar(&agentsSkillsOnly, "skills-only", false, "install or remove skills without changing MCP configuration")
		command.Flags().BoolVar(&agentsDryRun, "dry-run", false, "show changes without writing them")
	}
	agentsCmd.AddCommand(agentsInstallCmd, agentsUpdateCmd, agentsStatusCmd, agentsUninstallCmd)
	rootCmd.AddCommand(agentsCmd)
}
