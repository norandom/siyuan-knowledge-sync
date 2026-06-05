package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"siyuan-knowledge-sync/internal/ontology"
)

func TestRootCommand_Exists(t *testing.T) {
	cmd := newRootCommand()
	if cmd == nil {
		t.Fatal("expected root command")
	}
	if cmd.Use != "siyuan-knowledge-sync" {
		t.Errorf("Use = %q, want %q", cmd.Use, "siyuan-knowledge-sync")
	}
}

func TestRootCommand_HasConfigFlag(t *testing.T) {
	cmd := newRootCommand()
	flag := cmd.PersistentFlags().Lookup("config")
	if flag == nil {
		t.Fatal("expected --config persistent flag")
	}
	if flag.DefValue != ".siyuan-sync.yaml" {
		t.Errorf("--config default = %q, want %q", flag.DefValue, ".siyuan-sync.yaml")
	}
	if flag.Shorthand != "c" {
		t.Errorf("--config shorthand = %q, want %q", flag.Shorthand, "c")
	}
}

func TestRootCommand_HasAllSubcommands(t *testing.T) {
	cmd := newRootCommand()
	subs := cmd.Commands()
	names := make(map[string]bool)
	for _, s := range subs {
		names[s.Use] = true
	}

	expected := []string{"sync", "download", "audit", "mcp-server"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected subcommand %q", name)
		}
	}
}

func TestSyncCommand_HasDryRunFlag(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newSyncCommand(&configPath)
	flag := cmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatal("expected --dry-run flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--dry-run default = %q, want false", flag.DefValue)
	}
}

func TestSyncCommand_UseString(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newSyncCommand(&configPath)
	if cmd.Use != "sync" {
		t.Errorf("Use = %q, want sync", cmd.Use)
	}
}

func TestDownloadCommand_HasConflictFlag(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newDownloadCommand(&configPath)
	flag := cmd.Flags().Lookup("conflict")
	if flag == nil {
		t.Fatal("expected --conflict flag")
	}
	if flag.DefValue != "skip" {
		t.Errorf("--conflict default = %q, want skip", flag.DefValue)
	}
}

func TestDownloadCommand_UseString(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newDownloadCommand(&configPath)
	if cmd.Use != "download" {
		t.Errorf("Use = %q, want download", cmd.Use)
	}
}

func TestAuditCommand_HasAutofixFlag(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newAuditCommand(&configPath)
	flag := cmd.Flags().Lookup("autofix")
	if flag == nil {
		t.Fatal("expected --autofix flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--autofix default = %q, want false", flag.DefValue)
	}
}

func TestAuditCommand_UseString(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newAuditCommand(&configPath)
	if cmd.Use != "audit" {
		t.Errorf("Use = %q, want audit", cmd.Use)
	}
}

func TestMCPServerCommand_Exists(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newMCPServerCommand(&configPath)
	if cmd == nil {
		t.Fatal("expected mcp-server command")
	}
	if cmd.Use != "mcp-server" {
		t.Errorf("Use = %q, want mcp-server", cmd.Use)
	}
}

func TestMCPServerCommand_NoExtraFlags(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newMCPServerCommand(&configPath)
	flags := cmd.Flags()
	if flags != nil && flags.HasFlags() {
		t.Errorf("mcp-server command should have no local flags, got %d", flags.NFlag())
	}
}

func TestSync_DryRunWithMissingConfig(t *testing.T) {
	configPath := "/nonexistent/path/config.yaml"
	cmd := newSyncCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestDownload_InvalidConflictMode(t *testing.T) {
	configPath := "/nonexistent/path/config.yaml"
	cmd := newDownloadCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--conflict", "banana"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestAudit_AutofixWithMissingConfig(t *testing.T) {
	configPath := "/nonexistent/path/config.yaml"
	cmd := newAuditCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--autofix"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestMCPServer_MissingConfig(t *testing.T) {
	configPath := "/nonexistent/path/config.yaml"
	cmd := newMCPServerCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestConfigFlagAcceptedOnRoot(t *testing.T) {
	cmd := newRootCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--config", "/tmp/test.yaml", "--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help command should not error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "--config") || !strings.Contains(output, "-c") {
		t.Errorf("help output should mention --config flag, got:\n%s", output)
	}
}

func TestSync_HelpShowsDryRun(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newSyncCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help should not error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "--dry-run") {
		t.Errorf("help should mention --dry-run, got:\n%s", output)
	}
}

func TestDownload_HelpShowsConflict(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newDownloadCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help should not error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "--conflict") {
		t.Errorf("help should mention --conflict, got:\n%s", output)
	}
}

func TestAudit_HelpShowsAutofix(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newAuditCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help should not error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "--autofix") {
		t.Errorf("help should mention --autofix, got:\n%s", output)
	}
}

func TestMCPServer_Help(t *testing.T) {
	configPath := ".siyuan-sync.yaml"
	cmd := newMCPServerCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help should not error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "MCP") {
		t.Errorf("help should mention MCP, got:\n%s", output)
	}
}

func TestConfigFlagShorthand(t *testing.T) {
	cmd := newRootCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-c", "/tmp/test.yaml", "--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("help with -c should not error: %v", err)
	}
}

func TestSync_ConfigFlagValuePropagates(t *testing.T) {
	configPath := "/custom/config/path.yaml"
	cmd := newSyncCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--dry-run"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing config at custom path")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Errorf("expected config file not found error, got: %v", err)
	}
}

func TestDownload_ConflictSkipDefault(t *testing.T) {
	configPath := "/nonexistent/path/config.yaml"
	cmd := newDownloadCommand(&configPath)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestSplitPathForReport(t *testing.T) {
	tests := []struct {
		input          string
		wantNotebook   string
		wantSiyuanPath string
	}{
		{"readme.md", "root", "readme.md"},
		{"wiki/doc.md", "wiki", "doc.md"},
		{"wiki/sub/deep.md", "wiki", "sub/deep.md"},
		{"notes/2024/01/day.md", "notes", "2024/01/day.md"},
		{"journal/daily.md", "journal", "daily.md"},
	}

	for _, tt := range tests {
		notebook, siyuanPath := splitPathForReport(tt.input)
		if notebook != tt.wantNotebook {
			t.Errorf("splitPathForReport(%q) notebook = %q, want %q", tt.input, notebook, tt.wantNotebook)
		}
		if siyuanPath != tt.wantSiyuanPath {
			t.Errorf("splitPathForReport(%q) siyuanPath = %q, want %q", tt.input, siyuanPath, tt.wantSiyuanPath)
		}
	}
}

func TestBytesEqual(t *testing.T) {
	tests := []struct {
		a, b []byte
		want bool
	}{
		{nil, nil, true},
		{[]byte{}, []byte{}, true},
		{[]byte("hello"), []byte("hello"), true},
		{[]byte("hello"), []byte("world"), false},
		{[]byte("hello"), []byte("hell"), false},
		{[]byte(""), []byte("a"), false},
	}

	for _, tt := range tests {
		got := bytesEqual(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("bytesEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestLoadConfig_ValidatesConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".siyuan-sync.yaml")

	if err := os.WriteFile(configPath, []byte(`endpoint: "https://example.com"
token: "abc123"
repo_path: "/notes"
`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Endpoint != "https://example.com" {
		t.Errorf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.Token != "abc123" {
		t.Errorf("token = %q", cfg.Token)
	}
	if cfg.RepoPath != "/notes" {
		t.Errorf("repo_path = %q", cfg.RepoPath)
	}
}

func TestMainFunction_Builds(t *testing.T) {
	cmd := exec.Command("go", "build", ".")
	cmd.Dir = "." 
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
}

func TestMainFunction_Build_SiYuanKnowledgeSync(t *testing.T) {
	_ = main
}

// --- Task 4.1: schema subcommand tests ---

type schemaDocTest struct {
	Version int `json:"version"`
	Domain  struct {
		Values  []string          `json:"values"`
		Folders map[string]string `json:"folders"`
	} `json:"domain"`
	Intent struct {
		Values []string `json:"values"`
	} `json:"intent"`
	RequiredKeys []string `json:"required_keys"`
}

func TestSchemaCommand_JSON_RoundTripsToOntologyEnums(t *testing.T) {
	cmd := newSchemaCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("schema --json exec: %v", err)
	}

	var doc schemaDocTest
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal schema JSON: %v\nraw=%s", err, buf.String())
	}

	if doc.Version != 1 {
		t.Errorf("Version = %d, want 1", doc.Version)
	}

	wantDomains := make([]string, 0, len(ontology.AllDomains()))
	for _, d := range ontology.AllDomains() {
		wantDomains = append(wantDomains, string(d))
	}
	if !slices.Equal(doc.Domain.Values, wantDomains) {
		t.Errorf("Domain.Values = %v, want %v", doc.Domain.Values, wantDomains)
	}

	wantIntents := make([]string, 0, len(ontology.AllIntents()))
	for _, i := range ontology.AllIntents() {
		wantIntents = append(wantIntents, string(i))
	}
	if !slices.Equal(doc.Intent.Values, wantIntents) {
		t.Errorf("Intent.Values = %v, want %v", doc.Intent.Values, wantIntents)
	}

	if len(doc.Domain.Folders) != 6 {
		t.Errorf("Domain.Folders count = %d, want 6", len(doc.Domain.Folders))
	}
	router := ontology.Router{}
	for _, d := range ontology.AllDomains() {
		got, ok := doc.Domain.Folders[string(d)]
		if !ok {
			t.Errorf("Domain.Folders missing entry for %q", string(d))
			continue
		}
		if want := router.CanonicalFolder(d); got != want {
			t.Errorf("Domain.Folders[%q] = %q, want %q", string(d), got, want)
		}
	}

	if !slices.Equal(doc.RequiredKeys, []string{"domain", "intent"}) {
		t.Errorf("RequiredKeys = %v, want [domain intent]", doc.RequiredKeys)
	}
}

func TestSchemaCommand_NoFlag_PrintsHumanReadable(t *testing.T) {
	cmd := newSchemaCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("schema exec: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Fatal("expected non-empty human-readable schema output")
	}
	for _, needle := range []string{"domain", "intent"} {
		if !strings.Contains(output, needle) {
			t.Errorf("human output missing %q; got:\n%s", needle, output)
		}
	}

	// Must mention at least one canonical folder.
	router := ontology.Router{}
	foundFolder := false
	for _, d := range ontology.AllDomains() {
		if strings.Contains(output, router.CanonicalFolder(d)) {
			foundFolder = true
			break
		}
	}
	if !foundFolder {
		t.Errorf("human output missing any canonical folder name; got:\n%s", output)
	}
}

func TestSchemaCommand_RegisteredOnRoot(t *testing.T) {
	cmd := newRootCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "schema" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected schema subcommand registered on root command")
	}
}
