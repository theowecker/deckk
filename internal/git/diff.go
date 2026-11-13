package git

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Diff is the checkout's work since it left its default branch, committed or
// not, as a stat header, the names of new untracked files and the patch, cut
// to limit bytes. It is empty when there is nothing to show.
func Diff(dir string, limit int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	git := func(args ...string) string {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimRight(string(out), "\n")
	}
	base := "HEAD"
	for _, branch := range []string{strings.TrimPrefix(git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"), "refs/remotes/"), "origin/main", "main", "origin/master", "master"} {
		if branch == "" {
			continue
		}
		if mb := git("merge-base", "HEAD", branch); mb != "" {
			base = mb
			break
		}
	}
	stat := git("diff", "--stat", base)
	untracked := git("ls-files", "--others", "--exclude-standard")
	if stat == "" && untracked == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(stat)
	if untracked != "" {
		b.WriteString("\n\nNew files not yet added:\n" + untracked)
	}
	b.WriteString("\n\n" + git("diff", "--no-color", base))
	out := b.String()
	if len(out) > limit {
		out = out[:limit] + "\n[diff cut short]"
	}
	return out
}
