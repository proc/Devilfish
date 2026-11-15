//go:build unix

package coreshare

import (
	"os"
	"syscall"
	"time"
)

// lockFile is an exclusive advisory lock on a file. The zero value holds
// nothing.
type lockFile struct{ f *os.File }

// lockTry takes the lock on path if no one else holds it, creating the file
// if need be. Failing to open the file counts as not getting the lock.
func lockTry(path string) (lockFile, bool) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return lockFile{}, false
	}
	return lockOpened(f)
}

// isLocked reports whether another holder has the lock on path. A file that
// does not exist is not locked.
func isLocked(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	l, ok := lockOpened(f)
	l.unlock()
	return !ok
}

func lockOpened(f *os.File) (lockFile, bool) {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return lockFile{}, false
	}
	return lockFile{f}, true
}

// lockWait takes the lock on path, waiting a short time for a holder to let
// go. It gives up rather than block a search on a stuck process.
func lockWait(path string) (lockFile, bool) {
	for i := 0; i < 200; i++ {
		if l, ok := lockTry(path); ok {
			return l, true
		}
		time.Sleep(250 * time.Microsecond)
	}
	return lockFile{}, false
}

func (l lockFile) unlock() {
	if l.f != nil {
		_ = l.f.Close() // closing the file drops the lock
	}
}
