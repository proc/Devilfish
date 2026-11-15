//go:build !unix

package coreshare

// File locks are not implemented on this platform: every search gets its
// full thread count.

type lockFile struct{}

func lockTry(string) (lockFile, bool)  { return lockFile{}, false }
func lockWait(string) (lockFile, bool) { return lockFile{}, false }
func isLocked(string) bool             { return false }
func (lockFile) unlock()               {}
