package siyuan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return fmt.Errorf("parse response: %w", err)
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
