package book

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

func TestDrawkillerNames(t *testing.T) {
	for name, want := range map[string]string{
		"<drawkiller>": "0.90", "drawkiller": "0.90", "<DK>": "0.90", "<drawkiller_v5>": "0.90",
		"<drawkiller_1.00>": "1.00", "<drawkiller_1.2>": "1.20", "dk-1.50": "1.50",
		"<drawkiller+1.30>": "1.30", "<drawkiller_v5_1.40>": "1.40",
		"<drawkiller_2.00>": "", "<pohl>": "", "dkbook.bin": "", "": "",
	} {
		if got := drawkillerLevel(name); got != want {
			t.Errorf("drawkillerLevel(%q) = %q, want %q", name, got, want)
		}
	}
}

// Every embedded set loads, and following it for both sides from the start
// position plays a full Drawkiller line: the kings end in opposite corners,
// nothing has been captured, and the line is as long as the suite's (10 or 11
// shuffling moves, then 4 pawn moves each).
func TestDrawkillerBooksPlayLines(t *testing.T) {
	for _, level := range DrawkillerLevels {
		bk, err := LoadFile("<drawkiller_" + level + ">")
		if err != nil {
			t.Fatalf("%s: %v", level, err)
		}
		if bk.EntryCount() < 50_000 {
			t.Errorf("%s: only %d entries", level, bk.EntryCount())
		}
		for game := 0; game < 20; game++ {
			b, _ := board.ParseFEN("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
			plies := 0
			for ; plies < 60; plies++ {
				m := bk.Probe(b)
				if m == types.MoveNone {
					break
				}
				if !b.MakeMove(m) {
					t.Fatalf("%s: illegal book move %s", level, m)
				}
			}
			wk, bkSq := b.KingSquare(types.White), b.KingSquare(types.Black)
			corners := (wk == types.A1 && bkSq == types.H8) || (wk == types.H1 && bkSq == types.A8)
			pieces := 0
			for _, p := range b.Pieces {
				if p != types.PieceNone {
					pieces++
				}
			}
			if plies != 28 && plies != 30 || !corners || pieces != 32 {
				t.Errorf("%s game %d: left the book after %d plies, kings on %v/%v, %d pieces", level, game, plies, wk, bkSq, pieces)
			}
		}
	}
}
