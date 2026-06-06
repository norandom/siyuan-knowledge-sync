package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/config"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/sync"
)

// newDevResyncCommand registers the hidden `_resync <path>` subcommand
// that calls SyncEngine.RouteAndSync directly on a single file. Used to
// surgically re-sync a single doc after engine bug fixes without
// triggering the full-suite sync loop.
//
// `path` is interpreted as repo-relative slash-path.
func newDevResyncCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "_resync <path>",
		Short:  "Re-sync a single file via engine.RouteAndSync (dev tool)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig(*configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scanner, err := git.NewGitScanner(cfg.RepoPath)
			if err != nil {
				return fmt.Errorf("git scanner: %w", err)
			}
			tracker, err := state.NewStateTracker(cfg.RepoPath)
			if err != nil {
				return fmt.Errorf("state tracker: %w", err)
			}
			ce := compliance.NewComplianceEngine(cfg.AutoFix)
			client := newSiyuanClient(cfg)
			engine := sync.NewSyncEngine(client, scanner, tracker, ce)

			// Sanity-stat the file at the repo-relative path before
			// handing it to the engine; gives a clearer error than
			// "open: no such file" buried inside processFile.
			fullPath := filepath.Join(cfg.RepoPath, args[0])
			if _, err := os.Stat(fullPath); err != nil {
				return fmt.Errorf("stat target: %w", err)
			}

			ctx := context.Background()
			if err := engine.RouteAndSync(ctx, args[0]); err != nil {
				return fmt.Errorf("RouteAndSync: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "RouteAndSync OK")
			return nil
		},
	}
}
