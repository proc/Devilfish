package nnue

import (
	"encoding/binary"
	"math/bits"
	"unsafe"
)

// Portable reference versions of the accumulator kernels. The architecture
// files (vec_arm64.*, vec_amd64.*, vec_generic.go) provide the functions the
// rest of the package calls; these are the definition of what those functions
// must compute, used directly on architectures without assembly and by the
// tests that check the assembly.
//
// All int16 arithmetic wraps, as the SIMD versions do.

func refAddVector(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	for i := 0; i < HiddenSize; i++ {
		acc[i] += weights[i]
	}
}

func refSubVector(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	for i := 0; i < HiddenSize; i++ {
		acc[i] -= weights[i]
	}
}

func refAddVector1024(acc *[EightL1Size]int16, weights *int16) {
	wSlice := unsafe.Slice(weights, EightL1Size)
	for i := 0; i < EightL1Size; i++ {
		acc[i] += wSlice[i]
	}
}

func refSubVector1024(acc *[EightL1Size]int16, weights *int16) {
	wSlice := unsafe.Slice(weights, EightL1Size)
	for i := 0; i < EightL1Size; i++ {
		acc[i] -= wSlice[i]
	}
}

func refAddThreatVector1024(acc *[EightL1Size]int16, weights *int8) {
	wSlice := unsafe.Slice(weights, EightL1Size)
	for i := 0; i < EightL1Size; i++ {
		acc[i] += int16(wSlice[i])
	}
}

func refSubThreatVector1024(acc *[EightL1Size]int16, weights *int8) {
	wSlice := unsafe.Slice(weights, EightL1Size)
	for i := 0; i < EightL1Size; i++ {
		acc[i] -= int16(wSlice[i])
	}
}

// refActivateEightFT writes EightL1Size/2 bytes to out: for each i,
// clamp(pst[i]+thr[i], 0, 255) * clamp(pst[i+512]+thr[i+512], 0, 255) >> 9.
// The sums wrap in int16 before clamping.
func refActivateEightFT(out *uint8, pst *int16, thr *int16) {
	outSlice := unsafe.Slice(out, EightL1Size/2)
	pstSlice := unsafe.Slice(pst, EightL1Size)
	thrSlice := unsafe.Slice(thr, EightL1Size)

	for i := 0; i < EightL1Size/2; i++ {
		sLeft := int32(pstSlice[i] + thrSlice[i])
		sRight := int32(pstSlice[i+EightL1Size/2] + thrSlice[i+EightL1Size/2])

		if sLeft < 0 {
			sLeft = 0
		} else if sLeft > EightFTQuant {
			sLeft = EightFTQuant
		}

		if sRight < 0 {
			sRight = 0
		} else if sRight > EightFTQuant {
			sRight = EightFTQuant
		}

		prod := (sLeft * sRight) >> EightFTShift
		if prod < 0 {
			prod = 0
		} else if prod > 255 {
			prod = 255
		}

		outSlice[i] = uint8(prod)
	}
}

// refL1Sparse is the definition of the l1Sparse kernels. nz points to n
// entries, each an input index (low 16 bits) and its value (high 16 bits,
// 0..255); n is even. out receives, for each output j, the sum over entries
// of value * wt[index][j].
func refL1Sparse(nz *uint32, n int, wt *[EightL1Size][EightL2Size]int8, out *[EightL2Size]int32) {
	var acc [EightL2Size]int32
	for _, e := range unsafe.Slice(nz, n) {
		v := int32(e >> 16)
		w := &wt[e&0xFFFF]
		for j := range acc {
			acc[j] += v * int32(w[j])
		}
	}
	*out = acc
}

// refL1Nonzero is the definition of the l1Nonzero kernels: for each non-zero
// byte of ft, in order, it writes the entry index | value<<16 to nz and
// returns how many it wrote.
func refL1Nonzero(ft *[EightL1Size]uint8, nz *[EightL1Size + 1]uint32) int {
	n := 0
	for c := 0; c < EightL1Size/8; c++ {
		oct := binary.LittleEndian.Uint64(ft[c*8 : c*8+8])
		for oct != 0 {
			k := bits.TrailingZeros64(oct) >> 3
			nz[n] = uint32(c*8+k) | uint32(byte(oct>>(8*k)))<<16
			n++
			oct &^= 0xFF << (8 * k)
		}
	}
	return n
}

// refFusedPiece sets dst to src plus the piece-weight rows listed in add
// (nAdd of them) minus those listed in rem. dst and src may be the same
// array. This is the definition of the fusedPiece kernels.
func refFusedPiece(dst, src *[EightL1Size]int16, w *int16, add *int32, nAdd int, rem *int32, nRem int) {
	acc := *src
	for _, k := range unsafe.Slice(add, nAdd) {
		row := unsafe.Slice((*int16)(unsafe.Add(unsafe.Pointer(w), uintptr(k)*EightL1Size*2)), EightL1Size)
		for i := range acc {
			acc[i] += row[i]
		}
	}
	for _, k := range unsafe.Slice(rem, nRem) {
		row := unsafe.Slice((*int16)(unsafe.Add(unsafe.Pointer(w), uintptr(k)*EightL1Size*2)), EightL1Size)
		for i := range acc {
			acc[i] -= row[i]
		}
	}
	*dst = acc
}

// refFusedThreat is refFusedPiece for the int8 threat weights.
func refFusedThreat(dst, src *[EightL1Size]int16, w *int8, add *int32, nAdd int, rem *int32, nRem int) {
	acc := *src
	for _, k := range unsafe.Slice(add, nAdd) {
		row := unsafe.Slice((*int8)(unsafe.Add(unsafe.Pointer(w), uintptr(k)*EightL1Size)), EightL1Size)
		for i := range acc {
			acc[i] += int16(row[i])
		}
	}
	for _, k := range unsafe.Slice(rem, nRem) {
		row := unsafe.Slice((*int8)(unsafe.Add(unsafe.Pointer(w), uintptr(k)*EightL1Size)), EightL1Size)
		for i := range acc {
			acc[i] -= int16(row[i])
		}
	}
	*dst = acc
}

// refL2 is the definition of the l2Layer kernels: out[j] = clamp(bias[j] +
// sum over i of w[i][j]*x[i], 0, 1), the sum taken in order of i with every
// product and every addition rounded to float32 on its own. The explicit
// conversions stop the compiler fusing a multiply and an add, which it may do
// on some targets, so this is the same arithmetic as the SIMD versions.
func refL2(w *[EightL2Size][EightL3Size]float32, bias *[EightL3Size]float32, x *[EightL2Size]float32, out *[EightL3Size]float32) {
	for j := 0; j < EightL3Size; j++ {
		sum := bias[j]
		for i := 0; i < EightL2Size; i++ {
			sum = float32(sum + float32(w[i][j]*x[i]))
		}
		if sum < 0 {
			sum = 0
		} else if sum > 1 {
			sum = 1
		}
		out[j] = sum
	}
}

// refSparse32 is the definition of sparse32.
func refSparse32(idx *uint16, n int, in *uint8, wt *int8, out *[32]int32) {
	*out = [32]int32{}
	for _, i := range unsafe.Slice(idx, n) {
		v := int32(*(*uint8)(unsafe.Add(unsafe.Pointer(in), int(i))))
		row := (*[32]int8)(unsafe.Add(unsafe.Pointer(wt), int(i)*32))
		for o := range out {
			out[o] += v * int32(row[o])
		}
	}
}
