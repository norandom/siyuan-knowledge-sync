package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/config"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/mcp"
	"siyuan-knowledge-sync/internal/siyuan"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/sync"
	"siyuan-knowledge-sync/internal/types"
)

func main() {
	root := newRootCommand()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "siyuan-knowledge-sync",
		Short: "Sync git-tracked markdown notes with SiYuan",
	}

	cmd.PersistentFlags().StringVarP(&configPath, "config", "c", ".siyuan-sync.yaml", "path to config file")

	cmd.AddCommand(
		newSyncCommand(&configPath),
		newDownloadCommand(&configPath),
		newAuditCommand(&configPath),
		newMCPServerCommand(&configPath),
		newSchemaCommand(),
	)

	return cmd
}

func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// newSiyuanClient builds the SiYuan client and, when configured, attaches
// Cloudflare Access service-token headers so endpoints behind Cloudflare
// Access (Zero Trust) are reachable.
func newSiyuanClient(cfg *config.Config) *siyuan.Client {
	client := siyuan.NewClient(cfg.Endpoint, cfg.Token)
	if cfg.CFAccessClientID != "" {
		client.SetHeader("CF-Access-Client-Id", cfg.CFAccessClientID)
	}
	if cfg.CFAccessClientSecret != "" {
		client.SetHeader("CF-Access-Client-Secret", cfg.CFAccessClientSecret)
	}
	return client
}

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

func newAuditCommand(configPath *string) *cobra.Command {
	var autofix bool

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit local files for SiYuan compliance",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}

			scanner, err := git.NewGitScanner(cfg.RepoPath)
			if err != nil {
				return fmt.Errorf("git scanner: %w", err)
			}

			ce := compliance.NewComplianceEngine(autofix)

			files, err := scanner.ListTrackedMdFiles()
			if err != nil {
				return fmt.Errorf("list tracked files: %w", err)
			}

			repoPath := scanner.RepoPath()
			var allIssues []types.ComplianceIssue
			fixedCount := 0

			for _, tf := range files {
				fullPath := filepath.Join(repoPath, tf.Path)
				content, err := os.ReadFile(fullPath)
				if err != nil {
					allIssues = append(allIssues, types.ComplianceIssue{
						File:     tf.Path,
						Line:     0,
						Severity: "error",
						Message:  fmt.Sprintf("read file: %v", err),
					})
					continue
				}

				if autofix {
					fixedContent, issues, err := ce.AutoFix(tf.Path, content)
					if err != nil {
						allIssues = append(allIssues, types.ComplianceIssue{
							File:     tf.Path,
							Line:     0,
							Severity: "error",
							Message:  fmt.Sprintf("autofix: %v", err),
						})
						continue
					}
					allIssues = append(allIssues, issues...)

					if !bytesEqual(fixedContent, content) {
						if err := os.WriteFile(fullPath, fixedContent, 0644); err != nil {
							allIssues = append(allIssues, types.ComplianceIssue{
								File:     tf.Path,
								Line:     0,
								Severity: "error",
								Message:  fmt.Sprintf("write fixed content: %v", err),
							})
							continue
						}
						for _, iss := range issues {
							if iss.Fixable {
								fixedCount++
							}
						}
					}
				} else {
					issues, err := ce.Audit(tf.Path, content)
					if err != nil {
						allIssues = append(allIssues, types.ComplianceIssue{
							File:     tf.Path,
							Line:     0,
							Severity: "error",
							Message:  fmt.Sprintf("audit: %v", err),
						})
						continue
					}
					allIssues = append(allIssues, issues...)
				}
			}

			printAuditReport(allIssues, autofix, fixedCount)
			return nil
		},
	}

	cmd.Flags().BoolVar(&autofix, "autofix", false, "apply auto-fixes to correctable issues")

	return cmd
}

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

func printSyncReport(report *types.SyncReport) {
	fmt.Fprintf(os.Stderr, "Sync Report\n")
	fmt.Fprintf(os.Stderr, "  Created: %d\n", len(report.Created))
	fmt.Fprintf(os.Stderr, "  Updated: %d\n", len(report.Updated))
	fmt.Fprintf(os.Stderr, "  Pruned:  %d\n", len(report.Pruned))
	fmt.Fprintf(os.Stderr, "  Errors:  %d\n", len(report.Errors))

	if len(report.Created) > 0 {
		fmt.Fprintf(os.Stderr, "\n  Created:\n")
		for _, path := range report.Created {
			notebook, siyuanPath := splitPathForReport(path)
			fmt.Fprintf(os.Stderr, "    - %s -> (%s) /%s\n", path, notebook, siyuanPath)
		}
	}

	if len(report.Updated) > 0 {
		fmt.Fprintf(os.Stderr, "\n  Updated:\n")
		for _, path := range report.Updated {
			notebook, siyuanPath := splitPathForReport(path)
			fmt.Fprintf(os.Stderr, "    - %s -> (%s) /%s\n", path, notebook, siyuanPath)
		}
	}

	if len(report.Pruned) > 0 {
		fmt.Fprintf(os.Stderr, "\n  Pruned:\n")
		for _, path := range report.Pruned {
			fmt.Fprintf(os.Stderr, "    - %s\n", path)
		}
	}

	if len(report.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "\n  Errors:\n")
		for _, err := range report.Errors {
			fmt.Fprintf(os.Stderr, "    - %s: %s\n", err.File, err.Message)
		}
	}
}

func printAuditReport(issues []types.ComplianceIssue, autofix bool, fixedCount int) {
	if len(issues) == 0 {
		fmt.Fprintf(os.Stderr, "Audit Report\n")
		fmt.Fprintf(os.Stderr, "  No compliance issues found.\n")
		return
	}

	fmt.Fprintf(os.Stderr, "Audit Report\n")

	byFile := make(map[string][]types.ComplianceIssue)
	for _, iss := range issues {
		byFile[iss.File] = append(byFile[iss.File], iss)
	}

	var filenames []string
	for f := range byFile {
		filenames = append(filenames, f)
	}
	sort.Strings(filenames)

	for _, filename := range filenames {
		fileIssues := byFile[filename]
		sort.Slice(fileIssues, func(i, j int) bool {
			if fileIssues[i].Line != fileIssues[j].Line {
				return fileIssues[i].Line < fileIssues[j].Line
			}
			return fileIssues[i].Message < fileIssues[j].Message
		})

		fmt.Fprintf(os.Stderr, "  %s\n", filename)
		for _, iss := range fileIssues {
			fixable := ""
			if iss.Fixable {
				fixable = " (fixable)"
			}
			fmt.Fprintf(os.Stderr, "    Line %d: %s %s%s\n", iss.Line, strings.ToUpper(iss.Severity), iss.Message, fixable)
		}
	}

	if autofix && fixedCount > 0 {
		fmt.Fprintf(os.Stderr, "\n  Fixed %d issues automatically.\n", fixedCount)
	}

	unfixableCount := 0
	for _, iss := range issues {
		if !iss.Fixable {
			unfixableCount++
		}
	}
	if unfixableCount > 0 {
		fmt.Fprintf(os.Stderr, "  %d issues require manual resolution.\n", unfixableCount)
	}
}

func printComplianceIssues(issues []types.ComplianceIssue) {
	byFile := make(map[string][]types.ComplianceIssue)
	for _, iss := range issues {
		byFile[iss.File] = append(byFile[iss.File], iss)
	}

	var filenames []string
	for f := range byFile {
		filenames = append(filenames, f)
	}
	sort.Strings(filenames)

	for _, filename := range filenames {
		fileIssues := byFile[filename]
		sort.Slice(fileIssues, func(i, j int) bool {
			if fileIssues[i].Line != fileIssues[j].Line {
				return fileIssues[i].Line < fileIssues[j].Line
			}
			return fileIssues[i].Message < fileIssues[j].Message
		})

		fmt.Fprintf(os.Stderr, "  %s\n", filename)
		for _, iss := range fileIssues {
			fixable := ""
			if iss.Fixable {
				fixable = " (fixable)"
			}
			fmt.Fprintf(os.Stderr, "    Line %d: %s %s%s\n", iss.Line, strings.ToUpper(iss.Severity), iss.Message, fixable)
		}
	}
}

func splitPathForReport(localPath string) (notebook, siyuanPath string) {
	clean := filepath.ToSlash(filepath.Clean(localPath))
	dir := filepath.Dir(clean)
	if dir == "." {
		return "root", clean
	}
	parts := strings.SplitN(dir, "/", 2)
	notebook = parts[0]
	rest := strings.TrimPrefix(clean, notebook+"/")
	return notebook, rest
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
