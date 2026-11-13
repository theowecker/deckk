package web

import "time"

// State is everything the page shows, as the dashboard last saw it.
// It carries no timestamp of its own, so an unchanged dashboard publishes
// identical bytes and the page isn't redrawn for nothing.
type State struct {
	Counts     Counts  `json:"counts"`
	NextUp     []Next  `json:"nextUp"`
	Clashes    []Clash `json:"clashes"`
	Groups     []Group `json:"groups"`
	Projects   []Group `json:"projects"`
	Goals      string  `json:"goals"`
	Activity   []Event `json:"activity"`
	Today      Today   `json:"today"`
	DeckkState string  `json:"deckkState"`
	// Deckk is the deckk session itself, for the chat.
	Deckk Deckk `json:"deckk"`
	// Background are recent runs nobody typed into, kept apart.
	Background []Session `json:"background"`
	// Notice is the dashboard's latest message, such as why a prompt
	// couldn't be typed in.
	Notice Notice `json:"notice"`
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

// Next is a Next up entry: a suggestion for a session, an offer to resume
// one, or a proposed new session.
type Next struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Project string `json:"project"`
	Prompt  string `json:"prompt"`
	Why     string `json:"why,omitempty"`
	Ready   bool   `json:"ready"`
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
}

type Session struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Project    string    `json:"project"`
	Folder     string    `json:"folder"`
	Branch     string    `json:"branch,omitempty"`
	State      string    `json:"state"`
	Status     string    `json:"status"`
	Since      time.Time `json:"since"`
	PR         string    `json:"pr,omitempty"`
	PRURL      string    `json:"prUrl,omitempty"`
	LastPrompt string    `json:"lastPrompt,omitempty"`
	LastReply  string    `json:"lastReply,omitempty"`
	Model      string    `json:"model,omitempty"`
	Context    int       `json:"context,omitempty"`
	Changed    int       `json:"changed,omitempty"`
	Where      string    `json:"where,omitempty"`
	Pinned     bool      `json:"pinned,omitempty"`
	Suggested  bool      `json:"suggested,omitempty"`
	Clash      string    `json:"clash,omitempty"`
	// Summary is the deckk session's note on the session; Stale says the
	// session has moved on since it was written.
	Summary string `json:"summary,omitempty"`
	Stale   bool   `json:"stale,omitempty"`
	// The deckk session's suggestion for this session, if it has one.
	Suggestion string `json:"suggestion,omitempty"`
	Why        string `json:"why,omitempty"`
	Ready      bool   `json:"ready,omitempty"`
	Running    bool   `json:"running,omitempty"`
	Archived   bool   `json:"archived,omitempty"`
	// Hosted sessions can take a prompt from the page directly.
	Hosted bool `json:"hosted,omitempty"`
	// Trusted sessions get deckk's safe suggestions sent without review.
	Trusted bool `json:"trusted,omitempty"`
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
