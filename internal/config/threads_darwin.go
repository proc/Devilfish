package config

import (
	"runtime"
	"syscall"
)

// DefaultThreads is the search thread count used when none is configured:
// the number of performance cores. On Apple Silicon the efficiency cores add
// little search speed (measure with "bench threads"), and leaving them free
// keeps the GUI and an opponent engine from competing with the search.
func DefaultThreads() int {
	if n, err := syscall.SysctlUint32("hw.perflevel0.physicalcpu"); err == nil && n > 0 && int(n) <= runtime.NumCPU() {
		return int(n)
	}
	return runtime.NumCPU()
}
