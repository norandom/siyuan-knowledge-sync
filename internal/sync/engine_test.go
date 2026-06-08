package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/siyuan"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/testutil"
	"siyuan-knowledge-sync/internal/types"
)

func setupGitDir(t *testing.T) string {
	return testutil.SetupGitRepo(t, "sync-engine-test")
}

type mockSiYuanHandler struct {
	t             *testing.T
	notebooks     map[string]string
	docs          map[string]mockDocRecord
	docTrees      map[string][]types.TreeNode
	nextNBID      int
	nextDocID     int
	createdDocs   []createdDocRecord
	updatedDocs   []string
	createdNBs    []string
	removedDocIDs []string
	renamedTitles map[string]string
	setAttrs      map[string]map[string]string
	renameErr     bool
	setAttrsErr   bool
}

type mockDocRecord struct {
	NotebookID string
	HPath      string
	Markdown   string
	ID         string
}

type createdDocRecord struct {
	NotebookID string
	HPath      string
	Markdown   string
	ID         string
}

// userCreatedDocs returns h.createdDocs with the engine-owned per-intent
// index docs (HPath `/_<intent>_index.md`) filtered out. Tests that
// assert on the count or content of USER creates use this view; the
// indices are a derived artifact upserted once per canonical notebook
// by SyncEngine.ensureIntentIndices.
func (h *mockSiYuanHandler) userCreatedDocs() []createdDocRecord {
	out := make([]createdDocRecord, 0, len(h.createdDocs))
	for _, d := range h.createdDocs {
		if strings.HasPrefix(d.HPath, "/_") && strings.HasSuffix(d.HPath, "_index.md") {
			continue
		}
		out = append(out, d)
	}
	return out
}

func newMockSiYuanServer(t *testing.T) (*mockSiYuanHandler, *httptest.Server) {
	t.Helper()
	h := &mockSiYuanHandler{
		t:             t,
		notebooks:     make(map[string]string),
		docs:          make(map[string]mockDocRecord),
		renamedTitles: make(map[string]string),
		setAttrs:      make(map[string]map[string]string),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		var body map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}

		switch r.URL.Path {
		case "/api/notebook/lsNotebooks":
			nbs := make([]types.Notebook, 0, len(h.notebooks))
			for name, id := range h.notebooks {
				nbs = append(nbs, types.Notebook{ID: id, Name: name})
			}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"notebooks": nbs},
			})

		case "/api/notebook/createNotebook":
			name, ok := body["name"].(string)
			if !ok || name == "" {
				enc.Encode(map[string]any{"code": 1, "msg": "missing notebook name"})
				return
			}
			h.nextNBID++
			id := fmt.Sprintf("nb-%d", h.nextNBID)
			h.notebooks[name] = id
			h.createdNBs = append(h.createdNBs, name)
			nbResp := types.Notebook{ID: id, Name: name}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"notebook": nbResp},
			})

		case "/api/filetree/createDocWithMd":
			nbID, _ := body["notebook"].(string)
			hpath, _ := body["path"].(string)
			md, _ := body["markdown"].(string)
			h.nextDocID++
			id := fmt.Sprintf("doc-%d", h.nextDocID)
			h.docs[id] = mockDocRecord{
				NotebookID: nbID,
				HPath:      hpath,
				Markdown:   md,
				ID:         id,
			}
			h.createdDocs = append(h.createdDocs, createdDocRecord{
				NotebookID: nbID,
				HPath:      hpath,
				Markdown:   md,
				ID:         id,
			})
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": id,
			})

		case "/api/block/updateBlock":
			id, _ := body["id"].(string)
			md, _ := body["data"].(string)
			if _, exists := h.docs[id]; exists {
				h.docs[id] = mockDocRecord{
					ID:       id,
					Markdown: md,
				}
			}
			h.updatedDocs = append(h.updatedDocs, id)
			enc.Encode(map[string]any{"code": 0, "msg": ""})

		case "/api/filetree/listDocsByPath":
			notebookID, _ := body["notebook"].(string)
			tree := h.docTrees[notebookID]
			if tree == nil {
				tree = []types.TreeNode{}
			}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"files": tree},
			})

		case "/api/filetree/removeDocByID":
			id, _ := body["id"].(string)
			delete(h.docs, id)
			h.removedDocIDs = append(h.removedDocIDs, id)
			enc.Encode(map[string]any{"code": 0, "msg": ""})

		case "/api/filetree/renameDocByID":
			id, _ := body["id"].(string)
			title, _ := body["title"].(string)
			if h.renameErr {
				enc.Encode(map[string]any{"code": 1, "msg": "rename failed"})
				return
			}
			h.renamedTitles[id] = title
			enc.Encode(map[string]any{"code": 0, "msg": ""})

		case "/api/attr/setBlockAttrs":
			id, _ := body["id"].(string)
			if h.setAttrsErr {
				enc.Encode(map[string]any{"code": 1, "msg": "setBlockAttrs failed"})
				return
			}
			attrs := make(map[string]string)
			if raw, ok := body["attrs"].(map[string]any); ok {
				for k, v := range raw {
					sv, _ := v.(string)
					attrs[k] = sv
				}
			}
			h.setAttrs[id] = attrs
			enc.Encode(map[string]any{"code": 0, "msg": ""})

		case "/api/query/sql":
			// Indices' SQL queries return empty result sets in the mock.
			enc.Encode(map[string]any{"code": 0, "msg": "", "data": []map[string]any{}})

		default:
			enc.Encode(map[string]any{"code": 1, "msg": "unknown endpoint: " + r.URL.Path})
		}
	}))
	return h, server
}

func newSyncEngine(t *testing.T, server *httptest.Server, repoPath string, autofix bool) (*SyncEngine, *mockSiYuanHandler) {
	t.Helper()
	client := siyuan.NewClient(server.URL, "test-token")
	scanner, err := git.NewGitScanner(repoPath)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(repoPath)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(autofix)
	engine := NewSyncEngine(client, scanner, tracker, ce)
	return engine, nil
}

func TestSync_NewFiles_CreatesDocuments(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notebook/sub/file.md", "# Hello\n\nWorld\n")
	testutil.GitCmd(t, dir, "add", "notebook/sub/file.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d: %v", len(report.Created), report.Created)
	}
	if report.Created[0] != "notebook/sub/file.md" {
		t.Errorf("expected 'notebook/sub/file.md', got %q", report.Created[0])
	}
	if len(report.Updated) != 0 {
		t.Errorf("expected 0 updated, got %d", len(report.Updated))
	}
	if len(report.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(report.Errors), report.Errors)
	}

	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 document created in SiYuan, got %d", len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]
	if doc.HPath != "/sub/file.md" {
		t.Errorf("expected hpath '/sub/file.md', got %q", doc.HPath)
	}
	if doc.Markdown != "# Hello\n\nWorld\n" {
		t.Errorf("expected markdown '# Hello\\n\\nWorld\\n', got %q", doc.Markdown)
	}
	if _, ok := h.notebooks["notebook"]; !ok {
		t.Errorf("expected notebook 'notebook' to be created, got notebooks: %v", h.notebooks)
	}
}

func TestSync_ModifiedFiles_UpdatesDocuments(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/doc.md", "# Original")
	testutil.GitCmd(t, dir, "add", "notes/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	if len(report.Created) != 1 {
		t.Fatalf("first sync: expected 1 created, got %d", len(report.Created))
	}

	time.Sleep(100 * time.Millisecond)

	testutil.WriteFile(t, dir, "notes/doc.md", "# Modified Content")
	testutil.GitCmd(t, dir, "add", "notes/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "modified")

	scanner, err := git.NewGitScanner(dir)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}

	if len(report2.Updated) != 1 {
		t.Fatalf("second sync: expected 1 updated, got %d (created=%v updated=%v errors=%v)",
			len(report2.Updated), report2.Created, report2.Updated, report2.Errors)
	}
	if report2.Updated[0] != "notes/doc.md" {
		t.Errorf("expected 'notes/doc.md', got %q", report2.Updated[0])
	}
	if len(h.updatedDocs) != 1 {
		t.Errorf("expected 1 UpdateBlock call, got %d", len(h.updatedDocs))
	}
}

func TestSync_FolderHierarchyPreserved(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "journal/2024/01/entry.md", "# January Entry")
	testutil.WriteFile(t, dir, "projects/code/readme.md", "# Code Readme")
	testutil.GitCmd(t, dir, "add", "journal/2024/01/entry.md", "projects/code/readme.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 2 {
		t.Fatalf("expected 2 created, got %d", len(report.Created))
	}
	if len(h.userCreatedDocs()) != 2 {
		t.Fatalf("expected 2 documents created, got %d", len(h.userCreatedDocs()))
	}

	if _, ok := h.notebooks["journal"]; !ok {
		t.Errorf("expected notebook 'journal', got %v", h.notebooks)
	}
	if _, ok := h.notebooks["projects"]; !ok {
		t.Errorf("expected notebook 'projects', got %v", h.notebooks)
	}

	hpPaths := make(map[string]string)
	for _, d := range h.createdDocs {
		hpPaths[d.HPath] = d.NotebookID
	}
	journalNB := h.notebooks["journal"]
	projectsNB := h.notebooks["projects"]

	if hpPaths["/2024/01/entry.md"] != journalNB {
		t.Errorf("expected '/2024/01/entry.md' in notebook 'journal', got notebook %q",
			hpPaths["/2024/01/entry.md"])
	}
	if hpPaths["/code/readme.md"] != projectsNB {
		t.Errorf("expected '/code/readme.md' in notebook 'projects', got notebook %q",
			hpPaths["/code/readme.md"])
	}
}

func TestSync_SkipsUnchangedFiles(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/a.md", "# A")
	testutil.WriteFile(t, dir, "notes/b.md", "# B")
	testutil.GitCmd(t, dir, "add", "notes/a.md", "notes/b.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	if len(report.Created) != 2 {
		t.Fatalf("first sync: expected 2 created, got %d", len(report.Created))
	}

	testutil.WriteFile(t, dir, "notes/a.md", "# A - Modified")
	// Deterministically mark a.md as modified-after-sync. Relying on natural
	// mtime + a short sleep is flaky on filesystems with coarse mtime
	// granularity (e.g. containerized CI). git add/commit does not alter the
	// working-tree file's mtime, so this survives the commit below.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "notes/a.md"), future, future); err != nil {
		t.Fatal(err)
	}
	testutil.GitCmd(t, dir, "add", "notes/a.md")
	testutil.GitCmd(t, dir, "commit", "-m", "modify a")

	scanner, err := git.NewGitScanner(dir)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}

	if len(report2.Created) != 0 {
		t.Errorf("expected 0 created on second sync, got %d", len(report2.Created))
	}
	if len(report2.Updated) != 1 {
		t.Errorf("expected 1 updated (a.md), got %d", len(report2.Updated))
	}

	totalCalls := len(h.userCreatedDocs()) + len(h.updatedDocs)
	if totalCalls != 3 {
		t.Errorf("expected 3 total SiYuan operations (2 creates + 1 update), got %d", totalCalls)
	}
}

func TestSync_ReportCounts(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denied read path is not exercisable as root (containerized CI)")
	}
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "nb/new.md", "# New")
	testutil.WriteFile(t, dir, "nb/err.md", "# Error")
	testutil.GitCmd(t, dir, "add", "nb/new.md", "nb/err.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	errPath := filepath.Join(dir, "nb/err.md")
	if err := os.Chmod(errPath, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(errPath, 0o644)

	_, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) < 1 {
		t.Errorf("expected at least 1 created, got %d", len(report.Created))
	}
	if len(report.Errors) < 1 {
		t.Errorf("expected at least 1 error, got %d", len(report.Errors))
	}
}

func TestSync_ComplianceRunsBeforeUpload(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/doc.md", `---
title: Test
---

# Title

### Skipped H2

{: myattr="val"}

Content.
`)
	testutil.GitCmd(t, dir, "add", "notes/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, true)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d (errors=%v)", len(report.Created), report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 document created, got %d", len(h.userCreatedDocs()))
	}

	fixedContent := h.userCreatedDocs()[0].Markdown
	if !strings.Contains(fixedContent, "## Skipped H2") {
		t.Errorf("expected heading to be fixed (### -> ##), got:\n%s", fixedContent)
	}
	if !strings.Contains(fixedContent, "custom-myattr") {
		t.Errorf("expected attribute to be fixed (myattr -> custom-myattr), got:\n%s", fixedContent)
	}
}

func TestSync_EmptyRepo(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 0 {
		t.Errorf("expected 0 created, got %d", len(report.Created))
	}
	if len(report.Updated) != 0 {
		t.Errorf("expected 0 updated, got %d", len(report.Updated))
	}
	if len(report.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(report.Errors))
	}
	if len(h.userCreatedDocs()) != 0 {
		t.Errorf("expected 0 API calls, got %d", len(h.userCreatedDocs()))
	}
}

func TestSync_ErrorPerFileDoesNotAbort(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denied read path is not exercisable as root (containerized CI)")
	}
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "nb/good.md", "# Good")
	testutil.WriteFile(t, dir, "nb/bad_perm.md", "# Will Fail")
	testutil.WriteFile(t, dir, "nb/also_good.md", "# Also Good")
	testutil.GitCmd(t, dir, "add", "nb/good.md", "nb/bad_perm.md", "nb/also_good.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	badPath := filepath.Join(dir, "nb/bad_perm.md")
	if err := os.Chmod(badPath, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(badPath, 0o644)

	_, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) < 2 {
		t.Errorf("expected at least 2 created, got %d (errors=%v)", len(report.Created), report.Errors)
	}
	if len(report.Errors) < 1 {
		t.Errorf("expected at least 1 error, got %d", len(report.Errors))
	}
}

func TestSync_RootLevelMdMapsToDefaultNotebook(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "readme.md", "# Readme\n\nRoot level file.\n")
	testutil.GitCmd(t, dir, "add", "readme.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d (errors=%v)", len(report.Created), report.Errors)
	}
	if _, ok := h.notebooks["root"]; !ok {
		t.Errorf("expected default notebook 'root' to be created, got %v", h.notebooks)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 doc created, got %d", len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]
	if doc.HPath != "/readme.md" {
		t.Errorf("expected hpath '/readme.md', got %q", doc.HPath)
	}
}

func TestSync_StateTrackerUpdatedAfterSync(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/a.md", "# A")
	testutil.GitCmd(t, dir, "add", "notes/a.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	_, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	_, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	engineState := engine.state.All()
	if len(engineState) != 1 {
		t.Fatalf("expected 1 state entry after sync, got %d", len(engineState))
	}
	entry, ok := engineState["notes/a.md"]
	if !ok {
		t.Fatal("expected entry for 'notes/a.md'")
	}
	if entry.SiYuanID == "" {
		t.Error("expected non-empty SiYuanID")
	}
	if entry.NotebookID == "" {
		t.Error("expected non-empty NotebookID")
	}
	if entry.SyncedAt.IsZero() {
		t.Error("expected non-zero SyncedAt")
	}

	tracker, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	reloaded, ok := tracker.Get("notes/a.md")
	if !ok {
		t.Fatalf("expected entry to persist to disk, got state: %v", tracker.All())
	}
	if reloaded.SiYuanID != entry.SiYuanID {
		t.Errorf("persisted SiYuanID mismatch: %q vs %q", reloaded.SiYuanID, entry.SiYuanID)
	}
}

func TestSync_PreSeededState_ModifiedDetection(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/existing.md", "# Original")
	testutil.WriteFile(t, dir, "notes/new.md", "# New File")
	testutil.GitCmd(t, dir, "add", "notes/existing.md", "notes/new.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	_, server := newMockSiYuanServer(t)
	defer server.Close()

	tr, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	tr.Put(types.SyncEntry{
		LocalPath:  "notes/existing.md",
		SiYuanID:   "pre-existing-si-id",
		NotebookID: "nb-notes",
		SyncedAt:   time.Now().Add(-1 * time.Hour),
	})
	tr.Save()

	scanner, err := git.NewGitScanner(dir)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine := NewSyncEngine(client, scanner, tracker, ce)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Errorf("expected 1 created (new.md), got %d (created=%v updated=%v)",
			len(report.Created), report.Created, report.Updated)
	}
	if len(report.Updated) != 1 {
		t.Errorf("expected 1 updated (existing.md), got %d (created=%v updated=%v)",
			len(report.Updated), report.Created, report.Updated)
	}
}

func TestSync_SiYuanCreateErrorRecorded(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/fail.md", "# Will Fail")
	testutil.GitCmd(t, dir, "add", "notes/fail.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/notebook/lsNotebooks":
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"notebooks": []types.Notebook{}},
			})
		case "/api/notebook/createNotebook":
			nb := types.Notebook{ID: "nb-1", Name: "notes"}
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "",
				"data": nb,
			})
		case "/api/filetree/createDocWithMd":
			json.NewEncoder(w).Encode(map[string]any{
				"code": 500, "msg": "internal error",
			})
		default:
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": ""})
		}
	}))
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 0 {
		t.Errorf("expected 0 created, got %d", len(report.Created))
	}
	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(report.Errors))
	}
	if !strings.Contains(report.Errors[0].Message, "create document") {
		t.Errorf("expected create-document error, got %q", report.Errors[0].Message)
	}
}

func TestSync_NotebookReusedAcrossFiles(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "wiki/a.md", "# A")
	testutil.WriteFile(t, dir, "wiki/b.md", "# B")
	testutil.GitCmd(t, dir, "add", "wiki/a.md", "wiki/b.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 2 {
		t.Fatalf("expected 2 created, got %d", len(report.Created))
	}
	if len(h.createdNBs) > 1 {
		t.Errorf("expected at most 1 notebook created, got %d: %v", len(h.createdNBs), h.createdNBs)
	}
}

func TestSync_AutofixEnabled(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "wiki/doc.md", `### Bad Heading
Some content {: myattr="value"}
`)
	testutil.GitCmd(t, dir, "add", "wiki/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, true)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d (errors=%v)", len(report.Created), report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(h.userCreatedDocs()))
	}

	content := h.userCreatedDocs()[0].Markdown
	if !strings.Contains(content, "# Bad Heading") {
		t.Errorf("expected ### to become #, got:\n%s", content)
	}
	if !strings.Contains(content, "custom-myattr") {
		t.Errorf("expected myattr to become custom-myattr, got:\n%s", content)
	}
}

func TestSync_AutofixDisabled(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "wiki/doc.md", `### Bad Heading
{: myattr="value"}
`)
	testutil.GitCmd(t, dir, "add", "wiki/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d (errors=%v)", len(report.Created), report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(h.userCreatedDocs()))
	}

	content := h.userCreatedDocs()[0].Markdown
	if !strings.Contains(content, "### Bad Heading") {
		t.Errorf("expected ### to remain unchanged (no autofix), got:\n%s", content)
	}
	if !strings.Contains(content, `myattr="value"`) {
		t.Errorf("expected myattr to remain unchanged (no autofix), got:\n%s", content)
	}
}

func TestSync_StatePersistedOnError(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "nb/good.md", "# Good")
	testutil.WriteFile(t, dir, "nb/bad_perm.md", "# Bad")
	testutil.GitCmd(t, dir, "add", "nb/good.md", "nb/bad_perm.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	badPath := filepath.Join(dir, "nb/bad_perm.md")
	if err := os.Chmod(badPath, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(badPath, 0o644)

	_, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) < 1 {
		t.Errorf("expected at least 1 created, got %d (errors=%v)", len(report.Created), report.Errors)
	}

	entries := engine.state.All()
	if _, ok := entries["nb/good.md"]; !ok {
		t.Error("expected state entry for good.md")
	}
}

func TestSync_NotebookExists_UsesExisting(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "existing_nb/file.md", "# Content")
	testutil.GitCmd(t, dir, "add", "existing_nb/file.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h := &mockSiYuanHandler{
		t:         t,
		notebooks: map[string]string{"existing_nb": "existing-nb-id"},
		docs:      make(map[string]mockDocRecord),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		var body map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}

		switch r.URL.Path {
		case "/api/notebook/lsNotebooks":
			nbs := []types.Notebook{{ID: "existing-nb-id", Name: "existing_nb"}}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"notebooks": nbs},
			})
		case "/api/notebook/createNotebook":
			enc.Encode(map[string]any{"code": 1, "msg": "should not be called"})
		case "/api/filetree/createDocWithMd":
			id := "created-doc-1"
			h.docs[id] = mockDocRecord{ID: id}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": id,
			})
			h.createdDocs = append(h.createdDocs, createdDocRecord{
				NotebookID: "existing-nb-id",
				HPath:      body["path"].(string),
				ID:         id,
			})
		default:
			enc.Encode(map[string]any{"code": 0, "msg": ""})
		}
	}))
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d (errors=%v)", len(report.Created), report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(h.userCreatedDocs()))
	}
	if h.userCreatedDocs()[0].NotebookID != "existing-nb-id" {
		t.Errorf("expected existing notebook ID 'existing-nb-id', got %q", h.userCreatedDocs()[0].NotebookID)
	}
	if len(h.createdNBs) > 0 {
		t.Errorf("expected no notebook created, but got %v", h.createdNBs)
	}
}

func TestBuildHPath_Nested(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"readme.md", "/readme.md"},
		{"wiki/doc.md", "/doc.md"},
		{"wiki/sub/deep.md", "/sub/deep.md"},
		{"notes/2024/01/day.md", "/2024/01/day.md"},
		{"top/leaf.md", "/leaf.md"},
	}
	for _, c := range cases {
		got := buildHPath(c.input)
		if got != c.want {
			t.Errorf("buildHPath(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestTopLevelFolder(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"readme.md", "root"},
		{"wiki/doc.md", "wiki"},
		{"wiki/sub/deep.md", "wiki"},
		{"notes/2024/01/day.md", "notes"},
		{"top/leaf.md", "top"},
	}
	for _, c := range cases {
		got := topLevelFolder(c.input)
		if got != c.want {
			t.Errorf("topLevelFolder(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestNewSyncEngine(t *testing.T) {
	dir := t.TempDir()
	client := siyuan.NewClient("http://localhost", "token")
	scanner, err := git.NewGitScanner(dir)
	if err == nil {
		t.Fatal("expected error for non-git dir")
	}
	_ = client
	_ = scanner
}

type downloadTestDoc struct {
	ID      string
	HPath   string
	Content string
}

type downloadTestNotebook struct {
	ID   string
	Name string
	Docs []downloadTestDoc
}

func newDownloadMockServer(t *testing.T, notebooks []downloadTestNotebook) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		var body map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}

		switch r.URL.Path {
		case "/api/notebook/lsNotebooks":
			nbs := make([]types.Notebook, 0, len(notebooks))
			for _, nb := range notebooks {
				nbs = append(nbs, types.Notebook{ID: nb.ID, Name: nb.Name})
			}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"notebooks": nbs},
			})

		case "/api/filetree/listDocsByPath":
			notebookID, _ := body["notebook"].(string)
			var tree []types.TreeNode
			for _, nb := range notebooks {
				if nb.ID != notebookID {
					continue
				}
				for _, doc := range nb.Docs {
					tree = append(tree, types.TreeNode{
						ID:   doc.ID,
						Name: doc.HPath,
						Path: "/" + doc.ID + ".sy",
					})
				}
			}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"files": tree},
			})

		case "/api/export/exportMdContent":
			id, _ := body["id"].(string)
			for _, nb := range notebooks {
				for _, doc := range nb.Docs {
					if doc.ID == id {
						enc.Encode(map[string]any{
							"code": 0, "msg": "",
							"data": types.ExportResult{
								ID:      doc.ID,
								Content: doc.Content,
								HPath:   doc.HPath,
							},
						})
						return
					}
				}
			}
			enc.Encode(map[string]any{"code": 1, "msg": "document not found"})

		default:
			enc.Encode(map[string]any{"code": 0, "msg": ""})
		}
	}))
}

func setupDownloadEngine(t *testing.T, server *httptest.Server, repoPath string) *SyncEngine {
	t.Helper()
	client := siyuan.NewClient(server.URL, "test-token")
	scanner, err := git.NewGitScanner(repoPath)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(repoPath)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(false)
	return NewSyncEngine(client, scanner, tracker, ce)
}

func TestDownload_HierarchyPreserved(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-journal", Name: "journal",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/2024/01/entry.md", Content: "# January Entry\n\nSome content."},
				{ID: "doc-2", HPath: "/2024/02/summary.md", Content: "# February Summary"},
			},
		},
		{
			ID: "nb-projects", Name: "projects",
			Docs: []downloadTestDoc{
				{ID: "doc-3", HPath: "/code/readme.md", Content: "# Code Readme"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 3 {
		t.Fatalf("expected 3 created, got %d: %v", len(report.Created), report.Created)
	}
	if len(report.Updated) != 0 {
		t.Errorf("expected 0 updated, got %d", len(report.Updated))
	}
	if len(report.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d: %v", len(report.Errors), report.Errors)
	}

	paths := map[string]bool{
		filepath.Join("journal", "2024", "01", "entry.md"):   false,
		filepath.Join("journal", "2024", "02", "summary.md"): false,
		filepath.Join("projects", "code", "readme.md"):       false,
	}

	for _, p := range report.Created {
		if _, ok := paths[p]; ok {
			paths[p] = true
		}
	}

	for p, found := range paths {
		if !found {
			t.Errorf("expected path %q in created", p)
			continue
		}
		fullPath := filepath.Join(dir, p)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("read %s: %v", p, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("empty file: %s", p)
		}
	}
}

func TestDownload_NotebookToFolderMapping(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-1", Name: "journal",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/note.md", Content: "# Journal Note"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d", len(report.Created))
	}

	expectedRel := filepath.Join("journal", "note.md")
	if report.Created[0] != expectedRel {
		t.Errorf("expected local path %q, got %q", expectedRel, report.Created[0])
	}

	fullPath := filepath.Join(dir, expectedRel)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "# Journal Note" {
		t.Errorf("expected '# Journal Note', got %q", string(data))
	}
}

func TestDownload_ConflictSkip(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	existingPath := filepath.Join(dir, "journal", "note.md")
	if err := os.MkdirAll(filepath.Dir(existingPath), 0755); err != nil {
		t.Fatal(err)
	}
	existingContent := "# Original Local Content"
	if err := os.WriteFile(existingPath, []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-1", Name: "journal",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/note.md", Content: "# New SiYuan Content"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "skip")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 0 {
		t.Errorf("expected 0 created, got %d: %v", len(report.Created), report.Created)
	}
	if len(report.Updated) != 0 {
		t.Errorf("expected 0 updated, got %d: %v", len(report.Updated), report.Updated)
	}

	data, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != existingContent {
		t.Errorf("file should be unchanged.\nwant: %q\ngot:  %q", existingContent, string(data))
	}
}

func TestDownload_ConflictOverwrite(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	existingPath := filepath.Join(dir, "journal", "note.md")
	if err := os.MkdirAll(filepath.Dir(existingPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingPath, []byte("# Old Content"), 0644); err != nil {
		t.Fatal(err)
	}

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-1", Name: "journal",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/note.md", Content: "# New SiYuan Content"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 0 {
		t.Errorf("expected 0 created, got %d", len(report.Created))
	}
	if len(report.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %d", len(report.Updated))
	}

	data, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "# New SiYuan Content" {
		t.Errorf("expected '# New SiYuan Content', got %q", string(data))
	}
}

func TestDownload_ConflictMerge(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	existingPath := filepath.Join(dir, "journal", "note.md")
	if err := os.MkdirAll(filepath.Dir(existingPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingPath, []byte("# Local Content"), 0644); err != nil {
		t.Fatal(err)
	}

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-1", Name: "journal",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/note.md", Content: "# SiYuan Content"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "merge")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 0 {
		t.Errorf("expected 0 created, got %d", len(report.Created))
	}
	if len(report.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %d", len(report.Updated))
	}

	data, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "<<<<<<< local") {
		t.Error("expected merge conflict marker '<<<<<<< local'")
	}
	if !strings.Contains(content, "=======") {
		t.Error("expected merge conflict marker '======='")
	}
	if !strings.Contains(content, ">>>>>>> siyuan") {
		t.Error("expected merge conflict marker '>>>>>>> siyuan'")
	}
	if !strings.Contains(content, "# Local Content") {
		t.Error("expected local content in merge output")
	}
	if !strings.Contains(content, "# SiYuan Content") {
		t.Error("expected SiYuan content in merge output")
	}
}

func TestDownload_StateTrackerUpdated(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-1", Name: "wiki",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/page.md", Content: "# Wiki Page"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	_, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	allState := engine.state.All()
	localPath := filepath.Join("wiki", "page.md")
	entry, ok := allState[localPath]
	if !ok {
		t.Fatalf("expected state entry for %q, got state: %v", localPath, allState)
	}
	if entry.SiYuanID != "doc-1" {
		t.Errorf("expected SiYuanID 'doc-1', got %q", entry.SiYuanID)
	}
	if entry.NotebookID != "nb-1" {
		t.Errorf("expected NotebookID 'nb-1', got %q", entry.NotebookID)
	}
	if entry.SyncedAt.IsZero() {
		t.Error("expected non-zero SyncedAt")
	}

	tracker, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}
	reloaded, ok := tracker.Get(localPath)
	if !ok {
		t.Fatalf("expected persisted entry for %q", localPath)
	}
	if reloaded.SiYuanID != "doc-1" {
		t.Errorf("persisted SiYuanID mismatch: %q", reloaded.SiYuanID)
	}
}

func TestDownload_EmptyNotebook(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-empty", Name: "empty_nb",
			Docs: nil,
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 0 {
		t.Errorf("expected 0 created, got %d", len(report.Created))
	}
	if len(report.Updated) != 0 {
		t.Errorf("expected 0 updated, got %d", len(report.Updated))
	}
	if len(report.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(report.Errors))
	}
}

func TestDownload_ExportErrorPerDocument(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		var body map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}

		switch r.URL.Path {
		case "/api/notebook/lsNotebooks":
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{
					"notebooks": []types.Notebook{{ID: "nb-1", Name: "wiki"}},
				},
			})
		case "/api/filetree/listDocsByPath":
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{
					"files": []types.TreeNode{
						{ID: "doc-good", Name: "good.md", Path: "/doc-good.sy"},
						{ID: "doc-bad", Name: "bad.md", Path: "/doc-bad.sy"},
						{ID: "doc-also-good", Name: "also_good.md", Path: "/doc-also-good.sy"},
					},
				},
			})
		case "/api/export/exportMdContent":
			id, _ := body["id"].(string)
			switch id {
			case "doc-good":
				enc.Encode(map[string]any{
					"code": 0, "msg": "",
					"data": types.ExportResult{ID: "doc-good", Content: "# Good", HPath: "/good.md"},
				})
			case "doc-bad":
				enc.Encode(map[string]any{"code": 500, "msg": "internal error"})
			case "doc-also-good":
				enc.Encode(map[string]any{
					"code": 0, "msg": "",
					"data": types.ExportResult{ID: "doc-also-good", Content: "# Also Good", HPath: "/also_good.md"},
				})
			default:
				enc.Encode(map[string]any{"code": 1, "msg": "unknown"})
			}
		default:
			enc.Encode(map[string]any{"code": 0, "msg": ""})
		}
	}))
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 2 {
		t.Errorf("expected 2 created, got %d (errors: %v)", len(report.Created), report.Errors)
	}
	if len(report.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(report.Errors))
	}
}

func TestDownload_ContentMatchesSiYuanExport(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "# Hello\n\nWorld\n\n```go\nfmt.Println(\"hi\")\n```\n"

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-1", Name: "notes",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/code.md", Content: content},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	_, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	localPath := filepath.Join(dir, "notes", "code.md")
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	if string(data) != content {
		t.Errorf("content mismatch.\nwant: %q\ngot:  %q", content, string(data))
	}
}

func TestDownload_TreeWalkError(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		switch r.URL.Path {
		case "/api/notebook/lsNotebooks":
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{
					"notebooks": []types.Notebook{
						{ID: "nb-good", Name: "good"},
						{ID: "nb-bad", Name: "bad"},
					},
				},
			})
		case "/api/filetree/listDocsByPath":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			nbID, _ := body["notebook"].(string)
			if nbID == "nb-good" {
				enc.Encode(map[string]any{
					"code": 0, "msg": "",
					"data": map[string]any{
						"files": []types.TreeNode{
							{ID: "doc-1", Name: "a.md", Path: "/doc-1.sy"},
						},
					},
				})
			} else {
				enc.Encode(map[string]any{"code": 1, "msg": "notebook not found"})
			}
		case "/api/export/exportMdContent":
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": types.ExportResult{ID: "doc-1", Content: "# A", HPath: "/a.md"},
			})
		default:
			enc.Encode(map[string]any{"code": 0, "msg": ""})
		}
	}))
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 1 {
		t.Errorf("expected 1 created from good notebook, got %d", len(report.Created))
	}
	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 error from bad notebook, got %d (errors: %v)", len(report.Errors), report.Errors)
	}
	if !strings.Contains(report.Errors[0].Message, "list doc tree") {
		t.Errorf("expected 'list doc tree' error, got %q", report.Errors[0].Message)
	}
	if report.Errors[0].File != "bad" {
		t.Errorf("expected error File 'bad', got %q", report.Errors[0].File)
	}
}

func TestCollectDocIDs(t *testing.T) {
	tree := []types.TreeNode{
		{ID: "1", Name: "doc1", Path: "/1.sy"},
		{
			ID: "2", Name: "folder",
			Children: []types.TreeNode{
				{ID: "3", Name: "doc2", Path: "/3.sy"},
				{ID: "4", Name: "doc3", Path: "/4.sy"},
			},
		},
		{
			ID: "5", Name: "nested",
			Children: []types.TreeNode{
				{
					ID: "6", Name: "sub",
					Children: []types.TreeNode{
						{ID: "7", Name: "deep", Path: "/7.sy"},
					},
				},
				{ID: "8", Name: "doc4", Path: "/8.sy"},
			},
		},
	}

	ids := collectDocIDs(tree)
	expected := map[string]bool{"1": false, "3": false, "4": false, "7": false, "8": false}

	if len(ids) != len(expected) {
		t.Fatalf("expected %d doc IDs, got %d: %v", len(expected), len(ids), ids)
	}

	for _, id := range ids {
		if _, ok := expected[id]; ok {
			expected[id] = true
		} else {
			t.Errorf("unexpected ID %q", id)
		}
	}

	for id, found := range expected {
		if !found {
			t.Errorf("expected ID %q not found", id)
		}
	}
}

func TestLocalPathFromSiYuan(t *testing.T) {
	cases := []struct {
		notebook string
		hpath    string
		want     string
	}{
		{"journal", "/2024/01/entry.md", "journal/2024/01/entry.md"},
		{"projects", "/code/readme.md", "projects/code/readme.md"},
		{"wiki", "/page.md", "wiki/page.md"},
		{"notes", "/sub/deep/file.md", "notes/sub/deep/file.md"},
		{"root", "/readme.md", "root/readme.md"},
		// SiYuan hpaths have no extension; download must land them as .md
		// so the git scanner tracks them and sync does not prune them.
		{"Start", "/Because Security Wiki 2.0", "Start/Because Security Wiki 2.0.md"},
		{"DevOps", "/2 Zero Trust", "DevOps/2 Zero Trust.md"},
	}
	for _, c := range cases {
		got := localPathFromSiYuan(c.notebook, c.hpath)
		if got != c.want {
			t.Errorf("localPathFromSiYuan(%q, %q) = %q, want %q", c.notebook, c.hpath, got, c.want)
		}
	}
}

func TestMergeContent(t *testing.T) {
	got := mergeContent("# Local", "# SiYuan")
	if !strings.Contains(got, "<<<<<<< local") {
		t.Error("missing '<<<<<<< local' marker")
	}
	if !strings.Contains(got, "=======") {
		t.Error("missing '=======' separator")
	}
	if !strings.Contains(got, ">>>>>>> siyuan") {
		t.Error("missing '>>>>>>> siyuan' marker")
	}
	if !strings.Contains(got, "# Local") {
		t.Error("missing local content")
	}
	if !strings.Contains(got, "# SiYuan") {
		t.Error("missing SiYuan content")
	}
}

func TestDownload_NestedDocTree(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		var body map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}

		switch r.URL.Path {
		case "/api/notebook/lsNotebooks":
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{
					"notebooks": []types.Notebook{{ID: "nb-1", Name: "wiki"}},
				},
			})
		case "/api/filetree/listDocsByPath":
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{
					"files": []types.TreeNode{
						{
							ID: "folder-1", Name: "sub",
							Children: []types.TreeNode{
								{ID: "doc-1", Name: "nested.md", Path: "/doc-1.sy"},
							},
						},
						{ID: "doc-2", Name: "root.md", Path: "/doc-2.sy"},
					},
				},
			})
		case "/api/export/exportMdContent":
			id, _ := body["id"].(string)
			switch id {
			case "doc-1":
				enc.Encode(map[string]any{
					"code": 0, "msg": "",
					"data": types.ExportResult{ID: "doc-1", Content: "# Nested", HPath: "/sub/nested.md"},
				})
			case "doc-2":
				enc.Encode(map[string]any{
					"code": 0, "msg": "",
					"data": types.ExportResult{ID: "doc-2", Content: "# Root", HPath: "/root.md"},
				})
			default:
				enc.Encode(map[string]any{"code": 1, "msg": "unknown"})
			}
		default:
			enc.Encode(map[string]any{"code": 0, "msg": ""})
		}
	}))
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 2 {
		t.Fatalf("expected 2 created, got %d: %v (errors: %v)", len(report.Created), report.Created, report.Errors)
	}

	for _, p := range report.Created {
		fullPath := filepath.Join(dir, p)
		if _, err := os.Stat(fullPath); err != nil {
			t.Errorf("file %s does not exist: %v", p, err)
		}
	}
}

func TestDownload_MultipleNotebooks(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-a", Name: "alpha",
			Docs: []downloadTestDoc{
				{ID: "a1", HPath: "/one.md", Content: "# Alpha One"},
				{ID: "a2", HPath: "/two.md", Content: "# Alpha Two"},
			},
		},
		{
			ID: "nb-b", Name: "beta",
			Docs: []downloadTestDoc{
				{ID: "b1", HPath: "/x.md", Content: "# Beta X"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Created) != 3 {
		t.Fatalf("expected 3 created, got %d: %v", len(report.Created), report.Created)
	}

	hasAlpha := false
	hasBeta := false
	for _, p := range report.Created {
		top := topLevelFolder(p)
		switch top {
		case "alpha":
			hasAlpha = true
		case "beta":
			hasBeta = true
		}
	}
	if !hasAlpha {
		t.Error("expected files in 'alpha' notebook")
	}
	if !hasBeta {
		t.Error("expected files in 'beta' notebook")
	}
}

func TestDownload_WriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("unwritable-directory path is not exercisable as root (containerized CI)")
	}
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	lockedDir := filepath.Join(dir, "wiki")
	if err := os.MkdirAll(lockedDir, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(lockedDir, 0755)

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-1", Name: "wiki",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/page.md", Content: "# Content"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	report, err := engine.Download(context.Background(), "overwrite")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if len(report.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d (created=%v, errors=%v)", len(report.Errors), report.Created, report.Errors)
	}
	msg := report.Errors[0].Message
	if !strings.Contains(msg, "create dir") && !strings.Contains(msg, "write:") {
		t.Errorf("expected file-system error, got %q", msg)
	}
}

func TestDownload_InvalidConflictMode(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	notebooks := []downloadTestNotebook{
		{
			ID: "nb-1", Name: "wiki",
			Docs: []downloadTestDoc{
				{ID: "doc-1", HPath: "/page.md", Content: "# Content"},
			},
		},
	}

	server := newDownloadMockServer(t, notebooks)
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	_, err := engine.Download(context.Background(), "banana")
	if err == nil {
		t.Fatal("expected error for invalid conflict mode")
	}
	if !strings.Contains(err.Error(), "invalid conflict mode") {
		t.Errorf("expected 'invalid conflict mode' in error, got %q", err.Error())
	}
}

func TestDownload_ListNotebooksError(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.Encode(map[string]any{
			"code": 500, "msg": "internal server error",
		})
	}))
	defer server.Close()

	engine := setupDownloadEngine(t, server, dir)

	_, err := engine.Download(context.Background(), "overwrite")
	if err == nil {
		t.Fatal("expected error listing notebooks")
	}
	if !strings.Contains(err.Error(), "list notebooks") {
		t.Errorf("expected 'list notebooks' in error, got %q", err.Error())
	}
}

func TestPrune_DeletedFileRemovesSiYuanDocument(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/keep.md", "# Keep")
	testutil.WriteFile(t, dir, "notes/delete.md", "# Delete")
	testutil.GitCmd(t, dir, "add", "notes/keep.md", "notes/delete.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(report.Created) != 2 {
		t.Fatalf("first sync: expected 2 created, got %d", len(report.Created))
	}

	os.Remove(filepath.Join(dir, "notes/delete.md"))
	testutil.GitCmd(t, dir, "rm", "notes/delete.md")
	testutil.GitCmd(t, dir, "commit", "-m", "delete")

	scanner, err := git.NewGitScanner(dir)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if len(report2.Pruned) != 1 {
		t.Fatalf("expected 1 pruned, got %d", len(report2.Pruned))
	}
	if report2.Pruned[0] != "notes/delete.md" {
		t.Errorf("expected 'notes/delete.md', got %q", report2.Pruned[0])
	}

	if len(h.removedDocIDs) != 1 {
		t.Fatalf("expected 1 RemoveDocByID call, got %d", len(h.removedDocIDs))
	}

	allState := engine2.state.All()
	if _, ok := allState["notes/delete.md"]; ok {
		t.Error("expected state entry for delete.md to be removed")
	}
	if _, ok := allState["notes/keep.md"]; !ok {
		t.Error("expected state entry for keep.md to persist")
	}
}

func TestPrune_StateEntryRemovedAfterDeletion(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "wiki/doc.md", "# Wiki Doc")
	testutil.GitCmd(t, dir, "add", "wiki/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	_, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	_, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}

	stateEntries := engine.state.All()
	if len(stateEntries) != 1 {
		t.Fatalf("expected 1 state entry after first sync, got %d", len(stateEntries))
	}

	os.Remove(filepath.Join(dir, "wiki/doc.md"))
	testutil.GitCmd(t, dir, "rm", "wiki/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "delete")

	scanner, _ := git.NewGitScanner(dir)
	tracker, _ := state.NewStateTracker(dir)
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if len(report2.Pruned) != 1 {
		t.Fatalf("expected 1 pruned, got %d", len(report2.Pruned))
	}

	engineState := engine2.state.All()
	if _, ok := engineState["wiki/doc.md"]; ok {
		t.Error("expected state entry for wiki/doc.md to be removed")
	}

	tracker2, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}
	if _, ok := tracker2.Get("wiki/doc.md"); ok {
		t.Error("expected persisted state entry to be removed")
	}
}

func TestPrune_MultipleDeletedFiles(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/a.md", "# A")
	testutil.WriteFile(t, dir, "notes/b.md", "# B")
	testutil.WriteFile(t, dir, "notes/c.md", "# C")
	testutil.GitCmd(t, dir, "add", "notes/a.md", "notes/b.md", "notes/c.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(report.Created) != 3 {
		t.Fatalf("first sync: expected 3 created, got %d", len(report.Created))
	}

	os.Remove(filepath.Join(dir, "notes/a.md"))
	os.Remove(filepath.Join(dir, "notes/c.md"))
	testutil.GitCmd(t, dir, "rm", "notes/a.md", "notes/c.md")
	testutil.GitCmd(t, dir, "commit", "-m", "delete a and c")

	scanner, _ := git.NewGitScanner(dir)
	tracker, _ := state.NewStateTracker(dir)
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if len(report2.Pruned) != 2 {
		t.Fatalf("expected 2 pruned, got %d: %v", len(report2.Pruned), report2.Pruned)
	}
	if len(h.removedDocIDs) != 2 {
		t.Fatalf("expected 2 RemoveDocByID calls, got %d", len(h.removedDocIDs))
	}

	allState := engine2.state.All()
	if _, ok := allState["notes/b.md"]; !ok {
		t.Error("expected state entry for b.md to persist")
	}
	if _, ok := allState["notes/a.md"]; ok {
		t.Error("expected state entry for a.md to be removed")
	}
	if _, ok := allState["notes/c.md"]; ok {
		t.Error("expected state entry for c.md to be removed")
	}
}

func TestPrune_APIErrorDoesNotAbortPruning(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/good.md", "# Good")
	testutil.WriteFile(t, dir, "notes/bad.md", "# Bad")
	testutil.GitCmd(t, dir, "add", "notes/good.md", "notes/bad.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	_, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(report.Created) != 2 {
		t.Fatalf("first sync: expected 2 created, got %d", len(report.Created))
	}

	engineState := engine.state.All()
	badEntry, ok := engineState["notes/bad.md"]
	if !ok {
		t.Fatal("expected state entry for bad.md")
	}

	os.Remove(filepath.Join(dir, "notes/good.md"))
	os.Remove(filepath.Join(dir, "notes/bad.md"))
	testutil.GitCmd(t, dir, "rm", "notes/good.md", "notes/bad.md")
	testutil.GitCmd(t, dir, "commit", "-m", "delete all")

	failCount := 0
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		var body map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}

		switch r.URL.Path {
		case "/api/filetree/removeDocByID":
			id, _ := body["id"].(string)
			if id == badEntry.SiYuanID {
				failCount++
				enc.Encode(map[string]any{"code": 500, "msg": "internal error"})
				return
			}
			enc.Encode(map[string]any{"code": 0, "msg": ""})
		case "/api/filetree/listDocsByPath":
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"files": []types.TreeNode{}},
			})
		default:
			enc.Encode(map[string]any{"code": 0, "msg": ""})
		}
	}))
	defer failServer.Close()

	scanner, _ := git.NewGitScanner(dir)
	tracker, _ := state.NewStateTracker(dir)
	ce := compliance.NewComplianceEngine(false)
	failClient := siyuan.NewClient(failServer.URL, "test-token")
	engine2 := NewSyncEngine(failClient, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if failCount != 1 {
		t.Errorf("expected 1 failed RemoveDocByID call, got %d", failCount)
	}

	if len(report2.Pruned) != 1 {
		t.Fatalf("expected 1 pruned (good.md), got %d: %v (errors=%v)",
			len(report2.Pruned), report2.Pruned, report2.Errors)
	}
	if len(report2.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(report2.Errors), report2.Errors)
	}
	if !strings.Contains(report2.Errors[0].Message, "remove document") {
		t.Errorf("expected 'remove document' in error, got %q", report2.Errors[0].Message)
	}
}

func TestPrune_DependencyConflict_SkipsAndReports(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/parent.md", "# Parent")
	testutil.GitCmd(t, dir, "add", "notes/parent.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(report.Created) != 1 {
		t.Fatalf("first sync: expected 1 created, got %d", len(report.Created))
	}

	engineState := engine.state.All()
	parentEntry, ok := engineState["notes/parent.md"]
	if !ok {
		t.Fatal("expected state entry for parent.md")
	}

	nbID := h.notebooks["notes"]
	h.docTrees = map[string][]types.TreeNode{
		nbID: {
			{
				ID: parentEntry.SiYuanID, Name: "parent.md", Path: "/" + parentEntry.SiYuanID + ".sy",
				Children: []types.TreeNode{
					{ID: "orphan-child", Name: "child.md", Path: "/orphan-child.sy"},
				},
			},
		},
	}

	os.Remove(filepath.Join(dir, "notes/parent.md"))
	testutil.GitCmd(t, dir, "rm", "notes/parent.md")
	testutil.GitCmd(t, dir, "commit", "-m", "delete parent")

	scanner, _ := git.NewGitScanner(dir)
	tracker, _ := state.NewStateTracker(dir)
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if len(report2.Pruned) != 0 {
		t.Errorf("expected 0 pruned (conflict), got %d: %v", len(report2.Pruned), report2.Pruned)
	}

	if len(report2.Errors) != 1 {
		t.Fatalf("expected 1 dependency conflict error, got %d: %v", len(report2.Errors), report2.Errors)
	}
	if !strings.Contains(report2.Errors[0].Message, "dependency conflict") {
		t.Errorf("expected 'dependency conflict' in error, got %q", report2.Errors[0].Message)
	}
	if !strings.Contains(report2.Errors[0].Message, "orphan-child") {
		t.Errorf("expected orphan child ID in error, got %q", report2.Errors[0].Message)
	}

	if len(h.removedDocIDs) != 0 {
		t.Errorf("expected 0 RemoveDocByID calls, got %d", len(h.removedDocIDs))
	}

	allState := engine2.state.All()
	if _, ok := allState["notes/parent.md"]; !ok {
		t.Error("expected state entry for parent.md to persist (dependency conflict, retry on next sync)")
	}
}

func TestPrune_IntegratedInSyncFlow(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "wiki/doc.md", "# Doc")
	testutil.GitCmd(t, dir, "add", "wiki/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d", len(report.Created))
	}
	if len(report.Pruned) != 0 {
		t.Errorf("expected 0 pruned on first sync, got %d", len(report.Pruned))
	}

	testutil.WriteFile(t, dir, "wiki/new.md", "# New")
	os.Remove(filepath.Join(dir, "wiki/doc.md"))
	testutil.GitCmd(t, dir, "add", "wiki/new.md")
	testutil.GitCmd(t, dir, "rm", "wiki/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "add new, remove doc")

	scanner, _ := git.NewGitScanner(dir)
	tracker, _ := state.NewStateTracker(dir)
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if len(report2.Created) != 1 {
		t.Errorf("expected 1 created (new.md), got %d", len(report2.Created))
	}
	if len(report2.Pruned) != 1 {
		t.Fatalf("expected 1 pruned (doc.md), got %d", len(report2.Pruned))
	}
	if report2.Pruned[0] != "wiki/doc.md" {
		t.Errorf("expected 'wiki/doc.md' pruned, got %q", report2.Pruned[0])
	}
	if len(h.removedDocIDs) != 1 {
		t.Errorf("expected 1 RemoveDocByID call, got %d", len(h.removedDocIDs))
	}

	allState := engine2.state.All()
	if len(allState) != 1 {
		t.Errorf("expected 1 state entry (new.md), got %d: %v", len(allState), allState)
	}
	if _, ok := allState["wiki/new.md"]; !ok {
		t.Error("expected state entry for wiki/new.md")
	}
	if _, ok := allState["wiki/doc.md"]; ok {
		t.Error("expected state entry for wiki/doc.md to be removed")
	}
}

func TestPrune_NoDeletedFiles_EmptyReport(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/a.md", "# A")
	testutil.GitCmd(t, dir, "add", "notes/a.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(report.Created) != 1 {
		t.Fatalf("expected 1 created, got %d", len(report.Created))
	}

	scanner, _ := git.NewGitScanner(dir)
	tracker, _ := state.NewStateTracker(dir)
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if len(report2.Pruned) != 0 {
		t.Errorf("expected 0 pruned, got %d", len(report2.Pruned))
	}
	if len(h.removedDocIDs) != 0 {
		t.Errorf("expected 0 RemoveDocByID calls, got %d", len(h.removedDocIDs))
	}
}

// --- Task 7.5: Frontmatter-aware upload (Requirement 13) ---

// 13.1, 13.2, 13.4: a note WITH frontmatter (title + tags) syncs so that the
// created SiYuan body has no YAML frontmatter, renameDocByID is called with
// the frontmatter title, and setBlockAttrs is called with the custom- map.
func TestSync_FrontmatterStrippedTitleAndTagsApplied(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ntitle: My Real Title\ntags: [alpha, beta]\n---\n# Heading\n\nBody text\n"
	testutil.WriteFile(t, dir, "notebook/sub/file.md", content)
	testutil.GitCmd(t, dir, "add", "notebook/sub/file.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != "notebook/sub/file.md" {
		t.Fatalf("expected 1 created notebook/sub/file.md, got %v", report.Created)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("expected 0 errors, got %v", report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 created doc, got %d", len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]

	// 13.1: frontmatter block must NOT be in the uploaded body.
	if strings.Contains(doc.Markdown, "---") {
		t.Errorf("13.1: uploaded body still contains '---' frontmatter delimiter: %q", doc.Markdown)
	}
	if strings.Contains(doc.Markdown, "title: My Real Title") {
		t.Errorf("13.1: uploaded body still contains frontmatter title key: %q", doc.Markdown)
	}
	if !strings.Contains(doc.Markdown, "# Heading") || !strings.Contains(doc.Markdown, "Body text") {
		t.Errorf("13.1: uploaded body missing actual content: %q", doc.Markdown)
	}

	// 13.2: doc title set from frontmatter title.
	if got := h.renamedTitles[doc.ID]; got != "My Real Title" {
		t.Errorf("13.2: expected renameDocByID title %q, got %q (all=%v)", "My Real Title", got, h.renamedTitles)
	}

	// 13.4: tags applied as custom- block attributes.
	attrs := h.setAttrs[doc.ID]
	if attrs == nil {
		t.Fatalf("13.4: expected setBlockAttrs called for doc %s, got %v", doc.ID, h.setAttrs)
	}
	if _, ok := attrs["custom-alpha"]; !ok {
		t.Errorf("13.4: expected custom-alpha attr, got %v", attrs)
	}
	if _, ok := attrs["custom-beta"]; !ok {
		t.Errorf("13.4: expected custom-beta attr, got %v", attrs)
	}
}

// 13.3 (hpath-preservation regression guard): a note with NO frontmatter title
// must NOT trigger renameDocByID. renameDocByID mutates the SiYuan document's
// hpath; issuing a redundant filename->filename rename on the common
// no-frontmatter path changes /name.md and breaks hpath-based resolution
// (this is the regression that turned e2e/TestFullSyncE2E red). The document
// keeps the name SiYuan derives from the create path, which satisfies 13.3
// without an extra call. This unit test is the guard that would have caught
// the e2e regression.
func TestSync_NoFrontmatterTitle_DoesNotRename(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// No frontmatter at all — the common case exercised by the e2e suite.
	content := "# Content\n\nplain note, no frontmatter\n"
	testutil.WriteFile(t, dir, "notebook/sub/My Doc.md", content)
	testutil.GitCmd(t, dir, "add", "notebook/sub/My Doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if len(report.Created) != 1 || report.Created[0] != "notebook/sub/My Doc.md" {
		t.Fatalf("expected 1 created notebook/sub/My Doc.md, got %v (errors=%v)", report.Created, report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 created doc, got %d", len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]

	// The hpath-preservation invariant: NO renameDocByID call for this doc,
	// and in fact no rename recorded at all.
	if got, ok := h.renamedTitles[doc.ID]; ok {
		t.Errorf("13.3: expected NO renameDocByID for a no-frontmatter file (would mutate hpath), got %q", got)
	}
	if len(h.renamedTitles) != 0 {
		t.Errorf("13.3: expected no rename calls recorded at all, got %v", h.renamedTitles)
	}
}

// 13.5: malformed frontmatter -> file still created, full body uploaded,
// a report error recorded, no rename/attrs applied.
func TestSync_MalformedFrontmatter_DegradesGracefully(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// Unterminated/invalid YAML inside a frontmatter block.
	content := "---\ntitle: [unclosed\n  : : bad\n---\n# Body\n\nstuff\n"
	testutil.WriteFile(t, dir, "notebook/bad.md", content)
	testutil.GitCmd(t, dir, "add", "notebook/bad.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// File still created (degraded, not aborted).
	if len(report.Created) != 1 || report.Created[0] != "notebook/bad.md" {
		t.Fatalf("13.5: expected file still created, got %v", report.Created)
	}
	// A compliance/parse error recorded.
	if len(report.Errors) == 0 {
		t.Errorf("13.5: expected a report error for the frontmatter parse failure, got none")
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 created doc, got %d", len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]
	// Full body uploaded (frontmatter NOT stripped because parse failed).
	if doc.Markdown != content {
		t.Errorf("13.5: expected full original content uploaded, got %q", doc.Markdown)
	}
	// No title/attr mapping applied.
	if _, ok := h.renamedTitles[doc.ID]; ok {
		t.Errorf("13.5: expected no renameDocByID on parse failure, got %v", h.renamedTitles)
	}
	if _, ok := h.setAttrs[doc.ID]; ok {
		t.Errorf("13.5: expected no setBlockAttrs on parse failure, got %v", h.setAttrs)
	}
}

// Step 7 (non-fatal title API failure): renameDocByID returns an error ->
// the file is STILL recorded as created and the error is recorded.
func TestSync_RenameDocByIDError_NonFatal(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ntitle: T\n---\n# Body\n"
	testutil.WriteFile(t, dir, "notebook/x.md", content)
	testutil.GitCmd(t, dir, "add", "notebook/x.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()
	h.renameErr = true

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != "notebook/x.md" {
		t.Errorf("step 7: expected file still created despite rename error, got %v", report.Created)
	}
	if len(report.Errors) == 0 {
		t.Errorf("step 7: expected a per-file error recorded for the rename failure, got none")
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 created doc, got %d", len(h.userCreatedDocs()))
	}
}

// Step 7 / Error Handling row "Title/attr API": SetBlockAttrs returns an error
// -> the file is STILL recorded as created and a per-file error is recorded.
// This mirrors TestSync_RenameDocByIDError_NonFatal for the *attrs* half of the
// non-fatal title/attr API error policy, which was previously unguarded (the
// mock's setAttrsErr hook existed but was never exercised). It is a meaningful
// guard: if processFile ever treated a SetBlockAttrs failure as fatal (dropping
// the file from report.Created or returning early before recording the error),
// the report.Created length/identity assertion or the report.Errors-non-empty
// assertion below would fail. The frontmatter has a title so RenameDocByID
// succeeds first, isolating the SetBlockAttrs error path.
func TestSync_SetBlockAttrsError_NonFatal(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ntitle: T\ntags: [alpha, beta]\n---\n# Body\n"
	testutil.WriteFile(t, dir, "notebook/y.md", content)
	testutil.GitCmd(t, dir, "add", "notebook/y.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()
	h.setAttrsErr = true

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != "notebook/y.md" {
		t.Errorf("step 7: expected file still created despite setBlockAttrs error, got %v (errors=%v)", report.Created, report.Errors)
	}
	if len(report.Errors) == 0 {
		t.Errorf("step 7: expected a per-file error recorded for the setBlockAttrs failure, got none")
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 created doc, got %d", len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]
	// Title still applied (RenameDocByID precedes the failing SetBlockAttrs).
	if got := h.renamedTitles[doc.ID]; got != "T" {
		t.Errorf("step 7: expected title still applied before setBlockAttrs failure, got %q (all=%v)", got, h.renamedTitles)
	}
	// The failing call must not have persisted attrs in the mock.
	if _, ok := h.setAttrs[doc.ID]; ok {
		t.Errorf("step 7: setBlockAttrs failed so no attrs should be recorded, got %v", h.setAttrs)
	}
}

// 13.4 value-level (sync layer): for a doc with BOTH frontmatter tags and an
// inline tag, the exact custom- attr map sent to SetBlockAttrs must equal the
// union of frontmatter + inline tags — no missing keys, no extra keys, and the
// "custom-" prefix on every key. TestSync_FrontmatterStrippedTitleAndTagsApplied
// only asserts two keys are *present*; this asserts the *whole* map by value, so
// it bites if processFile ever sent a partial set, dropped the inline tag, lost
// the custom- prefix, or leaked the frontmatter "title" key into the attrs.
func TestSync_TagAttrs_ExactSetFromFrontmatterAndInline(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ntitle: Doc T\ntags: [alpha, beta]\n---\n# Heading\n\nBody with an #gamma inline tag.\n"
	testutil.WriteFile(t, dir, "notebook/tagged.md", content)
	testutil.GitCmd(t, dir, "add", "notebook/tagged.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if len(report.Created) != 1 || report.Created[0] != "notebook/tagged.md" {
		t.Fatalf("expected 1 created notebook/tagged.md, got %v (errors=%v)", report.Created, report.Errors)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("expected 0 errors, got %v", report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 created doc, got %d", len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]

	got := h.setAttrs[doc.ID]
	if got == nil {
		t.Fatalf("13.4: expected setBlockAttrs called for doc %s, got %v", doc.ID, h.setAttrs)
	}
	// Tag-marker attrs carry the "1" sentinel since SiYuan silently drops
	// empty-value attrs from storage; the marker preserves attr presence
	// for semantic-search-by-tag queries. The normalization happens inside
	// client.SetBlockAttrs, so the mock handler sees the post-substitution
	// values.
	want := map[string]string{
		"custom-alpha": "1",
		"custom-beta":  "1",
		"custom-gamma": "1",
		// SiYuan's visible-chip variant: the comma-separated list goes out
		// alongside the custom-<tag> markers so the UI renders the chips.
		"tags": "alpha,beta,gamma",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("13.4: attrs sent to setBlockAttrs mismatch.\n got:  %v\n want: %v", got, want)
	}
	// Defensive: the frontmatter "title" key must never appear as a tag attr.
	if _, leaked := got["custom-title"]; leaked {
		t.Errorf("13.4: frontmatter title key leaked into tag attrs: %v", got)
	}
	if _, leaked := got["title"]; leaked {
		t.Errorf("13.4: raw frontmatter 'title' key leaked into tag attrs: %v", got)
	}
}

// 13.2 on the update path: a modified file with frontmatter also sets the title.
func TestSync_UpdatePath_SetsTitleFromFrontmatter(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "notes/doc.md", "---\ntitle: First Title\n---\n# Original\n")
	testutil.GitCmd(t, dir, "add", "notes/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)
	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	if len(report.Created) != 1 {
		t.Fatalf("first sync: expected 1 created, got %v", report.Created)
	}

	time.Sleep(100 * time.Millisecond)
	testutil.WriteFile(t, dir, "notes/doc.md", "---\ntitle: Second Title\n---\n# Modified\n")
	testutil.GitCmd(t, dir, "add", "notes/doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "modified")

	scanner, _ := git.NewGitScanner(dir)
	tracker, _ := state.NewStateTracker(dir)
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine2 := NewSyncEngine(client, scanner, tracker, ce)

	report2, err := engine2.Sync(context.Background())
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if len(report2.Updated) != 1 {
		t.Fatalf("second sync: expected 1 updated, got %v (errors=%v)", report2.Updated, report2.Errors)
	}

	// The update path uses the existing SiYuan ID; title must be set on it.
	var docID string
	for id := range h.docs {
		docID = id
	}
	if docID == "" && len(h.userCreatedDocs()) > 0 {
		docID = h.userCreatedDocs()[0].ID
	}
	if got := h.renamedTitles[docID]; got != "Second Title" {
		t.Errorf("13.2 (update path): expected title %q, got %q (all=%v)", "Second Title", got, h.renamedTitles)
	}

	// Updated body must be frontmatter-stripped (13.1 also applies on update).
	rec := h.docs[docID]
	if strings.Contains(rec.Markdown, "---") || strings.Contains(rec.Markdown, "title:") {
		t.Errorf("13.1 (update path): updated body still has frontmatter: %q", rec.Markdown)
	}
}

// --- Task 3.1: Sync engine schema gate (ontology-gate) ---

// schemaViolationJSON mirrors the on-the-wire shape of
// ontology.SchemaViolation so engine_test.go can parse Errors[i].Message
// without coupling to that package's exported type. The fields and JSON
// tags must stay in lockstep with ontology.SchemaViolation.
type schemaViolationJSON struct {
	File           string   `json:"file"`
	Key            string   `json:"key"`
	OffendingValue string   `json:"offending_value"`
	Allowed        []string `json:"allowed"`
}

// Req 2.6 + Req 3.5 + design "sync/engine (extended) Step 1 (Schema gate)":
// in a batch where one file opts into the ontology and declares an
// out-of-enum intent, that file must be aborted with a structured
// SchemaViolation in report.Errors and absent from report.Created, while
// the conforming sibling file is created normally. Critically, the mock
// SiYuan handler must NOT see a createDocWithMd call for the aborted file
// (the gate fires before any SiYuan API call).
func TestSync_SchemaGate_AbortsViolatingFile_BatchContinues(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// a.md opts into the ontology with an out-of-enum intent ("braindump").
	testutil.WriteFile(t, dir, "wiki/a.md", "---\ndomain: devops\nintent: braindump\n---\n# Bad\n")
	// b.md opts in with a fully valid (domain, intent) pair.
	testutil.WriteFile(t, dir, "wiki/b.md", "---\ndomain: devops\nintent: sop\n---\n# Good\n")
	testutil.GitCmd(t, dir, "add", "wiki/a.md", "wiki/b.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// b.md is created normally. After task 3.2 (pre-sync ontology routing)
	// landed, a valid (devops, sop) file at the non-canonical path wiki/b.md
	// is `git mv`'d to Sysadmin & DevOps/b.md before upload, so Created
	// reports the canonical post-route path. The original 3.1 intent —
	// "the valid sibling syncs while the violating sibling is aborted" —
	// is preserved.
	canonicalB := "Sysadmin & DevOps/b.md"
	if len(report.Created) != 1 || report.Created[0] != canonicalB {
		t.Fatalf("Req 2.6 batch-continues: expected created=[%q], got %v (errors=%v)",
			canonicalB, report.Created, report.Errors)
	}
	// a.md is NEVER in report.Created or report.Updated.
	for _, p := range report.Created {
		if p == "wiki/a.md" {
			t.Errorf("Req 3.5: schema-violating file wiki/a.md must not appear in report.Created, got %v", report.Created)
		}
	}
	for _, p := range report.Updated {
		if p == "wiki/a.md" {
			t.Errorf("Req 3.5: schema-violating file wiki/a.md must not appear in report.Updated, got %v", report.Updated)
		}
	}

	// Exactly one Errors entry for wiki/a.md, JSON-decodable into a
	// SchemaViolation that names the offending intent value.
	gateErrs := make([]types.SyncError, 0)
	for _, e := range report.Errors {
		if e.File == "wiki/a.md" {
			gateErrs = append(gateErrs, e)
		}
	}
	if len(gateErrs) != 1 {
		t.Fatalf("design Step 1: expected exactly 1 gate Errors entry for wiki/a.md, got %d (all errors=%v)",
			len(gateErrs), report.Errors)
	}
	var sv schemaViolationJSON
	if err := json.Unmarshal([]byte(gateErrs[0].Message), &sv); err != nil {
		t.Fatalf("design Step 1: expected JSON-encoded SchemaViolation in Errors[].Message, got %q (err=%v)",
			gateErrs[0].Message, err)
	}
	if sv.Key != "intent" {
		t.Errorf("expected SchemaViolation.Key='intent', got %q (payload=%+v)", sv.Key, sv)
	}
	if sv.OffendingValue != "braindump" {
		t.Errorf("expected SchemaViolation.OffendingValue='braindump', got %q (payload=%+v)", sv.OffendingValue, sv)
	}
	if len(sv.Allowed) == 0 {
		t.Errorf("expected SchemaViolation.Allowed to enumerate the closed intent enum, got empty (payload=%+v)", sv)
	}

	// The gate fires BEFORE any upload: the mock saw exactly one createDocWithMd
	// call, and it was for b.md (under its canonical routed hpath), not wiki/a.md.
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("Req 3.5: gate must abort before SiYuan API; expected exactly 1 createDocWithMd (for b.md), got %d: %+v",
			len(h.userCreatedDocs()), h.createdDocs)
	}
	if h.userCreatedDocs()[0].HPath != "/b.md" {
		t.Errorf("Req 3.5 + 3.2: expected the one createDocWithMd to be for /b.md (post-route), got hpath %q",
			h.userCreatedDocs()[0].HPath)
	}
}

// design Step 1 (cardinality): a file with TWO schema violations (bad
// domain AND bad intent) must produce TWO Errors entries, each JSON-
// decodable into a SchemaViolation. This guards against a "collapse all
// violations into one entry" regression.
func TestSync_SchemaGate_MultipleViolationsProduceMultipleErrors(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "wiki/c.md", "---\ndomain: bogus\nintent: braindump\n---\n# Bad\n")
	testutil.GitCmd(t, dir, "add", "wiki/c.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 0 {
		t.Errorf("Req 3.5: schema-violating file must not be created, got %v", report.Created)
	}
	if len(h.userCreatedDocs()) != 0 {
		t.Errorf("Req 3.5: gate must abort before any SiYuan API call, got %d createDocWithMd calls",
			len(h.userCreatedDocs()))
	}

	gateErrs := make([]types.SyncError, 0)
	for _, e := range report.Errors {
		if e.File == "wiki/c.md" {
			gateErrs = append(gateErrs, e)
		}
	}
	if len(gateErrs) != 2 {
		t.Fatalf("design Step 1 (cardinality): expected 2 SchemaViolation errors for wiki/c.md, got %d (all=%v)",
			len(gateErrs), report.Errors)
	}

	seenKeys := make(map[string]string)
	for i, e := range gateErrs {
		var sv schemaViolationJSON
		if err := json.Unmarshal([]byte(e.Message), &sv); err != nil {
			t.Errorf("gate error #%d: expected JSON-encoded SchemaViolation, got %q (err=%v)",
				i, e.Message, err)
			continue
		}
		seenKeys[sv.Key] = sv.OffendingValue
	}
	if seenKeys["domain"] != "bogus" {
		t.Errorf("expected a domain violation with OffendingValue='bogus', got map=%v", seenKeys)
	}
	if seenKeys["intent"] != "braindump" {
		t.Errorf("expected an intent violation with OffendingValue='braindump', got map=%v", seenKeys)
	}
}

// Opt-in semantics: a legacy file that declares NEITHER `domain:` nor
// `intent:` must bypass the gate entirely. This is the design's
// declaration-driven gate: schema issues are still emitted by the
// compliance audit for the `audit` subcommand, but the sync engine does
// NOT abort the file. This preserves byte-equal behavior for every
// existing 13.x frontmatter test (which uses frontmatter without ontology
// keys) and for every plain-markdown sync test.
func TestSync_SchemaGate_NonOptInFile_BypassesGate(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// Frontmatter with neither `domain:` nor `intent:` → not opted in.
	testutil.WriteFile(t, dir, "wiki/legacy.md", "---\ntitle: Foo\n---\n# Body\n")
	testutil.GitCmd(t, dir, "add", "wiki/legacy.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != "wiki/legacy.md" {
		t.Fatalf("opt-in gate: a file with no ontology keys must sync normally, got created=%v errors=%v",
			report.Created, report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("opt-in gate: expected exactly 1 createDocWithMd, got %d", len(h.userCreatedDocs()))
	}
	// No JSON-encoded SchemaViolation should land in report.Errors for this file.
	for _, e := range report.Errors {
		if e.File != "wiki/legacy.md" {
			continue
		}
		var sv schemaViolationJSON
		if err := json.Unmarshal([]byte(e.Message), &sv); err == nil && sv.Key != "" {
			t.Errorf("opt-in gate: non-opt-in file should not emit SchemaViolation, got %+v", sv)
		}
	}
}

// --- Task 3.2: Sync engine pre-sync routing (ontology-gate) ---

// gitLogGrep runs `git log --grep=<pattern>` in dir and returns the matching
// subject lines (newline-separated, trailing newline trimmed). Used by the
// 3.2 routing tests to count `ontology-route:` rename commits.

// Req 3.2 + 3.3 + design "sync/engine (extended) Step 2 (Route)":
// a file at a non-canonical path that declares a valid domain must be
// `git mv`'d to its canonical folder, the rename committed with the exact
// `ontology-route:` subject line, the state tracker updated to the new
// path with the SiYuanID the create call returned, and the upload routed
// to the new hpath.
func TestSync_OntologyRouting_MovesAndCommits(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// Place the file at a non-canonical path under the `wiki` notebook.
	// Frontmatter declares domain=devops and intent=sop (both valid), so
	// the gate passes and the router emits RouteMove to
	// `Sysadmin & DevOps/foo.md`.
	content := "---\ndomain: devops\nintent: sop\n---\n# Foo\n\nBody.\n"
	testutil.WriteFile(t, dir, "wiki/misc/foo.md", content)
	testutil.GitCmd(t, dir, "add", "wiki/misc/foo.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// The file is reported as Created under its NEW canonical path.
	canonical := "Sysadmin & DevOps/foo.md"
	if len(report.Created) != 1 || report.Created[0] != canonical {
		t.Fatalf("Req 3.2: expected Created=[%q], got %v (errors=%v)", canonical, report.Created, report.Errors)
	}

	// Filesystem: file moved to the canonical folder; old path gone.
	if _, err := os.Stat(filepath.Join(dir, canonical)); err != nil {
		t.Errorf("Req 3.2: expected file at %q, stat err=%v", canonical, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "wiki/misc/foo.md")); !os.IsNotExist(err) {
		t.Errorf("Req 3.2: expected old path wiki/misc/foo.md to be gone, stat err=%v", err)
	}

	// Exactly one `ontology-route:` commit exists in the repo (Req 3.3).
	commits := testutil.GitLogGrep(t, dir, "ontology-route:")
	if len(commits) != 1 {
		t.Fatalf("Req 3.3: expected exactly 1 ontology-route commit, got %d: %v", len(commits), commits)
	}
	wantSubject := "ontology-route: wiki/misc/foo.md -> " + canonical
	if commits[0] != wantSubject {
		t.Errorf("Req 3.3: commit subject mismatch.\n got:  %q\n want: %q", commits[0], wantSubject)
	}

	// State: new key present with the SiYuanID the create returned;
	// old key absent.
	allState := engine.state.All()
	entry, ok := allState[canonical]
	if !ok {
		t.Fatalf("Req 3.2: expected state entry at %q, got %v", canonical, allState)
	}
	if entry.SiYuanID == "" {
		t.Errorf("Req 3.2: expected non-empty SiYuanID at new path, got %+v", entry)
	}
	if _, gone := allState["wiki/misc/foo.md"]; gone {
		t.Errorf("Req 3.2: expected old state key to be gone, got entries=%v", allState)
	}

	// SiYuan side: exactly one createDocWithMd, addressed at the new hpath.
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("Req 3.2: expected 1 createDocWithMd, got %d: %+v", len(h.userCreatedDocs()), h.createdDocs)
	}
	if got := h.userCreatedDocs()[0].HPath; got != "/foo.md" {
		t.Errorf("Req 3.2: expected create hpath /foo.md, got %q", got)
	}
}

// Req 3.6 + design Step 2: a file already at its canonical folder must
// NOT trigger `git mv` or produce an `ontology-route:` commit. The file
// is created normally with the existing path preserved.
func TestSync_OntologyRouting_NoopWhenAlreadyCanonical(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	canonical := "Sysadmin & DevOps/foo.md"
	content := "---\ndomain: devops\nintent: sop\n---\n# Foo\n"
	testutil.WriteFile(t, dir, canonical, content)
	testutil.GitCmd(t, dir, "add", canonical)
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != canonical {
		t.Fatalf("Req 3.6: expected Created=[%q], got %v (errors=%v)", canonical, report.Created, report.Errors)
	}

	// No ontology-route commits whatsoever.
	commits := testutil.GitLogGrep(t, dir, "ontology-route:")
	if len(commits) != 0 {
		t.Errorf("Req 3.6: expected 0 ontology-route commits, got %d: %v", len(commits), commits)
	}

	// File is still at the original (canonical) path.
	if _, err := os.Stat(filepath.Join(dir, canonical)); err != nil {
		t.Errorf("Req 3.6: expected file still at %q, stat err=%v", canonical, err)
	}

	// SiYuan got exactly one create, addressed at the canonical hpath.
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("Req 3.6: expected 1 createDocWithMd, got %d", len(h.userCreatedDocs()))
	}
}

// Req 3.4 + Req 9.x + design Step 2: an asset reference that the move
// would invalidate is surfaced as a warning entry in report.Errors but
// does NOT block the file. The file still moves and still appears in
// report.Created.
func TestSync_OntologyRouting_AssetWarning(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ndomain: forensics\nintent: sop\n---\n# Case\n\n![diagram](assets/case-1.png)\n"
	testutil.WriteFile(t, dir, "wiki/misc/case.md", content)
	testutil.GitCmd(t, dir, "add", "wiki/misc/case.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	_, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	canonical := "Digital Forensics/case.md"

	// File moved, file Created.
	if len(report.Created) != 1 || report.Created[0] != canonical {
		t.Fatalf("Req 9.4: warning must not block; expected Created=[%q], got %v (errors=%v)",
			canonical, report.Created, report.Errors)
	}
	if _, err := os.Stat(filepath.Join(dir, canonical)); err != nil {
		t.Errorf("Req 3.2: expected file at %q, stat err=%v", canonical, err)
	}

	// Exactly one asset warning entry for this file, message contains
	// the asset-reference marker.
	var assetWarnings []types.SyncError
	for _, e := range report.Errors {
		if e.File != "wiki/misc/case.md" && e.File != canonical {
			continue
		}
		if strings.Contains(e.Message, "asset reference") {
			assetWarnings = append(assetWarnings, e)
		}
	}
	if len(assetWarnings) != 1 {
		t.Fatalf("Req 3.4: expected exactly 1 asset-reference warning entry, got %d (all errors=%v)",
			len(assetWarnings), report.Errors)
	}
	if !strings.Contains(assetWarnings[0].Message, "assets/case-1.png") {
		t.Errorf("Req 3.4: expected warning to mention the reference path, got %q", assetWarnings[0].Message)
	}
}

// Legacy bypass: a file with frontmatter that has no `domain:` / `intent:`
// keys must skip routing entirely. No git mv, no `ontology-route:` commit,
// the file syncs at its original path. Guards against Req 13.x byte-equal
// behavior for non-opt-in frontmatter.
func TestSync_OntologyRouting_LegacyFileNoRoute(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ntitle: Old Note\n---\n# Body\n"
	testutil.WriteFile(t, dir, "wiki/misc/legacy.md", content)
	testutil.GitCmd(t, dir, "add", "wiki/misc/legacy.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != "wiki/misc/legacy.md" {
		t.Fatalf("legacy bypass: expected Created=[wiki/misc/legacy.md], got %v (errors=%v)",
			report.Created, report.Errors)
	}
	commits := testutil.GitLogGrep(t, dir, "ontology-route:")
	if len(commits) != 0 {
		t.Errorf("legacy bypass: expected 0 ontology-route commits, got %d: %v", len(commits), commits)
	}
	if _, err := os.Stat(filepath.Join(dir, "wiki/misc/legacy.md")); err != nil {
		t.Errorf("legacy bypass: expected file unchanged at original path, stat err=%v", err)
	}
	if len(h.userCreatedDocs()) != 1 || h.userCreatedDocs()[0].HPath != "/misc/legacy.md" {
		t.Errorf("legacy bypass: expected one create at /misc/legacy.md, got %+v", h.createdDocs)
	}
}

// state.ErrCollision: the target path already tracks a DIFFERENT SiYuanID.
// The router would route to the target, but state.Move must fail; the
// engine records the error and SKIPS the upload — no partial move, no
// create on the SiYuan side.
func TestSync_OntologyRouting_StateCollision(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ndomain: devops\nintent: sop\n---\n# Foo\n"
	testutil.WriteFile(t, dir, "wiki/misc/foo.md", content)
	testutil.GitCmd(t, dir, "add", "wiki/misc/foo.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	// Pre-seed state with entries at BOTH the source and the canonical
	// target, pointing at DIFFERENT SiYuanIDs. The source entry's mtime
	// is in the past so the engine treats the working-tree file (whose
	// mtime is newer than the seeded SyncedAt) as modified.
	tr, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	canonical := "Sysadmin & DevOps/foo.md"
	tr.Put(types.SyncEntry{
		LocalPath:  "wiki/misc/foo.md",
		SiYuanID:   "src-doc-id",
		NotebookID: "nb-wiki",
		SyncedAt:   time.Now().Add(-1 * time.Hour),
	})
	tr.Put(types.SyncEntry{
		LocalPath:  canonical,
		SiYuanID:   "different-target-doc-id",
		NotebookID: "nb-wiki",
		SyncedAt:   time.Now().Add(-1 * time.Hour),
	})
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	scanner, err := git.NewGitScanner(dir)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine := NewSyncEngine(client, scanner, tracker, ce)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// The colliding file must NOT be in Created or Updated.
	for _, p := range report.Created {
		if p == "wiki/misc/foo.md" || p == canonical {
			t.Errorf("collision: file must not be Created, got %v", report.Created)
		}
	}
	for _, p := range report.Updated {
		if p == "wiki/misc/foo.md" || p == canonical {
			t.Errorf("collision: file must not be Updated, got %v", report.Updated)
		}
	}

	// A "state collision" error is recorded for the file.
	found := false
	for _, e := range report.Errors {
		if e.File == "wiki/misc/foo.md" && strings.Contains(e.Message, "state collision") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("collision: expected a state-collision error for wiki/misc/foo.md, got errors=%v", report.Errors)
	}

	// No createDocWithMd / updateBlock for this file: the collision is detected
	// BEFORE the upload (skip semantics in 3.2).
	for _, d := range h.createdDocs {
		if d.HPath == "/misc/foo.md" || d.HPath == "/foo.md" {
			t.Errorf("collision: must not call createDocWithMd, got %+v", d)
		}
	}
	// No partial move: the file must still be at its original path on disk.
	// The implementation must probe the state collision BEFORE running
	// git mv so a half-applied move never lands.
	if _, err := os.Stat(filepath.Join(dir, "wiki/misc/foo.md")); err != nil {
		t.Errorf("collision: expected file still at wiki/misc/foo.md (no partial move), stat err=%v", err)
	}
}

// --- Task 3.3: SyncEngine.RouteAndSync exported entry point (ontology-gate) ---

// Req 4.1 + 6.4 + design "sync/engine (extended) RouteAndSync":
// the exported single-file entry point must run the schema gate, the
// pre-sync router, the upload, and the title/attrs application -- the
// same code path used by Sync()'s processFile. On a happy path:
//   - it returns nil,
//   - the file ends at the canonical post-route path on disk and in state,
//   - the mock SiYuan received a createDocWithMd with the frontmatter-
//     stripped body at the routed hpath,
//   - the attrs payload contains custom-domain, custom-intent, and any
//     tag-derived custom- keys.
func TestSync_RouteAndSync_HappyPath(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// Non-canonical wiki/misc/ path with a (devops, sop) ontology and two
	// frontmatter tags. The router will route to Sysadmin & DevOps/foo.md.
	content := "---\ndomain: devops\nintent: sop\ntags: [a, b]\n---\n# Foo\n\nBody.\n"
	testutil.WriteFile(t, dir, "wiki/misc/foo.md", content)
	testutil.GitCmd(t, dir, "add", "wiki/misc/foo.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	if err := engine.RouteAndSync(context.Background(), "wiki/misc/foo.md"); err != nil {
		t.Fatalf("RouteAndSync returned error: %v", err)
	}

	canonical := "Sysadmin & DevOps/foo.md"

	// Filesystem: file moved to canonical path.
	if _, err := os.Stat(filepath.Join(dir, canonical)); err != nil {
		t.Errorf("Req 6.4: expected file at canonical path %q, stat err=%v", canonical, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "wiki/misc/foo.md")); !os.IsNotExist(err) {
		t.Errorf("Req 6.4: expected old path wiki/misc/foo.md to be gone, stat err=%v", err)
	}

	// Mock SiYuan: exactly one createDocWithMd at the routed hpath, body
	// stripped of frontmatter.
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected exactly 1 createDocWithMd, got %d: %+v", len(h.userCreatedDocs()), h.createdDocs)
	}
	doc := h.userCreatedDocs()[0]
	if doc.HPath != "/foo.md" {
		t.Errorf("expected create hpath /foo.md, got %q", doc.HPath)
	}
	if strings.Contains(doc.Markdown, "---") {
		t.Errorf("Req 13.1: uploaded body still contains '---' frontmatter delimiter: %q", doc.Markdown)
	}
	if !strings.Contains(doc.Markdown, "# Foo") {
		t.Errorf("expected uploaded body to contain headings/content, got %q", doc.Markdown)
	}

	// State: entry at canonical path with the SiYuanID the mock generated.
	entry, ok := engine.state.All()[canonical]
	if !ok {
		t.Fatalf("expected state entry at canonical path %q, state=%v", canonical, engine.state.All())
	}
	if entry.SiYuanID != doc.ID {
		t.Errorf("expected state SiYuanID=%q (from create), got %q", doc.ID, entry.SiYuanID)
	}

	// Attrs: custom-domain, custom-intent, custom-a, custom-b all set.
	attrs := h.setAttrs[doc.ID]
	if attrs == nil {
		t.Fatalf("Req 4.1: expected setBlockAttrs called for doc %s, got %v", doc.ID, h.setAttrs)
	}
	if got := attrs["custom-domain"]; got != "devops" {
		t.Errorf("Req 4.1: expected custom-domain=devops, got %q (all=%v)", got, attrs)
	}
	if got := attrs["custom-intent"]; got != "sop" {
		t.Errorf("Req 4.1: expected custom-intent=sop, got %q (all=%v)", got, attrs)
	}
	if _, ok := attrs["custom-a"]; !ok {
		t.Errorf("Req 4.1: expected custom-a tag attr, got %v", attrs)
	}
	if _, ok := attrs["custom-b"]; !ok {
		t.Errorf("Req 4.1: expected custom-b tag attr, got %v", attrs)
	}
}

// Req 6.4 + design RouteAndSync: a schema violation aborts the file --
// no upload, no state entry, error returned with a JSON-decodable
// SchemaViolation payload (inherited from the schema gate, Req 2.6).
func TestSync_RouteAndSync_SchemaViolationReturnsError(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// Out-of-enum intent (`braindump`): gate aborts the file before upload.
	content := "---\ndomain: devops\nintent: braindump\n---\n# Bad\n"
	testutil.WriteFile(t, dir, "wiki/misc/bad.md", content)
	testutil.GitCmd(t, dir, "add", "wiki/misc/bad.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	err := engine.RouteAndSync(context.Background(), "wiki/misc/bad.md")
	if err == nil {
		t.Fatalf("Req 6.4: expected RouteAndSync to return an error for a schema-violating file")
	}
	if !strings.Contains(err.Error(), "wiki/misc/bad.md") {
		t.Errorf("expected error to mention the file path, got %q", err.Error())
	}

	// The error must carry a JSON-decodable SchemaViolation payload, same
	// shape the gate emits inside Sync().
	var sv schemaViolationJSON
	// The error message contains the file prefix; find the JSON object.
	msg := err.Error()
	if idx := strings.Index(msg, "{"); idx >= 0 {
		// Try to decode from the first '{' to find the SchemaViolation.
		// errors.Join concatenates each "<file>: <message>" via newlines,
		// where <message> here is the JSON payload itself.
		tail := msg[idx:]
		if end := strings.Index(tail, "\n"); end >= 0 {
			tail = tail[:end]
		}
		if jerr := json.Unmarshal([]byte(tail), &sv); jerr != nil {
			t.Errorf("expected JSON-decodable SchemaViolation in error msg, got %q (json err=%v)", tail, jerr)
		}
	} else {
		t.Errorf("expected '{' in error msg (SchemaViolation payload), got %q", msg)
	}
	if sv.Key != "intent" {
		t.Errorf("expected SchemaViolation.Key='intent', got %q (payload=%+v)", sv.Key, sv)
	}
	if sv.OffendingValue != "braindump" {
		t.Errorf("expected OffendingValue='braindump', got %q", sv.OffendingValue)
	}

	// No SiYuan API call.
	if len(h.userCreatedDocs()) != 0 {
		t.Errorf("Req 2.6: gate must abort before any createDocWithMd, got %d", len(h.userCreatedDocs()))
	}

	// No state entry at either the source or the (would-be) target path.
	allState := engine.state.All()
	if _, ok := allState["wiki/misc/bad.md"]; ok {
		t.Errorf("expected NO state entry at source path on schema violation, got %v", allState)
	}
	if _, ok := allState["Sysadmin & DevOps/bad.md"]; ok {
		t.Errorf("expected NO state entry at target path on schema violation, got %v", allState)
	}
}

// Req 4.1 + design RouteAndSync non-fatal semantics: a RenameDocByID
// failure is recorded as an error but the file is still uploaded and
// the state entry IS populated -- mirroring the Sync()/processFile
// title-failure policy inherited from siyuan-knowledge-sync Req 13.2.
// Callers can distinguish "synced with warnings" from "rejected" by
// checking state.Get(path) after a non-nil return.
func TestSync_RouteAndSync_TitleFailure_StillInState(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// Place at canonical path so no routing happens; the focus is the
	// title-failure path, not routing.
	canonical := "Sysadmin & DevOps/x.md"
	content := "---\ndomain: devops\nintent: sop\ntitle: My Title\n---\n# Body\n"
	testutil.WriteFile(t, dir, canonical, content)
	testutil.GitCmd(t, dir, "add", canonical)
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()
	// Force the mock to return an API error from renameDocByID.
	h.renameErr = true

	engine, _ := newSyncEngine(t, server, dir, false)

	err := engine.RouteAndSync(context.Background(), canonical)
	if err == nil {
		t.Fatalf("expected RouteAndSync to surface the rename error, got nil")
	}
	if !strings.Contains(err.Error(), "set document title") && !strings.Contains(err.Error(), "rename") {
		t.Errorf("expected error to mention title/rename failure, got %q", err.Error())
	}

	// The mock DID receive a createDocWithMd call -- the body was uploaded.
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("Req 13.2 non-fatal: title failure must not block upload, got %d createDocWithMd calls",
			len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]

	// State IS populated: callers that need to distinguish
	// "synced with warnings" from "rejected" check state membership.
	entry, ok := engine.state.All()[canonical]
	if !ok {
		t.Fatalf("Req 13.2 non-fatal: expected state entry at %q despite title error, state=%v",
			canonical, engine.state.All())
	}
	if entry.SiYuanID != doc.ID {
		t.Errorf("expected state SiYuanID=%q, got %q", doc.ID, entry.SiYuanID)
	}
}

// Req 6.4 + design RouteAndSync: a path that does not exist returns a
// "stat" error mentioning the path, and triggers no SiYuan API calls.
// This is the guard for the migrate apply executor, which dispatches by
// path -- a typo or stale path must surface clearly.
func TestSync_RouteAndSync_FileNotFound(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// Create the git repo but never write the target file.
	testutil.WriteFile(t, dir, "placeholder.md", "# placeholder\n")
	testutil.GitCmd(t, dir, "add", "placeholder.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	err := engine.RouteAndSync(context.Background(), "nonexistent.md")
	if err == nil {
		t.Fatal("expected RouteAndSync to return a non-nil error for a missing path")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Errorf("expected 'stat' in error message, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "nonexistent.md") {
		t.Errorf("expected the path in error message, got %q", err.Error())
	}

	// No SiYuan API calls.
	if len(h.userCreatedDocs()) != 0 {
		t.Errorf("expected no createDocWithMd calls on missing file, got %d", len(h.userCreatedDocs()))
	}
	if len(h.updatedDocs) != 0 {
		t.Errorf("expected no updateBlock calls on missing file, got %d", len(h.updatedDocs))
	}
}

// --- Task 5.1: TestOntologyGate umbrella + Req 4.1 Sync()-path guard ---
//
// Per-Requirement coverage mapping for ontology-gate task 5.1 (Req 2.6, 3.2,
// 3.3, 3.5, 3.6, 4.1, 9.2):
//
//   - Req 2.6 (batch continues across schema violations)
//     -> TestSync_SchemaGate_AbortsViolatingFile_BatchContinues
//        (engine_test.go ~2790-2871, task 3.1)
//
//   - Req 3.2 (router emits `git mv` to canonical folder; state moves; new
//     hpath used for upload)
//     -> TestSync_OntologyRouting_MovesAndCommits
//        (engine_test.go ~3003-3071, task 3.2)
//
//   - Req 3.3 (the move is auditable via an `ontology-route:` commit, exactly
//     one such commit per route)
//     -> TestSync_OntologyRouting_MovesAndCommits (subject-line assertion at
//        ~3041-3048, task 3.2)
//
//   - Req 3.5 (a schema-violating file is never silently routed; gate fires
//     before any SiYuan write or git mv)
//     -> TestSync_SchemaGate_AbortsViolatingFile_BatchContinues
//        (engine_test.go ~2822-2870, task 3.1)
//
//   - Req 3.6 (already-canonical path: no git mv, no extra commit)
//     -> TestSync_OntologyRouting_NoopWhenAlreadyCanonical
//        (engine_test.go ~3076-3115, task 3.2)
//
//   - Req 4.1 (custom-domain / custom-intent SetBlockAttrs reach SiYuan on
//     successful sync of an opt-in file)
//     -> TestSync_RouteAndSync_HappyPath asserts this on the RouteAndSync
//        single-file entry point (engine_test.go ~3319-3391, task 3.3)
//     -> TestOntologyGate_Sync_AppliesCustomDomainIntentAttrs (added below)
//        asserts the same on the regular Sync(ctx) batch entry point.
//
//   - Req 9.2 (asset reference warning surfaces with the original asset path,
//     does not block the move)
//     -> TestSync_OntologyRouting_AssetWarning
//        (engine_test.go ~3121-3169, task 3.2)
//
// State.Move collision (per-file error, not panic): covered by
// TestSync_OntologyRouting_StateCollision (engine_test.go ~3214-3305,
// task 3.2). Mirrored under TestOntologyGate_RoutingFlow_FourScenarios as
// the umbrella's "state_collision_per_file_error" subtest.
//
// Container-portability: every test referenced above uses setupGitDir + the
// mock SiYuan httptest server, with no os.Chmod(p, 0) or filesystem mtime
// reliance for the routing/gate scenarios. (TestSync_ReportCounts and
// TestSync_StatePersistedOnError already skip the root-vs-non-root path via
// `os.Geteuid() == 0`.)

// Req 4.1 on the regular Sync(ctx) batch path: a single opt-in file with
// valid (domain, intent) and frontmatter tags must end up with both
// `custom-domain` and `custom-intent` SiYuan block attributes set via
// SetBlockAttrs, plus the per-tag `custom-<tag>` map. The 3.3 test guards
// the RouteAndSync single-file entry point; this guards the Sync() batch
// entry point so a regression on either dispatch surface bites.
func TestOntologyGate_Sync_AppliesCustomDomainIntentAttrs(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	// Place the file at its canonical folder to keep the focus on the
	// attrs payload (no routing concerns; that is covered by 3.2 tests).
	canonical := "Sysadmin & DevOps/attrs.md"
	content := "---\ndomain: devops\nintent: sop\ntags: [a, b]\n---\n# Body\n"
	testutil.WriteFile(t, dir, canonical, content)
	testutil.GitCmd(t, dir, "add", canonical)
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != canonical {
		t.Fatalf("Req 4.1: expected Created=[%q], got %v (errors=%v)",
			canonical, report.Created, report.Errors)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("Req 4.1: expected 1 createDocWithMd, got %d", len(h.userCreatedDocs()))
	}
	doc := h.userCreatedDocs()[0]

	attrs := h.setAttrs[doc.ID]
	if attrs == nil {
		t.Fatalf("Req 4.1: expected setBlockAttrs called on Sync() path for doc %s, got %v",
			doc.ID, h.setAttrs)
	}
	if got := attrs["custom-domain"]; got != "devops" {
		t.Errorf("Req 4.1: expected custom-domain=devops on Sync() path, got %q (all=%v)",
			got, attrs)
	}
	if got := attrs["custom-intent"]; got != "sop" {
		t.Errorf("Req 4.1: expected custom-intent=sop on Sync() path, got %q (all=%v)",
			got, attrs)
	}
	// The tag attrs and ontology attrs must coexist (no key collision); the
	// frontmatter `domain`/`intent` keys must NOT leak as raw keys.
	if _, ok := attrs["custom-a"]; !ok {
		t.Errorf("Req 4.1: expected custom-a tag attr present alongside ontology attrs, got %v", attrs)
	}
	if _, ok := attrs["custom-b"]; !ok {
		t.Errorf("Req 4.1: expected custom-b tag attr present alongside ontology attrs, got %v", attrs)
	}
	if _, leaked := attrs["domain"]; leaked {
		t.Errorf("Req 4.1: raw frontmatter 'domain' key must not leak into attrs map, got %v", attrs)
	}
	if _, leaked := attrs["intent"]; leaked {
		t.Errorf("Req 4.1: raw frontmatter 'intent' key must not leak into attrs map, got %v", attrs)
	}
}

// TestOntologyGate_RoutingFlow_FourScenarios is the umbrella named-set proof
// the task brief calls out: a single `go test -run TestOntologyGate` selector
// must surface the four critical scenarios from the 5.1 brief as named
// subtests. Each subtest is a self-contained replay of the equivalent
// task-3.1 / task-3.2 scenario so the umbrella selector exercises real assert
// chains rather than only naming them.
//
// Subtests:
//
//	schema_violation_aborts_batch_continues
//	  Mirrors TestSync_SchemaGate_AbortsViolatingFile_BatchContinues — one
//	  opt-in file with an out-of-enum intent and one valid sibling; only the
//	  valid sibling reaches SiYuan; the gate emits exactly one Errors entry
//	  for the violating file. (Req 2.6, 3.5.)
//
//	valid_file_routed_with_state_and_commit
//	  Mirrors TestSync_OntologyRouting_MovesAndCommits — a (devops, sop)
//	  file at wiki/misc/foo.md is git-mv'd to Sysadmin & DevOps/foo.md
//	  with exactly one ontology-route: commit; state tracker reflects the
//	  new path. (Req 3.2, 3.3.)
//
//	canonical_path_noop
//	  Mirrors TestSync_OntologyRouting_NoopWhenAlreadyCanonical — a file
//	  already at its canonical folder syncs without git mv or
//	  ontology-route: commit. (Req 3.6.)
//
//	state_collision_per_file_error
//	  Mirrors TestSync_OntologyRouting_StateCollision — a state.Move
//	  collision surfaces as a per-file Errors entry (not a panic); the file
//	  stays on disk at the source; no SiYuan create issued.
func TestOntologyGate_SchemaViolationAbortsBatchContinues(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	testutil.WriteFile(t, dir, "wiki/a.md", "---\ndomain: devops\nintent: braindump\n---\n# Bad\n")
	testutil.WriteFile(t, dir, "wiki/b.md", "---\ndomain: devops\nintent: sop\n---\n# Good\n")
	testutil.GitCmd(t, dir, "add", "wiki/a.md", "wiki/b.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	canonicalB := "Sysadmin & DevOps/b.md"
	if len(report.Created) != 1 || report.Created[0] != canonicalB {
		t.Fatalf("expected Created=[%q] (batch continues past gate), got %v (errors=%v)",
			canonicalB, report.Created, report.Errors)
	}
	for _, p := range report.Created {
		if p == "wiki/a.md" {
			t.Errorf("schema-violating file must not be Created, got %v", report.Created)
		}
	}
	var gateErrs []types.SyncError
	for _, e := range report.Errors {
		if e.File == "wiki/a.md" {
			gateErrs = append(gateErrs, e)
		}
	}
	if len(gateErrs) != 1 {
		t.Fatalf("expected exactly 1 gate Errors entry for wiki/a.md, got %d (errors=%v)",
			len(gateErrs), report.Errors)
	}
	var sv schemaViolationJSON
	if err := json.Unmarshal([]byte(gateErrs[0].Message), &sv); err != nil {
		t.Fatalf("expected JSON-decodable SchemaViolation, got %q (err=%v)",
			gateErrs[0].Message, err)
	}
	if sv.Key != "intent" || sv.OffendingValue != "braindump" {
		t.Errorf("unexpected violation payload: %+v", sv)
	}
	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected exactly 1 createDocWithMd (for the valid sibling), got %d: %+v",
			len(h.userCreatedDocs()), h.createdDocs)
	}
	if h.userCreatedDocs()[0].HPath != "/b.md" {
		t.Errorf("expected createDocWithMd at /b.md, got %q", h.userCreatedDocs()[0].HPath)
	}
}

func TestOntologyGate_ValidFileRoutedWithStateAndCommit(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ndomain: devops\nintent: sop\n---\n# Foo\n"
	testutil.WriteFile(t, dir, "wiki/misc/foo.md", content)
	testutil.GitCmd(t, dir, "add", "wiki/misc/foo.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	canonical := "Sysadmin & DevOps/foo.md"
	if len(report.Created) != 1 || report.Created[0] != canonical {
		t.Fatalf("expected Created=[%q], got %v (errors=%v)",
			canonical, report.Created, report.Errors)
	}

	commits := testutil.GitLogGrep(t, dir, "ontology-route:")
	if len(commits) != 1 {
		t.Fatalf("expected exactly 1 ontology-route commit, got %d: %v", len(commits), commits)
	}
	wantSubject := "ontology-route: wiki/misc/foo.md -> " + canonical
	if commits[0] != wantSubject {
		t.Errorf("commit subject mismatch.\n got:  %q\n want: %q", commits[0], wantSubject)
	}

	if _, err := os.Stat(filepath.Join(dir, canonical)); err != nil {
		t.Errorf("expected file at canonical path %q, stat err=%v", canonical, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "wiki/misc/foo.md")); !os.IsNotExist(err) {
		t.Errorf("expected old path gone, stat err=%v", err)
	}

	allState := engine.state.All()
	entry, ok := allState[canonical]
	if !ok {
		t.Fatalf("expected state entry at %q, got %v", canonical, allState)
	}
	if entry.SiYuanID == "" {
		t.Errorf("expected non-empty SiYuanID at new path, got %+v", entry)
	}
	if _, gone := allState["wiki/misc/foo.md"]; gone {
		t.Errorf("expected old state key gone, got entries=%v", allState)
	}

	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 createDocWithMd, got %d", len(h.userCreatedDocs()))
	}
	if got := h.userCreatedDocs()[0].HPath; got != "/foo.md" {
		t.Errorf("expected create hpath /foo.md, got %q", got)
	}
}

func TestOntologyGate_CanonicalPathNoop(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	canonical := "Sysadmin & DevOps/foo.md"
	content := "---\ndomain: devops\nintent: sop\n---\n# Foo\n"
	testutil.WriteFile(t, dir, canonical, content)
	testutil.GitCmd(t, dir, "add", canonical)
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	engine, _ := newSyncEngine(t, server, dir, false)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if len(report.Created) != 1 || report.Created[0] != canonical {
		t.Fatalf("expected Created=[%q], got %v (errors=%v)",
			canonical, report.Created, report.Errors)
	}

	commits := testutil.GitLogGrep(t, dir, "ontology-route:")
	if len(commits) != 0 {
		t.Errorf("expected 0 ontology-route commits, got %d: %v", len(commits), commits)
	}

	if _, err := os.Stat(filepath.Join(dir, canonical)); err != nil {
		t.Errorf("expected file still at %q, stat err=%v", canonical, err)
	}

	if len(h.userCreatedDocs()) != 1 {
		t.Fatalf("expected 1 createDocWithMd, got %d", len(h.userCreatedDocs()))
	}
}

func TestOntologyGate_StateCollisionPerFileError(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	content := "---\ndomain: devops\nintent: sop\n---\n# Foo\n"
	testutil.WriteFile(t, dir, "wiki/misc/foo.md", content)
	testutil.GitCmd(t, dir, "add", "wiki/misc/foo.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	tr, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	canonical := "Sysadmin & DevOps/foo.md"
	tr.Put(types.SyncEntry{
		LocalPath:  "wiki/misc/foo.md",
		SiYuanID:   "src-doc-id",
		NotebookID: "nb-wiki",
		SyncedAt:   time.Now().Add(-1 * time.Hour),
	})
	tr.Put(types.SyncEntry{
		LocalPath:  canonical,
		SiYuanID:   "different-target-doc-id",
		NotebookID: "nb-wiki",
		SyncedAt:   time.Now().Add(-1 * time.Hour),
	})
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}

	h, server := newMockSiYuanServer(t)
	defer server.Close()

	scanner, err := git.NewGitScanner(dir)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(dir)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(false)
	client := siyuan.NewClient(server.URL, "test-token")
	engine := NewSyncEngine(client, scanner, tracker, ce)

	report, err := engine.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	for _, p := range report.Created {
		if p == "wiki/misc/foo.md" || p == canonical {
			t.Errorf("collision: file must not be Created, got %v", report.Created)
		}
	}
	for _, p := range report.Updated {
		if p == "wiki/misc/foo.md" || p == canonical {
			t.Errorf("collision: file must not be Updated, got %v", report.Updated)
		}
	}

	found := false
	for _, e := range report.Errors {
		if e.File == "wiki/misc/foo.md" && strings.Contains(e.Message, "state collision") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("collision: expected a state-collision error for wiki/misc/foo.md, got errors=%v",
			report.Errors)
	}

	for _, d := range h.createdDocs {
		if d.HPath == "/misc/foo.md" || d.HPath == "/foo.md" {
			t.Errorf("collision: must not call createDocWithMd, got %+v", d)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "wiki/misc/foo.md")); err != nil {
		t.Errorf("collision: expected file still at wiki/misc/foo.md (no partial move), stat err=%v", err)
	}
}

// TestHasSchemaCategoryIssue_WarningSeverityDoesNotAbort pins the cross-package
// contract between the compliance tag-vocabulary check (Req 4.2 / 4.3) and the
// sync engine's schema gate. The tag-vocab check emits issues with
// Category=="schema" and Severity=="warning"; the gate must NOT abort the
// file's sync on those entries — only on error-severity schema violations.
func TestHasSchemaCategoryIssue_WarningSeverityDoesNotAbort(t *testing.T) {
	issues := []types.ComplianceIssue{
		{
			File:     "test.md",
			Line:     0,
			Severity: "warning",
			Message:  `unrecognized tag "foo" — not in configured vocabulary`,
			Fixable:  false,
			Category: "schema",
		},
	}

	if hasSchemaCategoryIssue(issues) {
		t.Errorf("expected hasSchemaCategoryIssue to return false for a schema-category warning, got true")
	}
}

// TestHasSchemaCategoryIssue_ErrorSeverityAborts pins the other half of the
// contract: existing error-severity schema violations (missing required key,
// multi-value, out-of-enum) MUST still trip the gate.
func TestHasSchemaCategoryIssue_ErrorSeverityAborts(t *testing.T) {
	issues := []types.ComplianceIssue{
		{
			File:     "test.md",
			Line:     0,
			Severity: "error",
			Message:  `ontology schema violation in test.md: key="domain" offending="" allowed=[devops,...]`,
			Fixable:  false,
			Category: "schema",
		},
	}

	if !hasSchemaCategoryIssue(issues) {
		t.Errorf("expected hasSchemaCategoryIssue to return true for a schema-category error, got false")
	}
}

// TestHasSchemaCategoryIssue_MixedWarningAndError verifies that a warning
// alongside an error still aborts: the error wins.
func TestHasSchemaCategoryIssue_MixedWarningAndError(t *testing.T) {
	issues := []types.ComplianceIssue{
		{Category: "schema", Severity: "warning", Message: "tag warning"},
		{Category: "schema", Severity: "error", Message: "missing domain"},
	}

	if !hasSchemaCategoryIssue(issues) {
		t.Errorf("expected hasSchemaCategoryIssue to return true when a schema-category error is present alongside warnings, got false")
	}
}

func TestStripLeadingOriginallyWritten(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no line — body unchanged",
			in:   "# Title\n\nbody\n",
			want: "# Title\n\nbody\n",
		},
		{
			name: "single underscore italic line stripped",
			in:   "_Originally written: 2024-01-23_\n\n# Title\n\nbody\n",
			want: "# Title\n\nbody\n",
		},
		{
			name: "single asterisk italic line stripped",
			in:   "*Originally written: 2024-01-23*\n\n# Title\n\nbody\n",
			want: "# Title\n\nbody\n",
		},
		{
			name: "two stacked lines both stripped",
			in:   "_Originally written: 2024-02-25_\n\n*Originally written: 2024-01-23*\n\n# Title\n\nbody\n",
			want: "# Title\n\nbody\n",
		},
		{
			name: "leading whitespace before the line tolerated",
			in:   "\n\n_Originally written: 2024-01-23_\n\n# Title\n\nbody\n",
			want: "# Title\n\nbody\n",
		},
		{
			name: "Originally written elsewhere in body untouched",
			in:   "# Title\n\nsome prose _Originally written: 2024-01-23_ inline\n",
			want: "# Title\n\nsome prose _Originally written: 2024-01-23_ inline\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripLeadingOriginallyWritten(tc.in)
			if got != tc.want {
				t.Errorf("stripLeadingOriginallyWritten output mismatch:\nwant: %q\n got: %q", tc.want, got)
			}
		})
	}
}
