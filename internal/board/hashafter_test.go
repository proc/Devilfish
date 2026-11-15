package board_test

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
)

// HashAfter must agree with the hash MakeMove produces, for every legal move
// except castling.
func TestHashAfter(t *testing.T) {
	fens := []string{
		board.StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
		"4k3/P6P/8/8/8/8/p6p/4K3 w - - 0 1",
	}
	checked := 0
	for _, fen := range fens {
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range movegen.GenerateLegalMoves(b) {
			want := b.HashAfter(m)
			if !b.MakeMove(m) {
				continue
			}
			got := b.Hash
			b.UnmakeMove(m)
			if m.IsCastle() {
				continue
			}
			if got != want {
				t.Errorf("%s, move %s: HashAfter %x, MakeMove %x", fen, m, want, got)
			}
			checked++
		}
	}
	if checked < 150 {
		t.Fatalf("only %d moves checked", checked)
	}
}

// The partial hashes must match a from-scratch computation after any
// sequence of moves made and taken back.
func TestPartialHashesStayInStep(t *testing.T) {
	fens := []string{
		board.StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3",
		"4k3/P6P/8/8/8/8/p6p/4K3 w - - 0 1",
	}
	check := func(b *board.Board, where string) {
		np, minor := b.CalculatePartialHashes()
		if np != b.NonPawnHash || minor != b.MinorHash || b.PawnHash != b.CalculatePawnHash() {
			t.Fatalf("%s (%s): partial hashes out of step", where, b.ToFEN())
		}
	}
	var walk func(b *board.Board, depth int)
	walk = func(b *board.Board, depth int) {
		check(b, "after a move")
		if depth == 0 {
			return
		}
		for i, m := range movegen.GenerateLegalMoves(b) {
			if depth < 3 && i%4 != 0 {
				continue
			}
			if b.MakeMove(m) {
				walk(b, depth-1)
				b.UnmakeMove(m)
				check(b, "after taking back "+m.String())
			}
		}
		b.MakeNullMove()
		check(b, "after a null move")
		b.UnmakeNullMove()
	}
	for _, fen := range fens {
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		walk(b, 3)
		c := b.Clone()
		check(c, "clone")
	}
}
