package nnue

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
)

// Every legal move from positions chosen for the special cases (castling
// both ways, en passant, promotions with and without capture, discovered
// attacks): the accumulator built from the parent's by taking the threat
// changes from the move evaluates the same as one built from nothing, and
// the same as one built by comparing snapshots.
func TestMoveDeltaEveryMove(t *testing.T) {
	net := ActiveEightNetwork
	if net == nil || !net.IsSF() {
		t.Fatal("the default network is not the Stockfish one")
	}
	moveDeltaVerify = true
	defer func() { moveDeltaVerify, moveDelta = false, true }()
	fens := []string{
		board.StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R b KQkq - 0 1",
		"rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3",
		"rnbqkbnr/pp1ppppp/8/8/2pPP3/8/PPP2PPP/RNBQKBNR b KQkq d3 0 3",
		"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
		"4k3/P6P/8/8/8/8/p6p/4K3 w - - 0 1",
		"4k3/P6P/8/8/8/8/p6p/4K3 b - - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"3r2k1/5ppp/8/8/3B4/8/3R1PPP/6K1 w - - 0 1",
	}
	checked, fast := 0, 0
	for _, fen := range fens {
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range movegen.GenerateLegalMoves(b) {
			var got [2]int32
			for i, on := range []bool{true, false} {
				moveDelta = on
				st := NewEightState(8)
				st.Evaluate(net, b, 0, 0, false)
				parent := b.Hash
				before, rebuilds := st.Evals, st.ThreatRebuilds
				if !b.MakeMove(m) {
					t.Fatalf("%s: illegal %s", fen, m)
				}
				got[i] = st.Evaluate(net, b, 1, parent, true)
				if on && st.Evals == before+1 && st.ThreatRebuilds == rebuilds {
					fast++
				}
				if want := EvaluateEight(net, b); got[i] != want {
					t.Errorf("%s, %s (move delta %v): incremental %d, from scratch %d", fen, m, on, got[i], want)
				}
				b.UnmakeMove(m)
			}
			checked++
		}
	}
	if checked < 200 || fast < checked/2 {
		t.Fatalf("%d moves checked, %d through the move-delta path", checked, fast)
	}
}
