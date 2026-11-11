package nnue

import (
	"math/rand"
	"testing"
)

// checkFusedKernels compares fusedPiece/fusedThreat with their reference
// definitions, both into a separate destination and in place.
func checkFusedKernels(t *testing.T, rng *rand.Rand) {
	const pieceRows, threatRows = 40, 300
	pw := make([]int16, pieceRows*EightL1Size)
	tw := make([]int8, threatRows*EightL1Size)
	fillInt16(rng, pw)
	for i := range tw {
		tw[i] = int8(rng.Intn(256) - 128)
	}
	var add, rem [threatRows]int32
	for trial := 0; trial < 200; trial++ {
		var src [EightL1Size]int16
		fillInt16(rng, src[:])
		nAdd, nRem := rng.Intn(5), rng.Intn(5)
		switch trial % 4 {
		case 1:
			nAdd, nRem = 0, 0
		case 2:
			nAdd, nRem = 32, 0 // a full piece rebuild
		case 3:
			nAdd, nRem = 2+rng.Intn(60), rng.Intn(60)
		}

		for i := 0; i < max(nAdd, nRem); i++ {
			add[i] = int32(rng.Intn(pieceRows))
			rem[i] = int32(rng.Intn(pieceRows))
		}
		var got, want [EightL1Size]int16
		refFusedPiece(&want, &src, &pw[0], &add[0], nAdd, &rem[0], nRem)
		fusedPiece(&got, &src, &pw[0], &add[0], nAdd, &rem[0], nRem)
		if got != want {
			t.Fatalf("fusedPiece: %d adds, %d removes differ from reference", nAdd, nRem)
		}
		inPlace := src
		fusedPiece(&inPlace, &inPlace, &pw[0], &add[0], nAdd, &rem[0], nRem)
		if inPlace != want {
			t.Fatalf("fusedPiece in place: %d adds, %d removes differ from reference", nAdd, nRem)
		}

		if trial%4 == 3 {
			nAdd, nRem = rng.Intn(threatRows), rng.Intn(threatRows)
		}
		for i := 0; i < max(nAdd, nRem); i++ {
			add[i] = int32(rng.Intn(threatRows))
			rem[i] = int32(rng.Intn(threatRows))
		}
		refFusedThreat(&want, &src, &tw[0], &add[0], nAdd, &rem[0], nRem)
		fusedThreat(&got, &src, &tw[0], &add[0], nAdd, &rem[0], nRem)
		if got != want {
			t.Fatalf("fusedThreat: %d adds, %d removes differ from reference", nAdd, nRem)
		}
		inPlace = src
		fusedThreat(&inPlace, &inPlace, &tw[0], &add[0], nAdd, &rem[0], nRem)
		if inPlace != want {
			t.Fatalf("fusedThreat in place: %d adds, %d removes differ from reference", nAdd, nRem)
		}
	}
}

// fillInt16 fills s with values that include the int16 extremes, so wrapping
// is exercised as well as ordinary sums.
func fillInt16(rng *rand.Rand, s []int16) {
	for i := range s {
		switch rng.Intn(8) {
		case 0:
			s[i] = 32767 - int16(rng.Intn(4))
		case 1:
			s[i] = -32768 + int16(rng.Intn(4))
		default:
			s[i] = int16(rng.Intn(65536) - 32768)
		}
	}
}

func TestFusedKernels(t *testing.T) {
	checkFusedKernels(t, rand.New(rand.NewSource(5)))
}
