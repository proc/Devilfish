//go:build amd64

package nnue

// Kernels in sf_amd64.s.

//go:noescape
func nonzeroChunksAVX2(ft *[EightL1Size]uint8, out *uint16) int

//go:noescape
func nonzeroChunksSSE2(ft *[EightL1Size]uint8, out *uint16) int

//go:noescape
func chunkDot32AVX2(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32)

//go:noescape
func chunkDot32SSSE3(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32)

//go:noescape
func dot128AVX2(a *[128]uint8, w *[128]int8, out *[8]int32)

//go:noescape
func dot128SSSE3(a *[128]uint8, w *[128]int8, out *[8]int32)

//go:noescape
func sfAct7SSE2(x *[32]int32, bias *[32]int32, out *uint8)

//go:noescape
func sfAct6SSE2(x *[32]int32, bias *[32]int32, out *uint8)

//go:noescape
func psqtApplySSE2(dst *[8]int32, table *int32, add *int32, nAdd int, rem *int32, nRem int)

//go:noescape
func l1SparseIdxSSE2(idx *uint16, n int, ft *uint8, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32)

//go:noescape
func l1SparseIdxAVX2(idx *uint16, n int, ft *uint8, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32)

// hasSSSE3 reports whether the CPU has SSSE3 (PMADDUBSW), which the SSE tier
// uses for the Stockfish network's dense layers. Nearly every x86-64 CPU
// does; the few that do not (AMD K8/K10) take the plain SSE2 paths.
var hasSSSE3 = func() bool {
	_, _, ecx1, _ := cpuid(1, 0)
	return ecx1&(1<<9) != 0
}()

// sfDotTier says which group-of-four dot-product kernels to use: 2 for AVX2,
// 1 for SSSE3, 0 for none. simd changes only in tests.
func sfDotTier() int {
	switch {
	case simd >= simdAVX2:
		return 2
	case simd == simdSSE2 && hasSSSE3:
		return 1
	}
	return 0
}

// sparse32 adds in[i] * (weight row i) into out for each listed input i, for
// a layer of 32 outputs whose weights wt holds as one 32-byte row per input.
// out is overwritten.
func sparse32(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32) {
	refSparse32(idx, n, in, wt, out)
}

// sfFirstLayer computes the 32 first-layer sums (without bias) of a
// Stockfish layer stack from the feature transformer output, most of which
// is zero: one branch-free scan for the groups of four inputs that are not
// all zero, then a dot product of each such group with its weights. Without
// SSSE3 it runs as two 16-output layers through the 8Chess kernels.
func sfFirstLayer(ft *[EightL1Size]uint8, st *sfStack, out *[32]int32, idx *[EightL1Size + 8]uint16) {
	switch sfDotTier() {
	case 2:
		n := nonzeroChunksAVX2(ft, &idx[0])
		chunkDot32AVX2(&idx[0], n, &ft[0], &st.fc0Chunk[0][0][0], out)
	case 1:
		n := nonzeroChunksSSE2(ft, &idx[0])
		chunkDot32SSSE3(&idx[0], n, &ft[0], &st.fc0Chunk[0][0][0], out)
	default:
		for half := 0; half < 2; half++ {
			var pre [EightL2Size]int32
			eightL1Layer(ft, &st.fc0File[half], &st.fc0ByInput[half], &pre)
			copy(out[half*EightL2Size:], pre[:])
		}
	}
}

// sfAllGroups lists every group of four of the second layer's 64 inputs.
var sfAllGroups = [16]uint16{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// sfSecondLayer computes the 32 second-layer sums (without bias) from the
// 64 activations in act[:64].
func sfSecondLayer(act *[128]uint8, st *sfStack, out *[32]int32) {
	switch sfDotTier() {
	case 2:
		chunkDot32AVX2(&sfAllGroups[0], len(sfAllGroups), &act[0], &st.fc1Chunk[0][0][0], out)
	case 1:
		chunkDot32SSSE3(&sfAllGroups[0], len(sfAllGroups), &act[0], &st.fc1Chunk[0][0][0], out)
	default:
		refSFSecondLayer(act, st, out)
	}
}

// sfActivate writes the two activations of v = x[i] + bias[i] to out:
// out[i] = min(127, v*v >> (2*bits+7)) and out[32+i] = clamp(v >> bits, 0, 127),
// for bits 7 (after the first layer) or 6 (after the second).
func sfActivate(x, bias *[32]int32, out *[64]uint8, bits uint) {
	switch {
	case simd == simdGeneric:
		refSFActivate(x, bias, out, bits)
	case bits == 7:
		sfAct7SSE2(x, bias, &out[0])
	default:
		sfAct6SSE2(x, bias, &out[0])
	}
}

// sfDot128 is the dot product of a (each at most 127) and w.
func sfDot128(a *[128]uint8, w *[128]int8) int32 {
	var s [8]int32
	switch sfDotTier() {
	case 2:
		dot128AVX2(a, w, &s)
	case 1:
		dot128SSSE3(a, w, &s)
	default:
		return refDot128(a, w)
	}
	return s[0] + s[1] + s[2] + s[3] + s[4] + s[5] + s[6] + s[7]
}

// sfApplyPSQT adds the PSQT rows of add and subtracts those of rem.
func sfApplyPSQT(dst *[sfPSQTBuckets]int32, table []int32, add, rem *rowList) {
	if simd == simdGeneric {
		refApplyPSQT(dst, table, add, rem)
		return
	}
	if add.n+rem.n != 0 {
		psqtApplySSE2(dst, &table[0], &add.rows[0], add.n, &rem.rows[0], rem.n)
	}
}

// eightL1Idx is the first dense layer of an 8Chess network: a branch-free
// scan for the non-zero inputs, then one pass over them two at a time. (The
// older l1Nonzero kernels walk the set bits of each block in a loop whose
// length depends on the data, which mispredicts on most blocks.)
func eightL1Idx(ft *[EightL1Size]uint8, wt *[EightL1Size][EightL2Size]int8, pre *[EightL2Size]int32) {
	var idx [EightL1Size + 8]uint16
	n := l1NonzeroIdx(ft, &idx)
	var out [EightL2Size]int32
	if simd >= simdAVX2 {
		l1SparseIdxAVX2(&idx[0], n, &ft[0], wt, &out)
	} else {
		l1SparseIdxSSE2(&idx[0], n, &ft[0], wt, &out)
	}
	for j := range out {
		pre[j] += out[j]
	}
}
