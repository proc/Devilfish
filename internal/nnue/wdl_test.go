package nnue

import (
	"testing"

	"devilfish/internal/board"
)

// TestWDLMatchesStockfish checks the win/draw/loss chances against what
// Stockfish itself printed (UCI_ShowWDL) for the same position and score.
// Stockfish works from the unrounded score, so allow a few thousandths.
func TestWDLMatchesStockfish(t *testing.T) {
	cases := []struct {
		fen     string
		cp      int32
		w, d, l int
	}{
		{"rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", 26, 51, 943, 6},
		{"r1bq1rk1/pp2bppp/2n1pn2/3p4/2PP4/2N1PN2/PP2BPPP/R1BQ1RK1 w - - 0 9", 218, 992, 8, 0},
		{"8/5pk1/6p1/8/3R4/6P1/5PK1/1r6 w - - 0 40", 2, 4, 992, 4},
		{"r3k2r/ppp2ppp/2n5/3q4/8/2N5/PPP2PPP/R2QK2R b KQkq - 0 12", 63, 130, 870, 0},
		{"4k3/8/8/8/8/8/4P3/4K3 w - - 0 1", 721, 1000, 0, 0},
	}
	near := func(a, b int) bool { return a-b <= 3 && b-a <= 3 }
	for _, c := range cases {
		b, err := board.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(err)
		}
		w, d, l := WDL(b, c.cp)
		if !near(w, c.w) || !near(d, c.d) || !near(l, c.l) {
			t.Errorf("%s at %d cp: got %d %d %d, Stockfish %d %d %d", c.fen, c.cp, w, d, l, c.w, c.d, c.l)
		}
		if w+d+l != 1000 {
			t.Errorf("%s: chances sum to %d", c.fen, w+d+l)
		}
		// The other side's view is the mirror image.
		lw, ld, ll := WDL(b, -c.cp)
		if lw != l || ld != d || ll != w {
			t.Errorf("%s: %d cp gives %d %d %d but %d cp gives %d %d %d", c.fen, c.cp, w, d, l, -c.cp, lw, ld, ll)
		}
	}
}

// A score of 100 is by definition the advantage that wins half the time.
func TestWDLHundredIsEvenChance(t *testing.T) {
	b, _ := board.ParseFEN(board.StartFEN)
	if w, _, _ := WDL(b, 100); w != 500 {
		t.Errorf("win chance at 100 cp is %d, want 500", w)
	}
}
