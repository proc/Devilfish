package nnue

import (
	"math/rand"
	"testing"
)

// l1NonzeroIdx lists exactly the non-zero inputs, in order, at every density
// and for single inputs at each position.
func TestL1NonzeroIdx(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	check := func(ft *[EightL1Size]uint8) {
		t.Helper()
		var out [EightL1Size + 8]uint16
		n := l1NonzeroIdx(ft, &out)
		k := 0
		for i, v := range ft {
			if v == 0 {
				continue
			}
			if k >= n || int(out[k]) != i {
				t.Fatalf("entry %d: want index %d, have %d entries, got %v", k, i, n, out[:min(n, k+1)])
			}
			k++
		}
		if k != n {
			t.Fatalf("got %d entries, want %d", n, k)
		}
	}
	for trial := 0; trial < 600; trial++ {
		ft := randomFTOut(rng, []float64{0, 0.001, 0.05, 0.11, 0.3, 1}[trial%6])
		check(&ft)
	}
	for i := 0; i < EightL1Size; i++ {
		var ft [EightL1Size]uint8
		ft[i] = uint8(1 + i%255)
		check(&ft)
	}
	// Values with only bit 7, or only low bits, set.
	var ft [EightL1Size]uint8
	for i := range ft {
		ft[i] = []uint8{0x80, 0x01, 0, 0x7f, 0xff, 0, 0x40, 0}[i%8]
	}
	check(&ft)
}

// The two scans on inputs that differ from call to call, as in a search. (A
// benchmark that cycles through a few inputs lets the branch predictor learn
// them, which flatters the bit-walking scan in l1Nonzero.)
func variedFTOuts(n int) [][EightL1Size]uint8 {
	rng := rand.New(rand.NewSource(3))
	ins := make([][EightL1Size]uint8, n)
	for i := range ins {
		ins[i] = randomFTOut(rng, 0.11)
	}
	return ins
}

func BenchmarkL1NonzeroVaried(b *testing.B) {
	ins := variedFTOuts(4096)
	var nz [EightL1Size + 1]uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l1Nonzero(&ins[i%len(ins)], &nz)
	}
}

func BenchmarkL1NonzeroIdxVaried(b *testing.B) {
	ins := variedFTOuts(4096)
	var nz [EightL1Size + 8]uint16
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l1NonzeroIdx(&ins[i%len(ins)], &nz)
	}
}
