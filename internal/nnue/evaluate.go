package nnue

import (
	"devilfish/internal/types"
)

func addVector(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	for i := 0; i < HiddenSize; i++ {
		acc[i] += weights[i]
	}
}

func subVector(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	for i := 0; i < HiddenSize; i++ {
		acc[i] -= weights[i]
	}
}

func clamp8(val int16) int8 {
	if val <= 0 {
		return 0
	}
	if val >= 127 {
		return 127
	}
	return int8(val)
}

func clamp32(val int32) int8 {
	if val <= 0 {
		return 0
	}
	if val >= 127 {
		return 127
	}
	return int8(val)
}

// Evaluate performs neural forward inference on the Accumulator from perspective of side to move.
func Evaluate(acc *Accumulator, stm types.Color) int32 {
	if ActiveNetwork == nil {
		return 0
	}

	var usAcc, themAcc *[HiddenSize]int16
	if stm == types.White {
		usAcc = &acc.White
		themAcc = &acc.Black
	} else {
		usAcc = &acc.Black
		themAcc = &acc.White
	}

	// 1. CReLU Activation on Feature Transformer accumulators
	var crelu [2 * HiddenSize]int8
	for i := 0; i < HiddenSize; i++ {
		crelu[i] = clamp8(usAcc[i])
		crelu[HiddenSize+i] = clamp8(themAcc[i])
	}

	// 2. Layer 1 (512 -> 32)
	var l1Out [Layer1Size]int8
	for j := 0; j < Layer1Size; j++ {
		sum := ActiveNetwork.L1Biases[j]
		wRow := &ActiveNetwork.L1Weights[j]

		for i := 0; i < 2*HiddenSize; i++ {
			sum += int32(crelu[i]) * int32(wRow[i])
		}

		l1Out[j] = clamp32(sum / 128)
	}

	// 3. Output Layer (32 -> 1)
	out := ActiveNetwork.OutBias
	for j := 0; j < Layer1Size; j++ {
		out += int32(l1Out[j]) * int32(ActiveNetwork.OutWeights[j])
	}

	return out / OutputScale
}
