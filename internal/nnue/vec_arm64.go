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

//go:noescape
func l1NonzeroNEON(ft *[EightL1Size]uint8, nz *uint32) int

//go:noescape
func l1SparseNEON(nz *uint32, n int, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32)

func l1Nonzero(ft *[EightL1Size]uint8, nz *[EightL1Size + 1]uint32) int {
	return l1NonzeroNEON(ft, &nz[0])
}

func l1Sparse(nz *uint32, n int, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32) {
	l1SparseNEON(nz, n, wt, out)
}
