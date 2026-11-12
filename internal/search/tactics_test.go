package search

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// searchFor returns the engine's best move for fen at the given depth, also
// reporting the final score via the info callback. The opening book is
// disabled so the search performs real evaluation rather than a book probe.
func searchFor(t *testing.T, fen string, depth int8) (types.Move, int32) {
	t.Helper()
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatalf("ParseFEN(%q) failed: %v", fen, err)
	}
	eng := NewEngine(32, 1)
	eng.SetUseBook(false)
	var score int32
	mv, _ := eng.Search(b, SearchLimits{MaxDepth: depth}, func(si SearchInfo) {
		score = si.Score
	})
	return mv, score
}

// TestForcedMate verifies the engine finds a forced mate in 4 and reports a
// mate score. Several first moves mate equally fast (Qf8+ and Qf7+ both do),
// so the test checks the mate distance rather than one particular move.
func TestForcedMate(t *testing.T) {
	fen := "6k1/8/8/8/8/8/5Q2/5RK1 w - - 0 1"
	mv, score := searchFor(t, fen, 10)
	if score < MateThreshold {
		t.Fatalf("%s: expected mate score >= %d, got %d (move %s)", fen, MateThreshold, score, mv)
	}
	if plies := MateScore - score; plies > 7 {
		t.Errorf("%s: %s mates in %d plies, expected mate in 4 (7 plies)", fen, mv, plies)
	}
}

// TestMateInOne verifies the engine finds mate-in-one across two piece
// combinations and that the resulting position is genuinely checkmate (the
// receiver has no legal moves and is still in check).
func TestMateInOne(t *testing.T) {
	cases := []struct {
		name string
		fen  string
		best string
	}{
		{"rook lift", "6k1/5ppp/8/8/8/8/8/4R1K1 w - - 0 1", "e1e8"},
		{"queen lift", "6k1/5ppp/8/8/8/8/8/4Q1K1 w - - 0 1", "e1e8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mv, score := searchFor(t, tc.fen, 8)
			if mv.String() != tc.best {
				t.Errorf("%s: expected %s, got %s", tc.fen, tc.best, mv)
			}
			if score < MateThreshold {
				t.Errorf("%s: expected mate score >= %d, got %d", tc.fen, MateThreshold, score)
			}
			// Board-level confirmation: after the mate move the side to move
			// is in check with no legal moves.
			b, err := board.ParseFEN(tc.fen)
			if err != nil {
				t.Fatalf("ParseFEN(%q) failed: %v", tc.fen, err)
			}
			if !b.MakeMove(mv) {
				t.Fatalf("engine move %s was not even legal", mv)
			}
			if !b.IsInCheck(b.SideToMove) {
				t.Errorf("%s: post-mate position should have the side to move in check", tc.fen)
			}
			if legal := movegen.GenerateLegalMoves(b); len(legal) != 0 {
				t.Errorf("%s: expected 0 legal moves after mate, got %d: %v",
					tc.fen, len(legal), legal)
			}
		})
	}
}

// TestWinUndefendedRook verifies the engine wins an undefended rook, once via
// a queen diagonal (Qa1-h8) and once with a rook (Ra1-a8).
func TestWinUndefendedRook(t *testing.T) {
	cases := []struct {
		name string
		fen  string
		best string
	}{
		{"queen diagonal", "4k2r/8/8/8/8/8/8/Q6K w - - 0 1", "a1h8"},
		{"rook file", "r3k3/8/8/8/8/8/8/R7 w - - 0 1", "a1a8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mv, _ := searchFor(t, tc.fen, 10)
			if mv.String() != tc.best {
				t.Errorf("%s: expected %s, got %s", tc.fen, tc.best, mv)
			}
		})
	}
}

// TestForkWinsQueen verifies the engine finds a knight fork that wins the
// queen even though white starts the move down material (the score is
// negative before the fork is taken).
func TestForkWinsQueen(t *testing.T) {
	fen := "4k2r/2q1r3/8/3N4/4P3/8/6B1/6K1 w - - 0 1"
	mv, _ := searchFor(t, fen, 10)
	if mv.String() != "d5c7" {
		t.Errorf("%s: expected fork d5c7, got %s", fen, mv)
	}
	// The fork attacks the black king and queen simultaneously; confirm the
	// resulting position loses the queen for black.
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatalf("ParseFEN(%q) failed: %v", fen, err)
	}
	if !b.MakeMove(mv) {
		t.Fatalf("fork move %s was not legal", mv)
	}
	if !b.IsInCheck(types.Black) {
		t.Errorf("%s: expected d5c7 to give check to the black king", fen)
	}
	// The knight on c7 now occupies the square the black queen sat on: the fork
	// captured the queen while giving check.
	if b.PieceAt(types.C7) != types.WhiteKnight {
		t.Errorf("%s: expected white knight on c7 after Nxc7 (queen captured), got %v",
			fen, b.PieceAt(types.C7))
	}
}

// TestDiscoveredCheckMate verifies the engine finds a discovered-check mate:
// the knight leaves the e-file, revealing check from the rook, and the mate
// is delivered (Nd7#). This exercises the engine's ability to search a
// forcing discovered check to a mating conclusion.
func TestDiscoveredCheckMate(t *testing.T) {
	fen := "4k3/8/2P3P1/B3N3/8/8/8/4R2K w - - 0 1"
	mv, score := searchFor(t, fen, 10)
	if mv.String() != "e5d7" {
		t.Errorf("%s: expected discovered-check mate e5d7, got %s", fen, mv)
	}
	if score < MateThreshold {
		t.Errorf("%s: expected mate score >= %d, got %d", fen, MateThreshold, score)
	}
	// Confirm e5d7 is genuinely checkmate (side to move in check, no escapes).
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatalf("ParseFEN(%q) failed: %v", fen, err)
	}
	d7 := types.MakeMove(types.E5, types.D7, types.FlagQuiet)
	if !b.MakeMove(d7) {
		t.Fatalf("e5d7 was not legal")
	}
	if !b.IsInCheck(b.SideToMove) {
		t.Errorf("%s: expected e5d7 to give check", fen)
	}
	if legal := movegen.GenerateLegalMoves(b); len(legal) != 0 {
		t.Errorf("%s: expected 0 legal moves after Nd7#, got %d: %v", fen, len(legal), legal)
	}
}
