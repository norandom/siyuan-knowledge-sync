// Package migrate's apply.go is the per-entry executor that consumes a
// validated MigrationPlan and drives the keep / drop_local / retire_siyuan
// pipelines against the local git working tree, the SiYuan API, and the
// shared sync engine (ontology-gate spec task 3.4, design `migrate/apply`,
// Req 6.3 / 6.4 / 6.5 / 6.6 / 7.5 / 10.2 / 10.3 / 10.4).
//
// Atomicity model: per-plan pre-flight is fatal (plan.Validate failures
// abort before any side effect); per-entry failures are recorded in the
// returned MigrationReport as StatusError outcomes and the loop continues.
// This matches the design's "atomicity" note ("per-entry failures are
// recorded but do not abort the batch").
package migrate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"siyuan-knowledge-sync/internal/ontology"
	"siyuan-knowledge-sync/internal/siyuan"
)

// Syncer is the subset of the sync engine that migrate/apply needs.
// Defined here to invert the dependency — the concrete sync.SyncEngine
// satisfies this interface without migrate importing the sync package.
type Syncer interface {
	RouteAndSync(ctx context.Context, relPath string) error
}

// Apply executes a validated MigrationPlan, in plan order, against engine +
// client and returns a MigrationReport summarising every entry's outcome.
//
// The repoPath parameter is the absolute path to the working-tree root that
// contains plan.Entries[*].SourcePath; Apply needs it directly because
// SyncEngine's own repo path is unexported (the design lists this as the
// expected dependency surface for the migrate package). Callers wiring
// engine + client + repo from the same source must pass the same repoPath
// that the engine was constructed against; mismatch is undefined behavior.
//
// Return contract:
//   - On plan.Validate() failure: (nil, "apply: invalid plan: …") with NO
//     side effects (the loop does not start; no API calls, no git commits).
//   - Otherwise: returns (*MigrationReport, nil). Per-entry failures land in
//     the report as StatusError outcomes; the function never panics on a
//     single entry's misbehavior.
//
// Collision-on-overwrite hardening (Req 10.4: "report the hpath collision
// and require an explicit overwrite or rename decision") is deliberately
// deferred to a future plan-version field — the existing SiYuan
// createDocWithMd is idempotent by hpath, so a re-upload to the same
// hpath reuses the existing document rather than producing a silent
// duplicate (the engine's Req 4.3 behavior). This is documented per task
// brief; tightening to a structured pre-write probe is tracked as a
// follow-up.
func Apply(ctx context.Context, plan MigrationPlan, engine Syncer, client *siyuan.Client, repoPath string) (*MigrationReport, error) {
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("apply: invalid plan: %w", err)
	}

	report := &MigrationReport{
		PlanSource: plan.Source,
		Outcomes:   make([]EntryOutcome, 0, len(plan.Entries)),
	}

	for _, entry := range plan.Entries {
		outcome := EntryOutcome{
			SourcePath: entry.SourcePath,
			Op:         entry.Op,
		}

		// Honour context cancellation between entries: ongoing batches
		// must respect the shutdown signal but still hand back the
		// partial report so the caller can see what landed.
		select {
		case <-ctx.Done():
			outcome.Status = StatusError
			outcome.Error = fmt.Sprintf("context: %v", ctx.Err())
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		default:
		}

		switch entry.Op {
		case OpKeep:
			applyKeep(ctx, engine, repoPath, entry, &outcome)
		case OpDropLocal:
			applyDropLocal(repoPath, entry, &outcome)
		case OpRetireSiyuan:
			applyRetireSiyuan(ctx, client, entry, &outcome)
		default:
			// plan.Validate already rejects unknown ops, so this branch
			// is a defensive guard against a future enum drift.
			outcome.Status = StatusError
			outcome.Error = fmt.Sprintf("unknown op %q", string(entry.Op))
		}

		report.Outcomes = append(report.Outcomes, outcome)
	}

	return report, nil
}

// applyKeep implements the keep pipeline (Req 6.4, 7.5):
//  1. Read original file from disk.
//  2. If RewrittenBody != "" replace the post-frontmatter body with it.
//  3. AddOntology adds/overwrites only `domain:` and `intent:` (preservation
//     guard from ontology/frontmatter.go enforces every other key).
//  4. Write the result back to the source path.
//  5. git add + commit with the `ontology-rewrite:` subject.
//  6. engine.RouteAndSync runs schema gate + router + upload + attrs.
//
// Any step's error lands as StatusError; subsequent steps are skipped for
// that entry but the loop continues with the next plan entry.
func applyKeep(ctx context.Context, engine Syncer, repoPath string, entry PlanEntry, outcome *EntryOutcome) {
	fullPath := filepath.Join(repoPath, entry.SourcePath)
	original, err := os.ReadFile(fullPath)
	if err != nil {
		outcome.Status = StatusError
		outcome.Error = fmt.Sprintf("read: %v", err)
		return
	}

	content := original
	if entry.RewrittenBody != "" {
		// Preserve original frontmatter (if any) and replace the body.
		// Defensively strip any leading `---...---` block from the
		// rewritten body — cobesy rewrites are instructed to return BODY
		// ONLY, but some agent runs leak a frontmatter block back in.
		// Without this strip, applyKeep would produce a file with TWO
		// frontmatter blocks (the original + cobesy's leak), confusing
		// the engine's later Meta extraction and breaking setBlockAttrs.
		body := stripLeadingFrontmatter([]byte(entry.RewrittenBody))
		fmBlock := extractFrontmatterBlock(original)
		if fmBlock != nil {
			content = append([]byte{}, fmBlock...)
			if len(content) > 0 && content[len(content)-1] != '\n' {
				content = append(content, '\n')
			}
			content = append(content, body...)
		} else {
			// No frontmatter on the source: AddOntology will prepend a
			// fresh block; we just use the rewritten body as the input.
			content = body
		}
	}

	rewritten, err := ontology.AddOntology(content, entry.Domain, entry.Intent)
	if err != nil {
		outcome.Status = StatusError
		outcome.Error = fmt.Sprintf("add ontology: %v", err)
		return
	}

	if err := os.WriteFile(fullPath, rewritten, 0o644); err != nil {
		outcome.Status = StatusError
		outcome.Error = fmt.Sprintf("write: %v", err)
		return
	}

	// Stage + commit the rewrite before handing off to the engine; the
	// router's own git mv must not interleave with an uncommitted dirty
	// working tree.
	if err := gitAddCommit(repoPath, entry.SourcePath, "ontology-rewrite: "+entry.SourcePath); err != nil {
		outcome.Status = StatusError
		outcome.Error = fmt.Sprintf("git commit: %v", err)
		return
	}

	if err := engine.RouteAndSync(ctx, entry.SourcePath); err != nil {
		outcome.Status = StatusError
		outcome.Error = fmt.Sprintf("route and sync: %v", err)
		return
	}

	outcome.Status = StatusKept

	// Probe whether routing moved the file by checking the canonical
	// target. If the source no longer exists and the canonical target
	// does, record the new path; otherwise the source path stuck.
	canonical := filepath.ToSlash(filepath.Join(
		ontology.Router{}.CanonicalFolder(entry.Domain),
		filepath.Base(entry.SourcePath),
	))
	if _, srcErr := os.Stat(filepath.Join(repoPath, entry.SourcePath)); os.IsNotExist(srcErr) {
		if _, tgtErr := os.Stat(filepath.Join(repoPath, canonical)); tgtErr == nil {
			outcome.NewPath = canonical
			return
		}
	}
	outcome.NewPath = entry.SourcePath
}

// applyDropLocal implements the drop_local pipeline (Req 6.5): git rm +
// commit. No SiYuan side effect. State tracker is intentionally not touched
// — the next sync's prune step will reconcile it; explicit retirement of a
// SiYuan doc remains the OpRetireSiyuan path's job.
func applyDropLocal(repoPath string, entry PlanEntry, outcome *EntryOutcome) {
	rm := exec.Command("git", "-C", repoPath, "rm", entry.SourcePath)
	if out, err := rm.CombinedOutput(); err != nil {
		outcome.Status = StatusError
		outcome.Error = fmt.Sprintf("git rm: %v (%s)", err, strings.TrimSpace(string(out)))
		return
	}
	if err := gitCommit(repoPath, "ontology-drop: "+entry.SourcePath); err != nil {
		outcome.Status = StatusError
		outcome.Error = fmt.Sprintf("git commit: %v", err)
		return
	}
	outcome.Status = StatusDropped
}

// applyRetireSiyuan implements the retire_siyuan pipeline (Req 10.2, 10.3):
// call client.RemoveDocByID and nothing else. State reconciliation is
// deliberately deferred to the next normal Sync's prune step — Req 10.3
// forbids autonomous SiYuan side effects, and this executor sticks to the
// single approved retirement.
func applyRetireSiyuan(ctx context.Context, client *siyuan.Client, entry PlanEntry, outcome *EntryOutcome) {
	if err := client.RemoveDocByID(ctx, entry.SiYuanDocID); err != nil {
		outcome.Status = StatusError
		outcome.Error = fmt.Sprintf("retire siyuan: %v", err)
		return
	}
	outcome.Status = StatusRetired
}

// stripLeadingFrontmatter defensively removes a leading `---...---`
// YAML block from body. cobesy rewrites are spec'd to return BODY ONLY,
// but some agent runs include a frontmatter block; without this strip,
// applyKeep would produce a file with two frontmatter blocks, breaking
// downstream tag extraction and setBlockAttrs.
func stripLeadingFrontmatter(body []byte) []byte {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return body
	}
	afterOpen := trimmed[3:]
	if len(afterOpen) > 0 && afterOpen[0] == '\n' {
		afterOpen = afterOpen[1:]
	} else if len(afterOpen) > 1 && afterOpen[0] == '\r' && afterOpen[1] == '\n' {
		afterOpen = afterOpen[2:]
	}
	closeIdx := bytes.Index(afterOpen, []byte("\n---"))
	if closeIdx < 0 {
		return body // malformed YAML — leave untouched
	}
	rest := afterOpen[closeIdx+4:]
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}
	return rest
}

// extractFrontmatterBlock returns the entire `---\n…\n---\n` block
// (including both fence lines) when content opens with a recognisable
// frontmatter, or nil when it doesn't. CRLF inputs are normalised to LF so
// the splitter agrees with ontology.AddOntology's own normalisation.
func extractFrontmatterBlock(content []byte) []byte {
	c := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(bytes.TrimLeft(c, " \t\n"), []byte("---")) {
		return nil
	}
	// Find the opening fence (skip leading whitespace).
	openIdx := bytes.Index(c, []byte("---"))
	if openIdx < 0 {
		return nil
	}
	after := c[openIdx+3:]
	if len(after) == 0 || after[0] != '\n' {
		return nil
	}
	rest := after[1:]
	closeRel := bytes.Index(rest, []byte("\n---"))
	if closeRel < 0 {
		return nil
	}
	// The block runs from openIdx through the closing fence's "---" and
	// its trailing newline if present.
	end := openIdx + 3 + 1 + closeRel + 4 // open `---` + \n + body + `\n---`
	if end < len(c) && c[end] == '\n' {
		end++
	}
	return c[:end]
}

// gitAddCommit stages `path` (relative to repoPath) and creates a commit
// with the given subject. Empty subject is rejected by `git commit` and
// would surface as an error here — callers always pass a non-empty subject.
func gitAddCommit(repoPath, path, subject string) error {
	add := exec.Command("git", "-C", repoPath, "add", path)
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return gitCommit(repoPath, subject)
}

// gitCommit runs `git -C repoPath commit -q -m subject` with deterministic
// author/committer identities so commits succeed in CI sandboxes that
// don't have a user.email configured for the inheriting environment.
func gitCommit(repoPath, subject string) error {
	commit := exec.Command("git", "-C", repoPath, "commit", "-q", "-m", subject)
	commit.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=siyuan-knowledge-sync",
		"GIT_AUTHOR_EMAIL=siyuan-knowledge-sync@local",
		"GIT_COMMITTER_NAME=siyuan-knowledge-sync",
		"GIT_COMMITTER_EMAIL=siyuan-knowledge-sync@local",
	)
	if out, err := commit.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit %q: %w (%s)", subject, err, strings.TrimSpace(string(out)))
	}
	return nil
}
