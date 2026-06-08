package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"siyuan-knowledge-sync/internal/types"
)

// DocRecord represents a document stored in the mock SiYuan server.
type DocRecord struct {
	NotebookID string
	HPath      string
	Markdown   string
	ID         string
}

// MockSiYuan is a mock SiYuan HTTP server for testing sync/migrate operations.
// It covers the core endpoints: lsNotebooks, createNotebook, createDocWithMd,
// updateBlock, listDocsByPath, removeDocByID, renameDocByID, setBlockAttrs, query/sql.
//
// Use the various On* hooks to inject errors or custom behavior for specific endpoints.
type MockSiYuan struct {
	mu            sync.Mutex
	notebooks     map[string]string // name -> id
	docs          map[string]DocRecord
	nextNBID      int
	nextDocID     int
	CreatedDocs   []DocRecord
	UpdatedDocs   []string
	CreatedNBs    []string
	RemovedDocIDs []string
	RenamedTitles map[string]string
	SetAttrs      map[string]map[string]string

	// DocTrees maps notebook ID -> tree nodes for listDocsByPath responses.
	DocTrees map[string][]types.TreeNode

	// Hooks for injecting errors. Return (response, true) to use custom response;
	// return ("", false) for default behavior.
	OnRename    func(id, title string) (code int, msg string, handled bool)
	OnSetAttrs  func(id string, attrs map[string]string) (code int, msg string, handled bool)
	OnCreateDoc func(nbID, hpath, md string) (id string, code int, msg string, handled bool)

	Server *httptest.Server
}

// NewMockSiYuan creates and starts a mock SiYuan HTTP server.
// Register t.Cleanup to close the server automatically.
func NewMockSiYuan(t *testing.T) *MockSiYuan {
	t.Helper()
	m := &MockSiYuan{
		notebooks:     make(map[string]string),
		docs:          make(map[string]DocRecord),
		RenamedTitles: make(map[string]string),
		SetAttrs:      make(map[string]map[string]string),
	}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(m.Server.Close)
	return m
}

// FilterUserDocs returns docs with engine-owned intent-index docs removed.
// Index docs have HPath of the form `/_<intent>_index.md`.
func FilterUserDocs(docs []DocRecord) []DocRecord {
	out := make([]DocRecord, 0, len(docs))
	for _, d := range docs {
		if strings.HasPrefix(d.HPath, "/_") && strings.HasSuffix(d.HPath, "_index.md") {
			continue
		}
		out = append(out, d)
	}
	return out
}

func (m *MockSiYuan) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)

	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

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
		name, ok := body["name"].(string)
		if !ok || name == "" {
			_ = enc.Encode(map[string]any{"code": 1, "msg": "missing notebook name"})
			return
		}
		m.nextNBID++
		id := fmt.Sprintf("nb-%d", m.nextNBID)
		m.notebooks[name] = id
		m.CreatedNBs = append(m.CreatedNBs, name)
		_ = enc.Encode(map[string]any{
			"code": 0, "msg": "",
			"data": map[string]any{"notebook": types.Notebook{ID: id, Name: name}},
		})

	case "/api/filetree/createDocWithMd":
		nbID, _ := body["notebook"].(string)
		hpath, _ := body["path"].(string)
		md, _ := body["markdown"].(string)

		if m.OnCreateDoc != nil {
			id, code, msg, handled := m.OnCreateDoc(nbID, hpath, md)
			if handled {
				if code == 0 {
					m.docs[id] = DocRecord{NotebookID: nbID, HPath: hpath, Markdown: md, ID: id}
					m.CreatedDocs = append(m.CreatedDocs, DocRecord{NotebookID: nbID, HPath: hpath, Markdown: md, ID: id})
				}
				_ = enc.Encode(map[string]any{"code": code, "msg": msg, "data": id})
				return
			}
		}

		m.nextDocID++
		id := fmt.Sprintf("doc-%d", m.nextDocID)
		rec := DocRecord{NotebookID: nbID, HPath: hpath, Markdown: md, ID: id}
		m.docs[id] = rec
		m.CreatedDocs = append(m.CreatedDocs, rec)
		_ = enc.Encode(map[string]any{"code": 0, "msg": "", "data": id})

	case "/api/block/updateBlock":
		id, _ := body["id"].(string)
		md, _ := body["data"].(string)
		if rec, exists := m.docs[id]; exists {
			rec.Markdown = md
			m.docs[id] = rec
		}
		m.UpdatedDocs = append(m.UpdatedDocs, id)
		_ = enc.Encode(map[string]any{"code": 0, "msg": ""})

	case "/api/filetree/listDocsByPath":
		notebookID, _ := body["notebook"].(string)
		tree := m.DocTrees[notebookID]
		if tree == nil {
			tree = []types.TreeNode{}
		}
		_ = enc.Encode(map[string]any{
			"code": 0, "msg": "",
			"data": map[string]any{"files": tree},
		})

	case "/api/filetree/removeDocByID":
		id, _ := body["id"].(string)
		delete(m.docs, id)
		m.RemovedDocIDs = append(m.RemovedDocIDs, id)
		_ = enc.Encode(map[string]any{"code": 0, "msg": ""})

	case "/api/filetree/renameDocByID":
		id, _ := body["id"].(string)
		title, _ := body["title"].(string)
		if m.OnRename != nil {
			code, msg, handled := m.OnRename(id, title)
			if handled {
				if code == 0 {
					m.RenamedTitles[id] = title
				}
				_ = enc.Encode(map[string]any{"code": code, "msg": msg})
				return
			}
		}
		m.RenamedTitles[id] = title
		_ = enc.Encode(map[string]any{"code": 0, "msg": ""})

	case "/api/attr/setBlockAttrs":
		id, _ := body["id"].(string)
		attrs := make(map[string]string)
		if raw, ok := body["attrs"].(map[string]any); ok {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					attrs[k] = s
				}
			}
		}
		if m.OnSetAttrs != nil {
			code, msg, handled := m.OnSetAttrs(id, attrs)
			if handled {
				if code == 0 {
					m.SetAttrs[id] = attrs
				}
				_ = enc.Encode(map[string]any{"code": code, "msg": msg})
				return
			}
		}
		m.SetAttrs[id] = attrs
		_ = enc.Encode(map[string]any{"code": 0, "msg": ""})

	case "/api/query/sql":
		_ = enc.Encode(map[string]any{"code": 0, "msg": "", "data": []map[string]any{}})

	default:
		_ = enc.Encode(map[string]any{"code": 1, "msg": "unknown endpoint: " + r.URL.Path})
	}
}
