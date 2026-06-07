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

// TestLoadConfig_OntologySectionAbsent_FieldNil verifies that when the YAML
// file omits the `ontology:` section entirely, the decoded *Config has a nil
// Ontology pointer. This is the signal main.go uses to skip ontology.Configure
// and keep the compile-time default schema in effect (Requirement 1.2).
func TestLoadConfig_OntologySectionAbsent_FieldNil(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
token: "abc123"
repo_path: "/notes"
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ontology != nil {
		t.Errorf("cfg.Ontology = %+v, want nil (section absent)", cfg.Ontology)
	}
	// Existing fields must still decode correctly.
	if cfg.Endpoint != "https://example.com" {
		t.Errorf("endpoint = %q, want %q", cfg.Endpoint, "https://example.com")
	}
	if cfg.Token != "abc123" {
		t.Errorf("token = %q, want %q", cfg.Token, "abc123")
	}
	if cfg.RepoPath != "/notes" {
		t.Errorf("repo_path = %q, want %q", cfg.RepoPath, "/notes")
	}
}

// TestLoadConfig_OntologySectionPresent_FullShape verifies the YAML decode
// of a complete `ontology:` section: two domains, two intents, two tags.
// Order must be preserved literally (Requirements 1.1, 2.5, 3.3).
func TestLoadConfig_OntologySectionPresent_FullShape(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
token: "abc123"
repo_path: "/notes"
ontology:
  domains:
    - id: devops
      folder: "Linux & DevOps"
    - id: forensics
      folder: "Digital Forensics"
  intents:
    - id: config
    - id: sop
  tags:
    - claude
    - mcp
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ontology == nil {
		t.Fatal("cfg.Ontology = nil, want non-nil (section present)")
	}
	if len(cfg.Ontology.Domains) != 2 {
		t.Fatalf("len(domains) = %d, want 2", len(cfg.Ontology.Domains))
	}
	if cfg.Ontology.Domains[0].ID != "devops" {
		t.Errorf("domains[0].ID = %q, want %q", cfg.Ontology.Domains[0].ID, "devops")
	}
	if cfg.Ontology.Domains[0].Folder != "Linux & DevOps" {
		t.Errorf("domains[0].Folder = %q, want %q", cfg.Ontology.Domains[0].Folder, "Linux & DevOps")
	}
	if cfg.Ontology.Domains[1].ID != "forensics" {
		t.Errorf("domains[1].ID = %q, want %q", cfg.Ontology.Domains[1].ID, "forensics")
	}
	if cfg.Ontology.Domains[1].Folder != "Digital Forensics" {
		t.Errorf("domains[1].Folder = %q, want %q", cfg.Ontology.Domains[1].Folder, "Digital Forensics")
	}
	if len(cfg.Ontology.Intents) != 2 {
		t.Fatalf("len(intents) = %d, want 2", len(cfg.Ontology.Intents))
	}
	if cfg.Ontology.Intents[0].ID != "config" {
		t.Errorf("intents[0].ID = %q, want %q", cfg.Ontology.Intents[0].ID, "config")
	}
	if cfg.Ontology.Intents[1].ID != "sop" {
		t.Errorf("intents[1].ID = %q, want %q", cfg.Ontology.Intents[1].ID, "sop")
	}
	if len(cfg.Ontology.Tags) != 2 {
		t.Fatalf("len(tags) = %d, want 2", len(cfg.Ontology.Tags))
	}
	if cfg.Ontology.Tags[0] != "claude" || cfg.Ontology.Tags[1] != "mcp" {
		t.Errorf("tags = %v, want [claude mcp]", cfg.Ontology.Tags)
	}
}

// TestLoadConfig_OntologySectionWithoutTags_TagsNil verifies that omitting
// the `tags:` key inside an otherwise-present `ontology:` section yields a
// nil Tags slice. This is the open-vocabulary signal preserved through
// decode (Requirement 4.3).
func TestLoadConfig_OntologySectionWithoutTags_TagsNil(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
token: "abc123"
repo_path: "/notes"
ontology:
  domains:
    - id: devops
      folder: "Linux & DevOps"
  intents:
    - id: config
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ontology == nil {
		t.Fatal("cfg.Ontology = nil, want non-nil (section present)")
	}
	if cfg.Ontology.Tags != nil {
		t.Errorf("cfg.Ontology.Tags = %v, want nil (tags key absent → open vocabulary)", cfg.Ontology.Tags)
	}
}

// TestLoadConfig_OntologySectionWithEmptyTags_TagsNonNilEmpty verifies that
// an explicit empty list `tags: []` decodes to a non-nil but length-zero
// slice. This nil-vs-non-nil-empty distinction is load-bearing for
// Task 4.1's translation into ConfigureOptions.Tags (Requirement 4.1).
func TestLoadConfig_OntologySectionWithEmptyTags_TagsNonNilEmpty(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
token: "abc123"
repo_path: "/notes"
ontology:
  domains:
    - id: devops
      folder: "Linux & DevOps"
  intents:
    - id: config
  tags: []
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ontology == nil {
		t.Fatal("cfg.Ontology = nil, want non-nil (section present)")
	}
	if cfg.Ontology.Tags == nil {
		t.Errorf("cfg.Ontology.Tags = nil, want non-nil empty slice (explicit `tags: []`)")
	}
	if len(cfg.Ontology.Tags) != 0 {
		t.Errorf("len(cfg.Ontology.Tags) = %d, want 0", len(cfg.Ontology.Tags))
	}
}

// TestLoadConfig_OntologySectionWithGarbage_DoesNotErrorAtLoadTime verifies
// that LoadConfig only decodes — validation (charset, duplicate ids, reserved
// prefixes) belongs to ontology.Configure() called later from main.go.
// A YAML section that would fail validation must still decode successfully.
func TestLoadConfig_OntologySectionWithGarbage_DoesNotErrorAtLoadTime(t *testing.T) {
	p := writeTemp(t, ".siyuan-sync.yaml", `endpoint: "https://example.com"
token: "abc123"
repo_path: "/notes"
ontology:
  domains:
    - id: DevOps
      folder: "_reserved"
    - id: DevOps
      folder: "/absolute"
  intents:
    - id: config
    - id: config
  tags:
    - claude
    - claude
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig must not validate ontology contents, got error: %v", err)
	}
	if cfg.Ontology == nil {
		t.Fatal("cfg.Ontology = nil, want non-nil (section present, even if invalid)")
	}
	if len(cfg.Ontology.Domains) != 2 {
		t.Errorf("len(domains) = %d, want 2 (garbage values decode literally)", len(cfg.Ontology.Domains))
	}
}

// TestLoadConfig_RequiredFieldsStillValidated_WhenOntologyMissing confirms
// the pre-existing required-field error path still fires when ontology is
// absent. A regression check that adding the new field did not alter the
// existing validation order.
func TestLoadConfig_RequiredFieldsStillValidated_WhenOntologyMissing(t *testing.T) {
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
}
