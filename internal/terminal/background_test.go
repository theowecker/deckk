package terminal

import "testing"

func TestParseBackground(t *testing.T) {
	for reply, want := range map[string]string{
		"\x1b]11;rgb:2a2a/2a2a/4a4a\x1b\\\x1b[3;1R": "#2a2a4a",
		"\x1b]11;rgb:FF/80/0\a\x1b[1;1R":            "#ff8000",
		"\x1b[12;40R":                               "",
	} {
		got, ok := parseBackground([]byte(reply))
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q %v", reply, got, ok)
		}
	}
}
