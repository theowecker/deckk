package claude

import (
	"errors"
	"os/exec"
	"strings"
)

// Mentioning returns the transcripts among paths that contain every word of
// query anywhere, ignoring case. grep reads gigabytes of transcripts far
// faster than parsing them would.
func Mentioning(paths []string, query string) (map[string]bool, error) {
	for _, word := range strings.Fields(query) {
		if len(paths) == 0 {
			break
		}
		out, err := exec.Command("grep", append([]string{"-l", "-i", "-F", "-e", word, "--"}, paths...)...).Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			out, err = nil, nil
		}
		if err != nil {
			return nil, err
		}
		paths = strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' })
	}
	found := make(map[string]bool, len(paths))
	for _, p := range paths {
		found[p] = true
	}
	return found, nil
}
