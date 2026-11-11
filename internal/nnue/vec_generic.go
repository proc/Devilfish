//go:build !arm64

package nnue

import "unsafe"

const SIMDInstructionSet = "Scalar Generic"

func addVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	for i := 0; i < HiddenSize; i++ {
		acc[i] += weights[i]
	}
}

func subVectorNEON(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	for i := 0; i < HiddenSize; i++ {
		acc[i] -= weights[i]
	}
}

func addVector1024NEON(acc *[EightL1Size]int16, weights *int16) {
	wSlice := unsafe.Slice(weights, EightL1Size)
	for i := 0; i < EightL1Size; i++ {
		acc[i] += wSlice[i]
	}
}

func addThreatVector1024NEON(acc *[EightL1Size]int16, weights *int8) {
	wSlice := unsafe.Slice(weights, EightL1Size)
	for i := 0; i < EightL1Size; i++ {
		acc[i] += int16(wSlice[i])
	}
}

func activateEightFTNEON(out *uint8, pst *int16, thr *int16) {
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

		if sRight > EightFTQuant {
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
