package web

import "time"

// No timestamp, so an unchanged dashboard publishes identical bytes and the
// page isn't redrawn for nothing.
type State struct {
	Counts     Counts    `json:"counts"`
	NextUp     []Next    `json:"nextUp"`
	Clashes    []Clash   `json:"clashes"`
	Groups     []Group   `json:"groups"`
	Projects   []Group   `json:"projects"`
	Goals      string    `json:"goals"`
	Activity   []Event   `json:"activity"`
	Today      Today     `json:"today"`
	DeckkState string    `json:"deckkState"`
	Deckk      Deckk     `json:"deckk"`
	Background []Session `json:"background"`
	Notice     Notice    `json:"notice"`
	Folders    []string  `json:"folders"`
	// GoalList is each goal in the goals file with how its sessions stand.
	GoalList []Goal `json:"goalList"`
}

type Deckk struct {
	On      bool   `json:"on"`
	Running bool   `json:"running"`
	Working bool   `json:"working"`
	ID      string `json:"id,omitempty"`
}

type Notice struct {
	Text  string `json:"text,omitempty"`
	Error bool   `json:"error,omitempty"`
	// Seq changes with every notice, so the same words twice still show.
	Seq int `json:"seq,omitempty"`
}

type Counts struct {
	Waiting   int `json:"waiting"`
	Working   int `json:"working"`
	Resumable int `json:"resumable"`
}

type Next struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Project string `json:"project"`
	Prompt  string `json:"prompt"`
	Why     string `json:"why,omitempty"`
	Ready   bool   `json:"ready"`
	// Concerns is how many things the session's latest review flagged.
	Concerns int `json:"concerns,omitempty"`
}

type Clash struct {
	A     string   `json:"a"`
	B     string   `json:"b"`
	Files []string `json:"files"`
}

type Group struct {
	Title    string    `json:"title"`
	Sessions []Session `json:"sessions"`
	Hidden   int       `json:"hidden"`
	Folder   string    `json:"folder,omitempty"`
}

type Session struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Project string    `json:"project"`
	Folder  string    `json:"folder"`
	Branch  string    `json:"branch,omitempty"`
	State   string    `json:"state"`
	Status  string    `json:"status"`
	Since   time.Time `json:"since"`
	PR      string    `json:"pr,omitempty"`
	PRURL   string    `json:"prUrl,omitempty"`
	// PRState says where the PR stands, and PRNeedsYou that it's waiting on
	// the user, like failing checks or requested changes.
	PRState    string `json:"prState,omitempty"`
	PRNeedsYou bool   `json:"prNeedsYou,omitempty"`
	LastPrompt string `json:"lastPrompt,omitempty"`
	LastReply  string `json:"lastReply,omitempty"`
	Model      string `json:"model,omitempty"`
	Context    int    `json:"context,omitempty"`
	Changed    int    `json:"changed,omitempty"`
	Where      string `json:"where,omitempty"`
	Pinned     bool   `json:"pinned,omitempty"`
	Suggested  bool   `json:"suggested,omitempty"`
	Clash      string `json:"clash,omitempty"`
	// Stale says the session has moved on since the note was written.
	Summary    string `json:"summary,omitempty"`
	Stale      bool   `json:"stale,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	Why        string `json:"why,omitempty"`
	Ready      bool   `json:"ready,omitempty"`
	Running    bool   `json:"running,omitempty"`
	Archived   bool   `json:"archived,omitempty"`
	// Hosted sessions can take a prompt from the page directly.
	Hosted    bool     `json:"hosted,omitempty"`
	Closable  bool     `json:"closable,omitempty"`
	Trusted   bool     `json:"trusted,omitempty"`
	Review    *Verdict `json:"review,omitempty"`
	Reviewing bool     `json:"reviewing,omitempty"`
	// Changes says there is something to review: uncommitted work, commits
	// ahead or a pull request.
	Changes bool   `json:"changes,omitempty"`
	Goal    string `json:"goal,omitempty"`
}

type Event struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

type Today struct {
	Touched  int    `json:"touched"`
	PRs      int    `json:"prs"`
	Projects int    `json:"projects"`
	Busiest  string `json:"busiest,omitempty"`
}

type Verdict struct {
	OK       bool      `json:"ok"`
	Concerns []string  `json:"concerns,omitempty"`
	At       time.Time `json:"at"`
	// Earlier says the session has moved on since the review.
	Earlier bool `json:"earlier,omitempty"`
}

type Goal struct {
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Sessions []string `json:"sessions,omitempty"`
}
