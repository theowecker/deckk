package ui

import (
	"strings"
	"testing"
)

func TestFadeMutes256ColoursWithoutMisreadingTheIndex(t *testing.T) {
	grey := [3]int{0x5E, 0x5E, 0x72}
	got := fadeParams("38;5;39", grey, "38;2;94;94;114")
	if strings.Contains(got, "5;39") || !strings.HasPrefix(got, "38;2;") || strings.Count(got, ";") != 4 {
		t.Fatalf("got %q", got)
	}
	if got := fadeParams("48;5;0;1", grey, "B"); !strings.HasPrefix(got, "48;2;") || !strings.HasSuffix(got, ";1") {
		t.Fatalf("got %q", got)
	}
}
