// Package threadprio lowers and restores the scheduling priority of the
// calling OS thread, so a background search (pondering) gives way to other
// work on the machine.
package threadprio

// Supported reports whether SetLow changes anything on this platform.
func Supported() bool { return supported }

// SetLow gives the calling OS thread a low priority (low true) or its normal
// one (low false) and reports whether that worked. The caller must be locked
// to its thread (runtime.LockOSThread) for as long as the low priority should
// hold, and must restore the normal priority before unlocking.
func SetLow(low bool) bool { return setLow(low) }
