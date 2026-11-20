//go:build windows

package hud

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	lockRetryBudget      = 40 * time.Millisecond
	windowsStaleLockAge  = 5 * time.Second
	createNoWindowFlag   = 0x08000000
	detachedProcessFlags = 0x00000008
)

func withFileLock(lockPath string, fn func()) bool {
	deadline := time.Now().Add(lockRetryBudget)
	for {
		release, ok := tryAcquireLock(lockPath, windowsStaleLockAge)
		if ok {
			defer release()
			fn()
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func tryAcquireLock(lockPath string, staleAfter time.Duration) (func(), bool) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, false
	}
	if staleAfter <= 0 {
		staleAfter = windowsStaleLockAge
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, true
		}
		if st, statErr := os.Stat(lockPath); statErr == nil && time.Since(st.ModTime()) > staleAfter {
			_ = os.Remove(lockPath)
			continue
		}
		return nil, false
	}
	return nil, false
}

func isLockHeld(lockPath string, staleAfter time.Duration) bool {
	if staleAfter <= 0 {
		staleAfter = windowsStaleLockAge
	}
	st, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	return time.Since(st.ModTime()) <= staleAfter
}

func detachCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindowFlag | detachedProcessFlags,
	}
}
