//go:build arm64

package nnue

const SIMDInstructionSet = "ARM64 NEON SIMD"

//go:noescape
func addVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func subVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16)
