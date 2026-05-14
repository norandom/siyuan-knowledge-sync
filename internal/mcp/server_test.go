package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	mcppkg "github.com/modelcontextprotocol/go-sdk/mcp"

	"siyuan-knowledge-sync/internal/siyuan"
	"siyuan-knowledge-sync/internal/types"
)

func mockSiYuanServer(t *testing.T, responseCode int, responseData any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": responseCode,
			"msg":  "",
			"data": responseData,
		})
	}))
}

func TestHandleSearch_Success(t *testing.T) {
	expectedRows := []map[string]any{
		{"id": "doc-1", "name": "Test Doc", "box": "nb1", "hpath": "/test.md", "content": "This is a test document with some content."},
		{"id": "doc-2", "name": "Another", "box": "nb2", "hpath": "/another.md", "content": "Another doc matching query."},
	}
	server := mockSiYuanServer(t, 0, expectedRows)
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	result, _, err := s.handleSearch(context.Background(), nil, searchArgs{Query: "test"})
	if err != nil {
		t.Fatalf("handleSearch failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcppkg.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}

	var items []searchResultItem
	if err := json.Unmarshal([]byte(text.Text), &items); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "doc-1" {
		t.Errorf("items[0].ID = %q, want 'doc-1'", items[0].ID)
	}
	if items[0].Title != "Test Doc" {
		t.Errorf("items[0].Title = %q, want 'Test Doc'", items[0].Title)
	}
	if items[0].Path != "/test.md" {
		t.Errorf("items[0].Path = %q, want '/test.md'", items[0].Path)
	}
	if items[0].Excerpt == "" {
		t.Error("expected non-empty excerpt")
	}
}

func TestHandleSearch_EmptyResults(t *testing.T) {
	server := mockSiYuanServer(t, 0, []map[string]any{})
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	result, _, err := s.handleSearch(context.Background(), nil, searchArgs{Query: "nonexistent"})
	if err != nil {
		t.Fatalf("handleSearch failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	text := result.Content[0].(*mcppkg.TextContent)
	if text.Text != "[]" {
		t.Errorf("expected empty JSON array, got %s", text.Text)
	}
}

func TestHandleSearch_SiyuanError(t *testing.T) {
	server := mockSiYuanServer(t, 404, nil)
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	_, _, err := s.handleSearch(context.Background(), nil, searchArgs{Query: "test"})
	if err == nil {
		t.Fatal("expected error from API failure")
	}
}

func TestHandleSearch_SQLInjectionSanitization(t *testing.T) {
	server := mockSiYuanServer(t, 0, []map[string]any{})
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	_, _, err := s.handleSearch(context.Background(), nil, searchArgs{Query: "test' OR '1'='1"})
	if err != nil {
		t.Fatalf("handleSearch failed: %v", err)
	}
}

func TestHandleRetrieve_Success(t *testing.T) {
	exportData := types.ExportResult{
		ID:      "doc-1",
		Content: "# My Document\n\nSome content here.",
		HPath:   "/docs/my-doc.md",
	}
	metaData := []map[string]any{
		{"name": "My Document", "box": "nb1"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/export/exportMdContent" {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"msg":  "",
				"data": exportData,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"msg":  "",
				"data": metaData,
			})
		}
	}))
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	result, _, err := s.handleRetrieve(context.Background(), nil, retrieveArgs{ID: "doc-1"})
	if err != nil {
		t.Fatalf("handleRetrieve failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	text := result.Content[0].(*mcppkg.TextContent)

	var doc retrieveResult
	if err := json.Unmarshal([]byte(text.Text), &doc); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if doc.ID != "doc-1" {
		t.Errorf("ID = %q, want 'doc-1'", doc.ID)
	}
	if doc.Title != "My Document" {
		t.Errorf("Title = %q, want 'My Document'", doc.Title)
	}
	if doc.Markdown != "# My Document\n\nSome content here." {
		t.Errorf("Markdown = %q", doc.Markdown)
	}
	if doc.Notebook != "nb1" {
		t.Errorf("Notebook = %q, want 'nb1'", doc.Notebook)
	}
}

func TestHandleRetrieve_FallbackTitle(t *testing.T) {
	exportData := types.ExportResult{
		ID:      "doc-1",
		Content: "# Content",
		HPath:   "/docs/fallback.md",
	}
	metaData := []map[string]any{
		{"box": "nb1"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/export/exportMdContent" {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "", "data": exportData,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "", "data": metaData,
			})
		}
	}))
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	result, _, err := s.handleRetrieve(context.Background(), nil, retrieveArgs{ID: "doc-1"})
	if err != nil {
		t.Fatalf("handleRetrieve failed: %v", err)
	}
	text := result.Content[0].(*mcppkg.TextContent)
	var doc retrieveResult
	json.Unmarshal([]byte(text.Text), &doc)

	if doc.Title != "/docs/fallback.md" {
		t.Errorf("Title = %q, want fallback '/docs/fallback.md'", doc.Title)
	}
}

func TestHandleRetrieve_DocumentNotFound(t *testing.T) {
	server := mockSiYuanServer(t, 404, nil)
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	_, _, err := s.handleRetrieve(context.Background(), nil, retrieveArgs{ID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent document")
	}
}

func TestHandleListNotebooks_Success(t *testing.T) {
	nbData := map[string]any{
		"notebooks": []types.Notebook{
			{ID: "nb1", Name: "Notebook One", Icon: "icon1", Sort: 0, Closed: false},
			{ID: "nb2", Name: "Notebook Two", Icon: "icon2", Sort: 1, Closed: false},
		},
	}
	countData := []map[string]any{
		{"box": "nb1", "count": float64(42)},
		{"box": "nb2", "count": float64(7)},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/notebook/lsNotebooks" {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "", "data": nbData,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "", "data": countData,
			})
		}
	}))
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	result, _, err := s.handleListNotebooks(context.Background(), nil, listNotebooksArgs{})
	if err != nil {
		t.Fatalf("handleListNotebooks failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	text := result.Content[0].(*mcppkg.TextContent)

	var items []notebookItem
	if err := json.Unmarshal([]byte(text.Text), &items); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 notebooks, got %d", len(items))
	}
	if items[0].ID != "nb1" || items[0].Name != "Notebook One" {
		t.Errorf("items[0] = %+v", items[0])
	}
	if items[0].DocCount != 42 {
		t.Errorf("items[0].DocCount = %d, want 42", items[0].DocCount)
	}
	if items[1].ID != "nb2" || items[1].DocCount != 7 {
		t.Errorf("items[1] = %+v, want DocCount=7", items[1])
	}
}

func TestHandleListNotebooks_CountQueryFails(t *testing.T) {
	nbData := map[string]any{
		"notebooks": []types.Notebook{
			{ID: "nb1", Name: "Notebook One", Icon: "icon1", Sort: 0, Closed: false},
		},
	}

	var nbCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/notebook/lsNotebooks" {
			nbCalled = true
			json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "msg": "", "data": nbData,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 500, "msg": "internal error", "data": nil,
			})
		}
	}))
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	result, _, err := s.handleListNotebooks(context.Background(), nil, listNotebooksArgs{})
	if err != nil {
		t.Fatalf("handleListNotebooks failed: %v", err)
	}
	if !nbCalled {
		t.Error("expected notebook list to be called")
	}
	text := result.Content[0].(*mcppkg.TextContent)
	var items []notebookItem
	json.Unmarshal([]byte(text.Text), &items)

	if len(items) != 1 {
		t.Fatalf("expected 1 notebook, got %d", len(items))
	}
	if items[0].DocCount != 0 {
		t.Errorf("DocCount = %d, want 0 (graceful degradation)", items[0].DocCount)
	}
}

func TestHandleListNotebooks_SiyuanError(t *testing.T) {
	server := mockSiYuanServer(t, 401, nil)
	defer server.Close()

	client := siyuan.NewClient(server.URL, "test-token")
	s := &MCPServer{client: client}

	_, _, err := s.handleListNotebooks(context.Background(), nil, listNotebooksArgs{})
	if err == nil {
		t.Fatal("expected error from auth failure")
	}
}

func TestNewServer(t *testing.T) {
	client := siyuan.NewClient("https://example.com", "test-token")
	s := NewServer(client)

	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.client != client {
		t.Error("server client mismatch")
	}
	if s.srv == nil {
		t.Error("expected non-nil mcp.Server")
	}
}

func TestStrVal(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want string
	}{
		{"string value", map[string]any{"x": "hello"}, "x", "hello"},
		{"missing key", map[string]any{}, "x", ""},
		{"float64 int", map[string]any{"x": float64(42)}, "x", "42"},
		{"float64 decimal", map[string]any{"x": float64(3.14)}, "x", "3.14"},
		{"nil value", map[string]any{"x": nil}, "x", ""},
		{"bool value", map[string]any{"x": true}, "x", "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strVal(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("strVal() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIntVal(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want int
	}{
		{"float64", map[string]any{"x": float64(42)}, "x", 42},
		{"string number", map[string]any{"x": "99"}, "x", 99},
		{"missing key", map[string]any{}, "x", 0},
		{"nil value", map[string]any{"x": nil}, "x", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intVal(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("intVal() = %d, want %d", got, tt.want)
			}
		})
	}
}
