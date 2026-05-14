package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func gitCmd(t *testing.T, dir string, args ...string) {
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupGitRepo(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "gitscanner-test-*")
	if err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "init")
	return dir, func() { os.RemoveAll(dir) }
}

func TestListTrackedMdFiles_OnlyCommittedMd(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "a.md"), "# A")
	writeFile(t, filepath.Join(dir, "b.md"), "# B")
	writeFile(t, filepath.Join(dir, "c.txt"), "text")
	gitCmd(t, dir, "add", "a.md", "b.md", "c.txt")
	gitCmd(t, dir, "commit", "-m", "initial")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files, err := scanner.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	paths := map[string]bool{}
	for _, f := range files {
		paths[f.Path] = true
	}
	if !paths["a.md"] {
		t.Error("missing a.md")
	}
	if !paths["b.md"] {
		t.Error("missing b.md")
	}
	if paths["c.txt"] {
		t.Error("unexpected c.txt (non-md)")
	}
}

func TestListTrackedMdFiles_ExcludesUntracked(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "tracked.md"), "# Tracked")
	gitCmd(t, dir, "add", "tracked.md")
	gitCmd(t, dir, "commit", "-m", "initial")

	writeFile(t, filepath.Join(dir, "untracked.md"), "# Untracked")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files, err := scanner.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if files[0].Path != "tracked.md" {
		t.Errorf("expected tracked.md, got %s", files[0].Path)
	}
}

func TestListTrackedMdFiles_ExcludesNonMd(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "readme.md"), "# Readme")
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "config.yaml"), "key: val")
	gitCmd(t, dir, "add", "readme.md", "main.go", "config.yaml")
	gitCmd(t, dir, "commit", "-m", "initial")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files, err := scanner.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if files[0].Path != "readme.md" {
		t.Errorf("expected readme.md, got %s", files[0].Path)
	}
}

func TestListTrackedMdFiles_IncludesSubdirectories(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "notes/foo.md"), "# Foo")
	writeFile(t, filepath.Join(dir, "notes/bar.md"), "# Bar")
	gitCmd(t, dir, "add", "notes/foo.md", "notes/bar.md")
	gitCmd(t, dir, "commit", "-m", "initial")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files, err := scanner.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}

	paths := map[string]bool{}
	for _, f := range files {
		paths[f.Path] = true
	}
	if !paths["notes/foo.md"] {
		t.Error("missing notes/foo.md")
	}
	if !paths["notes/bar.md"] {
		t.Error("missing notes/bar.md")
	}
}

func TestListTrackedMdFiles_ModTimePresent(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "doc.md"), "# Doc")
	gitCmd(t, dir, "add", "doc.md")
	gitCmd(t, dir, "commit", "-m", "initial")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files, err := scanner.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].ModTime.IsZero() {
		t.Error("ModTime should not be zero")
	}

	info, err := os.Stat(filepath.Join(dir, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !files[0].ModTime.Equal(info.ModTime()) {
		t.Errorf("ModTime mismatch: got %v, want %v", files[0].ModTime, info.ModTime())
	}
}

func TestListTrackedMdFiles_NewlyAddedFile(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "first.md"), "# First")
	gitCmd(t, dir, "add", "first.md")
	gitCmd(t, dir, "commit", "-m", "first commit")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files1, err := scanner.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(files1) != 1 || files1[0].Path != "first.md" {
		t.Fatalf("first run: expected [first.md], got %v", files1)
	}

	writeFile(t, filepath.Join(dir, "second.md"), "# Second")
	gitCmd(t, dir, "add", "second.md")
	gitCmd(t, dir, "commit", "-m", "second commit")

	scanner2, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files2, err := scanner2.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(files2) != 2 {
		t.Fatalf("second run: expected 2 files, got %d: %v", len(files2), files2)
	}

	paths := map[string]bool{}
	for _, f := range files2 {
		paths[f.Path] = true
	}
	if !paths["first.md"] {
		t.Error("missing first.md")
	}
	if !paths["second.md"] {
		t.Error("missing second.md")
	}
}

func TestIsTracked_MdFile(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "tracked.md"), "# Tracked")
	writeFile(t, filepath.Join(dir, "untracked.md"), "# Untracked")
	gitCmd(t, dir, "add", "tracked.md")
	gitCmd(t, dir, "commit", "-m", "initial")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := scanner.IsTracked("tracked.md")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("tracked.md should be tracked")
	}
}

func TestIsTracked_UntrackedFile(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "tracked.md"), "# Tracked")
	writeFile(t, filepath.Join(dir, "untracked.md"), "# Untracked")
	gitCmd(t, dir, "add", "tracked.md")
	gitCmd(t, dir, "commit", "-m", "initial")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := scanner.IsTracked("untracked.md")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("untracked.md should not be tracked")
	}
}

func TestIsTracked_NonMdFile(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "readme.md"), "# Readme")
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	gitCmd(t, dir, "add", "readme.md", "main.go")
	gitCmd(t, dir, "commit", "-m", "initial")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := scanner.IsTracked("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("main.go should not be considered tracked (non-md)")
	}
}

func TestIsTracked_NonExistentFile(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "readme.md"), "# Readme")
	gitCmd(t, dir, "add", "readme.md")
	gitCmd(t, dir, "commit", "-m", "initial")

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	ok, err := scanner.IsTracked("nonexistent.md")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("nonexistent.md should not be tracked")
	}
}

func TestNewGitScanner_NonGitDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "gitscanner-nogit-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	_, err = NewGitScanner(dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestListTrackedMdFiles_EmptyRepo(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files, err := scanner.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected no files in empty repo, got %v", files)
	}
}

func TestListTrackedMdFiles_ModTimeReflectsFileSystem(t *testing.T) {
	dir, cleanup := setupGitRepo(t)
	defer cleanup()

	writeFile(t, filepath.Join(dir, "note.md"), "# Note")
	gitCmd(t, dir, "add", "note.md")
	gitCmd(t, dir, "commit", "-m", "initial")

	before := time.Now().Add(-1 * time.Second)
	os.Chtimes(filepath.Join(dir, "note.md"), before, before)

	scanner, err := NewGitScanner(dir)
	if err != nil {
		t.Fatal(err)
	}

	files, err := scanner.ListTrackedMdFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if !files[0].ModTime.Equal(before) {
		t.Errorf("ModTime should reflect filesystem time: got %v, want %v", files[0].ModTime, before)
	}
}
