package board_test

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
)

var givesCheckFENs = []string{
	board.StartFEN,
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
	"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
	"r4rk1/1pp1qppp/p1np1n2/2b1p1B1/2B1P1b1/P1NP1N2/1PP1QPPP/R4RK1 w - - 0 10",
	"8/8/8/8/2p1p3/3k4/1P2NP2/R3KB2 w Q - 0 1", // O-O-O gives mate
	"5k2/8/8/8/8/8/8/4K2R w K - 0 1",           // O-O gives check with the rook
	"8/8/8/2k5/3Pp3/8/8/4K2B b - d3 0 1",       // en passant, discovered lines
	"4k3/1b6/8/8/3pP3/8/8/7K b - e3 0 1",       // en passant removing a blocker
	"n1n5/PPPk4/8/8/8/8/4Kppp/5N1N b - - 0 1",  // promotions giving check
	"3k4/3P4/8/8/8/8/3p4/3K4 w - - 0 1",
}

// TestGivesCheckMatchesMakeMove compares the table-based GivesCheck with the
// make/unmake reference on every legal move of every position reachable within
// a few plies of the test positions.
func TestGivesCheckMatchesMakeMove(t *testing.T) {
	checked := 0
	var walk func(b *board.Board, depth int)
	walk = func(b *board.Board, depth int) {
		for _, m := range movegen.GenerateLegalMoves(b) {
			got := b.GivesCheck(m)
			want := b.GivesCheckSlow(m)
			if got != want {
				t.Fatalf("fen %s move %s: GivesCheck=%v, reference=%v", b.ToFEN(), m, got, want)
			}
			checked++
			if depth > 1 && b.MakeMove(m) {
				walk(b, depth-1)
				b.UnmakeMove(m)
			}
		}
	}
	for _, fen := range givesCheckFENs {
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatalf("%s: %v", fen, err)
		}
		walk(b, 3)
	}
	t.Logf("checked %d moves", checked)
}
