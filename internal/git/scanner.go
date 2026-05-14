package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"siyuan-knowledge-sync/internal/types"
)

type GitScanner struct {
	repo     *git.Repository
	repoPath string
}

func NewGitScanner(repoPath string) (*GitScanner, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("open git repository at %s: %w", repoPath, err)
	}
	return &GitScanner{repo: repo, repoPath: repoPath}, nil
}

func (s *GitScanner) ListTrackedMdFiles() ([]types.TrackedFile, error) {
	tree, err := s.headTree()
	if err == errNoHEAD {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var files []types.TrackedFile
	err = tree.Files().ForEach(func(f *object.File) error {
		if !strings.HasSuffix(f.Name, ".md") {
			return nil
		}
		fullPath := filepath.Join(s.repoPath, f.Name)
		info, statErr := os.Stat(fullPath)
		modTime := time.Time{}
		if statErr == nil {
			modTime = info.ModTime()
		}
		files = append(files, types.TrackedFile{
			Path:    f.Name,
			ModTime: modTime,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk tree: %w", err)
	}

	return files, nil
}

func (s *GitScanner) RepoPath() string {
	return s.repoPath
}

func (s *GitScanner) IsTracked(path string) (bool, error) {
	if !strings.HasSuffix(path, ".md") {
		return false, nil
	}

	tree, err := s.headTree()
	if err == errNoHEAD {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	_, err = tree.File(path)
	if err != nil {
		return false, nil
	}
	return true, nil
}

var errNoHEAD = fmt.Errorf("no HEAD")

func (s *GitScanner) headTree() (*object.Tree, error) {
	ref, err := s.repo.Head()
	if err != nil {
		return nil, errNoHEAD
	}

	commit, err := s.repo.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("get commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get tree: %w", err)
	}

	return tree, nil
}
