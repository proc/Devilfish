//go:build arm64

package nnue

const SIMDInstructionSet = "ARM64 NEON SIMD"

//go:noescape
func addVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func subVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func addVector1024NEON(acc *[EightL1Size]int16, weights *int16)

//go:noescape
func addThreatVector1024NEON(acc *[EightL1Size]int16, weights *int8)

//go:noescape
func subVector1024NEON(acc *[EightL1Size]int16, weights *int16)

//go:noescape
func subThreatVector1024NEON(acc *[EightL1Size]int16, weights *int8)

//go:noescape
func activateEightFTNEON(out *uint8, pst *int16, thr *int16)
