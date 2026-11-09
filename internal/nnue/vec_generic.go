//go:build !arm64

package nnue

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
