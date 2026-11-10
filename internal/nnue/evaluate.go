package nnue

import (
	"devilfish/internal/types"
)

func addVector(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	addVectorNEON(acc, weights)
}

func subVector(acc *[HiddenSize]int16, weights *[HiddenSize]int16) {
	subVectorNEON(acc, weights)
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

	// 1. Quantized Clipped ReLU activation on accumulator (512 inputs: 256 us, 256 them)
	var input [2 * HiddenSize]int8
	for i := 0; i < HiddenSize; i++ {
		input[i] = clamp8(usAcc[i])
		input[HiddenSize+i] = clamp8(themAcc[i])
	}

	// 2. Hidden Layer 1 (512 -> 32)
	var l1Out [Layer1Size]int8
	for i := 0; i < Layer1Size; i++ {
		sum := ActiveNetwork.L1Biases[i]
		weights := &ActiveNetwork.L1Weights[i]
		for j := 0; j < 2*HiddenSize; j++ {
			sum += int32(input[j]) * int32(weights[j])
		}
		// Clipped ReLU on Layer 1 output (scaled down by 128)
		l1Out[i] = clamp32(sum / 128)
	}

	// 3. Output Layer (32 -> 1)
	out := ActiveNetwork.OutBias
	for i := 0; i < Layer1Size; i++ {
		out += int32(l1Out[i]) * int32(ActiveNetwork.OutWeights[i])
	}

	return out / OutputScale
}
