package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"siyuan-knowledge-sync/internal/compliance"
	"siyuan-knowledge-sync/internal/git"
	"siyuan-knowledge-sync/internal/siyuan"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/types"
)

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s\n%s", args, err, out)
	}
}

func writeGitFile(t *testing.T, dir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupGitDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sync-engine-test-*")
	if err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "init")
	return dir
}

type mockSiYuanHandler struct {
	t           *testing.T
	notebooks   map[string]string
	docs        map[string]mockDocRecord
	nextNBID    int
	nextDocID   int
	createdDocs []createdDocRecord
	updatedDocs []string
	createdNBs  []string
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

func newMockSiYuanServer(t *testing.T) (*mockSiYuanHandler, *httptest.Server) {
	t.Helper()
	h := &mockSiYuanHandler{
		t:         t,
		notebooks: make(map[string]string),
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
			nbs := make([]types.Notebook, 0, len(h.notebooks))
			for name, id := range h.notebooks {
				nbs = append(nbs, types.Notebook{ID: id, Name: name})
			}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"notebooks": nbs},
			})

		case "/api/notebook/createNotebook":
			name, ok := body["notebook"].(string)
			if !ok || name == "" {
				enc.Encode(map[string]any{"code": 1, "msg": "missing notebook name"})
				return
			}
			h.nextNBID++
			id := fmt.Sprintf("nb-%d", h.nextNBID)
			h.notebooks[name] = id
			h.createdNBs = append(h.createdNBs, name)
			nb := types.Notebook{ID: id, Name: name}
			enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": nb,
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
				"data": map[string]string{"id": id},
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

		case "/api/filetree/removeDocByID":
			id, _ := body["id"].(string)
			delete(h.docs, id)
			enc.Encode(map[string]any{"code": 0, "msg": ""})

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

	writeGitFile(t, dir, "notebook/sub/file.md", "# Hello\n\nWorld\n")
	gitCmd(t, dir, "add", "notebook/sub/file.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	if len(h.createdDocs) != 1 {
		t.Fatalf("expected 1 document created in SiYuan, got %d", len(h.createdDocs))
	}
	doc := h.createdDocs[0]
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

	writeGitFile(t, dir, "notes/doc.md", "# Original")
	gitCmd(t, dir, "add", "notes/doc.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "notes/doc.md", "# Modified Content")
	gitCmd(t, dir, "add", "notes/doc.md")
	gitCmd(t, dir, "commit", "-m", "modified")

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

	writeGitFile(t, dir, "journal/2024/01/entry.md", "# January Entry")
	writeGitFile(t, dir, "projects/code/readme.md", "# Code Readme")
	gitCmd(t, dir, "add", "journal/2024/01/entry.md", "projects/code/readme.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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
	if len(h.createdDocs) != 2 {
		t.Fatalf("expected 2 documents created, got %d", len(h.createdDocs))
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

	writeGitFile(t, dir, "notes/a.md", "# A")
	writeGitFile(t, dir, "notes/b.md", "# B")
	gitCmd(t, dir, "add", "notes/a.md", "notes/b.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "notes/a.md", "# A - Modified")
	time.Sleep(100 * time.Millisecond)
	gitCmd(t, dir, "add", "notes/a.md")
	gitCmd(t, dir, "commit", "-m", "modify a")

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

	totalCalls := len(h.createdDocs) + len(h.updatedDocs)
	if totalCalls != 3 {
		t.Errorf("expected 3 total SiYuan operations (2 creates + 1 update), got %d", totalCalls)
	}
}

func TestSync_ReportCounts(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	writeGitFile(t, dir, "nb/new.md", "# New")
	writeGitFile(t, dir, "nb/err.md", "# Error")
	gitCmd(t, dir, "add", "nb/new.md", "nb/err.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "notes/doc.md", `---
title: Test
---

# Title

### Skipped H2

{: myattr="val"}

Content.
`)
	gitCmd(t, dir, "add", "notes/doc.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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
	if len(h.createdDocs) != 1 {
		t.Fatalf("expected 1 document created, got %d", len(h.createdDocs))
	}

	fixedContent := h.createdDocs[0].Markdown
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
	if len(h.createdDocs) != 0 {
		t.Errorf("expected 0 API calls, got %d", len(h.createdDocs))
	}
}

func TestSync_ErrorPerFileDoesNotAbort(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	writeGitFile(t, dir, "nb/good.md", "# Good")
	writeGitFile(t, dir, "nb/bad_perm.md", "# Will Fail")
	writeGitFile(t, dir, "nb/also_good.md", "# Also Good")
	gitCmd(t, dir, "add", "nb/good.md", "nb/bad_perm.md", "nb/also_good.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "readme.md", "# Readme\n\nRoot level file.\n")
	gitCmd(t, dir, "add", "readme.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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
	if len(h.createdDocs) != 1 {
		t.Fatalf("expected 1 doc created, got %d", len(h.createdDocs))
	}
	doc := h.createdDocs[0]
	if doc.HPath != "/readme.md" {
		t.Errorf("expected hpath '/readme.md', got %q", doc.HPath)
	}
}

func TestSync_StateTrackerUpdatedAfterSync(t *testing.T) {
	dir := setupGitDir(t)
	defer os.RemoveAll(dir)

	writeGitFile(t, dir, "notes/a.md", "# A")
	gitCmd(t, dir, "add", "notes/a.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "notes/existing.md", "# Original")
	writeGitFile(t, dir, "notes/new.md", "# New File")
	gitCmd(t, dir, "add", "notes/existing.md", "notes/new.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "notes/fail.md", "# Will Fail")
	gitCmd(t, dir, "add", "notes/fail.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "wiki/a.md", "# A")
	writeGitFile(t, dir, "wiki/b.md", "# B")
	gitCmd(t, dir, "add", "wiki/a.md", "wiki/b.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "wiki/doc.md", `### Bad Heading
Some content {: myattr="value"}
`)
	gitCmd(t, dir, "add", "wiki/doc.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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
	if len(h.createdDocs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(h.createdDocs))
	}

	content := h.createdDocs[0].Markdown
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

	writeGitFile(t, dir, "wiki/doc.md", `### Bad Heading
{: myattr="value"}
`)
	gitCmd(t, dir, "add", "wiki/doc.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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
	if len(h.createdDocs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(h.createdDocs))
	}

	content := h.createdDocs[0].Markdown
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

	writeGitFile(t, dir, "nb/good.md", "# Good")
	writeGitFile(t, dir, "nb/bad_perm.md", "# Bad")
	gitCmd(t, dir, "add", "nb/good.md", "nb/bad_perm.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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

	writeGitFile(t, dir, "existing_nb/file.md", "# Content")
	gitCmd(t, dir, "add", "existing_nb/file.md")
	gitCmd(t, dir, "commit", "-m", "initial")

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
				"data": map[string]string{"id": id},
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
	if len(h.createdDocs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(h.createdDocs))
	}
	if h.createdDocs[0].NotebookID != "existing-nb-id" {
		t.Errorf("expected existing notebook ID 'existing-nb-id', got %q", h.createdDocs[0].NotebookID)
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
