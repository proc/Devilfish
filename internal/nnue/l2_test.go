package nnue

import (
	"math/rand"
	"testing"
)

// checkL2 compares l2Layer with refL2, exactly, on random weights and inputs
// spread so that outputs land below 0, inside [0, 1] and above 1.
func checkL2(t *testing.T, rng *rand.Rand) {
	var w [EightL2Size][EightL3Size]float32
	var bias [EightL3Size]float32
	var x [EightL2Size]float32
	clipped := [2]int{}
	for trial := 0; trial < 2000; trial++ {
		scale := []float32{0.05, 0.3, 2}[trial%3]
		for i := range w {
			for j := range w[i] {
				w[i][j] = (rng.Float32()*2 - 1) * scale
			}
		}
		for j := range bias {
			bias[j] = rng.Float32()*1.2 - 0.6
		}
		for i := range x {
			if rng.Intn(4) == 0 {
				x[i] = 0
			} else {
				x[i] = rng.Float32()
			}
		}
		var got, want [EightL3Size]float32
		refL2(&w, &bias, &x, &want)
		l2Layer(&w, &bias, &x, &got)
		if got != want {
			t.Fatalf("trial %d: got %v\nwant %v", trial, got, want)
		}
		for _, v := range want {
			if v == 0 {
				clipped[0]++
			} else if v == 1 {
				clipped[1]++
			}
		}
	}
	if clipped[0] == 0 || clipped[1] == 0 {
		t.Fatalf("test data never reached a clamp edge: %v", clipped)
	}
}

func TestL2Layer(t *testing.T) {
	checkL2(t, rand.New(rand.NewSource(3)))
}
