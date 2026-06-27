package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/anirudhprakash/ttl/internal/mcp"
)

// mcpCmd starts the MCP server on stdio.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run the Model Context Protocol server on stdio",
	Long: `Run ttl as a Model Context Protocol server speaking JSON-RPC 2.0
over stdin/stdout. Use this to expose ttl tools to AI agents like
Claude, Cursor, or Cline.

Example MCP config (Claude Desktop):

  {
    "mcpServers": {
      "ttl": {
        "command": "ttl",
        "args": ["mcp"]
      }
    }
  }

Requires the CLI to be logged in (run 'ttl login' first).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := mcp.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "ttl mcp:", err)
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
