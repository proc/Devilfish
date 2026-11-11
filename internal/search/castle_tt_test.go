package search

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/tt"
	"devilfish/internal/types"
)

// A castling move stored as the transposition-table move used to be dropped
// from the search altogether: it failed IsPseudoLegal, so it was not tried as
// the TT move, and the main loop then skipped it for being the TT move.
// Here O-O-O is the only mate in one, and the table already names it.
func TestCastlingTTMoveIsSearched(t *testing.T) {
	b, err := board.ParseFEN("8/8/8/8/2p1p3/3k4/1P2NP2/R3KB2 w Q - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	castle := types.MakeMove(types.E1, types.C1, types.FlagQueenCastle)
	if !movegen.IsPseudoLegal(b, castle) {
		t.Fatal("IsPseudoLegal rejects a legal O-O-O")
	}

	e := NewEngine(16, 1)
	e.TT.Store(b.Hash, castle, tt.ScoreToTT(100, 0), 1, tt.LowerBound)

	var first SearchInfo
	seen := false
	best, _ := e.Search(b, SearchLimits{MaxDepth: 1}, func(i SearchInfo) {
		if i.StringMsg == "" && !seen {
			first, seen = i, true
		}
	})
	if best != castle || !first.IsMate {
		t.Fatalf("depth 1 with O-O-O as TT move: best %s, mate=%v (score %d); want %s mating", best, first.IsMate, first.Score, castle)
	}
}

func TestIsPseudoLegalCastle(t *testing.T) {
	wk := types.MakeMove(types.E1, types.G1, types.FlagKingCastle)
	wq := types.MakeMove(types.E1, types.C1, types.FlagQueenCastle)
	bk := types.MakeMove(types.E8, types.G8, types.FlagKingCastle)
	cases := []struct {
		fen  string
		m    types.Move
		want bool
	}{
		{"r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", wk, true},
		{"r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", wq, true},
		{"r3k2r/8/8/8/8/8/8/R3K2R b KQkq - 0 1", bk, true},
		{"r3k2r/8/8/8/8/8/8/R3K2R b KQkq - 0 1", wk, false},   // not white's move
		{"r3k2r/8/8/8/8/8/8/R3K2R w Qkq - 0 1", wk, false},    // right lost
		{"r3k2r/8/8/8/8/8/8/R3KB1R w KQkq - 0 1", wk, false},  // piece in the way
		{"r3k2r/8/8/8/8/5r2/8/R3K2R w KQkq - 0 1", wk, false}, // passes through check
		{"r3k2r/8/8/8/8/4r3/8/R3K2R w KQkq - 0 1", wk, false}, // in check
		{"r3k2r/8/8/8/8/8/8/R3K3 w KQkq - 0 1", wk, false},    // no rook
	}
	for _, c := range cases {
		b, err := board.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(err)
		}
		if got := movegen.IsPseudoLegal(b, c.m); got != c.want {
			t.Errorf("%s %s: got %v want %v", c.fen, c.m, got, c.want)
		}
	}
}
