package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/types"
)

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
