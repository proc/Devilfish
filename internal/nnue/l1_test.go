package nnue

import (
	"math/rand"
	"testing"
)

// randomFTOut builds a first-layer input with roughly the given fraction of
// non-zero bytes, using the full 0..255 range.
func randomFTOut(rng *rand.Rand, density float64) (ft [EightL1Size]uint8) {
	for i := range ft {
		if rng.Float64() < density {
			ft[i] = uint8(1 + rng.Intn(255))
		}
	}
	return
}

func randomL1Weights(rng *rand.Rand) (wt [EightL1Size][EightL2Size]int8, flat [EightL2Size * EightL1Size]int8) {
	for i := range wt {
		for j := range wt[i] {
			v := int8(rng.Intn(256) - 128)
			if rng.Intn(10) == 0 {
				v = -128 // most negative weight, to exercise the product bounds
			}
			wt[i][j] = v
			// Same numbers in the file layout read by eightL1Ref.
			flat[(i/4)*EightL2Size*4+j*4+i%4] = v
		}
	}
	return
}

// TestEightL1SparseMatchesDense checks eightL1 (the scalar non-zero scan plus
// this architecture's l1Sparse kernel) against the original dense loop, from
// empty inputs to fully dense ones.
func TestEightL1SparseMatchesDense(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	wt, flat := randomL1Weights(rng)
	for trial := 0; trial < 400; trial++ {
		density := []float64{0, 0.001, 0.05, 0.11, 0.3, 1}[trial%6]
		ft := randomFTOut(rng, density)
		if trial%50 == 1 {
			ft = [EightL1Size]uint8{}
			ft[rng.Intn(EightL1Size)] = 255 // a single input: odd count, padded
		}
		var got, want [EightL2Size]int32
		eightL1(&ft, &wt, &got)
		eightL1Ref(&ft, &flat, &want)
		if got != want {
			t.Fatalf("trial %d (density %.3f): got %v want %v", trial, density, got, want)
		}
	}
}
