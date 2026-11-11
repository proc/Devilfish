//go:build amd64 && darwin

package nnue

import "syscall"

// osSavesAVX512 reports whether AVX-512 can be used safely.
//
// Darwin kernels 19.6.0 through 21.2.0 do not restore the AVX-512 mask
// registers (K0-K7) on return from a signal, and the Go runtime signals
// running threads routinely to preempt them. Like Go itself (golang/go#49233),
// AVX-512 is only used from Darwin 21.3.0 (macOS 12.2) on.
//
// macOS also turns AVX-512 state saving on for a thread only when it first
// uses AVX-512, so XCR0 does not show it in advance; the kernel reports
// support through sysctl instead.
func osSavesAVX512(xcr0 uint32) bool {
	release, err := syscall.Sysctl("kern.osrelease")
	if err != nil || !darwinVersionAtLeast(release, 21, 3, 0) {
		return false
	}
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
