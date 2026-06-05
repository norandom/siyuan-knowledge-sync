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

// notebookIDByName returns the notebook ID for the named notebook, or the
// empty string when it is not present. Used by the ontology E2E tests where
// "still present" / "absent" assertions need to query a specific notebook
// without t.Fatal'ing on a missing notebook (exportDocByPath does t.Fatal,
// which is the wrong contract for negative assertions).
func notebookIDByName(t *testing.T, name string) string {
	t.Helper()
	result := siyuanAPI(t, "/api/notebook/lsNotebooks", "{}")
	data := result["data"].(map[string]any)
	nbs, _ := data["notebooks"].([]any)
	for _, n := range nbs {
		nb := n.(map[string]any)
		if nb["name"].(string) == name {
			return nb["id"].(string)
		}
	}
	return ""
}

// getDocIDsByHPath returns the SiYuan doc IDs at the given hpath inside the
// given notebook. An empty slice means the doc is absent — used by the
// retire-single-doc test to prove the targeted document is gone and a
// sibling document is still present.
func getDocIDsByHPath(t *testing.T, notebookID, hpath string) []string {
	t.Helper()
	result := siyuanAPI(t, "/api/filetree/getIDsByHPath",
		fmt.Sprintf(`{"notebook":%q,"path":%q}`, notebookID, hpath))
	raw, _ := result["data"].([]any)
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			ids = append(ids, s)
		}
	}
	return ids
}

// findDocIDByHPath looks up the SiYuan doc ID for the given hpath inside
// notebookID using a SQL query against the `blocks` table. This is more
// robust than `/api/filetree/getIDsByHPath` against the live container,
// which returns an empty array for hpaths containing characters like `&`
// or spaces even when the doc is plainly present.
//
// The SiYuan `blocks` table stores hpath WITH the `.md` extension on the
// leaf component (verified against the live container — see diagnostic
// dump in the retire test). We pass the hpath through verbatim.
//
// Returns the doc ID or an empty string when not found.
func findDocIDByHPath(t *testing.T, notebookID, hpath string) string {
	t.Helper()
	query := fmt.Sprintf(
		"SELECT id FROM blocks WHERE box='%s' AND type='d' AND hpath='%s' LIMIT 1",
		escapeSQL(notebookID), escapeSQL(hpath),
	)
	body, err := json.Marshal(map[string]string{"stmt": query})
	if err != nil {
		t.Fatalf("marshal sql body: %v", err)
	}
	result := siyuanAPI(t, "/api/query/sql", string(body))
	rows, _ := result["data"].([]any)
	if len(rows) == 0 {
		return ""
	}
	row, _ := rows[0].(map[string]any)
	if row == nil {
		return ""
	}
	id, _ := row["id"].(string)
	return id
}

// escapeSQL escapes single quotes for inline SQL string literals — sufficient
// for the SiYuan query API surface we exercise here (no untrusted input, all
// values originate from the test's own setup).
func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// waitForDocAtHPath polls SiYuan's block index for up to `timeout` for a doc
// to appear at the given hpath in notebookID. Useful immediately after
// createDocWithMd / sync, because SiYuan's SQL index may lag a few hundred
// milliseconds behind the operational filetree state.
func waitForDocAtHPath(t *testing.T, notebookID, hpath string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if id := findDocIDByHPath(t, notebookID, hpath); id != "" {
			return id
		}
		time.Sleep(200 * time.Millisecond)
	}
	return findDocIDByHPath(t, notebookID, hpath)
}

// waitForDocAbsent polls SiYuan's filetree (operational state — NOT the SQL
// block index, which lags behind removeDocByID) for the targeted doc to
// disappear from the given parent path. Returns true once it is gone, false
// after timeout. We probe the parent rather than the doc itself because the
// engine deletes by ID and the operational state reflects immediately in
// listDocsByPath; the SQL `blocks` table can lag a second or more behind.
func waitForDocAbsent(t *testing.T, notebookID, parentPath, docID string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		result := siyuanAPI(t, "/api/filetree/listDocsByPath",
			fmt.Sprintf(`{"notebook":%q,"path":%q}`, notebookID, parentPath))
		data, _ := result["data"].(map[string]any)
		files, _ := data["files"].([]any)
		found := false
		for _, f := range files {
			entry, _ := f.(map[string]any)
			if entry == nil {
				continue
			}
			if id, _ := entry["id"].(string); id == docID {
				found = true
				break
			}
		}
		if !found {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestOntology_CustomAttrsAppliedOnSync verifies Req 4.1 / 4.2 end-to-end:
// a file declaring a valid `domain:` + `intent:` pair is synced to the live
// container, and the resulting SiYuan document carries `custom-domain` and
// `custom-intent` block attributes (queried via `/api/attr/getBlockAttrs`).
//
// The source path `wiki/devops-area/sample.md` is intentionally NOT the
// canonical folder for `domain: devops`, so this test also implicitly
// exercises the routing path (TestOntology_RoutedFileReachableAtNewHpath
// asserts routing directly). The notebook here is `wiki` (the top-level
// folder), and the hpath after routing is `/Linux & DevOps/sample.md`.
func TestOntology_CustomAttrsAppliedOnSync(t *testing.T) {
	if !containerStarted {
		t.Skip("siyuan container not available")
	}

	dir, cleanup := createTestGitRepo(t)
	defer cleanup()
	writeConfig(t, dir)

	content := `---
title: E2E DevOps Sample
domain: devops
intent: sop
tags: [e2e-tag]
---
# E2E DevOps Sample

body content
`
	writeFile(t, dir, "wiki/devops-area/sample.md", content)
	runCmd(t, dir, "git", "add", "wiki/devops-area/sample.md")
	runCmd(t, dir, "git", "commit", "-m", "initial ontology sample")

	stdout, stderr := runBinary(t, dir, "sync")
	t.Logf("ontology sync stdout: %s", stdout)
	t.Logf("ontology sync stderr: %s", stderr)

	// Routing should have moved the file to the canonical devops folder.
	// We query the SiYuan side via exportDocByPath (which also gives us the
	// doc ID indirectly), then directly via getIDsByHPath to grab the doc ID
	// for the getBlockAttrs probe.
	notebookID := notebookIDByName(t, "wiki")
	if notebookID == "" {
		t.Fatalf("notebook %q not found after sync", "wiki")
	}

	// The engine derives the SiYuan document title from the frontmatter
	// `title:` field via RenameDocByID (siyuan-knowledge-sync Req 13.2),
	// so the doc's stored hpath uses the title text, not the source
	// filename. The source frontmatter declared `title: E2E DevOps Sample`,
	// so SiYuan stores it at `/Linux & DevOps/E2E DevOps Sample` (no .md
	// extension on the title leaf — verified against the live container).
	// `getIDsByHPath` returns empty for paths containing `&`/spaces, so we
	// resolve via the SQL block index instead.
	docID := waitForDocAtHPath(t, notebookID, "/Linux & DevOps/E2E DevOps Sample", 5*time.Second)
	if docID == "" {
		t.Fatalf("no doc found at title-derived hpath /Linux & DevOps/E2E DevOps Sample in notebook %q after routing sync",
			"wiki")
	}

	attrResult := siyuanAPI(t, "/api/attr/getBlockAttrs",
		fmt.Sprintf(`{"id":%q}`, docID))
	if code, ok := attrResult["code"].(float64); !ok || code != 0 {
		t.Fatalf("getBlockAttrs failed: %v", attrResult)
	}
	attrs, _ := attrResult["data"].(map[string]any)
	if attrs == nil {
		t.Fatalf("getBlockAttrs returned nil data for doc %s", docID)
	}

	if got := attrs["custom-domain"]; got != "devops" {
		t.Errorf("custom-domain = %v, want %q (full attrs: %v)", got, "devops", attrs)
	}
	if got := attrs["custom-intent"]; got != "sop" {
		t.Errorf("custom-intent = %v, want %q (full attrs: %v)", got, "sop", attrs)
	}
}

// TestOntology_RoutedFileReachableAtNewHpath verifies Req 4.1 / 3.2 / 3.3:
// a file whose declared `domain:` does not match its on-disk path is
// `git mv`'d into the canonical folder, a single `ontology-route:` commit is
// recorded, the local source path no longer exists, and the SiYuan side is
// reachable at the new hpath.
func TestOntology_RoutedFileReachableAtNewHpath(t *testing.T) {
	if !containerStarted {
		t.Skip("siyuan container not available")
	}

	dir, cleanup := createTestGitRepo(t)
	defer cleanup()
	writeConfig(t, dir)

	content := `---
title: E2E Routed Forensics
domain: forensics
intent: log
---
# E2E Routed Forensics

routed body
`
	writeFile(t, dir, "wiki/misc/routed.md", content)
	runCmd(t, dir, "git", "add", "wiki/misc/routed.md")
	runCmd(t, dir, "git", "commit", "-m", "pre-route forensics sample")

	stdout, stderr := runBinary(t, dir, "sync")
	t.Logf("route sync stdout: %s", stdout)
	t.Logf("route sync stderr: %s", stderr)

	// Local: source path is gone, canonical path exists.
	if _, err := os.Stat(filepath.Join(dir, "wiki/misc/routed.md")); !os.IsNotExist(err) {
		t.Errorf("source path wiki/misc/routed.md should be gone after route, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "wiki/Digital Forensics/routed.md")); err != nil {
		t.Errorf("canonical path wiki/Digital Forensics/routed.md should exist, stat err: %v", err)
	}

	// Git: exactly one `ontology-route:` commit was created.
	logCmd := exec.Command("git", "log", "--grep=ontology-route:", "--pretty=oneline")
	logCmd.Dir = dir
	logOut, err := logCmd.Output()
	if err != nil {
		t.Fatalf("git log --grep=ontology-route: failed: %v", err)
	}
	logLines := strings.Split(strings.TrimSpace(string(logOut)), "\n")
	// strings.Split of "" returns [""] — normalize that to 0.
	if len(logLines) == 1 && logLines[0] == "" {
		logLines = nil
	}
	if len(logLines) != 1 {
		t.Errorf("expected exactly 1 ontology-route: commit, got %d: %q", len(logLines), logLines)
	}

	// SiYuan: doc reachable at the new hpath in notebook "wiki". The engine
	// renames the SiYuan doc to the frontmatter title (E2E Routed Forensics),
	// so the stored hpath uses that title rather than the source filename.
	// Use the SQL-backed lookup because `getIDsByHPath` returns empty for
	// hpaths containing spaces against the live container.
	notebookID := notebookIDByName(t, "wiki")
	if notebookID == "" {
		t.Fatalf("notebook %q not found after route sync", "wiki")
	}
	docID := waitForDocAtHPath(t, notebookID, "/Digital Forensics/E2E Routed Forensics", 5*time.Second)
	if docID == "" {
		t.Fatalf("routed doc not found at title-derived hpath /Digital Forensics/E2E Routed Forensics in notebook %q",
			"wiki")
	}
	body, _ := exportDoc(t, docID)["content"].(string)
	if !strings.Contains(body, "routed body") {
		t.Errorf("routed doc body mismatch: %s", body)
	}
}

// TestOntology_RetireSingleDocViaMigrateApply verifies Req 10.2 / 10.4:
// a `migrate apply` plan with a single `retire_siyuan` entry removes only
// the targeted SiYuan document and leaves everything else untouched.
//
// Setup: two notebooks each holding one doc, created directly via the
// SiYuan API (no sync involved). The plan targets only Doc B. After
// `migrate apply`, Doc B's hpath must return zero IDs; Doc A's hpath must
// still return its original ID.
func TestOntology_RetireSingleDocViaMigrateApply(t *testing.T) {
	if !containerStarted {
		t.Skip("siyuan container not available")
	}

	suffix := time.Now().UnixNano() % 100000
	keepNbName := fmt.Sprintf("e2e_retire_keep_%d", suffix)
	dropNbName := fmt.Sprintf("e2e_retire_drop_%d", suffix)

	keepNbID := createNotebook(t, keepNbName)
	dropNbID := createNotebook(t, dropNbName)

	keepDocID := createDoc(t, keepNbID, "/keep-me.md", "# Keep Me\n\nkeep me content\n")
	dropDocID := createDoc(t, dropNbID, "/retire-me.md", "# Retire Me\n\nretire me content\n")

	// Sanity: both docs are present before we touch anything.
	if ids := getDocIDsByHPath(t, keepNbID, "/keep-me.md"); len(ids) == 0 || ids[0] != keepDocID {
		t.Fatalf("pre-condition: keep doc not at expected hpath; ids=%v want=%s", ids, keepDocID)
	}
	if ids := getDocIDsByHPath(t, dropNbID, "/retire-me.md"); len(ids) == 0 || ids[0] != dropDocID {
		t.Fatalf("pre-condition: drop doc not at expected hpath; ids=%v want=%s", ids, dropDocID)
	}

	// A migration plan needs a working git repo + config because
	// runMigrateApply builds a full SyncEngine surface (even though
	// retire_siyuan never touches local files). Use the standard
	// createTestGitRepo+writeConfig flow.
	dir, cleanup := createTestGitRepo(t)
	defer cleanup()
	writeConfig(t, dir)

	// migrate.PlanEntry.Validate requires SourcePath non-empty even for
	// retire_siyuan; use a placeholder.
	plan := fmt.Sprintf(`{
  "version": 1,
  "source": %q,
  "entries": [
    {
      "op": "retire_siyuan",
      "source_path": "ignored.md",
      "siyuan_doc_id": %q
    }
  ]
}
`, dir, dropDocID)

	planPath := filepath.Join(dir, "retire-plan.json")
	if err := os.WriteFile(planPath, []byte(plan), 0644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	stdout, stderr := runBinary(t, dir, "migrate", "apply", planPath)
	t.Logf("migrate apply stdout: %s", stdout)
	t.Logf("migrate apply stderr: %s", stderr)

	if !strings.Contains(stderr, "Retired:") {
		t.Errorf("migrate apply report should mention 'Retired:'; got: %s", stderr)
	}
	if !strings.Contains(stderr, "Retired: 1") {
		t.Errorf("migrate apply report should show 1 retired entry; got: %s", stderr)
	}

	// SiYuan-side: assert the drop doc is gone from the operational
	// filetree (the SQL block index may lag, but listDocsByPath reflects
	// removeDocByID immediately). Then assert the keep doc is still
	// reachable via the SQL block index (the index already had time to
	// settle since the keep doc was created earlier in the test).
	if ok := waitForDocAbsent(t, dropNbID, "/", dropDocID, 5*time.Second); !ok {
		t.Errorf("retire target %s should have been removed from notebook %s, but is still present", dropDocID, dropNbName)
	}
	if id := waitForDocAtHPath(t, keepNbID, "/keep-me.md", 5*time.Second); id == "" {
		t.Errorf("non-target doc %s should still be present at /keep-me.md in notebook %s", keepDocID, keepNbName)
	}
}
