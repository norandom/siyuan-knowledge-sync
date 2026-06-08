package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/sync"
)

func newDownloadCommand(configPath *string) *cobra.Command {
	var conflictMode string

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download all SiYuan content to local files",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}

			scanner, err := git.NewGitScanner(cfg.RepoPath)
			if err != nil {
				return fmt.Errorf("git scanner: %w", err)
			}

			tracker, err := state.NewStateTracker(cfg.RepoPath)
			if err != nil {
				return fmt.Errorf("state tracker: %w", err)
			}

			client := newSiyuanClient(cfg)
			ce := compliance.NewComplianceEngine(false)
			engine := sync.NewSyncEngine(client, scanner, tracker, ce)

			ctx := context.Background()
			report, err := engine.Download(ctx, conflictMode)
			if err != nil {
				return fmt.Errorf("download: %w", err)
			}

			printSyncReport(report)
			return nil
		},
	}

	cmd.Flags().StringVar(&conflictMode, "conflict", "skip", "conflict resolution: skip, overwrite, merge")

	return cmd
}
