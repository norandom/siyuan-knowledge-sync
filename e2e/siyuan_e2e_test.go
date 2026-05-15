package e2e

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var skipDocker = flag.Bool("skip-docker", false, "skip tests requiring a SiYuan Docker container")

var (
	binaryPath       string
	siyuanEndpoint   string
	siyuanToken      string
	containerName    string
	workspaceDir     string
	containerStarted bool
)

func TestMain(m *testing.M) {
	flag.Parse()

	if *skipDocker {
		os.Exit(m.Run())
	}

	if err := buildBinary(); err != nil {
		fmt.Fprintf(os.Stderr, "build binary: %v\n", err)
		os.Exit(1)
	}

	if err := startSiYuan(); err != nil {
		fmt.Fprintf(os.Stderr, "start siyuan: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	stopSiYuan()
	os.RemoveAll(workspaceDir)
	os.Exit(code)
}

func buildBinary() error {
	projectRoot, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}
	binaryPath = filepath.Join(os.TempDir(), "siyuan-knowledge-sync-e2e")
	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/siyuan-knowledge-sync")
	cmd.Dir = projectRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

func startSiYuan() error {
	var err error
	workspaceDir, err = os.MkdirTemp("", "siyuan-e2e-workspace-*")
	if err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}

	containerName = fmt.Sprintf("siyuan-e2e-%d", time.Now().UnixNano()%100000)

	cmd := exec.Command("docker", "run", "-d",
		"--name", containerName,
		"-p", "6806",
		"-v", workspaceDir+":/siyuan/workspace",
		"-e", "SIYUAN_ACCESS_AUTH_CODE=e2etest",
		"b3log/siyuan:latest",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run: %v: %s", err, out)
	}

	portCmd := exec.Command("docker", "port", containerName, "6806/tcp")
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker port: %v: %s", err, portOut)
	}
	hostPort := strings.TrimSpace(string(portOut))
	colonIdx := strings.LastIndex(hostPort, ":")
	if colonIdx >= 0 {
		hostPort = hostPort[colonIdx+1:]
	}
	siyuanEndpoint = "http://localhost:" + hostPort

	for i := 0; i < 30; i++ {
		resp, err := http.Post(siyuanEndpoint+"/api/system/version", "application/json", strings.NewReader("{}"))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if i == 29 {
			return fmt.Errorf("siyuan container did not become healthy within 30s")
		}
		time.Sleep(time.Second)
	}

	token, err := readAPIToken()
	if err != nil {
		return err
	}
	siyuanToken = token
	containerStarted = true

	if err := verifyAuth(); err != nil {
		return err
	}

	return nil
}

func readAPIToken() (string, error) {
	data, err := os.ReadFile(filepath.Join(workspaceDir, "conf", "conf.json"))
	if err != nil {
		return "", fmt.Errorf("read conf.json: %w", err)
	}
	var conf struct {
		API struct {
			Token string `json:"token"`
		} `json:"api"`
	}
	if err := json.Unmarshal(data, &conf); err != nil {
		return "", fmt.Errorf("parse conf.json: %w", err)
	}
	if conf.API.Token == "" {
		return "", errors.New("API token not found in conf.json")
	}
	return conf.API.Token, nil
}

func verifyAuth() error {
	payload := `{}`
	req, _ := http.NewRequest("POST", siyuanEndpoint+"/api/notebook/lsNotebooks", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+siyuanToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth check request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	json.Unmarshal(body, &env)
	if env.Code != 0 {
		return fmt.Errorf("API auth failed (code=%d): %s", env.Code, env.Msg)
	}
	return nil
}

func stopSiYuan() {
	if !containerStarted {
		return
	}
	exec.Command("docker", "stop", containerName).Run()
	exec.Command("docker", "rm", containerName).Run()
}

func createTestGitRepo(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "e2e-git-*")
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() { os.RemoveAll(dir) }

	runCmd(t, dir, "git", "init")
	runCmd(t, dir, "git", "config", "user.email", "test@e2e.test")
	runCmd(t, dir, "git", "config", "user.name", "e2e-test")

	return dir, cleanup
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

func writeConfig(t *testing.T, dir string) {
	t.Helper()
	configPath := filepath.Join(dir, ".siyuan-sync.yaml")
	content := fmt.Sprintf(`endpoint: %q
token: %q
repo_path: %q
autofix: false
`, siyuanEndpoint, siyuanToken, dir)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeConfigAutofix(t *testing.T, dir string) {
	t.Helper()
	configPath := filepath.Join(dir, ".siyuan-sync.yaml")
	content := fmt.Sprintf(`endpoint: %q
token: %q
repo_path: %q
autofix: true
`, siyuanEndpoint, siyuanToken, dir)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func runBinary(t *testing.T, dir string, args ...string) (string, string) {
	t.Helper()
	fullArgs := append([]string{"-c", filepath.Join(dir, ".siyuan-sync.yaml")}, args...)
	cmd := exec.Command(binaryPath, fullArgs...)
	cmd.Dir = dir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("binary stderr: %s", stderr.String())
		t.Logf("binary stdout: %s", stdout.String())
	}
	return stdout.String(), stderr.String()
}

func siyuanAPI(t *testing.T, path string, body string) map[string]any {
	t.Helper()
	req, err := http.NewRequest("POST", siyuanEndpoint+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Token "+siyuanToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("API call %s: %v", path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("parse API response for %s: %v\n%s", path, err, string(respBody))
	}
	return result
}

func createNotebook(t *testing.T, name string) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q}`, name)
	result := siyuanAPI(t, "/api/notebook/createNotebook", body)
	if code, ok := result["code"].(float64); !ok || code != 0 {
		t.Fatalf("create notebook %q: %v", name, result)
	}
	data := result["data"].(map[string]any)
	nb := data["notebook"].(map[string]any)
	return nb["id"].(string)
}

func createDoc(t *testing.T, notebookID, hpath, markdown string) string {
	t.Helper()
	body := fmt.Sprintf(`{"notebook":%q,"path":%q,"markdown":%q}`, notebookID, hpath, markdown)
	result := siyuanAPI(t, "/api/filetree/createDocWithMd", body)
	if code, ok := result["code"].(float64); !ok || code != 0 {
		t.Fatalf("create doc %q: %v", hpath, result)
	}
	return result["data"].(string)
}

func exportDoc(t *testing.T, docID string) map[string]any {
	t.Helper()
	body := fmt.Sprintf(`{"id":%q}`, docID)
	result := siyuanAPI(t, "/api/export/exportMdContent", body)
	if code, ok := result["code"].(float64); !ok || code != 0 {
		t.Fatalf("export doc %q: %v", docID, result)
	}
	return result["data"].(map[string]any)
}

func listNotebooks(t *testing.T) []string {
	t.Helper()
	result := siyuanAPI(t, "/api/notebook/lsNotebooks", "{}")
	data := result["data"].(map[string]any)
	nbs, _ := data["notebooks"].([]any)
	var names []string
	for _, n := range nbs {
		nb := n.(map[string]any)
		names = append(names, nb["name"].(string))
	}
	return names
}

func TestFullSyncE2E(t *testing.T) {
	if !containerStarted {
		t.Skip("siyuan container not available")
	}

	dir, cleanup := createTestGitRepo(t)
	defer cleanup()
	writeConfig(t, dir)

	writeFile(t, dir, "journal/daily.md", "# Daily Note\n\nToday's thoughts.\n")
	writeFile(t, dir, "projects/todo.md", "# TODO\n\n- [ ] Task 1\n- [ ] Task 2\n")
	runCmd(t, dir, "git", "add", "journal/daily.md", "projects/todo.md")
	runCmd(t, dir, "git", "commit", "-m", "initial")

	stdout, stderr := runBinary(t, dir, "sync")
	t.Logf("sync stdout: %s", stdout)
	t.Logf("sync stderr: %s", stderr)

	assertNotebookExists(t, "journal")
	assertNotebookExists(t, "projects")

	exportResult := exportDocByPath(t, "journal", "/daily.md")
	if !strings.Contains(exportResult["content"].(string), "Daily Note") {
		t.Errorf("journal/daily.md content mismatch: %s", exportResult["content"])
	}

	writeFile(t, dir, "journal/daily.md", "# Daily Note\n\nUpdated thoughts.\n")
	runCmd(t, dir, "git", "add", "journal/daily.md")
	runCmd(t, dir, "git", "commit", "-m", "update daily")

	stdout, stderr = runBinary(t, dir, "sync")
	t.Logf("sync2 stdout: %s", stdout)
	t.Logf("sync2 stderr: %s", stderr)

	exportResult = exportDocByPath(t, "journal", "/daily.md")
	if !strings.Contains(exportResult["content"].(string), "Updated thoughts") {
		t.Errorf("journal/daily.md was not updated: %s", exportResult["content"])
	}

	os.Remove(filepath.Join(dir, "journal/daily.md"))
	runCmd(t, dir, "git", "rm", "journal/daily.md")
	runCmd(t, dir, "git", "commit", "-m", "remove daily")

	stdout, stderr = runBinary(t, dir, "sync")
	t.Logf("sync3 stdout: %s", stdout)
	t.Logf("sync3 stderr: %s", stderr)

	if !strings.Contains(stderr, "Pruned") {
		t.Errorf("expected 'Pruned' in report, got: %s", stderr)
	}
	_ = stdout
}

func TestDownloadE2E(t *testing.T) {
	if !containerStarted {
		t.Skip("siyuan container not available")
	}

	nbName := fmt.Sprintf("download_nb_%d", time.Now().UnixNano()%100000)
	nbID := createNotebook(t, nbName)

	_ = createDoc(t, nbID, "/download-test.md", "# Download Test\n\nSome content here.\n")

	dir, cleanup := createTestGitRepo(t)
	defer cleanup()
	writeConfig(t, dir)

	writeFile(t, dir, "unused.md", "placeholder")
	runCmd(t, dir, "git", "add", "unused.md")
	runCmd(t, dir, "git", "commit", "-m", "init")

	stdout, stderr := runBinary(t, dir, "download", "--conflict", "overwrite")
	t.Logf("download stdout: %s", stdout)
	t.Logf("download stderr: %s", stderr)

	downloadedPath := filepath.Join(dir, nbName, "download-test.md")
	content, err := os.ReadFile(downloadedPath)
	if err != nil {
		t.Fatalf("expected downloaded file at %s: %v", downloadedPath, err)
	}
	if !strings.Contains(string(content), "Download Test") {
		t.Errorf("downloaded content mismatch: %s", string(content))
	}
}

func TestMCPE2E(t *testing.T) {
	if !containerStarted {
		t.Skip("siyuan container not available")
	}

	nbName := fmt.Sprintf("mcp_nb_%d", time.Now().UnixNano()%100000)
	nbID := createNotebook(t, nbName)

	docID := createDoc(t, nbID, "/mcp-search-test.md", "# MCP Search Test\n\nUniqueContentXYZ789\n")
	_ = docID

	dir, cleanup := createTestGitRepo(t)
	defer cleanup()
	writeConfig(t, dir)

	cmd := exec.Command(binaryPath, "-c", filepath.Join(dir, ".siyuan-sync.yaml"), "mcp-server")
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start MCP server: %v", err)
	}
	defer func() {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()

	reader := bufio.NewReader(stdout)

	initReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]string{"name": "e2e-test", "version": "1.0.0"},
		},
	})

	stdin.Write(initReq)
	stdin.Write([]byte("\n"))

	_, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read init response: %v", err)
	}

	initDone, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	stdin.Write(initDone)
	stdin.Write([]byte("\n"))
	time.Sleep(500 * time.Millisecond)

	listReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	stdin.Write(listReq)
	stdin.Write([]byte("\n"))

	listResp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read tools/list response: %v", err)
	}
	t.Logf("tools/list response: %s", listResp)

	if !strings.Contains(listResp, "search") {
		t.Errorf("tools/list should include search tool")
	}
	if !strings.Contains(listResp, "retrieve") {
		t.Errorf("tools/list should include retrieve tool")
	}

	searchReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "search",
			"arguments": map[string]string{"query": "mcp-search-test"},
		},
	})
	stdin.Write(searchReq)
	stdin.Write([]byte("\n"))

	respLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read search response: %v", err)
	}
	t.Logf("search response: %s", respLine)

	var searchMsg map[string]any
	json.Unmarshal([]byte(respLine), &searchMsg)
	res, _ := searchMsg["result"].(map[string]any)
	if res != nil {
		t.Logf("search result content: %v", res["content"])
	}

	_ = respLine

	retrieveReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "retrieve",
			"arguments": map[string]string{"id": docID},
		},
	})
	stdin.Write(retrieveReq)
	stdin.Write([]byte("\n"))

	retrieveResp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read retrieve response: %v", err)
	}
	t.Logf("retrieve response: %s", retrieveResp)

	if !strings.Contains(retrieveResp, docID) {
		t.Errorf("retrieve response should reference doc ID")
	}
}

func TestComplianceE2E(t *testing.T) {
	if !containerStarted {
		t.Skip("siyuan container not available")
	}

	dir, cleanup := createTestGitRepo(t)
	defer cleanup()
	writeConfig(t, dir)

	violationContent := `---
title: Violation Test
---
# Overview

# Section A

#### Deep Heading

Some text here.

# Section B

 {: id="placeholder_block" }

![image](C:\Users\test\image.png)
`
	writeFile(t, dir, "notes/badfile.md", violationContent)
	runCmd(t, dir, "git", "add", "notes/badfile.md")
	runCmd(t, dir, "git", "commit", "-m", "bad file")

	stdout, stderr := runBinary(t, dir, "audit")
	_ = stdout

	if !strings.Contains(stderr, "heading level skipped") {
		t.Errorf("audit should detect heading nesting issue")
	}
	if !strings.Contains(stderr, "placeholder block ID") {
		t.Errorf("audit should detect placeholder block ID")
	}
	if !strings.Contains(stderr, "absolute") || !strings.Contains(stderr, "asset") {
		t.Errorf("audit should detect absolute asset reference")
	}
	t.Logf("audit stderr: %s", stderr)

	writeConfigAutofix(t, dir)
	stdout2, stderr2 := runBinary(t, dir, "audit", "--autofix")
	t.Logf("autofix stderr: %s", stderr2)
	t.Logf("autofix stdout: %s", stdout2)

	if !strings.Contains(stderr2, "Fixed") {
		t.Errorf("autofix report should mention fixes applied")
	}

	fixedContent, err := os.ReadFile(filepath.Join(dir, "notes/badfile.md"))
	if err != nil {
		t.Fatalf("read fixed file: %v", err)
	}

	if strings.Contains(string(fixedContent), "####") {
		t.Errorf("fixed content should not contain H4 heading (was H####)")
	}
	if strings.Contains(string(fixedContent), "id=\"placeholder_block\"") {
		t.Errorf("fixed content should not contain placeholder block ID")
	}

	writeConfig(t, dir)
	stdout3, stderr3 := runBinary(t, dir, "audit")
	_ = stdout3

	if strings.Contains(stderr3, "heading level skipped") {
		t.Errorf("post-autofix audit should not detect heading nesting issue anymore")
	}
}

func writeFile(t *testing.T, dir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertNotebookExists(t *testing.T, name string) {
	t.Helper()
	result := siyuanAPI(t, "/api/notebook/lsNotebooks", "{}")
	data := result["data"].(map[string]any)
	nbs, _ := data["notebooks"].([]any)
	for _, n := range nbs {
		nb := n.(map[string]any)
		if nb["name"].(string) == name {
			return
		}
	}
	t.Errorf("notebook %q was not found in SiYuan", name)
}

func exportDocByPath(t *testing.T, notebookName, hpath string) map[string]any {
	t.Helper()
	result := siyuanAPI(t, "/api/notebook/lsNotebooks", "{}")
	data := result["data"].(map[string]any)
	nbs, _ := data["notebooks"].([]any)

	var notebookID string
	for _, n := range nbs {
		nb := n.(map[string]any)
		if nb["name"].(string) == notebookName {
			notebookID = nb["id"].(string)
			break
		}
	}
	if notebookID == "" {
		t.Fatalf("notebook %q not found", notebookName)
	}

	idResult := siyuanAPI(t, "/api/filetree/getIDsByHPath",
		fmt.Sprintf(`{"notebook":%q,"path":%q}`, notebookID, hpath))
	idData := idResult["data"].([]any)
	if len(idData) == 0 {
		t.Fatalf("no doc found at hpath %q in notebook %q", hpath, notebookName)
	}
	docID := idData[0].(string)
	return exportDoc(t, docID)
}
