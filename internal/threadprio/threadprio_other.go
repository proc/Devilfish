//go:build !darwin && !linux && !windows

package threadprio

const supported = false

func setLow(bool) bool { return false }
