package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/ontology"
	"siyuan-knowledge-sync/internal/siyuan"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/tags"
	"siyuan-knowledge-sync/internal/types"
)

const defaultNotebookName = "root"

type SyncEngine struct {
	client        *siyuan.Client
	scanner       *git.GitScanner
	state         *state.StateTracker
	compliance    *compliance.ComplianceEngine
	tags          *tags.TagExtractor
	notebookCache map[string]string
	repoPath      string
}

func NewSyncEngine(client *siyuan.Client, scanner *git.GitScanner, tracker *state.StateTracker, ce *compliance.ComplianceEngine) *SyncEngine {
	return &SyncEngine{
		client:        client,
		scanner:       scanner,
		state:         tracker,
		compliance:    ce,
		tags:          tags.NewTagExtractor(),
		notebookCache: make(map[string]string),
		repoPath:      scanner.RepoPath(),
	}
}

func (e *SyncEngine) Sync(ctx context.Context) (*types.SyncReport, error) {
	report := &types.SyncReport{}

	trackedFiles, err := e.scanner.ListTrackedMdFiles()
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}

	allState := e.state.All()

	for _, tf := range trackedFiles {
		entry, hasState := allState[tf.Path]

		if !hasState {
			e.processFile(ctx, report, tf, "", true)
		} else if tf.ModTime.After(entry.SyncedAt) {
			e.processFile(ctx, report, tf, entry.SiYuanID, false)
		}
	}

	// Re-scan tracked files before prune so paths that were `git mv`'d by
	// ontology routing (task 3.2) appear under their new canonical paths.
	// Without the re-scan, pruneDeleted would treat the post-route state
	// entry at the new path as "not tracked" and remove it, undoing the
	// sync that just happened.
	postRouteFiles, err := e.scanner.ListTrackedMdFiles()
	if err != nil {
		postRouteFiles = trackedFiles
	}

	e.pruneDeleted(ctx, postRouteFiles, report)

	if err := e.state.Save(); err != nil {
		return report, fmt.Errorf("save state: %w", err)
	}

	return report, nil
}

func (e *SyncEngine) processFile(ctx context.Context, report *types.SyncReport, tf types.TrackedFile, existingSiYuanID string, isNew bool) {
	fullPath := filepath.Join(e.repoPath, tf.Path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		report.Errors = append(report.Errors, types.SyncError{
			File: tf.Path, Message: fmt.Sprintf("read file: %v", err),
		})
		return
	}

	fixedContent, issues, err := e.compliance.AutoFix(tf.Path, content)
	if err != nil {
		report.Errors = append(report.Errors, types.SyncError{
			File: tf.Path, Message: fmt.Sprintf("compliance: %v", err),
		})
		return
	}

	// Step 1 (Schema gate, Req 2.6 + 3.5 + design "sync/engine (extended)
	// Step 1"): when the compliance audit has flagged any "schema"-category
	// issues, we re-derive the structured []ontology.SchemaViolation directly
	// from the (autofix-output) frontmatter and abort this file BEFORE any
	// notebook resolve, upload, title, or attrs call. The batch continues with
	// the next file.
	//
	// Opt-in via declaration: the gate fires only when the file declared at
	// least one of `domain:` / `intent:` in its frontmatter. Files that
	// predate the ontology (no opt-in) keep their legacy sync behavior; the
	// `audit` subcommand still surfaces their schema issues. This preserves
	// every existing 13.x frontmatter test and every plain-markdown test
	// byte-equal.
	if hasSchemaCategoryIssue(issues) {
		view := parseFrontmatterView(fixedContent)
		if view.DomainNode != nil || view.IntentNode != nil {
			violations := ontology.CheckOntologyFrontmatter(tf.Path, view)
			if len(violations) > 0 {
				for _, v := range violations {
					payload, mErr := json.Marshal(v)
					if mErr != nil {
						report.Errors = append(report.Errors, types.SyncError{
							File:    tf.Path,
							Message: fmt.Sprintf("schema gate: marshal violation: %v", mErr),
						})
						continue
					}
					report.Errors = append(report.Errors, types.SyncError{
						File:    tf.Path,
						Message: string(payload),
					})
				}
				return
			}
		}
	}

	// Step 2 (Frontmatter + tag extraction): hoisted before notebook /
	// hpath resolution because the routing decision (3.2) depends on
	// meta.Domain. After a successful RouteMove the file lives at a new
	// path, so resolveNotebook and buildHPath must run against THAT path.
	// On a frontmatter parse failure we fall through with metaErr non-nil
	// and the legacy 13.5 degraded-upload path takes over below.
	meta, metaErr := e.tags.ExtractMeta(fixedContent)

	// Step 2 (Route, Req 3.2/3.3/3.4/3.6 + design "sync/engine (extended)
	// Step 2"): opt-in via declaration — routing fires only when the file
	// declared a Domain. Files without a domain frontmatter keep their
	// legacy sync behavior (preserves every existing 13.x test + e2e).
	//
	// On RouteMove:
	//   1. Check state.Move(old, new) eligibility FIRST via a synthetic
	//      probe so an ErrCollision does NOT leave a half-applied move
	//      on disk (no partial state — task brief: "file still at
	//      original path").
	//   2. git mv old -> new (creating any missing parent dirs).
	//   3. git commit -m "ontology-route: <old> -> <new>" (exact subject).
	//   4. state.Move(old, new) — must succeed since we already probed.
	//   5. Rewrite tf.Path to the new path for the remainder of processFile.
	// AssetWarnings are appended to report.Errors as warning-shaped entries
	// (carry-in-error-channel per the existing per-file issue pattern) and
	// do NOT abort the file (Req 9.4).
	if metaErr == nil && meta.Domain != "" {
		router := ontology.NewRouter(e.repoPath)
		decision := router.Route(ontology.Domain(meta.Domain), tf.Path, meta.Body)

		for _, w := range decision.AssetWarnings {
			report.Errors = append(report.Errors, types.SyncError{
				File: tf.Path,
				Message: fmt.Sprintf(
					"asset reference would be invalidated by move: %s [%s -> %s, target_exists=%t]",
					w.Reference, w.OldResolved, w.NewResolved, w.TargetExists,
				),
			})
		}

		if decision.Action == ontology.RouteMove {
			// Probe state collision BEFORE touching the working tree.
			if collision := e.probeStateCollision(tf.Path, decision.TargetPath); collision != nil {
				report.Errors = append(report.Errors, types.SyncError{
					File:    tf.Path,
					Message: fmt.Sprintf("state collision: %v", collision),
				})
				return
			}
			if err := gitMvAndCommit(e.repoPath, tf.Path, decision.TargetPath); err != nil {
				report.Errors = append(report.Errors, types.SyncError{
					File:    tf.Path,
					Message: fmt.Sprintf("ontology route: %v", err),
				})
				return
			}
			if err := e.state.Move(tf.Path, decision.TargetPath); err != nil {
				report.Errors = append(report.Errors, types.SyncError{
					File:    tf.Path,
					Message: fmt.Sprintf("state collision: %v", err),
				})
				return
			}
			tf.Path = decision.TargetPath
		}
	}

	notebookID, err := e.resolveNotebook(ctx, tf.Path)
	if err != nil {
		report.Errors = append(report.Errors, types.SyncError{
			File: tf.Path, Message: fmt.Sprintf("notebook: %v", err),
		})
		return
	}

	hpath := buildHPath(tf.Path)

	// Step 3 / Step 6: choose the body to upload. On a successful parse we send
	// the frontmatter-stripped body (13.1); on a parse failure we record a
	// compliance issue and fall back to the full content (13.5).
	uploadBody := string(fixedContent)
	if metaErr == nil {
		uploadBody = string(meta.Body)
	} else {
		report.Errors = append(report.Errors, types.SyncError{
			File: tf.Path, Message: fmt.Sprintf("frontmatter parse: %v", metaErr),
		})
	}

	// Step 3: create or update first to establish the doc ID.
	var docID string
	if isNew || existingSiYuanID == "" {
		id, err := e.client.CreateDocWithMd(ctx, notebookID, hpath, uploadBody)
		if err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File: tf.Path, Message: fmt.Sprintf("create document: %v", err),
			})
			return
		}
		docID = id
		e.state.Put(types.SyncEntry{
			LocalPath:  tf.Path,
			SiYuanID:   docID,
			NotebookID: notebookID,
		})
		report.Created = append(report.Created, tf.Path)
	} else {
		if err := e.client.UpdateBlock(ctx, existingSiYuanID, uploadBody); err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File: tf.Path, Message: fmt.Sprintf("update document: %v", err),
			})
			return
		}
		docID = existingSiYuanID
		e.state.Put(types.SyncEntry{
			LocalPath:  tf.Path,
			SiYuanID:   docID,
			NotebookID: notebookID,
		})
		report.Updated = append(report.Updated, tf.Path)
	}

	// Step 6: on a frontmatter parse failure, skip title/attr mapping. The body
	// was already uploaded above and the file is recorded as created/updated.
	if metaErr != nil {
		return
	}

	// Step 4: set the document title from the frontmatter title ONLY when
	// present (13.2). When there is no frontmatter title we do NOT call
	// RenameDocByID: that call mutates the SiYuan document's hpath, and a
	// redundant filename->filename rename on the common no-frontmatter path
	// changes /name.md and breaks hpath-based resolution (regressed
	// e2e/TestFullSyncE2E). The document keeps the create-path-derived name,
	// which satisfies 13.3 without an extra call. Non-fatal: a failure is
	// recorded per-file but does not change the created/updated outcome.
	if meta.Title != "" {
		if err := e.client.RenameDocByID(ctx, docID, meta.Title); err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File: tf.Path, Message: fmt.Sprintf("set document title: %v", err),
			})
		}
	}

	// Step 5: apply extracted tags as custom- block attributes (13.4).
	// Non-fatal, same policy as the title step.
	if len(meta.Attrs) > 0 {
		if err := e.client.SetBlockAttrs(ctx, docID, meta.Attrs); err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File: tf.Path, Message: fmt.Sprintf("set block attributes: %v", err),
			})
		}
	}
}

// RouteAndSync runs the schema gate, the pre-sync router, the upload,
// and the title/attrs application for a single file -- the same code
// path Sync() uses inside processFile, exposed for the migrate apply
// executor (Req 6.4, design "sync/engine (extended) RouteAndSync").
//
// State is saved on success. Returns nil when the file was processed
// without any per-file errors.
//
// Non-fatal semantics (inherited from siyuan-knowledge-sync Req 13):
//   - Title (RenameDocByID) and attrs (SetBlockAttrs) failures DO produce
//     an error here, but the file IS already in state.Created/Updated.
//     Callers that need to distinguish "synced but with warnings" from
//     "rejected" can check e.state.Get(path) after a non-nil return.
//   - Create/Update API failures, schema violations, and routing
//     collisions DO NOT advance state and are fatal-for-that-file.
func (e *SyncEngine) RouteAndSync(ctx context.Context, path string) error {
	fullPath := filepath.Join(e.repoPath, path)
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("route-and-sync: stat %s: %w", path, err)
	}

	tf := types.TrackedFile{Path: path, ModTime: info.ModTime()}

	entry, hasState := e.state.Get(path)
	var existingSiYuanID string
	if hasState && entry != nil {
		existingSiYuanID = entry.SiYuanID
	}

	report := &types.SyncReport{}
	e.processFile(ctx, report, tf, existingSiYuanID, !hasState)

	if err := e.state.Save(); err != nil {
		return fmt.Errorf("route-and-sync: save state: %w", err)
	}

	if len(report.Errors) == 0 {
		return nil
	}
	errs := make([]error, 0, len(report.Errors))
	for _, se := range report.Errors {
		errs = append(errs, fmt.Errorf("%s: %s", se.File, se.Message))
	}
	return errors.Join(errs...)
}

func (e *SyncEngine) resolveNotebook(ctx context.Context, filePath string) (string, error) {
	notebookName := topLevelFolder(filePath)

	if id, ok := e.notebookCache[notebookName]; ok {
		return id, nil
	}

	notebooks, err := e.client.ListNotebooks(ctx)
	if err != nil {
		return "", fmt.Errorf("list notebooks: %w", err)
	}

	for _, nb := range notebooks {
		if nb.Name == notebookName {
			e.notebookCache[notebookName] = nb.ID
			return nb.ID, nil
		}
	}

	nb, err := e.client.CreateNotebook(ctx, notebookName)
	if err != nil {
		return "", fmt.Errorf("create notebook %q: %w", notebookName, err)
	}

	e.notebookCache[notebookName] = nb.ID
	return nb.ID, nil
}

func topLevelFolder(filePath string) string {
	clean := filepath.ToSlash(filepath.Clean(filePath))
	dir := filepath.Dir(clean)
	if dir == "." {
		return defaultNotebookName
	}
	parts := strings.SplitN(dir, "/", 2)
	return parts[0]
}

func buildHPath(filePath string) string {
	clean := filepath.ToSlash(filepath.Clean(filePath))
	top := topLevelFolder(clean)
	if top == defaultNotebookName {
		return "/" + clean
	}
	rest := strings.TrimPrefix(clean, top+"/")
	return "/" + rest
}

func (e *SyncEngine) Download(ctx context.Context, conflictMode string) (*types.SyncReport, error) {
	validModes := map[string]bool{"skip": true, "overwrite": true, "merge": true}
	if !validModes[conflictMode] {
		return nil, fmt.Errorf("invalid conflict mode %q: must be skip, overwrite, or merge", conflictMode)
	}

	report := &types.SyncReport{}

	notebooks, err := e.client.ListNotebooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list notebooks: %w", err)
	}

	for _, nb := range notebooks {
		e.downloadNotebook(ctx, nb, conflictMode, report)
	}

	if err := e.state.Save(); err != nil {
		return report, fmt.Errorf("save state: %w", err)
	}

	return report, nil
}

func (e *SyncEngine) downloadNotebook(ctx context.Context, nb types.Notebook, conflictMode string, report *types.SyncReport) {
	tree, err := e.client.ListDocTree(ctx, nb.ID, "/")
	if err != nil {
		report.Errors = append(report.Errors, types.SyncError{
			File:    nb.Name,
			Message: fmt.Sprintf("list doc tree: %v", err),
		})
		return
	}

	docIDs := collectDocIDs(tree)

	for _, docID := range docIDs {
		exportResult, err := e.client.ExportMdContent(ctx, docID)
		if err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File:    nb.Name + "/" + docID,
				Message: fmt.Sprintf("export: %v", err),
			})
			continue
		}

		localPath := localPathFromSiYuan(nb.Name, exportResult.HPath)
		fullPath := filepath.Join(e.repoPath, localPath)

		_, statErr := os.Stat(fullPath)
		fileExists := statErr == nil

		if fileExists && conflictMode == "skip" {
			continue
		}

		content := exportResult.Content
		if fileExists && conflictMode == "merge" {
			existing, err := os.ReadFile(fullPath)
			if err != nil {
				report.Errors = append(report.Errors, types.SyncError{
					File:    localPath,
					Message: fmt.Sprintf("read existing: %v", err),
				})
				continue
			}
			content = mergeContent(string(existing), content)
		}

		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File:    localPath,
				Message: fmt.Sprintf("create dir: %v", err),
			})
			continue
		}

		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File:    localPath,
				Message: fmt.Sprintf("write: %v", err),
			})
			continue
		}

		if fileExists {
			report.Updated = append(report.Updated, localPath)
		} else {
			report.Created = append(report.Created, localPath)
		}

		e.state.Put(types.SyncEntry{
			LocalPath:  localPath,
			SiYuanID:   exportResult.ID,
			NotebookID: nb.ID,
		})
	}
}

func collectDocIDs(nodes []types.TreeNode) []string {
	var ids []string
	for _, n := range nodes {
		if n.IsDoc() {
			ids = append(ids, n.ID)
		}
		ids = append(ids, collectDocIDs(n.Children)...)
	}
	return ids
}

func localPathFromSiYuan(notebookName, hpath string) string {
	clean := strings.TrimPrefix(hpath, "/")
	// SiYuan hpaths carry no file extension. Without ".md" the downloaded
	// file is invisible to the git scanner (which only tracks *.md), which
	// also makes the next sync treat every downloaded doc as "locally
	// deleted" and prune it from SiYuan. Always land downloads as .md.
	if !strings.HasSuffix(clean, ".md") {
		clean += ".md"
	}
	return filepath.Join(notebookName, clean)
}

func mergeContent(existing, incoming string) string {
	return "<<<<<<< local\n" + existing + "\n=======\n" + incoming + "\n>>>>>>> siyuan\n"
}

// hasSchemaCategoryIssue is the fast path that decides whether the schema
// gate needs to inspect the frontmatter at all. It scans the compliance
// audit output for any issue whose Category is "schema" — the marker the
// audit layer uses for ontology violations (Req 1, Req 2).
func hasSchemaCategoryIssue(issues []types.ComplianceIssue) bool {
	for _, i := range issues {
		if i.Category == "schema" {
			return true
		}
	}
	return false
}

// parseFrontmatterView extracts the top-level YAML mapping from a markdown
// file's frontmatter block and returns the value nodes for the two ontology
// keys. A nil node means the key was absent. When the content has no
// frontmatter, both nodes are nil. A YAML parse failure also returns an
// empty view — CheckOntologyFrontmatter then emits two missing-key
// violations, which is the correct gate response for malformed YAML that
// the file's author meant to declare ontology in.
func parseFrontmatterView(content []byte) ontology.FrontmatterView {
	fmBytes, ok := extractFrontmatterBytes(content)
	if !ok {
		return ontology.FrontmatterView{}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(fmBytes, &doc); err != nil {
		return ontology.FrontmatterView{}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return ontology.FrontmatterView{}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return ontology.FrontmatterView{}
	}
	var view ontology.FrontmatterView
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			continue
		}
		switch k.Value {
		case "domain":
			view.DomainNode = v
		case "intent":
			view.IntentNode = v
		}
	}
	return view
}

// extractFrontmatterBytes returns the YAML body between the two `---`
// fences, excluding the fences themselves. It returns (nil, false) when the
// content has no recognizable frontmatter block. This mirrors the parsing
// shape used by the compliance audit so the engine's gate and the audit
// agree on what counts as "frontmatter".
func extractFrontmatterBytes(content []byte) ([]byte, bool) {
	trimmed := bytes.TrimLeft(content, " \t\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, false
	}
	afterOpen := trimmed[3:]
	if len(afterOpen) == 0 || afterOpen[0] != '\n' {
		return nil, false
	}
	afterOpen = afterOpen[1:]
	closeIdx := bytes.Index(afterOpen, []byte("\n---"))
	if closeIdx < 0 {
		return nil, false
	}
	return afterOpen[:closeIdx], true
}

// probeStateCollision is a non-mutating check that mirrors what
// StateTracker.Move would return if invoked. Returning nil means a later
// state.Move is guaranteed not to fail with ErrCollision for this pair.
// Used before git mv so a collision never produces a half-applied move on
// disk (Req 3.2: file still at original path on collision).
//
// It returns state.ErrCollision when the target path already tracks a
// DIFFERENT SiYuanID than the source. All other configurations are
// permitted (oldPath == newPath, missing source, same-SiYuanID target).
func (e *SyncEngine) probeStateCollision(oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}
	src, hasSrc := e.state.Get(oldPath)
	tgt, hasTgt := e.state.Get(newPath)
	if !hasSrc || !hasTgt {
		return nil
	}
	if src.SiYuanID == tgt.SiYuanID {
		return nil
	}
	return state.ErrCollision
}

// gitMvAndCommit runs `git mv` from oldPath to newPath relative to
// repoPath and then commits the rename with the exact subject line the
// 3.2 routing test grep'd for. Parent directories for newPath are created
// as needed (git mv requires the destination parent to exist).
//
// On any subprocess failure the function returns a wrapped error and the
// caller records a per-file SyncError + returns, skipping the upload.
// Partial state is unsafe: a half-applied move followed by an upload
// would leave the working tree at the new path with no commit.
func gitMvAndCommit(repoPath, oldPath, newPath string) error {
	targetDir := filepath.Dir(filepath.Join(repoPath, newPath))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", targetDir, err)
	}

	mv := exec.Command("git", "-C", repoPath, "mv", oldPath, newPath)
	if out, err := mv.CombinedOutput(); err != nil {
		return fmt.Errorf("git mv %s -> %s: %w (%s)", oldPath, newPath, err, strings.TrimSpace(string(out)))
	}

	subject := fmt.Sprintf("ontology-route: %s -> %s", oldPath, newPath)
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

