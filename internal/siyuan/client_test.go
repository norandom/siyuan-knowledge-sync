package siyuan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"siyuan-knowledge-sync/internal/types"
)

type capturedRequest struct {
	path string
	auth string
	body map[string]any
}

func mockServer(t *testing.T, responseCode int, responseData any, capture *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture.path = r.URL.Path
			capture.auth = r.Header.Get("Authorization")
			if r.Body != nil {
				json.NewDecoder(r.Body).Decode(&capture.body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": responseCode,
			"msg":  "test message",
			"data": responseData,
		})
	}))
}

func TestNewClient(t *testing.T) {
	c := NewClient("https://example.com", "secret-token")
	if c.baseURL != "https://example.com" {
		t.Errorf("expected baseURL 'https://example.com', got %q", c.baseURL)
	}
	if c.token != "secret-token" {
		t.Errorf("expected token 'secret-token', got %q", c.token)
	}
	if c.httpClient == nil {
		t.Error("expected non-nil httpClient")
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", c.httpClient.Timeout)
	}
}

func TestListNotebooks_Success(t *testing.T) {
	expected := []types.Notebook{
		{ID: "nb1", Name: "Notebook 1", Icon: "icon1", Sort: 0, Closed: false},
		{ID: "nb2", Name: "Notebook 2", Icon: "icon2", Sort: 1, Closed: true},
	}
	var cap capturedRequest
	server := mockServer(t, 0, map[string]any{"notebooks": expected}, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	notebooks, err := client.ListNotebooks(context.Background())
	if err != nil {
		t.Fatalf("ListNotebooks failed: %v", err)
	}
	if cap.path != "/api/notebook/lsNotebooks" {
		t.Errorf("path: got %q, want /api/notebook/lsNotebooks", cap.path)
	}
	if cap.auth != "Token test-token" {
		t.Errorf("auth: got %q, want 'Token test-token'", cap.auth)
	}
	if len(notebooks) != 2 {
		t.Fatalf("expected 2 notebooks, got %d", len(notebooks))
	}
	if notebooks[0].ID != "nb1" || notebooks[0].Name != "Notebook 1" {
		t.Errorf("notebooks[0] = %+v, want ID=nb1 Name=Notebook 1", notebooks[0])
	}
	if notebooks[1].ID != "nb2" || notebooks[1].Closed != true {
		t.Errorf("notebooks[1].Closed = %v, want true", notebooks[1].Closed)
	}
}

func TestListNotebooks_Empty(t *testing.T) {
	server := mockServer(t, 0, map[string]any{"notebooks": []types.Notebook{}}, nil)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	notebooks, err := client.ListNotebooks(context.Background())
	if err != nil {
		t.Fatalf("ListNotebooks failed: %v", err)
	}
	if len(notebooks) != 0 {
		t.Errorf("expected 0 notebooks, got %d", len(notebooks))
	}
}

func TestCreateNotebook_Success(t *testing.T) {
	expected := map[string]any{"notebook": types.Notebook{ID: "nb-new", Name: "My Notebook", Icon: "icon", Sort: 0, Closed: false}}
	var cap capturedRequest
	server := mockServer(t, 0, expected, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	nb, err := client.CreateNotebook(context.Background(), "My Notebook")
	if err != nil {
		t.Fatalf("CreateNotebook failed: %v", err)
	}
	if cap.path != "/api/notebook/createNotebook" {
		t.Errorf("path: got %q", cap.path)
	}
	if cap.auth != "Token test-token" {
		t.Errorf("auth: got %q, want 'Token test-token'", cap.auth)
	}
	if v, ok := cap.body["name"]; !ok || v != "My Notebook" {
		t.Errorf("body.name: got %v, want 'My Notebook'", v)
	}
	if nb.ID != "nb-new" {
		t.Errorf("notebook ID: got %q, want 'nb-new'", nb.ID)
	}
}

func TestRemoveNotebook_Success(t *testing.T) {
	var cap capturedRequest
	server := mockServer(t, 0, nil, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.RemoveNotebook(context.Background(), "nb-to-remove")
	if err != nil {
		t.Fatalf("RemoveNotebook failed: %v", err)
	}
	if cap.path != "/api/notebook/removeNotebook" {
		t.Errorf("path: got %q", cap.path)
	}
	if v, ok := cap.body["notebook"]; !ok || v != "nb-to-remove" {
		t.Errorf("body.notebook: got %v, want 'nb-to-remove'", v)
	}
}

func TestCreateDocWithMd_Success(t *testing.T) {
	var cap capturedRequest
	server := mockServer(t, 0, "doc-123", &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	id, err := client.CreateDocWithMd(context.Background(), "nb1", "/path/doc.md", "# Hello")
	if err != nil {
		t.Fatalf("CreateDocWithMd failed: %v", err)
	}
	if cap.path != "/api/filetree/createDocWithMd" {
		t.Errorf("path: got %q", cap.path)
	}
	if id != "doc-123" {
		t.Errorf("id: got %q, want 'doc-123'", id)
	}
	if v, _ := cap.body["notebook"]; v != "nb1" {
		t.Errorf("body.notebook: got %v, want 'nb1'", v)
	}
	if v, _ := cap.body["path"]; v != "/path/doc.md" {
		t.Errorf("body.path: got %v", v)
	}
	if v, _ := cap.body["markdown"]; v != "# Hello" {
		t.Errorf("body.markdown: got %v", v)
	}
}

func TestRemoveDocByID_Success(t *testing.T) {
	var cap capturedRequest
	server := mockServer(t, 0, nil, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.RemoveDocByID(context.Background(), "doc-to-remove")
	if err != nil {
		t.Fatalf("RemoveDocByID failed: %v", err)
	}
	if cap.path != "/api/filetree/removeDocByID" {
		t.Errorf("path: got %q", cap.path)
	}
	if v, _ := cap.body["id"]; v != "doc-to-remove" {
		t.Errorf("body.id: got %v, want 'doc-to-remove'", v)
	}
}

func TestRenameDoc_Success(t *testing.T) {
	var cap capturedRequest
	server := mockServer(t, 0, nil, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.RenameDoc(context.Background(), "nb1", "/old/path", "New Title")
	if err != nil {
		t.Fatalf("RenameDoc failed: %v", err)
	}
	if cap.path != "/api/filetree/renameDoc" {
		t.Errorf("path: got %q", cap.path)
	}
	if v, _ := cap.body["notebook"]; v != "nb1" {
		t.Errorf("body.notebook: got %v, want 'nb1'", v)
	}
	if v, _ := cap.body["title"]; v != "New Title" {
		t.Errorf("body.title: got %v", v)
	}
}

func TestGetIDsByHPath_Success(t *testing.T) {
	expected := []string{"doc-1", "doc-2"}
	var cap capturedRequest
	server := mockServer(t, 0, expected, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ids, err := client.GetIDsByHPath(context.Background(), "nb1", "/some/path")
	if err != nil {
		t.Fatalf("GetIDsByHPath failed: %v", err)
	}
	if cap.path != "/api/filetree/getIDsByHPath" {
		t.Errorf("path: got %q", cap.path)
	}
	if len(ids) != 2 || ids[0] != "doc-1" || ids[1] != "doc-2" {
		t.Errorf("ids: got %v, want [doc-1 doc-2]", ids)
	}
	if v, _ := cap.body["notebook"]; v != "nb1" {
		t.Errorf("body.notebook: got %v", v)
	}
}

func TestListDocTree_Success(t *testing.T) {
	expected := []types.TreeNode{
		{ID: "n1", Name: "Doc 1", Path: "/n1.sy"},
		{ID: "n2", Name: "Folder", Path: "/n2",
			Children: []types.TreeNode{
				{ID: "n3", Name: "Child", Path: "/n3.sy"},
			},
		},
	}
	var cap capturedRequest
	server := mockServer(t, 0, map[string]any{"files": expected}, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	tree, err := client.ListDocTree(context.Background(), "nb1", "/")
	if err != nil {
		t.Fatalf("ListDocTree failed: %v", err)
	}
	if cap.path != "/api/filetree/listDocsByPath" {
		t.Errorf("path: got %q", cap.path)
	}
	if len(tree) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(tree))
	}
	if tree[1].Children[0].ID != "n3" {
		t.Errorf("nested child ID: got %q, want 'n3'", tree[1].Children[0].ID)
	}
	if !tree[0].IsDoc() {
		t.Errorf("Doc 1 should be a document")
	}
	if tree[1].IsDoc() {
		t.Errorf("Folder should not be a document")
	}
}

func TestUpdateBlock_Success(t *testing.T) {
	var cap capturedRequest
	server := mockServer(t, 0, nil, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.UpdateBlock(context.Background(), "block-1", "**updated**")
	if err != nil {
		t.Fatalf("UpdateBlock failed: %v", err)
	}
	if cap.path != "/api/block/updateBlock" {
		t.Errorf("path: got %q", cap.path)
	}
	if v, _ := cap.body["id"]; v != "block-1" {
		t.Errorf("body.id: got %v", v)
	}
	if v, _ := cap.body["dataType"]; v != "markdown" {
		t.Errorf("body.dataType: got %v, want 'markdown'", v)
	}
	if v, _ := cap.body["data"]; v != "**updated**" {
		t.Errorf("body.data: got %v", v)
	}
}

func TestDeleteBlock_Success(t *testing.T) {
	var cap capturedRequest
	server := mockServer(t, 0, nil, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.DeleteBlock(context.Background(), "block-to-delete")
	if err != nil {
		t.Fatalf("DeleteBlock failed: %v", err)
	}
	if cap.path != "/api/block/deleteBlock" {
		t.Errorf("path: got %q", cap.path)
	}
	if v, _ := cap.body["id"]; v != "block-to-delete" {
		t.Errorf("body.id: got %v", v)
	}
}

func TestExportMdContent_Success(t *testing.T) {
	expected := types.ExportResult{ID: "doc-1", Content: "# Hello\nWorld", HPath: "/doc.md"}
	var cap capturedRequest
	server := mockServer(t, 0, expected, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	result, err := client.ExportMdContent(context.Background(), "doc-1")
	if err != nil {
		t.Fatalf("ExportMdContent failed: %v", err)
	}
	if cap.path != "/api/export/exportMdContent" {
		t.Errorf("path: got %q", cap.path)
	}
	if result.ID != "doc-1" || result.Content != "# Hello\nWorld" {
		t.Errorf("result: got %+v", result)
	}
}

func TestSetBlockAttrs_Success(t *testing.T) {
	var cap capturedRequest
	server := mockServer(t, 0, nil, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	attrs := map[string]string{"tag": "go", "priority": "high"}
	err := client.SetBlockAttrs(context.Background(), "block-1", attrs)
	if err != nil {
		t.Fatalf("SetBlockAttrs failed: %v", err)
	}
	if cap.path != "/api/attr/setBlockAttrs" {
		t.Errorf("path: got %q", cap.path)
	}
	if v, _ := cap.body["id"]; v != "block-1" {
		t.Errorf("body.id: got %v", v)
	}
	bodyAttrs, ok := cap.body["attrs"].(map[string]any)
	if !ok {
		t.Fatal("body.attrs not a map")
	}
	if bodyAttrs["tag"] != "go" || bodyAttrs["priority"] != "high" {
		t.Errorf("body.attrs: got %v", bodyAttrs)
	}
}

func TestGetBlockAttrs_Success(t *testing.T) {
	expected := map[string]any{"id": "block-1", "attrs": map[string]any{"tag": "go", "priority": "high"}}
	var cap capturedRequest
	server := mockServer(t, 0, expected, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	attrs, err := client.GetBlockAttrs(context.Background(), "block-1")
	if err != nil {
		t.Fatalf("GetBlockAttrs failed: %v", err)
	}
	if cap.path != "/api/attr/getBlockAttrs" {
		t.Errorf("path: got %q", cap.path)
	}
	if attrs["tag"] != "go" || attrs["priority"] != "high" {
		t.Errorf("attrs: got %v", attrs)
	}
}

func TestSQLQuery_Success(t *testing.T) {
	expected := []map[string]any{
		{"id": "doc-1", "title": "First"},
		{"id": "doc-2", "title": "Second"},
	}
	var cap capturedRequest
	server := mockServer(t, 0, expected, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	results, err := client.SQLQuery(context.Background(), "SELECT * FROM blocks")
	if err != nil {
		t.Fatalf("SQLQuery failed: %v", err)
	}
	if cap.path != "/api/query/sql" {
		t.Errorf("path: got %q", cap.path)
	}
	if v, _ := cap.body["stmt"]; v != "SELECT * FROM blocks" {
		t.Errorf("body.stmt: got %v", v)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}
	if results[0]["title"] != "First" {
		t.Errorf("results[0][title]: got %v", results[0]["title"])
	}
}

func TestAPIError_NonZeroCode(t *testing.T) {
	server := mockServer(t, 404, nil, nil)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.ListNotebooks(context.Background())
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 404 {
		t.Errorf("expected code 404, got %d", apiErr.Code)
	}
	if !strings.Contains(apiErr.Error(), "404") {
		t.Errorf("error message missing code: %s", apiErr.Error())
	}
}

func TestAPIError_MessageIncluded(t *testing.T) {
	server := mockServer(t, 500, nil, nil)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.ListNotebooks(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "test message") {
		t.Errorf("error message should contain 'test message': %s", err.Error())
	}
}

func TestNetworkTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	client.httpClient.Timeout = 50 * time.Millisecond

	_, err := client.ListNotebooks(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "http request") && !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected timeout-related error, got: %v", err)
	}
}

func TestInvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>not json</html>"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.ListNotebooks(context.Background())
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("expected 'parse response' error, got: %v", err)
	}
}

func TestContextCancellation(t *testing.T) {
	server := mockServer(t, 0, map[string]any{"notebooks": []types.Notebook{}}, nil)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ListNotebooks(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestAllEndpointPaths(t *testing.T) {
	tests := []struct {
		name     string
		call     func(c *Client, cap *capturedRequest) error
		wantPath string
	}{
		{
			name: "ListNotebooks",
			call: func(c *Client, cap *capturedRequest) error {
				var result struct {
					Notebooks []types.Notebook `json:"notebooks"`
				}
				return c.doRequest(context.Background(), "/api/notebook/lsNotebooks", map[string]string{}, &result)
			},
			wantPath: "/api/notebook/lsNotebooks",
		},
		{
			name: "CreateNotebook",
			call: func(c *Client, cap *capturedRequest) error {
				var nb types.Notebook
				return c.doRequest(context.Background(), "/api/notebook/createNotebook", map[string]string{"notebook": "x"}, &nb)
			},
			wantPath: "/api/notebook/createNotebook",
		},
		{
			name: "RemoveNotebook",
			call: func(c *Client, cap *capturedRequest) error {
				return c.doRequest(context.Background(), "/api/notebook/removeNotebook", map[string]string{"notebook": "x"}, nil)
			},
			wantPath: "/api/notebook/removeNotebook",
		},
		{
			name: "CreateDocWithMd",
			call: func(c *Client, cap *capturedRequest) error {
				var result struct {
					ID string `json:"id"`
				}
				return c.doRequest(context.Background(), "/api/filetree/createDocWithMd", map[string]string{"notebook": "x", "path": "/", "markdown": "md"}, &result)
			},
			wantPath: "/api/filetree/createDocWithMd",
		},
		{
			name: "RemoveDocByID",
			call: func(c *Client, cap *capturedRequest) error {
				return c.doRequest(context.Background(), "/api/filetree/removeDocByID", map[string]string{"id": "x"}, nil)
			},
			wantPath: "/api/filetree/removeDocByID",
		},
		{
			name: "RenameDoc",
			call: func(c *Client, cap *capturedRequest) error {
				return c.doRequest(context.Background(), "/api/filetree/renameDoc", map[string]string{"notebook": "x", "path": "/", "title": "t"}, nil)
			},
			wantPath: "/api/filetree/renameDoc",
		},
		{
			name: "GetIDsByHPath",
			call: func(c *Client, cap *capturedRequest) error {
				var ids []string
				return c.doRequest(context.Background(), "/api/filetree/getIDsByHPath", map[string]string{"notebook": "x", "path": "/"}, &ids)
			},
			wantPath: "/api/filetree/getIDsByHPath",
		},
		{
			name: "ListDocTree",
			call: func(c *Client, cap *capturedRequest) error {
				var result struct {
					Tree []types.TreeNode `json:"tree"`
				}
				return c.doRequest(context.Background(), "/api/filetree/listDocsByPath", map[string]string{"notebook": "x", "path": "/"}, &result)
			},
			wantPath: "/api/filetree/listDocsByPath",
		},
		{
			name: "UpdateBlock",
			call: func(c *Client, cap *capturedRequest) error {
				return c.doRequest(context.Background(), "/api/block/updateBlock", map[string]string{"id": "x", "dataType": "markdown", "data": "md"}, nil)
			},
			wantPath: "/api/block/updateBlock",
		},
		{
			name: "DeleteBlock",
			call: func(c *Client, cap *capturedRequest) error {
				return c.doRequest(context.Background(), "/api/block/deleteBlock", map[string]string{"id": "x"}, nil)
			},
			wantPath: "/api/block/deleteBlock",
		},
		{
			name: "ExportMdContent",
			call: func(c *Client, cap *capturedRequest) error {
				var result types.ExportResult
				return c.doRequest(context.Background(), "/api/export/exportMdContent", map[string]string{"id": "x"}, &result)
			},
			wantPath: "/api/export/exportMdContent",
		},
		{
			name: "SetBlockAttrs",
			call: func(c *Client, cap *capturedRequest) error {
				return c.doRequest(context.Background(), "/api/attr/setBlockAttrs", map[string]any{"id": "x", "attrs": map[string]string{}}, nil)
			},
			wantPath: "/api/attr/setBlockAttrs",
		},
		{
			name: "GetBlockAttrs",
			call: func(c *Client, cap *capturedRequest) error {
				var result struct {
					ID    string            `json:"id"`
					Attrs map[string]string `json:"attrs"`
				}
				return c.doRequest(context.Background(), "/api/attr/getBlockAttrs", map[string]string{"id": "x"}, &result)
			},
			wantPath: "/api/attr/getBlockAttrs",
		},
		{
			name: "SQLQuery",
			call: func(c *Client, cap *capturedRequest) error {
				var result []map[string]any
				return c.doRequest(context.Background(), "/api/query/sql", map[string]string{"stmt": "SELECT 1"}, &result)
			},
			wantPath: "/api/query/sql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cap capturedRequest
			server := mockServer(t, 0, nil, &cap)
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			err := tt.call(client, &cap)
			if err != nil {
				t.Fatalf("call failed: %v", err)
			}
			if cap.path != tt.wantPath {
				t.Errorf("path mismatch: got %q, want %q", cap.path, tt.wantPath)
			}
			if cap.auth != "Token test-token" {
				t.Errorf("auth header missing or wrong: got %q", cap.auth)
			}
		})
	}
}
