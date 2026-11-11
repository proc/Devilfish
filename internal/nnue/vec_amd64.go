//go:build amd64

package nnue

import (
	"os"
	"strings"
)

// x86-64 kernels, in three tiers. SSE2 is part of the amd64 baseline, so it
// is always available. AVX2 (256-bit) is used when the CPU and OS support it,
// and AVX-512 (F + BW) when they support that. The best tier is chosen once
// at startup.
//
// DEVILFISH_SIMD caps the tier: avx2, sse2, or generic (the portable Go
// code). Use it to compare speed, or on a CPU that clocks down under 512-bit
// load. A tier the machine cannot run is never chosen; asking for one gives
// the best tier below it.

type simdLevel int

const (
	simdGeneric simdLevel = iota
	simdSSE2
	simdAVX2
	simdAVX512
)

var simd = detectSIMD()

// SIMDInstructionSet names the kernels in use, for the startup banner.
var SIMDInstructionSet = simdName(simd)

func simdName(l simdLevel) string {
	switch l {
	case simdAVX512:
		return "x86-64 AVX-512BW"
	case simdAVX2:
		return "x86-64 AVX2"
	case simdSSE2:
		return "x86-64 SSE2"
	}
	return "Scalar Generic"
}

func detectSIMD() simdLevel {
	limit := simdAVX512
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DEVILFISH_SIMD"))) {
	case "generic", "scalar", "none":
		limit = simdGeneric
	case "sse", "sse2":
		limit = simdSSE2
	case "avx2":
		limit = simdAVX2
	}
	best := bestSIMD()
	if best > limit {
		return limit
	}
	return best
}

// bestSIMD returns the highest tier this CPU and OS can run.
func bestSIMD() simdLevel {
	if hasAVX512BW() {
		return simdAVX512
	}
	if hasAVX2() {
		return simdAVX2
	}
	return simdSSE2
}

// hasAVX2 reports whether the CPU implements AVX and AVX2 and the OS saves
// the YMM register state.
func hasAVX2() bool {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 7 {
		return false
	}
	_, _, ecx1, _ := cpuid(1, 0)
	if ecx1&(1<<27) == 0 || ecx1&(1<<28) == 0 { // OSXSAVE, AVX
		return false
	}
	_, ebx7, _, _ := cpuid(7, 0)
	if ebx7&(1<<5) == 0 { // AVX2
		return false
	}
	xcr0, _ := xgetbv()
	return xcr0&0x6 == 0x6 // XMM and YMM state
}

// hasAVX512BW reports whether the CPU implements AVX-512 F and BW and the OS
// saves the vector state they use.
func hasAVX512BW() bool {
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 7 {
		return false
	}
	_, _, ecx1, _ := cpuid(1, 0)
	if ecx1&(1<<27) == 0 { // OSXSAVE: XGETBV usable
		return false
	}
	_, ebx7, _, _ := cpuid(7, 0)
	if ebx7&(1<<16) == 0 || ebx7&(1<<30) == 0 { // AVX512F, AVX512BW
		return false
	}
	xcr0, _ := xgetbv()
	if xcr0&0x6 != 0x6 { // XMM and YMM state
		return false
	}
	return osSavesAVX512(xcr0)
}

func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

func xgetbv() (eax, edx uint32)

// Assembly kernels (vec_amd64.s).

//go:noescape
func addVector256SSE2(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func subVector256SSE2(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func addVector1024SSE2(acc *[EightL1Size]int16, weights *int16)

//go:noescape
func subVector1024SSE2(acc *[EightL1Size]int16, weights *int16)

//go:noescape
func addThreatVector1024SSE2(acc *[EightL1Size]int16, weights *int8)

//go:noescape
func subThreatVector1024SSE2(acc *[EightL1Size]int16, weights *int8)

//go:noescape
func activateEightFTSSE2(out *uint8, pst *int16, thr *int16)

//go:noescape
func addVector256AVX2(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func subVector256AVX2(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func addVector1024AVX2(acc *[EightL1Size]int16, weights *int16)

//go:noescape
func subVector1024AVX2(acc *[EightL1Size]int16, weights *int16)

//go:noescape
func addThreatVector1024AVX2(acc *[EightL1Size]int16, weights *int8)

//go:noescape
func subThreatVector1024AVX2(acc *[EightL1Size]int16, weights *int8)

//go:noescape
func activateEightFTAVX2(out *uint8, pst *int16, thr *int16)

//go:noescape
func addVector256AVX512(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func subVector256AVX512(acc *[HiddenSize]int16, weights *[HiddenSize]int16)

//go:noescape
func addVector1024AVX512(acc *[EightL1Size]int16, weights *int16)

//go:noescape
func subVector1024AVX512(acc *[EightL1Size]int16, weights *int16)

//go:noescape
func addThreatVector1024AVX512(acc *[EightL1Size]int16, weights *int8)

//go:noescape
func subThreatVector1024AVX512(acc *[EightL1Size]int16, weights *int8)

//go:noescape
func activateEightFTAVX512(out *uint8, pst *int16, thr *int16)

//go:noescape
func l1NonzeroSSE2(ft *[EightL1Size]uint8, nz *uint32) int

//go:noescape
func l1NonzeroAVX2(ft *[EightL1Size]uint8, nz *uint32) int

//go:noescape
func l1NonzeroAVX512(ft *[EightL1Size]uint8, nz *uint32) int

//go:noescape
func l1SparseSSE2(nz *uint32, n int, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32)

//go:noescape
func l1SparseAVX2(nz *uint32, n int, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32)

//go:noescape
func l1SparseAVX512(nz *uint32, n int, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32)

// Dispatch. The function names follow the arm64 originals so the callers are
// the same on every architecture. simd never changes after startup (except in
// tests), so the branches predict perfectly.

func addVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	switch simd {
	case simdAVX512:
		addVector256AVX512(acc, weights)
	case simdAVX2:
		addVector256AVX2(acc, weights)
	case simdSSE2:
		addVector256SSE2(acc, weights)
	default:
		refAddVector(acc, weights)
	}
}

func subVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	switch simd {
	case simdAVX512:
		subVector256AVX512(acc, weights)
	case simdAVX2:
		subVector256AVX2(acc, weights)
	case simdSSE2:
		subVector256SSE2(acc, weights)
	default:
		refSubVector(acc, weights)
	}
}

func addVector1024NEON(acc *[EightL1Size]int16, weights *int16) {
	switch simd {
	case simdAVX512:
		addVector1024AVX512(acc, weights)
	case simdAVX2:
		addVector1024AVX2(acc, weights)
	case simdSSE2:
		addVector1024SSE2(acc, weights)
	default:
		refAddVector1024(acc, weights)
	}
}

func subVector1024NEON(acc *[EightL1Size]int16, weights *int16) {
	switch simd {
	case simdAVX512:
		subVector1024AVX512(acc, weights)
	case simdAVX2:
		subVector1024AVX2(acc, weights)
	case simdSSE2:
		subVector1024SSE2(acc, weights)
	default:
		refSubVector1024(acc, weights)
	}
}

func addThreatVector1024NEON(acc *[EightL1Size]int16, weights *int8) {
	switch simd {
	case simdAVX512:
		addThreatVector1024AVX512(acc, weights)
	case simdAVX2:
		addThreatVector1024AVX2(acc, weights)
	case simdSSE2:
		addThreatVector1024SSE2(acc, weights)
	default:
		refAddThreatVector1024(acc, weights)
	}
}

func subThreatVector1024NEON(acc *[EightL1Size]int16, weights *int8) {
	switch simd {
	case simdAVX512:
		subThreatVector1024AVX512(acc, weights)
	case simdAVX2:
		subThreatVector1024AVX2(acc, weights)
	case simdSSE2:
		subThreatVector1024SSE2(acc, weights)
	default:
		refSubThreatVector1024(acc, weights)
	}
}

func activateEightFTNEON(out *uint8, pst *int16, thr *int16) {
	switch simd {
	case simdAVX512:
		activateEightFTAVX512(out, pst, thr)
	case simdAVX2:
		activateEightFTAVX2(out, pst, thr)
	case simdSSE2:
		activateEightFTSSE2(out, pst, thr)
	default:
		refActivateEightFT(out, pst, thr)
	}
}

func l1Sparse(nz *uint32, n int, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32) {
	switch simd {
	case simdAVX512:
		l1SparseAVX512(nz, n, wt, out)
	case simdAVX2:
		l1SparseAVX2(nz, n, wt, out)
	case simdSSE2:
		l1SparseSSE2(nz, n, wt, out)
	default:
		refL1Sparse(nz, n, wt, out)
	}
}

func l1Nonzero(ft *[EightL1Size]uint8, nz *[EightL1Size + 1]uint32) int {
	switch simd {
	case simdAVX512:
		return l1NonzeroAVX512(ft, &nz[0])
	case simdAVX2:
		return l1NonzeroAVX2(ft, &nz[0])
	case simdSSE2:
		return l1NonzeroSSE2(ft, &nz[0])
	}
	return refL1Nonzero(ft, nz)
}
