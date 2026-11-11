//go:build amd64 && !darwin

package nnue

// osSavesAVX512 reports whether the OS saves the opmask and ZMM register
// state (XCR0 bits 5, 6 and 7).
func osSavesAVX512(xcr0 uint32) bool {
	return xcr0&0xE0 == 0xE0
}
