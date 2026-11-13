//go:build arm64

package nnue

import (
	"os"
	"runtime"
	"strings"
)

//go:noescape
func sparse32NEON(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32)

//go:noescape
func sfAct7NEON(x *[32]int32, bias *[32]int32, out *uint8)

//go:noescape
func sfAct6NEON(x *[32]int32, bias *[32]int32, out *uint8)

//go:noescape
func dot128NEON(a *[128]uint8, w *[128]int8, out *[4]int32)

//go:noescape
func psqtApplyNEON(dst *[8]int32, table *int32, add *int32, nAdd int, rem *int32, nRem int)

//go:noescape
func chunkDot32NEON(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32)

// hasDotProd reports whether the CPU has the dot-product instructions
// (SDOT). Every Apple Silicon CPU does; elsewhere the kernel says.
var hasDotProd = func() bool {
	switch runtime.GOOS {
	case "darwin", "ios":
		return true
	case "linux", "android":
		data, err := os.ReadFile("/proc/cpuinfo")
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Features") {
				for _, f := range strings.Fields(line) {
					if f == "asimddp" {
						return true
					}
				}
				return false
			}
		}
	}
	return false
}()

// sparse32 adds in[i] * (weight row i) into out for each listed input i, for
// a layer of 32 outputs whose weights wt holds as one 32-byte row per input.
// out is overwritten.
func sparse32(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32) {
	sparse32NEON(idx, n, in, wt, out)
}

// sfFirstLayer computes the 32 first-layer sums (without bias) of a
// Stockfish layer stack from the feature transformer output, most of which
// is zero. With dot-product instructions it works on groups of four inputs:
// one scan for the groups that are not all zero, then eight SDOTs per group.
// Otherwise it lists the non-zero inputs and multiplies each into its weight
// row. idx is scratch space.
func sfFirstLayer(ft *[EightL1Size]uint8, st *sfStack, out *[32]int32, idx *[EightL1Size + 8]uint16) {
	if hasDotProd {
		n := l1NonzeroChunks(ft, idx)
		chunkDot32NEON(&idx[0], n, &ft[0], &st.fc0Chunk[0][0][0], out)
		return
	}
	n := l1NonzeroIdx(ft, idx)
	sparse32NEON(&idx[0], n, &ft[0], &st.fc0[0][0], out)
}

// sfActivate writes the two activations of v = x[i] + bias[i] to out:
// out[i] = min(127, v*v >> (2*bits+7)) and out[32+i] = clamp(v >> bits, 0, 127),
// for bits 7 (after the first layer) or 6 (after the second).
func sfActivate(x, bias *[32]int32, out *[64]uint8, bits uint) {
	if bits == 7 {
		sfAct7NEON(x, bias, &out[0])
	} else {
		sfAct6NEON(x, bias, &out[0])
	}
}

// sfDot128 is the dot product of a (each at most 127) and w.
func sfDot128(a *[128]uint8, w *[128]int8) int32 {
	if !hasDotProd {
		return refDot128(a, w)
	}
	var s [4]int32
	dot128NEON(a, w, &s)
	return s[0] + s[1] + s[2] + s[3]
}

// sfApplyPSQT adds the PSQT rows of add and subtracts those of rem.
func sfApplyPSQT(dst *[sfPSQTBuckets]int32, table []int32, add, rem *rowList) {
	if add.n+rem.n != 0 {
		psqtApplyNEON(dst, &table[0], &add.rows[0], add.n, &rem.rows[0], rem.n)
	}
}

// sfSecondLayer computes the 32 second-layer sums (without bias) from the
// 64 activations in act[:64], many of which are zero.
func sfSecondLayer(act *[128]uint8, st *sfStack, out *[32]int32) {
	var nz [2*sfL2 + 8]uint16
	n := nonzero64((*[64]uint8)(act[:64]), &nz)
	sparse32NEON(&nz[0], n, &act[0], &st.fc1[0][0], out)
}
