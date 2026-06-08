package main

import (
	"context"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/mcp"
)

func newMCPServerCommand(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp-server",
		Short: "Start MCP server for agent access",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}

			client := newSiyuanClient(cfg)
			server := mcp.NewServer(client)

			ctx := context.Background()
			return server.Run(ctx)
		},
	}

	return cmd
}
