package claude

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
