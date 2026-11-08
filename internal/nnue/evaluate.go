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

	// Midgame average across channels 0..63
	var mgSum int32
	for i := 0; i < 64; i++ {
		mgSum += int32(usAcc[i]) - int32(themAcc[i])
	}
	mgScore := mgSum / (64 * 2)

	// Endgame average across channels 64..127
	var egSum int32
	for i := 64; i < 128; i++ {
		egSum += int32(usAcc[i]) - int32(themAcc[i])
	}
	egScore := egSum / (64 * 2)

	// Mobility & Center control average across channels 128..191
	var mobSum int32
	for i := 128; i < 192; i++ {
		mobSum += int32(usAcc[i]) - int32(themAcc[i])
	}
	mobScore := mobSum / (64 * 2)

	// Combine components: 50% MG, 30% EG, 20% Positional
	finalScore := (mgScore*50 + egScore*30 + mobScore*20) / 100

	// Tempo bonus (15 cp)
	finalScore += 15

	return finalScore
}
