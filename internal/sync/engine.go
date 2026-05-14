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
	"siyuan-knowledge-sync/internal/types"
)

const defaultNotebookName = "root"

type SyncEngine struct {
	client        *siyuan.Client
	scanner       *git.GitScanner
	state         *state.StateTracker
	compliance    *compliance.ComplianceEngine
	notebookCache map[string]string
	repoPath      string
}

func NewSyncEngine(client *siyuan.Client, scanner *git.GitScanner, tracker *state.StateTracker, ce *compliance.ComplianceEngine) *SyncEngine {
	return &SyncEngine{
		client:        client,
		scanner:       scanner,
		state:         tracker,
		compliance:    ce,
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

	if isNew || existingSiYuanID == "" {
		docID, err := e.client.CreateDocWithMd(ctx, notebookID, hpath, string(fixedContent))
		if err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File: tf.Path, Message: fmt.Sprintf("create document: %v", err),
			})
			return
		}
		e.state.Put(types.SyncEntry{
			LocalPath:  tf.Path,
			SiYuanID:   docID,
			NotebookID: notebookID,
		})
		report.Created = append(report.Created, tf.Path)
	} else {
		if err := e.client.UpdateBlock(ctx, existingSiYuanID, string(fixedContent)); err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File: tf.Path, Message: fmt.Sprintf("update document: %v", err),
			})
			return
		}
		e.state.Put(types.SyncEntry{
			LocalPath:  tf.Path,
			SiYuanID:   existingSiYuanID,
			NotebookID: notebookID,
		})
		report.Updated = append(report.Updated, tf.Path)
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
