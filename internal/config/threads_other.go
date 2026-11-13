//go:build !darwin

package config

import "runtime"

// DefaultThreads is the search thread count used when none is configured.
func DefaultThreads() int {
	return runtime.NumCPU()
}
