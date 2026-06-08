package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GitCmd runs a git command in dir, failing the test on error.
// Sets author/committer env vars so commits succeed without global config.
func GitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s\n%s", args, err, out)
	}
}

// WriteFile creates a file at dir/path with the given content,
// creating parent directories as needed.
func WriteFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// SetupGitRepo creates a temp directory with a git repository initialized.
// It registers t.Cleanup to remove the directory and configures git
// settings (user.email, user.name, commit.gpgsign=false) so commits
// succeed without global config. Returns the repo path.
func SetupGitRepo(t *testing.T, namePrefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", namePrefix+"-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	GitCmd(t, dir, "init")
	GitCmd(t, dir, "config", "user.email", "test@test.com")
	GitCmd(t, dir, "config", "user.name", "test")
	GitCmd(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

// GitLogGrep returns subject lines from `git log` matching pattern.
func GitLogGrep(t *testing.T, dir, pattern string) []string {
	t.Helper()
	cmd := exec.Command("git", "log", "--grep="+pattern, "--pretty=%s")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log --grep failed: %s\n%s", err, out)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
