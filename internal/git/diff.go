package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// Diff is the checkout's work since it left its default branch, committed or
// not, as a stat header, the names of new untracked files and the patch. It
// is empty when there is nothing to show. Key identifies all of it, including
// what a caller cuts off before reading.
func Diff(dir string) (diff, key string) {
	git := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
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
		return "", ""
	}
	var b strings.Builder
	b.WriteString(stat)
	if untracked != "" {
		b.WriteString("\n\nNew files not yet added:\n" + untracked)
	}
	b.WriteString("\n\n" + git("diff", "--no-color", base))
	diff = b.String()
	sum := sha256.Sum256([]byte(diff))
	return diff, hex.EncodeToString(sum[:8])
}

// Cut keeps the first limit bytes of a diff without splitting a character.
func Cut(diff string, limit int) string {
	if len(diff) <= limit {
		return diff
	}
	for limit > 0 && !utf8.RuneStart(diff[limit]) {
		limit--
	}
	return diff[:limit] + "\n[diff cut short]"
}
