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

//go:noescape
func fusedPieceNEON(dst, src *[EightL1Size]int16, w *int16, add *int32, nAdd int, rem *int32, nRem int)

//go:noescape
func fusedThreatNEON(dst, src *[EightL1Size]int16, w *int8, add *int32, nAdd int, rem *int32, nRem int)

func fusedPiece(dst, src *[EightL1Size]int16, w *int16, add *int32, nAdd int, rem *int32, nRem int) {
	fusedPieceNEON(dst, src, w, add, nAdd, rem, nRem)
}

func fusedThreat(dst, src *[EightL1Size]int16, w *int8, add *int32, nAdd int, rem *int32, nRem int) {
	fusedThreatNEON(dst, src, w, add, nAdd, rem, nRem)
}

//go:noescape
func l2LayerNEON(w *[EightL2Size][EightL3Size]float32, bias *[EightL3Size]float32, x *[EightL2Size]float32, out *[EightL3Size]float32)

func l2Layer(w *[EightL2Size][EightL3Size]float32, bias *[EightL3Size]float32, x *[EightL2Size]float32, out *[EightL3Size]float32) {
	l2LayerNEON(w, bias, x, out)
}

//go:noescape
func l1SparseIdxNEON(idx *uint16, n int, ft *[EightL1Size]uint8, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32)

// eightL1Layer is the first dense layer. It computes what eightL1 computes,
// but finds the non-zero inputs without branches (l1NonzeroIdx): the scan in
// l1NonzeroNEON walks the set bits of each block in a loop whose length
// depends on the data, and mispredicted on most blocks.
func eightL1Layer(ft *[EightL1Size]uint8, wFile *[EightL2Size * EightL1Size]int8, wByInput *[EightL1Size][EightL2Size]int8, pre *[EightL2Size]int32) {
	var idx [EightL1Size + 8]uint16
	n := l1NonzeroIdx(ft, &idx)
	var out [EightL2Size]int32
	l1SparseIdxNEON(&idx[0], n, ft, wByInput, &out)
	for j := range out {
		pre[j] += out[j]
	}
}
