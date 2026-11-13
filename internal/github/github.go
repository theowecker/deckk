package github

import (
	"context"
	"encoding/json"
	"os/exec"
	"sort"
	"sync"
	"time"
)

type PR struct {
	// State is open, draft, merged or closed.
	State string
	// Checks is passing, failing, pending, or empty when the PR has none.
	Checks  string
	Failing []string
	// Review is approved, changes requested, or empty.
	Review string
}

func (p PR) Done() bool { return p.State == "merged" || p.State == "closed" }

// Open PRs change by the minute; finished ones only if someone reopens them.
const (
	openFor     = 2 * time.Minute
	finishedFor = time.Hour
	parallel    = 4
)

var cache = struct {
	sync.Mutex
	at map[string]time.Time
	pr map[string]PR
}{at: map[string]time.Time{}, pr: map[string]PR{}}

var available = sync.OnceValue(func() bool {
	_, err := exec.LookPath("gh")
	return err == nil
})

// Statuses looks up each PR through the gh CLI, a few at a time, reusing
// recent answers. PRs gh can't read are left out.
func Statuses(urls []string) map[string]PR {
	out := map[string]PR{}
	if !available() {
		return out
	}
	var stale []string
	cache.Lock()
	for _, url := range urls {
		pr, known := cache.pr[url]
		ttl := openFor
		if pr.Done() {
			ttl = finishedFor
		}
		if known && time.Since(cache.at[url]) < ttl {
			out[url] = pr
		} else {
			stale = append(stale, url)
		}
	}
	cache.Unlock()

	var wg sync.WaitGroup
	var mu sync.Mutex
	slots := make(chan struct{}, parallel)
	for _, url := range stale {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			pr, ok := fetch(url)
			cache.Lock()
			cache.at[url] = time.Now()
			if ok {
				cache.pr[url] = pr
			}
			cache.Unlock()
			if ok {
				mu.Lock()
				out[url] = pr
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

func fetch(url string) (PR, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "gh", "pr", "view", url, "--json", "state,isDraft,reviewDecision,statusCheckRollup").Output()
	if err != nil {
		return PR{}, false
	}
	return parse(data)
}

type check struct {
	Name       string `json:"name"`
	Context    string `json:"context"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

func parse(data []byte) (PR, bool) {
	var raw struct {
		State          string  `json:"state"`
		IsDraft        bool    `json:"isDraft"`
		ReviewDecision string  `json:"reviewDecision"`
		Checks         []check `json:"statusCheckRollup"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.State == "" {
		return PR{}, false
	}
	pr := PR{State: map[string]string{"OPEN": "open", "MERGED": "merged", "CLOSED": "closed"}[raw.State]}
	if pr.State == "open" && raw.IsDraft {
		pr.State = "draft"
	}
	pr.Review = map[string]string{"APPROVED": "approved", "CHANGES_REQUESTED": "changes requested"}[raw.ReviewDecision]
	pending := false
	for _, c := range raw.Checks {
		name := c.Name
		if name == "" {
			name = c.Context
		}
		switch {
		case failed[c.Conclusion] || c.State == "FAILURE" || c.State == "ERROR":
			pr.Failing = append(pr.Failing, name)
		case c.Status != "" && c.Status != "COMPLETED", c.State == "PENDING", c.State == "EXPECTED":
			pending = true
		}
	}
	sort.Strings(pr.Failing)
	switch {
	case len(pr.Failing) > 0:
		pr.Checks = "failing"
	case pending:
		pr.Checks = "pending"
	case len(raw.Checks) > 0:
		pr.Checks = "passing"
	}
	return pr, true
}

var failed = map[string]bool{"FAILURE": true, "TIMED_OUT": true, "CANCELLED": true, "ACTION_REQUIRED": true, "STARTUP_FAILURE": true}
