package types

import (
	"strings"
	"time"
)

// APIEnvelope is the generic SiYuan API response wrapper.
// All SiYuan API endpoints return {code, msg, data}.
type APIEnvelope[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

// Notebook represents a SiYuan notebook.
type Notebook struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Icon   string `json:"icon"`
	Sort   int    `json:"sort"`
	Closed bool   `json:"closed"`
}

// Document represents SiYuan document metadata.
type Document struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Icon       string `json:"icon,omitempty"`
	HPath      string `json:"hpath"`
	NotebookID string `json:"box"`
	IAL        string `json:"ial,omitempty"`
}

// TreeNode represents a node in the SiYuan document tree.
type TreeNode struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Children []TreeNode `json:"children,omitempty"`
}

func (n *TreeNode) IsDoc() bool {
	return strings.HasSuffix(n.Path, ".sy")
}

// ExportResult contains exported markdown content from SiYuan.
type ExportResult struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	HPath   string `json:"hPath"`
}

// SyncEntry maps a local file path to a SiYuan document.
type SyncEntry struct {
	LocalPath  string    `json:"local_path"`
	SiYuanID   string    `json:"siyuan_id"`
	NotebookID string    `json:"notebook_id"`
	SyncedAt   time.Time `json:"synced_at"`
}

// SyncState is the persistent sync state mapping local paths to SiYuan documents.
type SyncState struct {
	Entries map[string]SyncEntry `json:"entries"`
}

// TrackedFile represents a git-tracked markdown file.
type TrackedFile struct {
	Path    string
	ModTime time.Time
}

// ComplianceIssue represents a SiYuan compliance audit finding.
type ComplianceIssue struct {
	File     string
	Line     int
	Severity string // "error", "warning"
	Message  string
	Fixable  bool
}

// SyncReport summarizes the results of a sync operation.
type SyncReport struct {
	Created []string
	Updated []string
	Pruned  []string
	Errors  []SyncError
}

// SyncError represents an individual error within a sync report.
type SyncError struct {
	File    string
	Message string
}

// BlockAttrs represents SiYuan block attribute key-value pairs.
type BlockAttrs map[string]string

// SQLResult represents a row from a SiYuan SQL query result.
type SQLResult map[string]any
