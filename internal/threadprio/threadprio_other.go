//go:build !darwin

package threadprio

const supported = false

func setLow(bool) bool { return false }
