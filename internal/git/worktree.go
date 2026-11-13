package git

import (
	"os"
	"path/filepath"
	"strings"
)

// A linked worktree has a .git file, not a directory, that points into the
// main checkout's .git/worktrees.
func worktree(dir string) (repo, name string) {
	for d := dir; d != "" && d != "/" && d != "."; d = filepath.Dir(d) {
		data, err := os.ReadFile(filepath.Join(d, ".git"))
		if err != nil {
			if _, statErr := os.Stat(filepath.Join(d, ".git")); statErr == nil {
				return "", ""
			}
			continue
		}
		gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
		main, _, found := strings.Cut(gitdir, string(filepath.Separator)+".git"+string(filepath.Separator)+"worktrees"+string(filepath.Separator))
		if !ok || !found {
			return "", ""
		}
		return main, filepath.Base(d)
	}
	return "", ""
}

// Root is the top of the checkout dir is in: the nearest folder holding
// .git, which is a file in linked worktrees. Two folders with the same root
// share one working tree. It is empty outside git.
func Root(dir string) string {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		if parent := filepath.Dir(d); parent == d {
			return ""
		}
	}
}
