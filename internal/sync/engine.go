package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/git"
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

	e.pruneDeleted(ctx, trackedFiles, report)

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

	fixedContent, _, err := e.compliance.AutoFix(tf.Path, content)
	if err != nil {
		report.Errors = append(report.Errors, types.SyncError{
			File: tf.Path, Message: fmt.Sprintf("compliance: %v", err),
		})
		return
	}

	notebookID, err := e.resolveNotebook(ctx, tf.Path)
	if err != nil {
		report.Errors = append(report.Errors, types.SyncError{
			File: tf.Path, Message: fmt.Sprintf("notebook: %v", err),
		})
		return
	}

	hpath := buildHPath(tf.Path)

	// Step 2: single-pass frontmatter + tag extraction.
	meta, metaErr := e.tags.ExtractMeta(fixedContent)

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
