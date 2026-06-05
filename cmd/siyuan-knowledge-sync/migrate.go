// Package main — `migrate` Cobra subcommand (ontology-gate spec task 4.2,
// Req 6.1 / 10.1). The `apply` sub-action loads a plan JSON file, validates
// the existing config path, builds the same sync engine surface used by
// `sync` and `download`, and hands the plan to `migrate.Apply`. The
// resulting `MigrationReport` is rendered to stderr in the same style as the
// existing `printSyncReport`.
//
// Atomicity contract (matches `migrate.Apply`): per-entry failures land in
// the report's StatusError outcomes; the CLI still exits zero so an
// automated agent (the siyuan-ontology AI Skill) consumes the report as the
// per-entry signal rather than the process exit code. Only a pre-flight
// failure (config load, plan read/parse, plan.Validate) returns a non-nil
// error from RunE and surfaces as a non-zero CLI exit.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/migrate"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/sync"
)

// newMigrateCommand returns the `migrate` Cobra parent command. It owns one
// sub-action (`apply`) and threads the persistent --config path through to
// it. Future sub-actions (e.g. a `validate` dry-run) would attach here.
func newMigrateCommand(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply a migration plan (ontology-gate)",
	}
	cmd.AddCommand(newMigrateApplyCommand(configPath))
	return cmd
}

// newMigrateApplyCommand returns the `migrate apply <plan.json>` sub-action.
// It accepts exactly one positional argument (the plan-JSON path) and
// delegates to runMigrateApply for all behavior.
func newMigrateApplyCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "apply <plan.json>",
		Short: "Execute a migration plan (keep / drop_local / retire_siyuan) against the configured SiYuan instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrateApply(cmd.Context(), cmd.OutOrStderr(), *configPath, args[0])
		},
	}
}

// runMigrateApply is the testable core: it owns config load, plan
// read+parse, engine wiring, the call into migrate.Apply, and the report
// render. Returning nil after a successful Apply (even when individual
// entries errored) matches the design's `printSyncReport` precedent — the
// report carries the per-entry detail, the exit code carries the pre-flight
// signal.
func runMigrateApply(ctx context.Context, out io.Writer, configPath, planPath string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("migrate apply: read plan: %w", err)
	}

	var plan migrate.MigrationPlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return fmt.Errorf("migrate apply: parse plan: %w", err)
	}

	// Validate the plan up-front so a structurally bad plan (bad version,
	// unknown op, missing required fields) fails fast with a clear "invalid
	// plan" error before we touch the git working tree or build a SiYuan
	// client. migrate.Apply re-runs Validate internally, so this is a
	// belt-and-suspenders fast-fail rather than a behavioral change.
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("migrate apply: invalid plan: %w", err)
	}

	scanner, err := git.NewGitScanner(cfg.RepoPath)
	if err != nil {
		return fmt.Errorf("migrate apply: git scanner: %w", err)
	}

	tracker, err := state.NewStateTracker(cfg.RepoPath)
	if err != nil {
		return fmt.Errorf("migrate apply: state tracker: %w", err)
	}

	ce := compliance.NewComplianceEngine(cfg.AutoFix)
	client := newSiyuanClient(cfg)
	engine := sync.NewSyncEngine(client, scanner, tracker, ce)

	report, applyErr := migrate.Apply(ctx, plan, engine, client, cfg.RepoPath)
	if applyErr != nil {
		return fmt.Errorf("migrate apply: %w", applyErr)
	}

	printMigrationReport(out, report)
	return nil
}

// printMigrationReport renders the MigrationReport in the same shape as
// printSyncReport: a summary header with per-status counts, then a per-entry
// section. The style is deliberately line-oriented so log scrapers and the
// siyuan-ontology Skill can parse it without depending on the JSON form of
// the report (which remains the machine-readable contract).
func printMigrationReport(out io.Writer, r *migrate.MigrationReport) {
	var kept, dropped, retired, errs int
	for _, o := range r.Outcomes {
		switch o.Status {
		case migrate.StatusKept:
			kept++
		case migrate.StatusDropped:
			dropped++
		case migrate.StatusRetired:
			retired++
		case migrate.StatusError:
			errs++
		}
	}

	fmt.Fprintf(out, "Migration Report\n")
	fmt.Fprintf(out, "  Source: %s\n", r.PlanSource)
	fmt.Fprintf(out, "  Outcomes: %d\n", len(r.Outcomes))
	fmt.Fprintf(out, "    Kept:    %d\n", kept)
	fmt.Fprintf(out, "    Dropped: %d\n", dropped)
	fmt.Fprintf(out, "    Retired: %d\n", retired)
	fmt.Fprintf(out, "    Errors:  %d\n", errs)

	if len(r.Outcomes) > 0 {
		fmt.Fprintln(out, "\n  Per-entry:")
		for _, o := range r.Outcomes {
			switch o.Status {
			case migrate.StatusKept:
				if o.NewPath != "" && o.NewPath != o.SourcePath {
					fmt.Fprintf(out, "    [kept]    %s -> %s\n", o.SourcePath, o.NewPath)
				} else {
					fmt.Fprintf(out, "    [kept]    %s\n", o.SourcePath)
				}
			case migrate.StatusDropped:
				fmt.Fprintf(out, "    [dropped] %s\n", o.SourcePath)
			case migrate.StatusRetired:
				fmt.Fprintf(out, "    [retired] %s\n", o.SourcePath)
			case migrate.StatusError:
				fmt.Fprintf(out, "    [error]   %s: %s\n", o.SourcePath, o.Error)
			}
		}
	}
}
