//go:build amd64 && darwin

package nnue

import "syscall"

// macOS turns AVX-512 state saving on for a thread only when it first uses
// AVX-512, so XCR0 does not show it in advance. The kernel reports support
// through sysctl instead.
func osSavesAVX512(xcr0 uint32) bool {
	if xcr0&0xE0 == 0xE0 {
		return true
	}
	f, err := syscall.SysctlUint32("hw.optional.avx512f")
	if err != nil || f == 0 {
		return false
	}
	bw, err := syscall.SysctlUint32("hw.optional.avx512bw")
	return err == nil && bw != 0
}
