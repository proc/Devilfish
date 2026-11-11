package movegen

import (
	"math/rand"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

// TestIsPseudoLegalMatchesGenerator checks every one of the 65536 possible
// move encodings against the generator on a spread of positions. TT, killer
// and counter moves are validated only by IsPseudoLegal before MakeMove, so
// anything it accepts that the generator would not produce can corrupt the
// board (e.g. a pawn "capturing" across the a/h-file edge onto a king).
func TestIsPseudoLegalMatchesGenerator(t *testing.T) {
	fens := []string{
		board.StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
		"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
		// Pawns on the a/h files next to a king on the opposite edge
		"8/1p4p1/7p/4k2P/PpKp4/8/2P5/8 b - - 1 37",
		"8/8/K6p/6q1/7k/3P4/4Q3/8 b - - 0 1",
		"8/8/k7/P7/8/8/8/7K w - - 0 1",
		// En passant available
		"rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3",
	}
	rng := rand.New(rand.NewSource(1))
	checked := 0
	for _, fen := range fens {
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q): %v", fen, err)
		}
		// The FEN itself plus a few random continuations from it
		for walk := 0; walk < 6; walk++ {
			pos := b.Clone()
			for ply := 0; ply < walk*3; ply++ {
				legal := GenerateLegalMoves(pos)
				if len(legal) == 0 {
					break
				}
				pos.MakeMove(legal[rng.Intn(len(legal))])
			}
			comparePseudoLegal(t, pos)
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no positions checked")
	}
}

func comparePseudoLegal(t *testing.T, b *board.Board) {
	t.Helper()
	var ml MoveList
	GenerateMoves(b, false, &ml)
	generated := make(map[types.Move]bool, ml.Count)
	for i := 0; i < ml.Count; i++ {
		generated[ml.Moves[i]] = true
	}
	for v := 0; v < 1<<16; v++ {
		m := types.Move(v)
		got := IsPseudoLegal(b, m)
		if got != generated[m] {
			t.Fatalf("%s: IsPseudoLegal(%s, flags=%d) = %v, generator says %v",
				b.ToFEN(), m, m.Flags(), got, generated[m])
		}
	}
}
