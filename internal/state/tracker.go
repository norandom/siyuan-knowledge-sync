package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"siyuan-knowledge-sync/internal/types"
)

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
		if os.IsNotExist(err) {
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
