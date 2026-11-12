// Package git reads the state of the checkout a session works in.
package git

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status struct {
	Branch  string
	Changed int
	Ahead   int
	Behind  int
	// Worktree names the linked worktree, and Repo is the checkout it
	// belongs to. Both are empty in a main checkout.
	Worktree string
	Repo     string
}

// git status on a big repo takes a while, and deckk reloads every couple of
// seconds, so each folder is read at most this often.
const fresh = 10 * time.Second

var cache = struct {
	sync.Mutex
	at     map[string]time.Time
	status map[string]Status
}{at: map[string]time.Time{}, status: map[string]Status{}}

func Read(dir string) (Status, bool) {
	cache.Lock()
	at, seen := cache.at[dir]
	s := cache.status[dir]
	cache.Unlock()
	if seen && time.Since(at) < fresh {
		return s, s.Branch != ""
	}

	s = status(dir)
	s.Repo, s.Worktree = worktree(dir)
	cache.Lock()
	cache.at[dir], cache.status[dir] = time.Now(), s
	cache.Unlock()
	return s, s.Branch != ""
}

func status(dir string) Status {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain=v2", "--branch").Output()
	if err != nil {
		return Status{}
	}
	return parse(out)
}

func parse(out []byte) Status {
	var s Status
	lines := bufio.NewScanner(bytes.NewReader(out))
	for lines.Scan() {
		line := lines.Text()
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			s.Branch = strings.TrimPrefix(line, "# branch.head ")
		case strings.HasPrefix(line, "# branch.ab "):
			ab := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(ab) == 2 {
				s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(ab[0], "+"))
				s.Behind, _ = strconv.Atoi(strings.TrimPrefix(ab[1], "-"))
			}
		case !strings.HasPrefix(line, "#") && line != "":
			s.Changed++
		}
	}
	return s
}
