//go:build amd64

package nnue

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"devilfish/internal/board"
)

// sfTiers runs f once for each way the Stockfish network's kernels can be
// chosen on this machine: every SIMD tier, and the SSE tier without SSSE3.
func sfTiers(t *testing.T, f func(t *testing.T)) {
	savedSSSE3 := hasSSSE3
	defer func() { hasSSSE3 = savedSSSE3 }()
	for _, l := range availableSIMD() {
		for _, ssse3 := range []bool{true, false} {
			if ssse3 && !savedSSSE3 {
				continue
			}
			if !ssse3 && l != simdSSE2 {
				continue // SSSE3 only matters to the SSE tier
			}
			hasSSSE3 = ssse3
			name := simdName(l)
			if l == simdSSE2 && ssse3 {
				name += "+SSSE3"
			}
			withSIMD(l, func() { t.Run(name, f) })
		}
	}
}

// Every tier's kernels for the dense layers agree with the definitions.
func TestAMD64SFKernelsMatchReference(t *testing.T) {
	sfTiers(t, func(t *testing.T) {
		rng := rand.New(rand.NewSource(31))
		st := &sfStack{}
		for i := 0; i < EightL1Size; i++ {
			for o := 0; o < sfL2; o++ {
				v := int8(rng.Intn(256) - 128)
				if rng.Intn(8) == 0 {
					v = -128
				}
				half, j := o/EightL2Size, o%EightL2Size
				st.fc0[i][o] = v
				st.fc0Chunk[i/4][o][i%4] = v
				st.fc0ByInput[half][i][j] = v
				st.fc0File[half][(i/4)*EightL2Size*4+j*4+i%4] = v
			}
		}
		for i := 0; i < 2*sfL2; i++ {
			for o := 0; o < sfL3; o++ {
				v := int8(rng.Intn(256) - 128)
				st.fc1[i][o] = v
				st.fc1Chunk[i/4][o][i%4] = v
			}
		}
		for trial := 0; trial < 300; trial++ {
			// First layer, at every density; inputs up to the maximum 127.
			var ft [EightL1Size]uint8
			density := []float64{0, 0.002, 0.05, 0.11, 0.4, 1}[trial%6]
			for i := range ft {
				if rng.Float64() < density {
					ft[i] = uint8(1 + rng.Intn(127))
					if rng.Intn(4) == 0 {
						ft[i] = 127
					}
				}
			}
			var want [32]int32
			for i, v := range ft {
				for o := range want {
					want[o] += int32(v) * int32(st.fc0[i][o])
				}
			}
			var idx [EightL1Size + 8]uint16
			got := [32]int32{5, 6, 7}
			sfFirstLayer(&ft, st, &got, &idx)
			if got != want {
				t.Fatalf("trial %d (density %.3f): first layer %v, want %v", trial, density, got, want)
			}

			// Second layer.
			var act [128]uint8
			for i := 0; i < 64; i++ {
				if rng.Intn(3) != 0 {
					act[i] = uint8(rng.Intn(128))
				}
			}
			var got2, want2 [32]int32
			got2[0] = 9
			sfSecondLayer(&act, st, &got2)
			refSFSecondLayer(&act, st, &want2)
			if got2 != want2 {
				t.Fatalf("trial %d: second layer %v, want %v", trial, got2, want2)
			}
		}
	})
}

// The group scans list exactly the groups of four with a non-zero byte.
func TestAMD64NonzeroChunks(t *testing.T) {
	rng := rand.New(rand.NewSource(33))
	scans := map[string]func(*[EightL1Size]uint8, *uint16) int{"SSE2": nonzeroChunksSSE2}
	for _, l := range availableSIMD() {
		if l == simdAVX2 {
			scans["AVX2"] = nonzeroChunksAVX2
		}
	}
	for name, scan := range scans {
		for trial := 0; trial < 600; trial++ {
			ft := randomFTOut(rng, []float64{0, 0.001, 0.03, 0.11, 0.5, 1}[trial%6])
			if trial%7 == 3 {
				ft = [EightL1Size]uint8{}
				ft[rng.Intn(EightL1Size)] = 0x80
			}
			var got, want [EightL1Size + 8]uint16
			n := scan(&ft, &got[0])
			wantN := l1NonzeroChunks(&ft, &want)
			if n != wantN {
				t.Fatalf("%s trial %d: %d groups, want %d", name, trial, n, wantN)
			}
			for i := 0; i < n; i++ {
				if got[i] != want[i] {
					t.Fatalf("%s trial %d: entry %d is %d, want %d", name, trial, i, got[i], want[i])
				}
			}
		}
	}
}

// The index-list forms of the 8Chess first layer agree with the dense
// definition, for even and odd numbers of non-zero inputs.
func TestAMD64L1SparseIdxKernels(t *testing.T) {
	rng := rand.New(rand.NewSource(35))
	wt, flat := randomL1Weights(rng)
	for _, l := range availableSIMD() {
		if l == simdGeneric {
			continue
		}
		withSIMD(l, func() {
			for trial := 0; trial < 400; trial++ {
				ft := randomFTOut(rng, []float64{0, 0.001, 0.05, 0.11, 0.3, 1}[trial%6])
				if trial%9 == 1 {
					ft = [EightL1Size]uint8{}
					ft[rng.Intn(EightL1Size)] = 255 // one input: the unpaired tail alone
				}
				var got, want [EightL2Size]int32
				eightL1Idx(&ft, &wt, &got)
				eightL1Ref(&ft, &flat, &want)
				if got != want {
					t.Fatalf("%s trial %d: got %v want %v", simdName(l), trial, got, want)
				}
			}
		})
	}
}

// With every tier, the Stockfish network gives Stockfish's own numbers, and
// the small kernels agree with their definitions.
func TestAMD64SFMatchesStockfishAcrossKernels(t *testing.T) {
	net := loadSFTestNet(t)
	type ref struct {
		want int
		fen  string
	}
	var refs []ref
	f, err := os.Open("testdata/sf_evals.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for sc := bufio.NewScanner(f); sc.Scan(); {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want, fen, _ := strings.Cut(line, "\t")
		v, err := strconv.Atoi(want)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref{v, fen})
	}
	sfTiers(t, func(t *testing.T) {
		for _, r := range refs {
			b, err := board.ParseFEN(r.fen)
			if err != nil {
				t.Fatal(err)
			}
			out, ok := SFEvaluateRaw(net, b)
			if !ok || int(out.PSQT+out.Positional) != r.want {
				t.Fatalf("%s: got %d, Stockfish %d", r.fen, out.PSQT+out.Positional, r.want)
			}
		}
		TestSFDenseKernelsMatchReference(t)
		TestSFIncrementalMatchesScratch(t)
	})
	t.Log(fmt.Sprintf("best tier here: %s, SSSE3 %v", simdName(bestSIMD()), hasSSSE3))
}

func benchSFTier(b *testing.B, l simdLevel, ssse3 bool) {
	ok := false
	for _, a := range append(availableSIMD(), simdGeneric) {
		ok = ok || a == l
	}
	if !ok {
		b.Skip("tier not available")
	}
	saved := hasSSSE3
	hasSSSE3 = ssse3 && saved
	defer func() { hasSSSE3 = saved }()
	withSIMD(l, func() { BenchmarkSFIncremental(b) })
}

func BenchmarkSFIncrementalGeneric(b *testing.B)  { benchSFTier(b, simdGeneric, false) }
func BenchmarkSFIncrementalSSE2Only(b *testing.B) { benchSFTier(b, simdSSE2, false) }
func BenchmarkSFIncrementalSSSE3(b *testing.B)    { benchSFTier(b, simdSSE2, true) }
func BenchmarkSFIncrementalAVX2(b *testing.B)     { benchSFTier(b, simdAVX2, true) }

// The 8Chess first layer on inputs that differ from call to call, by the
// older bit-walking scan (eightL1) and the branch-free one (eightL1Idx).
func benchEightL1(b *testing.B, l simdLevel, idx bool) {
	ok := false
	for _, a := range availableSIMD() {
		ok = ok || a == l
	}
	if !ok {
		b.Skip("tier not available")
	}
	ins := variedFTOuts(4096)
	rng := rand.New(rand.NewSource(5))
	wt, _ := randomL1Weights(rng)
	withSIMD(l, func() {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			var out [EightL2Size]int32
			if idx {
				eightL1Idx(&ins[i%len(ins)], &wt, &out)
			} else {
				eightL1(&ins[i%len(ins)], &wt, &out)
			}
		}
	})
}

func BenchmarkEightL1WalkSSE2(b *testing.B) { benchEightL1(b, simdSSE2, false) }
func BenchmarkEightL1IdxSSE2(b *testing.B)  { benchEightL1(b, simdSSE2, true) }
func BenchmarkEightL1WalkAVX2(b *testing.B) { benchEightL1(b, simdAVX2, false) }
func BenchmarkEightL1IdxAVX2(b *testing.B)  { benchEightL1(b, simdAVX2, true) }
