package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/sync"
	"siyuan-knowledge-sync/internal/types"
)

func newSyncCommand(configPath *string) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Full sync: upload changes, prune deletions",
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

			ce := compliance.NewComplianceEngine(cfg.AutoFix)

			if dryRun {
				return runDryRunAudit(scanner, tracker, ce)
			}

			client := newSiyuanClient(cfg)
			engine := sync.NewSyncEngine(client, scanner, tracker, ce)

			ctx := context.Background()
			report, err := engine.Sync(ctx)
			if err != nil {
				return fmt.Errorf("sync: %w", err)
			}

			printSyncReport(report)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "audit only, no changes")

	return cmd
}

func runDryRunAudit(scanner *git.GitScanner, tracker *state.StateTracker, ce *compliance.ComplianceEngine) error {
	files, err := scanner.ListTrackedMdFiles()
	if err != nil {
		return fmt.Errorf("list tracked files: %w", err)
	}

	stateEntries := tracker.All()

	var wouldCreate, wouldUpdate []string
	for _, tf := range files {
		entry, hasState := stateEntries[tf.Path]
		if !hasState {
			wouldCreate = append(wouldCreate, tf.Path)
		} else if tf.ModTime.After(entry.SyncedAt) {
			wouldUpdate = append(wouldUpdate, tf.Path)
		}
	}

	var wouldPrune []string
	trackedSet := make(map[string]bool, len(files))
	for _, tf := range files {
		trackedSet[tf.Path] = true
	}
	for path := range stateEntries {
		if !trackedSet[path] {
			wouldPrune = append(wouldPrune, path)
		}
	}

	fmt.Fprintf(os.Stderr, "Dry-Run Sync Preview\n")
	fmt.Fprintf(os.Stderr, "  Would create: %d\n", len(wouldCreate))
	fmt.Fprintf(os.Stderr, "  Would update: %d\n", len(wouldUpdate))
	fmt.Fprintf(os.Stderr, "  Would prune:  %d\n", len(wouldPrune))

	var complianceIssues []types.ComplianceIssue
	repoPath := scanner.RepoPath()

	allChecked := append(append([]string{}, wouldCreate...), wouldUpdate...)
	for _, path := range allChecked {
		fullPath := filepath.Join(repoPath, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s: ERROR reading file: %v\n", path, err)
			continue
		}
		issues, err := ce.Audit(path, content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  %s: ERROR auditing: %v\n", path, err)
			continue
		}
		complianceIssues = append(complianceIssues, issues...)
	}

	if len(complianceIssues) > 0 {
		fmt.Fprintf(os.Stderr, "\nCompliance Issues Found (%d):\n", len(complianceIssues))
		printComplianceIssues(complianceIssues)
	}

	return nil
}
