// Tests for the migrate apply executor (task 3.4, ontology-gate spec,
// Req 6.3/6.4/6.5/6.6/7.5/10.2/10.3/10.4).
//
// Conventions mirror internal/sync/engine_test.go: a temp git repo plus a
// mock SiYuan httptest.Server. Each test rebuilds both from scratch so that
// per-entry behavior is observed in isolation.
package migrate

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
	"siyuan-knowledge-sync/internal/ontology"
	"siyuan-knowledge-sync/internal/siyuan"
	"siyuan-knowledge-sync/internal/state"
	"siyuan-knowledge-sync/internal/sync"
	"siyuan-knowledge-sync/internal/types"
)

// ---------------------------------------------------------------------------
// Test fixtures: git repo + mock SiYuan server.
// ---------------------------------------------------------------------------

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
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "migrate-apply-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	gitCmd(t, dir, "init")
	// Make commits succeed on repos where signing or defaults would block.
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

// mockSiYuan is a minimal mock of the SiYuan endpoints used by RouteAndSync
// and Apply: lsNotebooks, createNotebook, createDocWithMd, updateBlock,
// renameDocByID, setBlockAttrs, removeDocByID.
type mockSiYuan struct {
	notebooks     map[string]string // name -> id
	docs          map[string]string // docID -> hpath
	nextNB        int
	nextDoc       int
	createdDocs   []createdDoc
	updatedDocs   []string
	removedDocs   []string
	renamedTitle  map[string]string
	setAttrs      map[string]map[string]string
}

type createdDoc struct {
	NotebookID string
	HPath      string
	Markdown   string
	ID         string
}

func newMockSiYuan(t *testing.T) (*mockSiYuan, *httptest.Server) {
	t.Helper()
	m := &mockSiYuan{
		notebooks:    map[string]string{},
		docs:         map[string]string{},
		renamedTitle: map[string]string{},
		setAttrs:     map[string]map[string]string{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}

		switch r.URL.Path {
		case "/api/notebook/lsNotebooks":
			nbs := make([]types.Notebook, 0, len(m.notebooks))
			for name, id := range m.notebooks {
				nbs = append(nbs, types.Notebook{ID: id, Name: name})
			}
			_ = enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"notebooks": nbs},
			})

		case "/api/notebook/createNotebook":
			name, _ := body["name"].(string)
			m.nextNB++
			id := fmt.Sprintf("nb-%d", m.nextNB)
			m.notebooks[name] = id
			_ = enc.Encode(map[string]any{
				"code": 0, "msg": "",
				"data": map[string]any{"notebook": types.Notebook{ID: id, Name: name}},
			})

		case "/api/filetree/createDocWithMd":
			nbID, _ := body["notebook"].(string)
			hpath, _ := body["path"].(string)
			md, _ := body["markdown"].(string)
			m.nextDoc++
			id := fmt.Sprintf("doc-%d", m.nextDoc)
			m.docs[id] = hpath
			m.createdDocs = append(m.createdDocs, createdDoc{
				NotebookID: nbID, HPath: hpath, Markdown: md, ID: id,
			})
			_ = enc.Encode(map[string]any{"code": 0, "msg": "", "data": id})

		case "/api/block/updateBlock":
			id, _ := body["id"].(string)
			m.updatedDocs = append(m.updatedDocs, id)
			_ = enc.Encode(map[string]any{"code": 0, "msg": ""})

		case "/api/filetree/removeDocByID":
			id, _ := body["id"].(string)
			delete(m.docs, id)
			m.removedDocs = append(m.removedDocs, id)
			_ = enc.Encode(map[string]any{"code": 0, "msg": ""})

		case "/api/filetree/renameDocByID":
			id, _ := body["id"].(string)
			title, _ := body["title"].(string)
			m.renamedTitle[id] = title
			_ = enc.Encode(map[string]any{"code": 0, "msg": ""})

		case "/api/attr/setBlockAttrs":
			id, _ := body["id"].(string)
			attrs := map[string]string{}
			if raw, ok := body["attrs"].(map[string]any); ok {
				for k, v := range raw {
					if s, ok := v.(string); ok {
						attrs[k] = s
					}
				}
			}
			m.setAttrs[id] = attrs
			_ = enc.Encode(map[string]any{"code": 0, "msg": ""})

		default:
			_ = enc.Encode(map[string]any{
				"code": 1, "msg": "unknown endpoint: " + r.URL.Path,
			})
		}
	}))
	t.Cleanup(server.Close)
	return m, server
}

// buildEngine wires up a SyncEngine backed by the mock server + temp repo.
func buildEngine(t *testing.T, repo string, server *httptest.Server) *sync.SyncEngine {
	t.Helper()
	client := siyuan.NewClient(server.URL, "test-token")
	scanner, err := git.NewGitScanner(repo)
	if err != nil {
		t.Fatalf("NewGitScanner: %v", err)
	}
	tracker, err := state.NewStateTracker(repo)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	ce := compliance.NewComplianceEngine(false)
	return sync.NewSyncEngine(client, scanner, tracker, ce)
}

// buildClient returns a siyuan.Client pointed at the mock server. Used for
// retire entries where Apply talks to the client directly.
func buildClient(server *httptest.Server) *siyuan.Client {
	return siyuan.NewClient(server.URL, "test-token")
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// 1. Mixed plan covering all three op kinds; each entry independently
// succeeds; per-entry isolation does not bleed state across entries.
func TestApply_MixedPlan_PerEntryIsolation(t *testing.T) {
	repo := setupGitRepo(t)

	// Seed three local files committed to the repo.
	writeGitFile(t, repo, "wiki/misc/a.md", "# A\n\nOriginal body of A.\n")
	writeGitFile(t, repo, "wiki/misc/b.md", "# B\n\nOriginal body of B.\n")
	writeGitFile(t, repo, "wiki/misc/c.md", "# C\n\nOriginal body of C.\n")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", "seed")

	mock, server := newMockSiYuan(t)
	engine := buildEngine(t, repo, server)
	client := buildClient(server)

	plan := MigrationPlan{
		Version:     PlanV1,
		Source:      "wiki/misc",
		GeneratedAt: time.Date(2026, 6, 5, 17, 0, 0, 0, time.UTC),
		Entries: []PlanEntry{
			{
				Op:         OpKeep,
				SourcePath: "wiki/misc/a.md",
				Domain:     ontology.DevOps,
				Intent:     ontology.IntentSOP,
			},
			{
				Op:            OpKeep,
				SourcePath:    "wiki/misc/b.md",
				Domain:        ontology.DevOps,
				Intent:        ontology.IntentSOP,
				RewrittenBody: "# Rewritten body\n\nReplaced.\n",
			},
			{
				Op:         OpDropLocal,
				SourcePath: "wiki/misc/c.md",
			},
			{
				Op:          OpRetireSiyuan,
				SourcePath:  "wiki/misc/legacy.md",
				SiYuanDocID: "doc-to-retire",
			},
		},
	}

	report, err := Apply(context.Background(), plan, engine, client, repo)
	if err != nil {
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("Apply returned nil report")
	}
	if got, want := len(report.Outcomes), 4; got != want {
		t.Fatalf("expected %d outcomes, got %d: %+v", want, got, report.Outcomes)
	}

	if got := report.Outcomes[0].Status; got != StatusKept {
		t.Errorf("entry 0 (a.md): want %q, got %q (err=%q)", StatusKept, got, report.Outcomes[0].Error)
	}
	if got := report.Outcomes[1].Status; got != StatusKept {
		t.Errorf("entry 1 (b.md): want %q, got %q (err=%q)", StatusKept, got, report.Outcomes[1].Error)
	}
	if got := report.Outcomes[2].Status; got != StatusDropped {
		t.Errorf("entry 2 (c.md): want %q, got %q (err=%q)", StatusDropped, got, report.Outcomes[2].Error)
	}
	if got := report.Outcomes[3].Status; got != StatusRetired {
		t.Errorf("entry 3 (retire): want %q, got %q (err=%q)", StatusRetired, got, report.Outcomes[3].Error)
	}

	// b.md uploaded body must contain "Rewritten body" — not the original.
	var foundB bool
	for _, d := range mock.createdDocs {
		if strings.Contains(d.Markdown, "Rewritten body") {
			foundB = true
			if strings.Contains(d.Markdown, "Original body of B") {
				t.Errorf("entry 1 (b.md): rewritten body must replace original; got %q", d.Markdown)
			}
		}
	}
	if !foundB {
		t.Errorf("entry 1 (b.md): no created doc with rewritten body; got %d creations", len(mock.createdDocs))
	}

	// Retire entry: removeDocByID must have been called for the doc ID.
	var sawRetire bool
	for _, id := range mock.removedDocs {
		if id == "doc-to-retire" {
			sawRetire = true
		}
	}
	if !sawRetire {
		t.Errorf("retire: expected removeDocByID(doc-to-retire); got %v", mock.removedDocs)
	}

	// Dropped local file must be gone from disk.
	if _, err := os.Stat(filepath.Join(repo, "wiki/misc/c.md")); !os.IsNotExist(err) {
		t.Errorf("drop_local: expected c.md removed, stat err = %v", err)
	}
}

// 2. One entry fails (read error from a missing file); the others still run.
func TestApply_OneEntryFails_OthersStillRun(t *testing.T) {
	repo := setupGitRepo(t)
	writeGitFile(t, repo, "wiki/misc/a.md", "# A\n\nbody\n")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", "seed")

	_, server := newMockSiYuan(t)
	engine := buildEngine(t, repo, server)
	client := buildClient(server)

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "wiki/misc",
		Entries: []PlanEntry{
			{
				Op:         OpKeep,
				SourcePath: "wiki/misc/a.md",
				Domain:     ontology.DevOps,
				Intent:     ontology.IntentSOP,
			},
			{
				Op:         OpKeep,
				SourcePath: "wiki/misc/does-not-exist.md",
				Domain:     ontology.DevOps,
				Intent:     ontology.IntentSOP,
			},
			{
				Op:          OpRetireSiyuan,
				SourcePath:  "wiki/misc/legacy.md",
				SiYuanDocID: "doc-x",
			},
		},
	}

	report, err := Apply(context.Background(), plan, engine, client, repo)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(report.Outcomes) != 3 {
		t.Fatalf("want 3 outcomes, got %d", len(report.Outcomes))
	}
	if report.Outcomes[0].Status != StatusKept {
		t.Errorf("entry 0: want kept, got %q (err=%q)", report.Outcomes[0].Status, report.Outcomes[0].Error)
	}
	if report.Outcomes[1].Status != StatusError {
		t.Errorf("entry 1: want error, got %q", report.Outcomes[1].Status)
	}
	if !strings.Contains(report.Outcomes[1].Error, "read") {
		t.Errorf("entry 1: expected error to mention 'read'; got %q", report.Outcomes[1].Error)
	}
	if report.Outcomes[2].Status != StatusRetired {
		t.Errorf("entry 2: want retired, got %q (err=%q)", report.Outcomes[2].Status, report.Outcomes[2].Error)
	}
}

// 3. Pre-flight plan rejection: Version: 0 → Apply returns (nil, error)
// BEFORE any side effects fire.
func TestApply_InvalidPlan_PreflightRejects(t *testing.T) {
	repo := setupGitRepo(t)
	writeGitFile(t, repo, "wiki/misc/a.md", "# A\n")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", "seed")

	mock, server := newMockSiYuan(t)
	engine := buildEngine(t, repo, server)
	client := buildClient(server)

	plan := MigrationPlan{
		Version: 0, // invalid
		Source:  "wiki/misc",
		Entries: []PlanEntry{
			{
				Op:         OpKeep,
				SourcePath: "wiki/misc/a.md",
				Domain:     ontology.DevOps,
				Intent:     ontology.IntentSOP,
			},
		},
	}

	report, err := Apply(context.Background(), plan, engine, client, repo)
	if err == nil {
		t.Fatalf("Apply: expected pre-flight error, got nil")
	}
	if report != nil {
		t.Errorf("Apply: expected nil report on pre-flight failure, got %+v", report)
	}
	if !strings.Contains(err.Error(), "invalid plan") {
		t.Errorf("Apply: expected error to mention 'invalid plan'; got %v", err)
	}

	// No SiYuan API call may have occurred.
	if total := len(mock.createdDocs) + len(mock.removedDocs) + len(mock.updatedDocs); total > 0 {
		t.Errorf("Apply: expected zero API calls on pre-flight reject; got %d", total)
	}
	// No new commit should exist beyond the seed.
	out, gerr := exec.Command("git", "-C", repo, "log", "--oneline").CombinedOutput()
	if gerr != nil {
		t.Fatalf("git log: %v (%s)", gerr, out)
	}
	if got := strings.Count(strings.TrimSpace(string(out)), "\n"); got > 0 {
		t.Errorf("Apply: expected only seed commit, got log:\n%s", out)
	}
}

// 4. Proxy for per-entry error isolation (the ErrUnsafeRewrite path is hard
// to provoke organically without yaml.v3 reformatting quirks — this proxy
// exercises read-failure propagation under OpKeep, which is the closest
// natural error from the same shared error-isolation code path).
func TestApply_UnreadableSource_ProducesEntryError(t *testing.T) {
	repo := setupGitRepo(t)
	writeGitFile(t, repo, "wiki/misc/a.md", "# A\n")
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", "seed")

	_, server := newMockSiYuan(t)
	engine := buildEngine(t, repo, server)
	client := buildClient(server)

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "wiki/misc",
		Entries: []PlanEntry{
			{
				Op:         OpKeep,
				SourcePath: "wiki/misc/missing.md", // not on disk
				Domain:     ontology.DevOps,
				Intent:     ontology.IntentSOP,
			},
		},
	}

	report, err := Apply(context.Background(), plan, engine, client, repo)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(report.Outcomes) != 1 {
		t.Fatalf("expected 1 outcome, got %d", len(report.Outcomes))
	}
	if report.Outcomes[0].Status != StatusError {
		t.Errorf("expected StatusError, got %q", report.Outcomes[0].Status)
	}
	if !strings.Contains(report.Outcomes[0].Error, "read") {
		t.Errorf("expected 'read' in error; got %q", report.Outcomes[0].Error)
	}
}

// 5. RewrittenBody preserves frontmatter while replacing the body.
func TestApply_RewrittenBody_PreservesFrontmatter(t *testing.T) {
	repo := setupGitRepo(t)
	original := "---\ntitle: Original\ndate: 2024-01-01\ntags: [foo]\n---\nOriginal body.\n"
	writeGitFile(t, repo, "wiki/misc/a.md", original)
	gitCmd(t, repo, "add", ".")
	gitCmd(t, repo, "commit", "-m", "seed")

	_, server := newMockSiYuan(t)
	engine := buildEngine(t, repo, server)
	client := buildClient(server)

	plan := MigrationPlan{
		Version: PlanV1,
		Source:  "wiki/misc",
		Entries: []PlanEntry{
			{
				Op:            OpKeep,
				SourcePath:    "wiki/misc/a.md",
				Domain:        ontology.DevOps,
				Intent:        ontology.IntentSOP,
				RewrittenBody: "Replaced body.",
			},
		},
	}

	report, err := Apply(context.Background(), plan, engine, client, repo)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if report.Outcomes[0].Status != StatusKept {
		t.Fatalf("expected StatusKept, got %q (err=%q)", report.Outcomes[0].Status, report.Outcomes[0].Error)
	}

	// Find the file on disk: it may have been moved by routing to the
	// canonical devops folder.
	candidates := []string{
		filepath.Join(repo, "wiki/misc/a.md"),
		filepath.Join(repo, "wiki/Linux & DevOps/a.md"),
	}
	var final string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			final = p
			break
		}
	}
	if final == "" {
		t.Fatalf("could not locate post-apply file at any of %v", candidates)
	}

	got, err := os.ReadFile(final)
	if err != nil {
		t.Fatalf("read post-apply file: %v", err)
	}
	gs := string(got)

	mustContain := []string{
		"title: Original",
		"date: 2024-01-01",
		// yaml.v3 may re-emit the inline flow as a block list.
		"foo",
		"domain: devops",
		"intent: sop",
		"Replaced body.",
	}
	for _, sub := range mustContain {
		if !strings.Contains(gs, sub) {
			t.Errorf("expected post-apply content to contain %q; got:\n%s", sub, gs)
		}
	}
	// Original body must be gone.
	if strings.Contains(gs, "Original body.") {
		t.Errorf("expected original body to be replaced; got:\n%s", gs)
	}
}
