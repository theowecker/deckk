package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	out := "# branch.oid abc\n# branch.head checkout\n# branch.upstream origin/checkout\n# branch.ab +2 -1\n1 .M N... 100644 100644 100644 a b app.rb\n? new.rb\n"
	got := parse([]byte(out))
	if got != (Status{Branch: "checkout", Changed: 2, Ahead: 2, Behind: 1}) {
		t.Fatalf("got %+v", got)
	}
}

func TestWorktreeFindsTheMainCheckout(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "shop")
	tree := filepath.Join(repo, ".claude", "worktrees", "checkout")
	if err := os.MkdirAll(filepath.Join(tree, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git", "worktrees", "checkout"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitdir := "gitdir: " + filepath.Join(repo, ".git", "worktrees", "checkout") + "\n"
	if err := os.WriteFile(filepath.Join(tree, ".git"), []byte(gitdir), 0o644); err != nil {
		t.Fatal(err)
	}
	if main, name := worktree(filepath.Join(tree, "app")); main != repo || name != "checkout" {
		t.Fatalf("got %q %q", main, name)
	}
	if main, name := worktree(repo); main != "" || name != "" {
		t.Fatalf("the main checkout is no worktree, got %q %q", main, name)
	}
}
