package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return p
}

func TestLoadConfig_ValidFull(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
token: "abc123"
repo_path: "/notes"
autofix: true
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "https://example.com" {
		t.Errorf("endpoint = %q, want %q", cfg.Endpoint, "https://example.com")
	}
	if cfg.Token != "abc123" {
		t.Errorf("token = %q, want %q", cfg.Token, "abc123")
	}
	if cfg.RepoPath != "/notes" {
		t.Errorf("repo_path = %q, want %q", cfg.RepoPath, "/notes")
	}
	if !cfg.AutoFix {
		t.Errorf("autofix = false, want true")
	}
}

func TestLoadConfig_MissingEndpoint(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `token: "abc123"
repo_path: "/notes"
`)
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "endpoint") {
		t.Errorf("error should mention endpoint: %v", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("error should mention config file path: %v", err)
	}
}

func TestLoadConfig_MissingToken(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
repo_path: "/notes"
`)
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error should mention token: %v", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("error should mention config file path: %v", err)
	}
}

func TestLoadConfig_MissingRepoPath(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
token: "abc123"
`)
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "repo_path") {
		t.Errorf("error should mention repo_path: %v", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("error should mention config file path: %v", err)
	}
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", "")
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty: %v", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("error should mention config file path: %v", err)
	}
}

func TestLoadConfig_AutoFixDefaultsToFalse(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
token: "abc123"
repo_path: "/notes"
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AutoFix {
		t.Errorf("autofix = true, want false (default when omitted)")
	}
}

func TestLoadConfig_MalformedYAML(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", "this is not : valid ::: yaml {{{")
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parsing: %v", err)
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("error should mention config file path: %v", err)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/.siyuan-sync.yaml")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}
