package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/config"
	"siyuan-knowledge-sync/internal/ontology"
	"siyuan-knowledge-sync/internal/siyuan"
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
		// PersistentPreRunE fires once per CLI invocation, before any
		// subcommand's RunE. This is where we apply an operator's optional
		// `ontology:` overrides so subcommands that never call loadConfig
		// (notably `schema --json`) still see the configured enums.
		//
		// Failure model:
		//   - Config file missing or unreadable: silently treated as "no
		//     overrides" so help, schema, and other config-independent
		//     subcommands keep working. Subcommands that need the config
		//     re-raise the same error in their own RunE.
		//   - Config decoded, no `ontology:` section: compile-time defaults
		//     stay in effect; Configure is not called.
		//   - Config decoded with `ontology:` section: translated to
		//     ConfigureOptions and passed to ontology.Configure. Any
		//     validation error is surfaced through cobra so the process
		//     exits non-zero before any subcommand performs side effects.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				// Silent skip: let subcommands that need a config raise
				// the error themselves. Help, schema, etc. proceed.
				return nil
			}
			if cfg.Ontology == nil {
				return nil
			}
			return ontology.Configure(toConfigureOptions(cfg.Ontology))
		},
	}

	cmd.PersistentFlags().StringVarP(&configPath, "config", "c", ".siyuan-sync.yaml", "path to config file")

	cmd.AddCommand(
		newSyncCommand(&configPath),
		newDownloadCommand(&configPath),
		newAuditCommand(&configPath),
		newMCPServerCommand(&configPath),
		newSchemaCommand(),
		newMigrateCommand(&configPath),
		newDevResyncCommand(&configPath),
	)

	return cmd
}

// toConfigureOptions translates the decoded `ontology:` config section into
// the validated ConfigureOptions shape expected by ontology.Configure. It
// preserves slice order and the nil-vs-non-nil-empty distinction on Tags
// so the open/closed/closed-but-empty vocabulary semantics survive the hop
// from YAML decode to package state.
func toConfigureOptions(oc *config.OntologyConfig) ontology.ConfigureOptions {
	if oc == nil {
		return ontology.ConfigureOptions{}
	}
	domains := make([]ontology.ConfigureDomain, len(oc.Domains))
	for i, d := range oc.Domains {
		domains[i] = ontology.ConfigureDomain{ID: d.ID, Folder: d.Folder}
	}
	intents := make([]ontology.ConfigureIntent, len(oc.Intents))
	for i, in := range oc.Intents {
		intents[i] = ontology.ConfigureIntent{ID: in.ID}
	}
	return ontology.ConfigureOptions{
		Domains: domains,
		Intents: intents,
		Tags:    oc.Tags,
	}
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
