package eval

import (
	"fmt"
	"testing"

	"devilfish/internal/board"
)

func scr(fen string) int32 {
	b, err := board.ParseFEN(fen)
	if err != nil {
		return -9999
	}
	return EvaluateClassical(b)
}

func TestScratchEval(t *testing.T) {
	// Endgame: KQ vs K (3 pieces) -> eg dominates.
	fmt.Printf("endgame KQvsK =%d\n", scr("4k3/8/8/8/8/8/8/Q3K3 w - - 0 1"))
	// Midgame: white up a queen on a near-full board -> mg dominates.
	fmt.Printf("midgame W+Q    =%d\n", scr("rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"))
	// Endgame KRvsK vs Midgame KRvsK-ish.
	fmt.Printf("endgame KRvsK =%d\n", scr("4k3/8/8/8/8/8/8/R3K3 w - - 0 1"))
	fmt.Printf("midgame W+R    =%d\n", scr("rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"))
}
