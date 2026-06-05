package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"siyuan-knowledge-sync/internal/types"
)

func TestNewStateTracker_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	tr, err := NewStateTracker(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tr.All()) != 0 {
		t.Errorf("expected empty state, got %d entries", len(tr.All()))
	}
}

func TestNewStateTracker_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".siyuan-sync-state.json"), []byte("{not valid}"), 0644); err != nil {
		t.Fatal(err)
	}
	tr, err := NewStateTracker(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tr.All()) != 0 {
		t.Errorf("expected empty state after corrupt JSON, got %d entries", len(tr.All()))
	}
}

func TestNewStateTracker_ValidFile(t *testing.T) {
	dir := t.TempDir()
	syncedAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	state := types.SyncState{
		Entries: map[string]types.SyncEntry{
			"notes/test.md": {
				LocalPath:  "notes/test.md",
				SiYuanID:   "20240101120000-abc123",
				NotebookID: "20240101120000-nb1",
				SyncedAt:   syncedAt,
			},
		},
	}
	writeStateFile(t, dir, state)

	tr, err := NewStateTracker(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, ok := tr.Get("notes/test.md")
	if !ok {
		t.Fatal("expected entry not found")
	}
	if entry.SiYuanID != "20240101120000-abc123" {
		t.Errorf("expected siyuan_id '20240101120000-abc123', got %q", entry.SiYuanID)
	}
	if entry.NotebookID != "20240101120000-nb1" {
		t.Errorf("expected notebook_id '20240101120000-nb1', got %q", entry.NotebookID)
	}
	if !entry.SyncedAt.Equal(syncedAt) {
		t.Errorf("expected synced_at %v, got %v", syncedAt, entry.SyncedAt)
	}
}

func TestGet_NotFound(t *testing.T) {
	tr := newTracker(t)
	entry, ok := tr.Get("nonexistent.md")
	if ok {
		t.Errorf("expected not found, got entry: %+v", entry)
	}
}

func TestGetBySiYuanID(t *testing.T) {
	tr := newTracker(t)
	entry := types.SyncEntry{
		LocalPath:  "a.md",
		SiYuanID:   "id-a",
		NotebookID: "nb1",
	}
	tr.Put(entry)

	found, ok := tr.GetBySiYuanID("id-a")
	if !ok {
		t.Fatal("expected to find entry by SiYuan ID")
	}
	if found.LocalPath != "a.md" {
		t.Errorf("expected local_path 'a.md', got %q", found.LocalPath)
	}
}

func TestGetBySiYuanID_NotFound(t *testing.T) {
	tr := newTracker(t)
	_, ok := tr.GetBySiYuanID("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestRemove(t *testing.T) {
	tr := newTracker(t)
	tr.Put(types.SyncEntry{LocalPath: "x.md", SiYuanID: "id-x"})
	tr.Remove("x.md")

	_, ok := tr.Get("x.md")
	if ok {
		t.Error("expected entry to be removed")
	}
	if len(tr.All()) != 0 {
		t.Errorf("expected empty all(), got %d entries", len(tr.All()))
	}
}

func TestAll(t *testing.T) {
	tr := newTracker(t)
	tr.Put(types.SyncEntry{LocalPath: "a.md", SiYuanID: "id-a"})
	tr.Put(types.SyncEntry{LocalPath: "b.md", SiYuanID: "id-b"})

	all := tr.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}
	if all["a.md"].SiYuanID != "id-a" {
		t.Errorf("expected id-a, got %q", all["a.md"].SiYuanID)
	}
	if all["b.md"].SiYuanID != "id-b" {
		t.Errorf("expected id-b, got %q", all["b.md"].SiYuanID)
	}
}

func TestPut_UpdatesSyncedAt(t *testing.T) {
	tr := newTracker(t)
	entry := types.SyncEntry{LocalPath: "a.md", SiYuanID: "id-a"}
	tr.Put(entry)

	got, ok := tr.Get("a.md")
	if !ok {
		t.Fatal("expected entry")
	}
	if got.SyncedAt.IsZero() {
		t.Error("expected non-zero SyncedAt")
	}
}

func TestPut_UpdateExisting(t *testing.T) {
	tr := newTracker(t)
	tr.Put(types.SyncEntry{LocalPath: "a.md", SiYuanID: "id-a"})
	tr.Put(types.SyncEntry{LocalPath: "a.md", SiYuanID: "id-a-v2"})

	got, ok := tr.Get("a.md")
	if !ok {
		t.Fatal("expected entry")
	}
	if got.SiYuanID != "id-a-v2" {
		t.Errorf("expected updated id 'id-a-v2', got %q", got.SiYuanID)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	tr, err := NewStateTracker(dir)
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC()
	entry := types.SyncEntry{
		LocalPath:  "notes/hello.md",
		SiYuanID:   "20240101-hello",
		NotebookID: "nb-hello",
		SyncedAt:   before,
	}
	tr.Put(entry)

	if err := tr.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	reloaded, err := NewStateTracker(dir)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	got, ok := reloaded.Get("notes/hello.md")
	if !ok {
		t.Fatal("entry missing after reload")
	}
	if got.SiYuanID != "20240101-hello" {
		t.Errorf("siyuan_id mismatch: got %q", got.SiYuanID)
	}
	if got.NotebookID != "nb-hello" {
		t.Errorf("notebook_id mismatch: got %q", got.NotebookID)
	}
	if !got.SyncedAt.Equal(before) {
		t.Errorf("synced_at mismatch: before=%v after=%v", before, got.SyncedAt)
	}
}

func TestMultipleEntriesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	tr, err := NewStateTracker(dir)
	if err != nil {
		t.Fatal(err)
	}

	for i, item := range []struct{ path, id, nb string }{
		{"a.md", "id-a", "nb-1"},
		{"b.md", "id-b", "nb-1"},
		{"c.md", "id-c", "nb-2"},
	} {
		tr.Put(types.SyncEntry{
			LocalPath:  item.path,
			SiYuanID:   item.id,
			NotebookID: item.nb,
			SyncedAt:   time.Date(2024, 1, 1+i, 12, 0, 0, 0, time.UTC),
		})
	}

	if err := tr.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	reloaded, err := NewStateTracker(dir)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	all := reloaded.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(all))
	}

	for _, item := range []struct{ path, id, nb string }{
		{"a.md", "id-a", "nb-1"},
		{"b.md", "id-b", "nb-1"},
		{"c.md", "id-c", "nb-2"},
	} {
		e, ok := all[item.path]
		if !ok {
			t.Errorf("entry %q missing", item.path)
			continue
		}
		if e.SiYuanID != item.id {
			t.Errorf("%s: siyuan_id %q, want %q", item.path, e.SiYuanID, item.id)
		}
		if e.NotebookID != item.nb {
			t.Errorf("%s: notebook_id %q, want %q", item.path, e.NotebookID, item.nb)
		}
	}
}

func TestSave_WritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	tr, err := NewStateTracker(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr.Put(types.SyncEntry{
		LocalPath:  "a.md",
		SiYuanID:   "id-a",
		NotebookID: "nb-1",
		SyncedAt:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
	})

	if err := tr.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".siyuan-sync-state.json"))
	if err != nil {
		t.Fatalf("cannot read state file: %v", err)
	}

	var loaded types.SyncState
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}

	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry in JSON, got %d", len(loaded.Entries))
	}
	e, ok := loaded.Entries["a.md"]
	if !ok {
		t.Fatal("entry 'a.md' missing")
	}
	if e.SiYuanID != "id-a" {
		t.Errorf("siyuan_id %q, want 'id-a'", e.SiYuanID)
	}
}

func TestMove_HappyPath_PreservesIdentityFields(t *testing.T) {
	tr := newTracker(t)
	syncedAt := time.Date(2024, 3, 15, 9, 30, 0, 0, time.UTC)
	tr.Put(types.SyncEntry{
		LocalPath:  "a.md",
		SiYuanID:   "20240315093000-abcdef",
		NotebookID: "nb-devops",
		SyncedAt:   syncedAt,
	})

	if err := tr.Move("a.md", "wiki/Linux & DevOps/a.md"); err != nil {
		t.Fatalf("Move returned unexpected error: %v", err)
	}

	if _, ok := tr.Get("a.md"); ok {
		t.Error("expected old key 'a.md' to be gone after Move")
	}

	got, ok := tr.Get("wiki/Linux & DevOps/a.md")
	if !ok {
		t.Fatal("expected new key 'wiki/Linux & DevOps/a.md' to be present after Move")
	}
	if got.SiYuanID != "20240315093000-abcdef" {
		t.Errorf("SiYuanID not preserved: got %q want %q", got.SiYuanID, "20240315093000-abcdef")
	}
	if got.NotebookID != "nb-devops" {
		t.Errorf("NotebookID not preserved: got %q want %q", got.NotebookID, "nb-devops")
	}
	if !got.SyncedAt.Equal(syncedAt) {
		t.Errorf("SyncedAt not preserved: got %v want %v", got.SyncedAt, syncedAt)
	}
	if got.LocalPath != "wiki/Linux & DevOps/a.md" {
		t.Errorf("LocalPath not updated: got %q want %q", got.LocalPath, "wiki/Linux & DevOps/a.md")
	}
}

func TestMove_SamePaths_NoOp(t *testing.T) {
	tr := newTracker(t)
	syncedAt := time.Date(2024, 4, 1, 10, 0, 0, 0, time.UTC)
	tr.Put(types.SyncEntry{
		LocalPath:  "a.md",
		SiYuanID:   "id-a",
		NotebookID: "nb-1",
		SyncedAt:   syncedAt,
	})

	if err := tr.Move("a.md", "a.md"); err != nil {
		t.Fatalf("Move(same,same) returned unexpected error: %v", err)
	}

	got, ok := tr.Get("a.md")
	if !ok {
		t.Fatal("entry 'a.md' should still be present after no-op Move")
	}
	if got.SiYuanID != "id-a" || got.NotebookID != "nb-1" || !got.SyncedAt.Equal(syncedAt) {
		t.Errorf("entry mutated unexpectedly: %+v", got)
	}
}

func TestMove_NoSourceEntry_NoOp(t *testing.T) {
	tr := newTracker(t)
	if err := tr.Move("missing.md", "x.md"); err != nil {
		t.Fatalf("Move(missing,x) returned unexpected error: %v", err)
	}
	if _, ok := tr.Get("missing.md"); ok {
		t.Error("'missing.md' should not exist")
	}
	if _, ok := tr.Get("x.md"); ok {
		t.Error("'x.md' should not exist (Move from missing source must not synthesize a target)")
	}
}

func TestMove_Collision_DifferentSiYuanIDs(t *testing.T) {
	tr := newTracker(t)
	aSynced := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	bSynced := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	tr.Put(types.SyncEntry{LocalPath: "a.md", SiYuanID: "id-a", NotebookID: "nb-1", SyncedAt: aSynced})
	tr.Put(types.SyncEntry{LocalPath: "b.md", SiYuanID: "id-b", NotebookID: "nb-1", SyncedAt: bSynced})

	err := tr.Move("a.md", "b.md")
	if err == nil {
		t.Fatal("expected ErrCollision, got nil")
	}
	if !errors.Is(err, ErrCollision) {
		t.Errorf("expected errors.Is(err, ErrCollision) to be true, got err=%v", err)
	}

	gotA, okA := tr.Get("a.md")
	if !okA {
		t.Error("source 'a.md' should still be present after collision")
	} else if gotA.SiYuanID != "id-a" || !gotA.SyncedAt.Equal(aSynced) {
		t.Errorf("'a.md' mutated after collision: %+v", gotA)
	}

	gotB, okB := tr.Get("b.md")
	if !okB {
		t.Error("target 'b.md' should still be present after collision")
	} else if gotB.SiYuanID != "id-b" || !gotB.SyncedAt.Equal(bSynced) {
		t.Errorf("'b.md' mutated after collision: %+v", gotB)
	}
}

func TestMove_IdempotentRetry_SameSiYuanID(t *testing.T) {
	tr := newTracker(t)
	aSynced := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	bSynced := time.Date(2024, 5, 2, 12, 0, 0, 0, time.UTC)
	// Simulates a previously-half-applied move: target already holds the same SiYuanID.
	tr.Put(types.SyncEntry{LocalPath: "a.md", SiYuanID: "id-a", NotebookID: "nb-1", SyncedAt: aSynced})
	tr.Put(types.SyncEntry{LocalPath: "b.md", SiYuanID: "id-a", NotebookID: "nb-1", SyncedAt: bSynced})

	if err := tr.Move("a.md", "b.md"); err != nil {
		t.Fatalf("idempotent retry Move returned unexpected error: %v", err)
	}

	if _, ok := tr.Get("a.md"); ok {
		t.Error("'a.md' should be gone after idempotent retry Move")
	}
	gotB, ok := tr.Get("b.md")
	if !ok {
		t.Fatal("'b.md' should still be present after idempotent retry Move")
	}
	if gotB.SiYuanID != "id-a" {
		t.Errorf("'b.md' SiYuanID changed: got %q want %q", gotB.SiYuanID, "id-a")
	}
	if !gotB.SyncedAt.Equal(bSynced) {
		t.Errorf("'b.md' SyncedAt mutated: got %v want %v (target should be left as-is)", gotB.SyncedAt, bSynced)
	}
}

func TestMove_PersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	tr, err := NewStateTracker(dir)
	if err != nil {
		t.Fatal(err)
	}
	syncedAt := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	tr.Put(types.SyncEntry{
		LocalPath:  "a.md",
		SiYuanID:   "id-move-rt",
		NotebookID: "nb-rt",
		SyncedAt:   syncedAt,
	})

	if err := tr.Move("a.md", "wiki/Linux & DevOps/a.md"); err != nil {
		t.Fatalf("Move failed: %v", err)
	}
	if err := tr.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	reloaded, err := NewStateTracker(dir)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if _, ok := reloaded.Get("a.md"); ok {
		t.Error("reloaded state should not contain old key 'a.md'")
	}
	got, ok := reloaded.Get("wiki/Linux & DevOps/a.md")
	if !ok {
		t.Fatal("reloaded state missing new key 'wiki/Linux & DevOps/a.md'")
	}
	if got.SiYuanID != "id-move-rt" {
		t.Errorf("SiYuanID lost across save/reload: got %q", got.SiYuanID)
	}
	if got.NotebookID != "nb-rt" {
		t.Errorf("NotebookID lost across save/reload: got %q", got.NotebookID)
	}
	if !got.SyncedAt.Equal(syncedAt) {
		t.Errorf("SyncedAt lost across save/reload: got %v want %v", got.SyncedAt, syncedAt)
	}
}

func newTracker(t *testing.T) *StateTracker {
	t.Helper()
	tr, err := NewStateTracker(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func writeStateFile(t *testing.T, dir string, state types.SyncState) {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".siyuan-sync-state.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}
