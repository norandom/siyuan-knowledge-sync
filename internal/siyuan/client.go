package siyuan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"siyuan-knowledge-sync/internal/types"
)

type Client struct {
	httpClient   *http.Client
	baseURL      string
	token        string
	extraHeaders map[string]string
}

func NewClient(endpoint, token string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    endpoint,
		token:      token,
	}
}

// SetHeader registers an extra HTTP header sent on every request. Used for
// Cloudflare Access service tokens (CF-Access-Client-Id/Secret) when the
// SiYuan endpoint sits behind Cloudflare Access.
func (c *Client) SetHeader(key, value string) {
	if c.extraHeaders == nil {
		c.extraHeaders = make(map[string]string)
	}
	c.extraHeaders[key] = value
}

type APIError struct {
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("siyuan api error (code=%d): %s", e.Code, e.Msg)
}

// CloudflareAccessError indicates the SiYuan endpoint sits behind Cloudflare
// Access and the request was met with an Access challenge rather than a SiYuan
// API response (no valid service-token credentials were presented). It is
// actionable and never carries credential values.
type CloudflareAccessError struct {
	Endpoint string
}

func (e *CloudflareAccessError) Error() string {
	return fmt.Sprintf("siyuan endpoint %s requires Cloudflare Access; set cf_access_client_id/cf_access_client_secret in the config", e.Endpoint)
}

// hasCloudflareAccessHost reports whether the URL's host is a Cloudflare Access
// login host (*.cloudflareaccess.com). nil-safe.
func hasCloudflareAccessHost(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "cloudflareaccess.com" || strings.HasSuffix(host, ".cloudflareaccess.com")
}

// hasCloudflareAccessMarkers reports whether the response carries signals of a
// Cloudflare Access challenge: a final or redirected URL on a
// *.cloudflareaccess.com host, a Cloudflare challenge/mitigation header, a
// CF-Access-* / cf-ray response header, or an empty body on a 401/403.
func hasCloudflareAccessMarkers(resp *http.Response, body []byte) bool {
	if resp == nil {
		return false
	}

	// Walk the response chain: the current response plus every prior
	// response in the redirect history (Go's http.Client follows redirects
	// by default and links them via Request.Response). The CF Access
	// challenge markers (Location to the login host, Cf-* headers) typically
	// live on the intermediate redirect response, not the final one.
	for r := resp; r != nil; {
		// Final/requested URL on a *.cloudflareaccess.com host.
		if r.Request != nil && hasCloudflareAccessHost(r.Request.URL) {
			return true
		}
		// Redirect Location pointing at a CF Access login host.
		if loc := r.Header.Get("Location"); loc != "" {
			if locURL, err := url.Parse(loc); err == nil && hasCloudflareAccessHost(locURL) {
				return true
			}
		}
		// Cloudflare Access / challenge response headers.
		if r.Header.Get("Cf-Mitigated") != "" || r.Header.Get("Cf-Ray") != "" {
			return true
		}
		for k := range r.Header {
			lk := strings.ToLower(k)
			if strings.HasPrefix(lk, "cf-access") || lk == "cf-mitigated" {
				return true
			}
		}
		if r.Request == nil {
			break
		}
		r = r.Request.Response
	}

	// Empty body on an auth-challenge status: classic silent CF Access block.
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && len(bytes.TrimSpace(body)) == 0 {
		return true
	}

	return false
}

func (c *Client) doRequest(ctx context.Context, path string, reqBody, respData any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+c.token)
	for k, v := range c.extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Classify the response before attempting to decode the SiYuan
	// envelope. A Cloudflare Access challenge (or any non-SiYuan gateway
	// response) is not JSON, and decoding it would surface the opaque
	// "unexpected end of JSON input" error. We never include credential
	// header values in any error produced here (Req 12.5).
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	decodeErr := json.Unmarshal(respBytes, &envelope)

	contentType := resp.Header.Get("Content-Type")
	looksJSON := strings.Contains(strings.ToLower(contentType), "json")

	if !looksJSON || decodeErr != nil {
		if hasCloudflareAccessMarkers(resp, respBytes) {
			return &CloudflareAccessError{Endpoint: c.baseURL}
		}
		ct := contentType
		if ct == "" {
			ct = "(none)"
		}
		return fmt.Errorf("siyuan endpoint returned a non-JSON response (HTTP %d, content-type %q); expected a SiYuan API envelope", resp.StatusCode, ct)
	}

	if envelope.Code != 0 {
		return &APIError{Code: envelope.Code, Msg: envelope.Msg}
	}

	if respData != nil {
		if err := json.Unmarshal(envelope.Data, respData); err != nil {
			return fmt.Errorf("parse data: %w", err)
		}
	}

	return nil
}

func (c *Client) ListNotebooks(ctx context.Context) ([]types.Notebook, error) {
	var result struct {
		Notebooks []types.Notebook `json:"notebooks"`
	}
	err := c.doRequest(ctx, "/api/notebook/lsNotebooks", map[string]string{}, &result)
	return result.Notebooks, err
}

func (c *Client) CreateNotebook(ctx context.Context, name string) (*types.Notebook, error) {
	var result struct {
		Notebook types.Notebook `json:"notebook"`
	}
	err := c.doRequest(ctx, "/api/notebook/createNotebook", map[string]string{"name": name}, &result)
	if err != nil {
		return nil, err
	}
	return &result.Notebook, nil
}

func (c *Client) RemoveNotebook(ctx context.Context, id string) error {
	return c.doRequest(ctx, "/api/notebook/removeNotebook", map[string]string{"notebook": id}, nil)
}

func (c *Client) CreateDocWithMd(ctx context.Context, notebookID, hpath, markdown string) (string, error) {
	var docID string
	err := c.doRequest(ctx, "/api/filetree/createDocWithMd", map[string]string{
		"notebook": notebookID,
		"path":     hpath,
		"markdown": markdown,
	}, &docID)
	return docID, err
}


func (c *Client) RemoveDocByID(ctx context.Context, id string) error {
	return c.doRequest(ctx, "/api/filetree/removeDocByID", map[string]string{"id": id}, nil)
}

func (c *Client) RenameDoc(ctx context.Context, notebookID, path, title string) error {
	return c.doRequest(ctx, "/api/filetree/renameDoc", map[string]string{
		"notebook": notebookID,
		"path":     path,
		"title":    title,
	}, nil)
}

func (c *Client) GetIDsByHPath(ctx context.Context, notebookID, hpath string) ([]string, error) {
	var ids []string
	err := c.doRequest(ctx, "/api/filetree/getIDsByHPath", map[string]string{
		"notebook": notebookID,
		"path":     hpath,
	}, &ids)
	return ids, err
}

func (c *Client) ListDocTree(ctx context.Context, notebookID, path string) ([]types.TreeNode, error) {
	var result struct {
		Files []types.TreeNode `json:"files"`
	}
	err := c.doRequest(ctx, "/api/filetree/listDocsByPath", map[string]string{
		"notebook": notebookID,
		"path":     path,
	}, &result)
	return result.Files, err
}

func (c *Client) UpdateBlock(ctx context.Context, id, markdown string) error {
	return c.doRequest(ctx, "/api/block/updateBlock", map[string]string{
		"id":       id,
		"dataType": "markdown",
		"data":     markdown,
	}, nil)
}

func (c *Client) DeleteBlock(ctx context.Context, id string) error {
	return c.doRequest(ctx, "/api/block/deleteBlock", map[string]string{"id": id}, nil)
}

func (c *Client) ExportMdContent(ctx context.Context, id string) (*types.ExportResult, error) {
	var result types.ExportResult
	err := c.doRequest(ctx, "/api/export/exportMdContent", map[string]string{"id": id}, &result)
	if err != nil {
		return nil, err
	}
	result.ID = id
	return &result, nil
}

func (c *Client) SetBlockAttrs(ctx context.Context, id string, attrs map[string]string) error {
	return c.doRequest(ctx, "/api/attr/setBlockAttrs", map[string]any{
		"id":    id,
		"attrs": attrs,
	}, nil)
}

func (c *Client) GetBlockAttrs(ctx context.Context, id string) (map[string]string, error) {
	var result struct {
		ID    string            `json:"id"`
		Attrs map[string]string `json:"attrs"`
	}
	err := c.doRequest(ctx, "/api/attr/getBlockAttrs", map[string]string{"id": id}, &result)
	if err != nil {
		return nil, err
	}
	return result.Attrs, nil
}

func (c *Client) SQLQuery(ctx context.Context, stmt string) ([]map[string]any, error) {
	var result []map[string]any
	err := c.doRequest(ctx, "/api/query/sql", map[string]string{"stmt": stmt}, &result)
	return result, err
}
