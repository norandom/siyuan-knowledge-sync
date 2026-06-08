package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"siyuan-knowledge-sync/internal/types"
)

// ErrCollision is returned by StateTracker.Move when the target path already
// tracks a different SiYuan document (different SiYuanID). Callers should use
// errors.Is to detect it.
var ErrCollision = errors.New("state: target path already tracks a different SiYuan document")

type StateTracker struct {
	filePath string
	state    types.SyncState
}

func NewStateTracker(repoPath string) (*StateTracker, error) {
	t := &StateTracker{
		filePath: filepath.Join(repoPath, ".siyuan-sync-state.json"),
		state: types.SyncState{
			Entries: make(map[string]types.SyncEntry),
		},
	}

	data, err := os.ReadFile(t.filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return t, nil
		}
		return t, nil
	}

	var loaded types.SyncState
	if err := json.Unmarshal(data, &loaded); err != nil {
		fmt.Fprintf(os.Stderr, "siyuan-sync: corrupt state file %s, starting with empty state: %v\n", t.filePath, err)
		return t, nil
	}

	if loaded.Entries == nil {
		loaded.Entries = make(map[string]types.SyncEntry)
	}
	t.state = loaded
	return t, nil
}

func (t *StateTracker) Get(path string) (*types.SyncEntry, bool) {
	entry, ok := t.state.Entries[path]
	if !ok {
		return nil, false
	}
	return &entry, true
}

func (t *StateTracker) GetBySiYuanID(id string) (*types.SyncEntry, bool) {
	for _, entry := range t.state.Entries {
		if entry.SiYuanID == id {
			return &entry, true
		}
	}
	return nil, false
}

func (t *StateTracker) Put(entry types.SyncEntry) {
	if entry.SyncedAt.IsZero() {
		entry.SyncedAt = time.Now()
	}
	t.state.Entries[entry.LocalPath] = entry
}

func (t *StateTracker) Remove(path string) {
	delete(t.state.Entries, path)
}

func (t *StateTracker) All() map[string]types.SyncEntry {
	return t.state.Entries
}

// Move renames an entry from oldPath to newPath, preserving SiYuanID,
// NotebookID, and SyncedAt. It mirrors Put/Remove semantics: in-memory only,
// callers must invoke Save() to persist.
//
// Behavior:
//   - oldPath == newPath: no-op, returns nil (the entry, if any, is untouched).
//   - source entry missing: no-op, returns nil. A move with nothing to move
//     leaves state unchanged; we deliberately do not synthesize a target entry.
//   - target entry exists with the same SiYuanID as the source: treated as an
//     idempotent retry of a previously half-applied move. The source entry is
//     removed and the existing target entry is left as-is.
//   - target entry exists with a different SiYuanID: returns ErrCollision
//     without mutating state.
//   - happy path: target entry is set to the source entry (with LocalPath
//     updated to newPath) and the source entry is deleted.
func (t *StateTracker) Move(oldPath, newPath string) error {
	if oldPath == newPath {
		return nil
	}

	src, ok := t.state.Entries[oldPath]
	if !ok {
		// Nothing to move; treat as no-op so callers can retry without
		// having to first probe Get(oldPath).
		return nil
	}

	if existing, exists := t.state.Entries[newPath]; exists {
		if existing.SiYuanID != src.SiYuanID {
			return ErrCollision
		}
		// Same SiYuanID at target: previously half-applied move. Drop the
		// source and leave the target as-is.
		delete(t.state.Entries, oldPath)
		return nil
	}

	moved := src
	moved.LocalPath = newPath
	t.state.Entries[newPath] = moved
	delete(t.state.Entries, oldPath)
	return nil
}

func (t *StateTracker) Save() error {
	data, err := json.MarshalIndent(t.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(t.filePath, data, 0644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}
