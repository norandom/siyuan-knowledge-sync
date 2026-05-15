package sync

import (
	"context"
	"fmt"

	"siyuan-knowledge-sync/internal/types"
)

func (e *SyncEngine) pruneDeleted(ctx context.Context, trackedFiles []types.TrackedFile, report *types.SyncReport) {
	trackedSet := make(map[string]bool, len(trackedFiles))
	for _, tf := range trackedFiles {
		trackedSet[tf.Path] = true
	}

	allState := e.state.All()

	for path, entry := range allState {
		if trackedSet[path] {
			continue
		}

		if entry.SiYuanID == "" {
			report.Errors = append(report.Errors, types.SyncError{
				File:    path,
				Message: "dependency conflict: no SiYuan document ID in state (not created by sync)",
			})
			e.state.Remove(path)
			continue
		}

		if conflict := e.checkPruneDependency(ctx, entry); conflict != "" {
			report.Errors = append(report.Errors, types.SyncError{
				File:    path,
				Message: conflict,
			})
			continue
		}

		if err := e.client.RemoveDocByID(ctx, entry.SiYuanID); err != nil {
			report.Errors = append(report.Errors, types.SyncError{
				File:    path,
				Message: fmt.Sprintf("remove document: %v", err),
			})
			continue
		}

		e.state.Remove(path)
		report.Pruned = append(report.Pruned, path)
	}
}

func (e *SyncEngine) checkPruneDependency(ctx context.Context, entry types.SyncEntry) string {
	tree, err := e.client.ListDocTree(ctx, entry.NotebookID, "/")
	if err != nil {
		return ""
	}

	children := findChildDocIDs(tree, entry.SiYuanID)
	for _, childID := range children {
		if _, ok := e.state.GetBySiYuanID(childID); !ok {
			return fmt.Sprintf("dependency conflict: document %q has child %q not created by sync", entry.SiYuanID, childID)
		}
	}

	return ""
}

func findChildDocIDs(nodes []types.TreeNode, targetID string) []string {
	for _, n := range nodes {
		if n.ID == targetID {
			return collectDocIDs(n.Children)
		}
		if ids := findChildDocIDs(n.Children, targetID); len(ids) > 0 {
			return ids
		}
	}
	return nil
}
