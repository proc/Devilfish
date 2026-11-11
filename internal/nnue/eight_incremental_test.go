package nnue

import (
	"math/rand"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
)

var eightTestFENs = []string{
	board.StartFEN,
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
	"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
	"r4rk1/1pp1qppp/p1np1n2/2b1p1B1/2B1P1b1/P1NP1N2/1PP1QPPP/R4RK1 w - - 0 10",
	"4k3/P6P/8/8/8/8/p6p/4K3 w - - 0 1",
}

// TestEightIncrementalMatchesScratch walks random move sequences the way a
// search does (down a line, back up, across to siblings, with gaps where no
// evaluation happens) and checks every incremental evaluation against the
// from-scratch one.
func TestEightIncrementalMatchesScratch(t *testing.T) {
	net := ActiveEightNetwork
	if net == nil {
		t.Skip("8Chess network not embedded")
	}
	rng := rand.New(rand.NewSource(1))
	st := NewEightState(128)
	checked := 0

	var walk func(b *board.Board, ply, depth int)
	walk = func(b *board.Board, ply, depth int) {
		// Skip some evaluations so that parents are sometimes missing.
		if rng.Intn(5) != 0 {
			var parentHash uint64
			hasParent := false
			if ply > 0 && len(b.UndoStack) > 0 {
				parentHash = b.UndoStack[len(b.UndoStack)-1].Hash
				hasParent = true
			}
			got := st.Evaluate(net, b, ply, parentHash, hasParent)
			want := EvaluateEight(net, b)
			if got != want {
				t.Fatalf("ply %d fen %s: incremental %d, scratch %d", ply, b.ToFEN(), got, want)
			}
			checked++
		}
		if depth == 0 {
			return
		}
		moves := movegen.GenerateLegalMoves(b)
		if len(moves) == 0 {
			return
		}
		tries := 1 + rng.Intn(3)
		for i := 0; i < tries; i++ {
			m := moves[rng.Intn(len(moves))]
			if b.MakeMove(m) {
				walk(b, ply+1, depth-1)
				b.UnmakeMove(m)
			}
		}
		if rng.Intn(4) == 0 {
			b.MakeNullMove()
			walk(b, ply+1, depth-1)
			b.UnmakeNullMove()
		}
	}

	for round := 0; round < 4; round++ {
		for _, fen := range eightTestFENs {
			b, err := board.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			walk(b, 0, 9)
		}
	}
	if checked < 2000 {
		t.Fatalf("only %d positions checked", checked)
	}
	if st.RowOps >= st.ScratchOps {
		t.Fatalf("incremental did no better than scratch: %d ops vs %d", st.RowOps, st.ScratchOps)
	}
	t.Logf("checked %d positions; %d row ops vs %d from scratch (%.1fx fewer)",
		checked, st.RowOps, st.ScratchOps, float64(st.ScratchOps)/float64(st.RowOps))
}

func BenchmarkEightScratch(b *testing.B) {
	net := ActiveEightNetwork
	if net == nil {
		b.Skip("8Chess network not embedded")
	}
	pos, _ := board.ParseFEN(eightTestFENs[5])
	for i := 0; i < b.N; i++ {
		EvaluateEight(net, pos)
	}
}

// BenchmarkEightIncremental alternates between a position and its children,
// which is the pattern a search produces.
func BenchmarkEightIncremental(b *testing.B) {
	net := ActiveEightNetwork
	if net == nil {
		b.Skip("8Chess network not embedded")
	}
	pos, _ := board.ParseFEN(eightTestFENs[5])
	moves := movegen.GenerateLegalMoves(pos)
	st := NewEightState(8)
	st.Evaluate(net, pos, 0, 0, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := moves[i%len(moves)]
		parent := pos.Hash
		pos.MakeMove(m)
		st.Evaluate(net, pos, 1, parent, true)
		pos.UnmakeMove(m)
	}
}

// eightTestInputs returns first-layer inputs for a spread of real positions.
func eightTestInputs(t testing.TB) (ins [][EightL1Size]uint8, buckets []int) {
	net := ActiveEightNetwork
	st := NewEightState(4)
	for _, fen := range eightTestFENs {
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range append(movegen.GenerateLegalMoves(b)[:4:4], 0) {
			if m != 0 && !b.MakeMove(m) {
				continue
			}
			if st.Prepare(net, b, 0, 0, false) {
				var ft [EightL1Size]uint8
				stm := int(b.SideToMove)
				for flip := 0; flip < 2; flip++ {
					pov := stm ^ flip
					activateEightFTNEON(&ft[flip*(EightL1Size/2)], &st.plies[0].pst[pov][0], &st.plies[0].thr[pov][0])
				}
				ins = append(ins, ft)
				n := 0
				for sq := 0; sq < 64; sq++ {
					if b.Pieces[sq] != 0 {
						n++
					}
				}
				buckets = append(buckets, EightOutputBucketsLayout[n])
			}
			if m != 0 {
				b.UnmakeMove(m)
			}
		}
	}
	return
}

func TestEightL1MatchesReference(t *testing.T) {
	net := ActiveEightNetwork
	if net == nil {
		t.Skip("8Chess network not embedded")
	}
	ins, buckets := eightTestInputs(t)
	nz, nzq := 0, 0
	for i := range ins {
		var got, want [EightL2Size]int32
		eightL1(&ins[i], &net.L1WeightsByInput[buckets[i]], &got)
		eightL1Ref(&ins[i], &net.L1Weights[buckets[i]], &want)
		if got != want {
			t.Fatalf("input %d: got %v want %v", i, got, want)
		}
		for c := 0; c < EightL1Size; c += 4 {
			q := 0
			for k := 0; k < 4; k++ {
				if ins[i][c+k] != 0 {
					nz++
					q = 1
				}
			}
			nzq += q
		}
	}
	t.Logf("%d inputs; %.1f%% of bytes and %.1f%% of 4-byte groups non-zero",
		len(ins), 100*float64(nz)/float64(len(ins)*EightL1Size), 100*float64(nzq)/float64(len(ins)*EightL1Size/4))
}

func BenchmarkEightL1Ref(b *testing.B) {
	net := ActiveEightNetwork
	if net == nil {
		b.Skip("8Chess network not embedded")
	}
	ins, buckets := eightTestInputs(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out [EightL2Size]int32
		k := i % len(ins)
		eightL1Ref(&ins[k], &net.L1Weights[buckets[k]], &out)
	}
}

func BenchmarkEightL1(b *testing.B) {
	net := ActiveEightNetwork
	if net == nil {
		b.Skip("8Chess network not embedded")
	}
	ins, buckets := eightTestInputs(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out [EightL2Size]int32
		k := i % len(ins)
		eightL1(&ins[k], &net.L1WeightsByInput[buckets[k]], &out)
	}
}
