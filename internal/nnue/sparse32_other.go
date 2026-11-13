//go:build !arm64

package nnue

// sparse32 adds in[i] * (weight row i) into out for each listed input i, for
// a layer of 32 outputs whose weights wt holds as one 32-byte row per input.
// out is overwritten.
func sparse32(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32) {
	refSparse32(idx, n, in, wt, out)
}

// sfFirstLayer computes the 32 first-layer sums (without bias) of a
// Stockfish layer stack, as two 16-output layers through this
// architecture's first-layer kernels. idx is unused here.
func sfFirstLayer(ft *[EightL1Size]uint8, st *sfStack, out *[32]int32, idx *[EightL1Size + 8]uint16) {
	for half := 0; half < 2; half++ {
		var pre [EightL2Size]int32
		eightL1Layer(ft, &st.fc0File[half], &st.fc0ByInput[half], &pre)
		copy(out[half*EightL2Size:], pre[:])
	}
}

// sfActivate writes the two activations of v = x[i] + bias[i] to out:
// out[i] = min(127, v*v >> (2*bits+7)) and out[32+i] = clamp(v >> bits, 0, 127).
func sfActivate(x, bias *[32]int32, out *[64]uint8, bits uint) {
	refSFActivate(x, bias, out, bits)
}

// sfDot128 is the dot product of a (each at most 127) and w.
func sfDot128(a *[128]uint8, w *[128]int8) int32 { return refDot128(a, w) }

// sfApplyPSQT adds the PSQT rows of add and subtracts those of rem.
func sfApplyPSQT(dst *[sfPSQTBuckets]int32, table []int32, add, rem *rowList) {
	refApplyPSQT(dst, table, add, rem)
}
