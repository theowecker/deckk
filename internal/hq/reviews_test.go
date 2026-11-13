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
