package claude

import (
	"errors"
	"syscall"
	"time"
)

// Waits so the transcript is complete before the session resumes elsewhere.
func Stop(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("claude did not quit within 5 seconds")
}
