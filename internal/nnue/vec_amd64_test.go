//go:build amd64

package nnue

import (
	"math/rand"
	"testing"
	"unsafe"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// availableSIMD lists the kernel sets this machine can run.
func availableSIMD() []simdLevel {
	levels := []simdLevel{simdSSE2}
	if hasAVX2() {
		levels = append(levels, simdAVX2)
	}
	if hasAVX512BW() {
		levels = append(levels, simdAVX512)
	}
	return levels
}

// withSIMD runs f with the given kernel set selected, using VNNI where that
// tier offers it.
func withSIMD(l simdLevel, f func()) {
	withSIMDVNNI(l, vnniFor(l), f)
}

func withSIMDVNNI(l simdLevel, useVNNI bool, f func()) {
	oldSIMD, oldVNNI := simd, vnni
	simd, vnni = l, useVNNI && vnniFor(l)
	defer func() { simd, vnni = oldSIMD, oldVNNI }()
	f()
}

// fillActivation fills s with values clustered around the clamp edges (0 and
// 255) the activation cares about.
func fillActivation(rng *rand.Rand, s []int16) {
	for i := range s {
		switch rng.Intn(6) {
		case 0:
			s[i] = int16(rng.Intn(5) - 2)
		case 1:
			s[i] = int16(253 + rng.Intn(5))
		case 2:
			s[i] = int16(rng.Intn(65536) - 32768)
		default:
			s[i] = int16(rng.Intn(400) - 72)
		}
	}
}

// misalignedInt16 returns a pointer to n int16s inside buf starting off bytes past a
// 64-byte boundary, so the kernels see addresses that are not vector-aligned.
func misalignedInt16(buf []int16, off int) []int16 {
	base := uintptr(unsafe.Pointer(&buf[0]))
	skip := int((64 - base%64) % 64 / 2)
	return buf[skip+off/2:]
}

func TestAMD64KernelsMatchReference(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for _, level := range availableSIMD() {
		t.Run(simdName(level), func(t *testing.T) {
			withSIMD(level, func() {
				for trial := 0; trial < 300; trial++ {
					off := 2 * rng.Intn(32) // 0..62 bytes past a 64-byte boundary

					// 256-wide add/sub (768->256 network).
					{
						bufA := make([]int16, HiddenSize+64)
						bufW := make([]int16, HiddenSize+64)
						a := (*[HiddenSize]int16)(misalignedInt16(bufA, off))
						wt := (*[HiddenSize]int16)(misalignedInt16(bufW, 62-off))
						fillInt16(rng, a[:])
						fillInt16(rng, wt[:])
						want := *a
						refAddVector(&want, wt)
						addVectorNEON(a, wt)
						if *a != want {
							t.Fatalf("addVector mismatch (offset %d)", off)
						}
						refSubVector(&want, wt)
						subVectorNEON(a, wt)
						if *a != want {
							t.Fatalf("subVector mismatch (offset %d)", off)
						}
					}

					// 1024-wide add/sub with int16 and int8 weights.
					{
						bufA := make([]int16, EightL1Size+64)
						bufW := make([]int16, EightL1Size+64)
						a := (*[EightL1Size]int16)(misalignedInt16(bufA, off))
						wt := misalignedInt16(bufW, 62-off)[:EightL1Size]
						fillInt16(rng, a[:])
						fillInt16(rng, wt)
						want := *a

						refAddVector1024(&want, &wt[0])
						addVector1024NEON(a, &wt[0])
						if *a != want {
							t.Fatalf("addVector1024 mismatch (offset %d)", off)
						}
						refSubVector1024(&want, &wt[0])
						subVector1024NEON(a, &wt[0])
						if *a != want {
							t.Fatalf("subVector1024 mismatch (offset %d)", off)
						}

						tbuf := make([]int8, EightL1Size+64)
						tw := tbuf[rng.Intn(64):][:EightL1Size]
						for i := range tw {
							tw[i] = int8(rng.Intn(256) - 128)
						}
						refAddThreatVector1024(&want, &tw[0])
						addThreatVector1024NEON(a, &tw[0])
						if *a != want {
							t.Fatalf("addThreatVector1024 mismatch (offset %d)", off)
						}
						refSubThreatVector1024(&want, &tw[0])
						subThreatVector1024NEON(a, &tw[0])
						if *a != want {
							t.Fatalf("subThreatVector1024 mismatch (offset %d)", off)
						}
					}

					// Activation.
					{
						bufP := make([]int16, EightL1Size+64)
						bufT := make([]int16, EightL1Size+64)
						pst := misalignedInt16(bufP, off)[:EightL1Size]
						thr := misalignedInt16(bufT, 62-off)[:EightL1Size]
						fillActivation(rng, pst)
						fillActivation(rng, thr)
						outBuf := make([]uint8, EightL1Size/2+64)
						got := outBuf[rng.Intn(64):][:EightL1Size/2]
						var want [EightL1Size / 2]uint8
						refActivateEightFT(&want[0], &pst[0], &thr[0])
						activateEightFTNEON(&got[0], &pst[0], &thr[0])
						for i := range want {
							if got[i] != want[i] {
								t.Fatalf("activate mismatch at %d: got %d want %d (pst %d+%d, thr %d+%d)",
									i, got[i], want[i], pst[i], pst[i+512], thr[i], thr[i+512])
							}
						}
					}
				}
			})
		})
	}
}

func TestAMD64L1SparseKernels(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	wt, _ := randomL1Weights(rng)
	for _, level := range availableSIMD() {
		t.Run(simdName(level), func(t *testing.T) {
			withSIMD(level, func() {
				for trial := 0; trial < 500; trial++ {
					n := 2 * rng.Intn(EightL1Size/2+1) // even, 0..1024
					if trial < 3 {
						n = []int{0, 2, EightL1Size}[trial]
					}
					nz := make([]uint32, n+1)
					for i := 0; i < n; i++ {
						v := uint32(rng.Intn(256))
						if rng.Intn(4) == 0 {
							v = 255
						}
						nz[i] = uint32(rng.Intn(EightL1Size)) | v<<16
					}
					var got, want [EightL2Size]int32
					got[0] = 12345 // the kernel must overwrite, not accumulate
					refL1Sparse(&nz[0], n, &wt, &want)
					l1Sparse(&nz[0], n, &wt, &got)
					if got != want {
						t.Fatalf("n=%d: got %v want %v", n, got, want)
					}
				}
			})
		})
	}
}

func TestAMD64L1NonzeroKernels(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for _, level := range availableSIMD() {
		t.Run(simdName(level), func(t *testing.T) {
			withSIMD(level, func() {
				for trial := 0; trial < 300; trial++ {
					ft := randomFTOut(rng, []float64{0, 0.01, 0.11, 0.5, 1}[trial%5])
					if trial%5 == 4 {
						for i := range ft {
							ft[i] = 255 // every byte non-zero, all at maximum
						}
					}
					var got, want [EightL1Size + 1]uint32
					nWant := refL1Nonzero(&ft, &want)
					nGot := l1Nonzero(&ft, &got)
					if nGot != nWant || got != want {
						t.Fatalf("trial %d: got %d entries, want %d", trial, nGot, nWant)
					}
				}
			})
		})
	}
}

func TestAMD64FusedKernels(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	for _, level := range availableSIMD() {
		t.Run(simdName(level), func(t *testing.T) {
			withSIMD(level, func() { checkFusedKernels(t, rng) })
		})
	}
}

func TestAMD64L2Layer(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	for _, level := range availableSIMD() {
		t.Run(simdName(level), func(t *testing.T) {
			withSIMD(level, func() { checkL2(t, rng) })
		})
	}
}

// The VNNI first layer must match the reference exactly: all densities,
// full-range inputs, the most negative weights.
func TestAMD64VNNIFirstLayer(t *testing.T) {
	rng := rand.New(rand.NewSource(23))
	_, flat := randomL1Weights(rng)
	tested := 0
	for _, level := range availableSIMD() {
		if !vnniFor(level) {
			continue
		}
		tested++
		withSIMD(level, func() {
			var byInput [EightL1Size][EightL2Size]int8 // unused by the VNNI path
			for trial := 0; trial < 600; trial++ {
				ft := randomFTOut(rng, []float64{0, 0.002, 0.05, 0.11, 0.4, 1}[trial%6])
				if trial%6 == 5 {
					for i := range ft {
						ft[i] = 255
					}
				}
				var got, want [EightL2Size]int32
				got[3] = 7 // eightL1Layer accumulates into its output
				want[3] = 7
				eightL1Ref(&ft, &flat, &want)
				eightL1Layer(&ft, &flat, &byInput, &got)
				if got != want {
					t.Fatalf("%s trial %d: got %v want %v", simdName(level), trial, got, want)
				}
			}
		})
	}
	if tested == 0 {
		t.Skip("no VNNI on this CPU")
	}
}

// The network evaluations must not depend on which kernels are in use.
func TestAMD64EvaluationsMatchAcrossKernels(t *testing.T) {
	if ActiveEightNetwork == nil || ActiveNetwork == nil {
		t.Skip("networks not embedded")
	}
	var positions []*board.Board
	for _, fen := range eightTestFENs {
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		positions = append(positions, b)
		for _, m := range movegen.GenerateLegalMoves(b) {
			c := b.Clone()
			if c.MakeMove(m) {
				positions = append(positions, c)
			}
		}
	}

	evalAll := func() (eight, small []int32) {
		for _, b := range positions {
			eight = append(eight, EvaluateEight(ActiveEightNetwork, b))
			var acc Accumulator
			// Refresh is a no-op while the 8Chess net is active; build the
			// small-net accumulator directly.
			acc.White = ActiveNetwork.Biases
			acc.Black = ActiveNetwork.Biases
			for sq := 0; sq < 64; sq++ {
				p := b.Pieces[sq]
				if p == types.PieceNone {
					continue
				}
				addVector(&acc.White, &ActiveNetwork.Weights[FeatureIndex(types.White, p, types.Square(sq))])
				addVector(&acc.Black, &ActiveNetwork.Weights[FeatureIndex(types.Black, p, types.Square(sq))])
			}
			small = append(small, Evaluate(&acc, b.SideToMove))
		}
		return
	}

	var wantEight, wantSmall []int32
	withSIMD(simdGeneric, func() { wantEight, wantSmall = evalAll() })
	for _, level := range availableSIMD() {
		for _, useVNNI := range []bool{false, true} {
			if useVNNI && !vnniFor(level) {
				continue
			}
			withSIMDVNNI(level, useVNNI, func() {
				gotEight, gotSmall := evalAll()
				for i := range positions {
					if gotEight[i] != wantEight[i] || gotSmall[i] != wantSmall[i] {
						t.Fatalf("%s (VNNI %v), %s: 8chess %d (generic %d), small %d (generic %d)",
							simdName(level), useVNNI, positions[i].ToFEN(), gotEight[i], wantEight[i], gotSmall[i], wantSmall[i])
					}
				}
			})
		}
	}
	t.Logf("%d positions agree across generic, %v", len(positions), availableSIMD())
}

func benchKernels(b *testing.B, level simdLevel) {
	if level == simdAVX512 && !hasAVX512BW() {
		b.Skip("no AVX-512BW")
	}
	if level == simdAVX2 && !hasAVX2() {
		b.Skip("no AVX2")
	}
	withSIMD(level, func() {
		net := ActiveEightNetwork
		if net == nil {
			b.Skip("8Chess network not embedded")
		}
		pos, _ := board.ParseFEN(eightTestFENs[5])
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			EvaluateEight(net, pos)
		}
	})
}

func BenchmarkEvaluateEightGeneric(b *testing.B) { benchKernels(b, simdGeneric) }
func BenchmarkEvaluateEightSSE2(b *testing.B)    { benchKernels(b, simdSSE2) }
func BenchmarkEvaluateEightAVX2(b *testing.B)    { benchKernels(b, simdAVX2) }
func BenchmarkEvaluateEightAVX512(b *testing.B)  { benchKernels(b, simdAVX512) }

// TestSIMDCap checks that DEVILFISH_SIMD only ever lowers the tier.
func TestSIMDCap(t *testing.T) {
	best := bestSIMD()
	for env, want := range map[string]simdLevel{
		"generic": simdGeneric,
		"sse2":    simdSSE2,
		"avx2":    min(simdAVX2, best),
		"":        best,
		"bogus":   best,
	} {
		t.Setenv("DEVILFISH_SIMD", env)
		if got := detectSIMD(); got != want {
			t.Errorf("DEVILFISH_SIMD=%q: got %s, want %s", env, simdName(got), simdName(want))
		}
	}
}
