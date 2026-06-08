package git

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"siyuan-knowledge-sync/internal/testutil"
)

// Helpers delegated to internal/testutil.

func TestListTrackedMdFiles_OnlyCommittedMd(t *testing.T) {
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "a.md", "# A")
	testutil.WriteFile(t, dir, "b.md", "# B")
	testutil.WriteFile(t, dir, "c.txt", "text")
	testutil.GitCmd(t, dir, "add", "a.md", "b.md", "c.txt")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "tracked.md", "# Tracked")
	testutil.GitCmd(t, dir, "add", "tracked.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

	testutil.WriteFile(t, dir, "untracked.md", "# Untracked")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "readme.md", "# Readme")
	testutil.WriteFile(t, dir, "main.go", "package main")
	testutil.WriteFile(t, dir, "config.yaml", "key: val")
	testutil.GitCmd(t, dir, "add", "readme.md", "main.go", "config.yaml")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "notes/foo.md", "# Foo")
	testutil.WriteFile(t, dir, "notes/bar.md", "# Bar")
	testutil.GitCmd(t, dir, "add", "notes/foo.md", "notes/bar.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "doc.md", "# Doc")
	testutil.GitCmd(t, dir, "add", "doc.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "first.md", "# First")
	testutil.GitCmd(t, dir, "add", "first.md")
	testutil.GitCmd(t, dir, "commit", "-m", "first commit")

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

	testutil.WriteFile(t, dir, "second.md", "# Second")
	testutil.GitCmd(t, dir, "add", "second.md")
	testutil.GitCmd(t, dir, "commit", "-m", "second commit")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "tracked.md", "# Tracked")
	testutil.WriteFile(t, dir, "untracked.md", "# Untracked")
	testutil.GitCmd(t, dir, "add", "tracked.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "tracked.md", "# Tracked")
	testutil.WriteFile(t, dir, "untracked.md", "# Untracked")
	testutil.GitCmd(t, dir, "add", "tracked.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "readme.md", "# Readme")
	testutil.WriteFile(t, dir, "main.go", "package main")
	testutil.GitCmd(t, dir, "add", "readme.md", "main.go")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "readme.md", "# Readme")
	testutil.GitCmd(t, dir, "add", "readme.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

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
	dir := testutil.SetupGitRepo(t, "gitscanner-test")

	testutil.WriteFile(t, dir, "note.md", "# Note")
	testutil.GitCmd(t, dir, "add", "note.md")
	testutil.GitCmd(t, dir, "commit", "-m", "initial")

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
