//go:build !arm64 && !amd64

package nnue

// Architectures without hand-written kernels use the portable versions.
// The function names follow the arm64 originals.

const SIMDInstructionSet = "Scalar Generic"

func addVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	refAddVector(acc, weights)
}

func subVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	refSubVector(acc, weights)
}

func addVector1024NEON(acc *[EightL1Size]int16, weights *int16) {
	refAddVector1024(acc, weights)
}

func subVector1024NEON(acc *[EightL1Size]int16, weights *int16) {
	refSubVector1024(acc, weights)
}

func addThreatVector1024NEON(acc *[EightL1Size]int16, weights *int8) {
	refAddThreatVector1024(acc, weights)
}

func subThreatVector1024NEON(acc *[EightL1Size]int16, weights *int8) {
	refSubThreatVector1024(acc, weights)
}

func activateEightFTNEON(out *uint8, pst *int16, thr *int16) {
	refActivateEightFT(out, pst, thr)
}

func l1Sparse(nz *uint32, n int, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32) {
	refL1Sparse(nz, n, wt, out)
}

func l1Nonzero(ft *[EightL1Size]uint8, nz *[EightL1Size + 1]uint32) int {
	return refL1Nonzero(ft, nz)
}

func fusedPiece(dst, src *[EightL1Size]int16, w *int16, add *int32, nAdd int, rem *int32, nRem int) {
	refFusedPiece(dst, src, w, add, nAdd, rem, nRem)
}

func fusedThreat(dst, src *[EightL1Size]int16, w *int8, add *int32, nAdd int, rem *int32, nRem int) {
	refFusedThreat(dst, src, w, add, nAdd, rem, nRem)
}

func l2Layer(w *[EightL2Size][EightL3Size]float32, bias *[EightL3Size]float32, x *[EightL2Size]float32, out *[EightL3Size]float32) {
	refL2(w, bias, x, out)
}

// eightL1Layer is the first dense layer (see eightL1).
func eightL1Layer(ft *[EightL1Size]uint8, wFile *[EightL2Size * EightL1Size]int8, wByInput *[EightL1Size][EightL2Size]int8, pre *[EightL2Size]int32) {
	eightL1(ft, wByInput, pre)
}
