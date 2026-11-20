//go:build unix

package hud

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const lockRetryBudget = 40 * time.Millisecond

// withFileLock acquires an advisory exclusive flock on lockPath for the duration of fn.
// It bounds waiting to keep statusline rendering non-blocking under heavy contention.
func withFileLock(lockPath string, fn func()) bool {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return false
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()

	deadline := time.Now().Add(lockRetryBudget)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
			fn()
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// tryAcquireLock attempts a non-blocking exclusive flock on lockPath.
// Used to deduplicate background git refresh helpers.
func tryAcquireLock(lockPath string, _ time.Duration) (func(), bool) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, false
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, false
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true
}

// isLockHeld checks whether another process currently holds an exclusive flock on lockPath.
func isLockHeld(lockPath string, _ time.Duration) bool {
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// detachCmd isolates a short-lived background helper into its own process group so
// cancellation of the parent statusline render does not abort cache updates mid-write.
func detachCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
