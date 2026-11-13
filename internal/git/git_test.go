package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Root(deep); got != repo {
		t.Fatalf("got %q", got)
	}
	if got := Root(t.TempDir()); got != "" {
		t.Fatalf("outside git got %q", got)
	}
}

func TestDiffShowsWorkSinceTheDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "first")
	if d, _ := Diff(dir); d != "" {
		t.Fatal("a clean main checkout has nothing to review")
	}
	run("checkout", "-qb", "feature")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("commit", "-qam", "second")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, key := Diff(dir)
	if !strings.Contains(d, "+two") || !strings.Contains(d, "b.txt") || key == "" {
		t.Fatalf("diff %q key %q", d, key)
	}
	if cut := Cut("ab\u00e9cd", 3); cut != "ab\n[diff cut short]" {
		t.Fatalf("a cut mustn't split é: %q", cut)
	}
}
