package siyuan

import (
	"context"
	"encoding/json"
	"errors"
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

func TestRenameDocByID_Success(t *testing.T) {
	var cap capturedRequest
	server := mockServer(t, 0, nil, &cap)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.RenameDocByID(context.Background(), "doc-123", "New Title")
	if err != nil {
		t.Fatalf("RenameDocByID failed: %v", err)
	}
	if cap.path != "/api/filetree/renameDocByID" {
		t.Errorf("path: got %q, want /api/filetree/renameDocByID", cap.path)
	}
	if cap.auth != "Token test-token" {
		t.Errorf("auth: got %q, want 'Token test-token'", cap.auth)
	}
	if v, _ := cap.body["id"]; v != "doc-123" {
		t.Errorf("body.id: got %v, want 'doc-123'", v)
	}
	if v, _ := cap.body["title"]; v != "New Title" {
		t.Errorf("body.title: got %v, want 'New Title'", v)
	}
}

func TestRenameDocByID_APIError(t *testing.T) {
	server := mockServer(t, 500, nil, nil)
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	err := client.RenameDocByID(context.Background(), "doc-123", "New Title")
	if err == nil {
		t.Fatal("expected error for non-zero envelope code")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 500 {
		t.Errorf("expected code 500, got %d", apiErr.Code)
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
		t.Fatal("expected error for invalid JSON")
	}
	// Plain non-JSON 200 with no Cloudflare Access markers must NOT be
	// classified as a CF Access challenge.
	var cfErr *CloudflareAccessError
	if errors.As(err, &cfErr) {
		t.Errorf("plain non-JSON 200 should not be a CloudflareAccessError, got: %v", err)
	}
	// The generic error must be clearer than the previous opaque
	// "parse response: unexpected end of JSON input": it should name the
	// status code and content-type.
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("error should not be the opaque JSON-decode message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "200") {
		t.Errorf("generic error should include HTTP status 200, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "text/html") {
		t.Errorf("generic error should include content-type, got: %v", err)
	}
}

func TestCloudflareAccessError_Message(t *testing.T) {
	e := &CloudflareAccessError{Endpoint: "https://docs.example.com"}
	want := "siyuan endpoint https://docs.example.com requires Cloudflare Access; set cf_access_client_id/cf_access_client_secret in the config"
	if e.Error() != want {
		t.Errorf("CloudflareAccessError.Error()\n got:  %q\n want: %q", e.Error(), want)
	}
}

// TestCloudflareAccess_RedirectToAccessHost simulates a Cloudflare Access
// challenge: the protected endpoint 302-redirects to a *.cloudflareaccess.com
// login host. Go's http.Client follows the redirect, so the final response is
// a non-JSON login page served from the CF Access host.
func TestCloudflareAccess_RedirectToAccessHost(t *testing.T) {
	// CF Access login host (simulated). Its URL host is treated as the marker.
	cfAccess := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>Sign in with Cloudflare Access</body></html>"))
	}))
	defer cfAccess.Close()

	// Rewrite the simulated login host so it ends with cloudflareaccess.com.
	cfHost := strings.TrimPrefix(cfAccess.URL, "http://")
	protected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", cfAccess.URL+"/cdn-cgi/access/login")
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusFound)
	}))
	defer protected.Close()

	client := NewClient(protected.URL, "test-token")
	client.SetHeader("CF-Access-Client-Secret", "super-secret-value-DO-NOT-LEAK")
	_, err := client.ListNotebooks(context.Background())
	if err == nil {
		t.Fatal("expected CloudflareAccessError for CF Access challenge")
	}
	var cfErr *CloudflareAccessError
	if !errors.As(err, &cfErr) {
		t.Fatalf("expected *CloudflareAccessError, got %T: %v", err, err)
	}
	if cfErr.Endpoint != protected.URL {
		t.Errorf("CloudflareAccessError.Endpoint = %q, want %q", cfErr.Endpoint, protected.URL)
	}
	if strings.Contains(err.Error(), "super-secret-value-DO-NOT-LEAK") {
		t.Errorf("error must never contain the service-token secret: %v", err)
	}
	_ = cfHost
}

// TestCloudflareAccess_HTMLChallengeWithHeaderMarker covers the common case
// where the CF Access challenge is served directly (no observable redirect):
// non-JSON HTML body plus a CF marker header.
func TestCloudflareAccess_HTMLChallengeWithHeaderMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("<html><body>Cloudflare Access</body></html>"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	client.SetHeader("CF-Access-Client-Id", "client-id-value")
	client.SetHeader("CF-Access-Client-Secret", "secret-value-DO-NOT-LEAK")
	_, err := client.ListNotebooks(context.Background())
	if err == nil {
		t.Fatal("expected CloudflareAccessError")
	}
	var cfErr *CloudflareAccessError
	if !errors.As(err, &cfErr) {
		t.Fatalf("expected *CloudflareAccessError, got %T: %v", err, err)
	}
	if strings.Contains(err.Error(), "secret-value-DO-NOT-LEAK") || strings.Contains(err.Error(), "client-id-value") {
		t.Errorf("error must never contain CF credential values: %v", err)
	}
}

// TestCloudflareAccess_EmptyForbiddenBody covers a 403 with an empty body and
// no CF creds configured — treated as a CF Access challenge.
func TestCloudflareAccess_EmptyForbiddenBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		// empty body
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.ListNotebooks(context.Background())
	if err == nil {
		t.Fatal("expected CloudflareAccessError for empty 403 body")
	}
	var cfErr *CloudflareAccessError
	if !errors.As(err, &cfErr) {
		t.Fatalf("expected *CloudflareAccessError, got %T: %v", err, err)
	}
	if cfErr.Endpoint != server.URL {
		t.Errorf("Endpoint = %q, want %q", cfErr.Endpoint, server.URL)
	}
}

// TestNonChallengeNonJSON_GenericError ensures a plain non-JSON 200 body with
// no CF markers yields a clearer generic error (status + content-type) and
// never the configured service token.
func TestNonChallengeNonJSON_GenericError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("oops, something broke"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	client.SetHeader("CF-Access-Client-Secret", "leak-me-not-secret")
	_, err := client.ListNotebooks(context.Background())
	if err == nil {
		t.Fatal("expected generic error for non-JSON 200")
	}
	var cfErr *CloudflareAccessError
	if errors.As(err, &cfErr) {
		t.Errorf("non-CF non-JSON should not be CloudflareAccessError, got: %v", err)
	}
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("should not be the opaque JSON message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "200") {
		t.Errorf("generic error should include status 200, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "text/plain") {
		t.Errorf("generic error should include content-type, got: %v", err)
	}
	if strings.Contains(err.Error(), "leak-me-not-secret") {
		t.Errorf("error must never contain the service-token secret: %v", err)
	}
}

// TestCloudflareCredentialsNeverLeak is the consolidated Req 12.5 guard. It
// configures BOTH the CF-Access-Client-Id and CF-Access-Client-Secret service-
// token headers with distinctive sample values and exercises every error path
// doRequest can produce (Cloudflare Access challenge, generic non-JSON, and a
// normal SiYuan *APIError). For each path it asserts the produced error is the
// expected type AND that neither sample credential value appears anywhere in
// err.Error(). These are the meaningful guards for 12.5: if doRequest (or any
// future error-formatting change) ever interpolated request header values into
// an error, the strings.Contains assertions below would fail; if the CF
// classification in doRequest regressed, the per-path type assertions (errors.As
// for the challenge case, the type switch for APIError, the negative errors.As
// for the generic case) would fail.
func TestCloudflareCredentialsNeverLeak(t *testing.T) {
	const (
		sampleID     = "cf-access-client-id-SAMPLE-DO-NOT-LEAK"
		sampleSecret = "cf-access-client-secret-SAMPLE-DO-NOT-LEAK"
	)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		// check validates the error type for this path. It returns a short
		// description used only in failure messages.
		check func(t *testing.T, err error)
	}{
		{
			name: "cloudflare access challenge (403 + CF marker, non-JSON)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cf-Mitigated", "challenge")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte("<html><body>Cloudflare Access login</body></html>"))
			},
			check: func(t *testing.T, err error) {
				var cfErr *CloudflareAccessError
				if !errors.As(err, &cfErr) {
					t.Fatalf("expected *CloudflareAccessError, got %T: %v", err, err)
				}
			},
		},
		{
			name: "generic non-JSON response (200 text/plain, no CF markers)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("oops, something broke"))
			},
			check: func(t *testing.T, err error) {
				var cfErr *CloudflareAccessError
				if errors.As(err, &cfErr) {
					t.Fatalf("non-CF non-JSON must not be *CloudflareAccessError, got: %v", err)
				}
				if strings.Contains(err.Error(), "unexpected end of JSON input") {
					t.Errorf("should not be the opaque JSON message, got: %v", err)
				}
				if !strings.Contains(err.Error(), "200") {
					t.Errorf("generic error should include HTTP status 200, got: %v", err)
				}
			},
		},
		{
			name: "normal siyuan APIError (non-zero envelope code)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"code": 500,
					"msg":  "internal failure",
					"data": nil,
				})
			},
			check: func(t *testing.T, err error) {
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("expected *APIError, got %T: %v", err, err)
				}
				if apiErr.Code != 500 {
					t.Errorf("expected code 500, got %d", apiErr.Code)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			client.SetHeader("CF-Access-Client-Id", sampleID)
			client.SetHeader("CF-Access-Client-Secret", sampleSecret)

			_, err := client.ListNotebooks(context.Background())
			if err == nil {
				t.Fatal("expected an error for this path")
			}
			tt.check(t, err)

			msg := err.Error()
			if strings.Contains(msg, sampleID) {
				t.Errorf("error must never contain the CF-Access-Client-Id value; got: %v", err)
			}
			if strings.Contains(msg, sampleSecret) {
				t.Errorf("error must never contain the CF-Access-Client-Secret value; got: %v", err)
			}
		})
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

// TestUploadAsset_RoundTripsStoredPath asserts that UploadAsset POSTs a
// multipart/form-data request with the file under the `file[]` field and
// returns the SiYuan-assigned `assets/<orig>-<ts>-<rand>.<ext>` path from
// the succMap.
func TestUploadAsset_RoundTripsStoredPath(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := tmpDir + "/test.png"
	if err := writeFile(t, localPath, []byte("FAKE_PNG_BYTES")); err != nil {
		t.Fatal(err)
	}

	var capturedFilename, capturedContentType, capturedAuth, capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		capturedContentType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		files := r.MultipartForm.File["file[]"]
		if len(files) == 1 {
			capturedFilename = files[0].Filename
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"msg":"","data":{"errFiles":null,"succMap":{"test.png":"assets/test-20260606-abc.png"}}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	stored, err := client.UploadAsset(context.Background(), localPath)
	if err != nil {
		t.Fatalf("UploadAsset: %v", err)
	}
	if stored != "assets/test-20260606-abc.png" {
		t.Errorf("stored path = %q, want %q", stored, "assets/test-20260606-abc.png")
	}
	if capturedPath != "/api/asset/upload" {
		t.Errorf("path = %q, want /api/asset/upload", capturedPath)
	}
	if capturedAuth != "Token test-token" {
		t.Errorf("auth = %q, want %q", capturedAuth, "Token test-token")
	}
	if !strings.HasPrefix(capturedContentType, "multipart/form-data") {
		t.Errorf("content-type = %q, want multipart/form-data prefix", capturedContentType)
	}
	if capturedFilename != "test.png" {
		t.Errorf("filename = %q, want test.png", capturedFilename)
	}
}

// TestUploadAsset_APIErrorPropagated asserts SiYuan code != 0 surfaces as
// *APIError (caller can decide whether to make it fatal).
func TestUploadAsset_APIErrorPropagated(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := tmpDir + "/x.png"
	if err := writeFile(t, localPath, []byte("X")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":-1,"msg":"asset rejected","data":null}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.UploadAsset(context.Background(), localPath)
	if err == nil {
		t.Fatal("expected error for code -1")
	}
	apiErr := new(APIError)
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != -1 {
		t.Errorf("APIError.Code = %d, want -1", apiErr.Code)
	}
}

// TestUploadAsset_MissingFromSuccMap asserts that a success envelope
// without the file's basename in succMap surfaces as a descriptive error
// (defensive guard — SiYuan's contract says the basename is always there
// when code == 0, but partial-success edge cases should not silently
// return an empty stored path).
func TestUploadAsset_MissingFromSuccMap(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := tmpDir + "/y.png"
	if err := writeFile(t, localPath, []byte("Y")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"msg":"","data":{"errFiles":["y.png"],"succMap":{}}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	_, err := client.UploadAsset(context.Background(), localPath)
	if err == nil {
		t.Fatal("expected error when succMap lacks the basename")
	}
	if !strings.Contains(err.Error(), "y.png") || !strings.Contains(err.Error(), "succMap") {
		t.Errorf("error must reference the missing basename + succMap, got: %v", err)
	}
}

// writeFile is a test helper that writes content using os semantics.
// Kept local to avoid pulling os into the test imports above.
func writeFile(t *testing.T, path string, content []byte) error {
	t.Helper()
	return writeTestFile(path, content)
}
