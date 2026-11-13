//go:build arm64

package nnue

import (
	"math/rand"
	"testing"
)

// Both forms of the first layer (dot product over groups of four inputs,
// and one weight row per non-zero input) agree with the plain definition.
func TestSFFirstLayerForms(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	st := &sfStack{}
	for i := 0; i < EightL1Size; i++ {
		for o := 0; o < sfL2; o++ {
			v := int8(rng.Intn(256) - 128)
			st.fc0[i][o] = v
			st.fc0Chunk[i/4][o][i%4] = v
		}
	}
	saved := hasDotProd
	defer func() { hasDotProd = saved }()
	for trial := 0; trial < 300; trial++ {
		var ft [EightL1Size]uint8
		density := []float64{0, 0.002, 0.05, 0.11, 0.4, 1}[trial%6]
		for i := range ft {
			if rng.Float64() < density {
				ft[i] = uint8(1 + rng.Intn(127)) // the transformer's outputs are at most 127
			}
		}
		var want [32]int32
		for i, v := range ft {
			for o := range want {
				want[o] += int32(v) * int32(st.fc0[i][o])
			}
		}
		for _, dot := range []bool{false, true} {
			if dot && !saved {
				continue // no dot-product instructions on this CPU
			}
			hasDotProd = dot
			var idx [EightL1Size + 8]uint16
			got := [32]int32{1, 2, 3}
			sfFirstLayer(&ft, st, &got, &idx)
			if got != want {
				t.Fatalf("trial %d (density %.3f, dot product %v): got %v want %v", trial, density, dot, got, want)
			}
		}
	}
}

func TestL1NonzeroChunks(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	for trial := 0; trial < 600; trial++ {
		ft := randomFTOut(rng, []float64{0, 0.001, 0.03, 0.11, 0.5, 1}[trial%6])
		if trial%7 == 3 {
			ft = [EightL1Size]uint8{}
			ft[rng.Intn(EightL1Size)] = 0x80
		}
		var out [EightL1Size + 8]uint16
		n := l1NonzeroChunks(&ft, &out)
		k := 0
		for c := 0; c < EightL1Size/4; c++ {
			if ft[c*4]|ft[c*4+1]|ft[c*4+2]|ft[c*4+3] == 0 {
				continue
			}
			if k >= n || int(out[k]) != c {
				t.Fatalf("trial %d: entry %d should be group %d (have %d entries)", trial, k, c, n)
			}
			k++
		}
		if k != n {
			t.Fatalf("trial %d: %d entries, want %d", trial, n, k)
		}
	}
}
