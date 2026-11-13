package web

import "time"

// State is everything the page shows, as the dashboard last saw it.
type State struct {
	Updated    time.Time `json:"updated"`
	Counts     Counts    `json:"counts"`
	NextUp     []Next    `json:"nextUp"`
	Clashes    []Clash   `json:"clashes"`
	Groups     []Group   `json:"groups"`
	Goals      string    `json:"goals"`
	Activity   []Event   `json:"activity"`
	Today      Today     `json:"today"`
	DeckkState string    `json:"deckkState"`
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
