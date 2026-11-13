package hq

import "testing"

func TestParseReview(t *testing.T) {
	if r := parseReview("OK\n"); !r.OK || r.Text != "" {
		t.Fatalf("%+v", r)
	}
	r := parseReview("**CONCERNS**\n- debug print left in api.go\n- no test for the retry")
	if r.OK || r.Text != "- debug print left in api.go\n- no test for the retry" {
		t.Fatalf("%+v", r)
	}
	if r := parseReview("I looked and it seems fine"); r.OK || r.Text == "" {
		t.Fatalf("an answer without a verdict counts as a concern to read: %+v", r)
	}
}

func TestSoundsDone(t *testing.T) {
	for reply, want := range map[string]bool{
		"Tests pass and the build is clean. Ready to commit when you are.": true,
		"Committed as a1b2c3 and pushed to the branch.":                    true,
		"Opened a PR: #212": true,
		"Should the retry live in the client or the server?":     false,
		"I read the handler; the bug is in how it parses dates.": false,
	} {
		if SoundsDone(reply) != want {
			t.Errorf("%q: want %v", reply, want)
		}
	}
}
