package terminal

import (
	"bytes"
	"os"
	"regexp"
	"time"

	"golang.org/x/sys/unix"
)

// Background asks the terminal for its background colour as #rrggbb. The
// cursor report asked for after it is one every terminal sends, so its arrival
// ends the wait early when the terminal doesn't know the colour question.
func Background(in, out *os.File, timeout time.Duration) (string, bool) {
	fd := int(in.Fd())
	old, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return "", false
	}
	quiet := *old
	quiet.Lflag &^= unix.ECHO | unix.ICANON
	if unix.IoctlSetTermios(fd, unix.TIOCSETA, &quiet) != nil {
		return "", false
	}
	defer unix.IoctlSetTermios(fd, unix.TIOCSETA, old) //nolint:errcheck

	if _, err := out.WriteString("\x1b]11;?\x1b\\\x1b[6n"); err != nil {
		return "", false
	}
	var reply []byte
	buf := make([]byte, 256)
	deadline := time.Now().Add(timeout)
	for !cursorReport.Match(reply) {
		left := time.Until(deadline)
		if left <= 0 || !readable(fd, left) {
			break
		}
		n, err := unix.Read(fd, buf)
		if err != nil || n == 0 {
			break
		}
		reply = append(reply, buf[:n]...)
	}
	if !cursorReport.Match(reply) {
		// A terminal answering late would type its answer into tmux.
		for readable(fd, 100*time.Millisecond) {
			n, err := unix.Read(fd, buf)
			if err != nil || n == 0 {
				break
			}
			reply = append(reply, buf[:n]...)
		}
	}
	return parseBackground(reply)
}

var (
	cursorReport = regexp.MustCompile(`\x1b\[\d+;\d+R`)
	colourReply  = regexp.MustCompile(`\]11;rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})`)
)

func parseBackground(reply []byte) (string, bool) {
	m := colourReply.FindSubmatch(reply)
	if m == nil {
		return "", false
	}
	hex := []byte{'#'}
	for _, c := range m[1:] {
		c = bytes.ToLower(c)
		if len(c) == 1 {
			c = append(c, c[0])
		}
		hex = append(hex, c[:2]...)
	}
	return string(hex), true
}

func readable(fd int, wait time.Duration) bool {
	var set unix.FdSet
	set.Set(fd)
	tv := unix.NsecToTimeval(wait.Nanoseconds())
	n, err := unix.Select(fd+1, &set, nil, nil, &tv)
	return err == nil && n > 0
}
