package config

import (
	"runtime"
	"syscall"
)

// DefaultThreads is the search thread count used when none is configured:
// the number of performance cores. On Apple Silicon a search thread that
// lands on an efficiency core runs far slower than the rest, and using every
// core measured more than twice as slow to a given depth as using the
// performance cores alone.
func DefaultThreads() int {
	if n, err := syscall.SysctlUint32("hw.perflevel0.physicalcpu"); err == nil && n > 0 && int(n) <= runtime.NumCPU() {
		return int(n)
	}
	return runtime.NumCPU()
}
