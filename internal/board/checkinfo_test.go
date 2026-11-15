package board_test

import (
	"math/rand"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
)

// On random game positions, for every pseudo-legal move: GivesCheckWith
// agrees with GivesCheck, and a move SkipsKingTest lets through is legal.
func TestCheckInfoAgreesWithBoard(t *testing.T) {
	fens := []string{
		board.StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
		"4k3/8/8/b7/8/8/3R4/r2QK2R w K - 0 1",
		"8/8/8/2k5/2pP4/8/B7/4K3 b - d3 0 1",
	}
	rng := rand.New(rand.NewSource(7))
	moves, skipped := 0, 0
	for _, fen := range fens {
		for game := 0; game < 30; game++ {
			b, err := board.ParseFEN(fen)
			if err != nil {
				t.Fatal(err)
			}
			for ply := 0; ply < 60; ply++ {
				ci := b.NewCheckInfo()
				inCheck := b.IsInCheck(b.SideToMove)
				var ml movegen.MoveList
				movegen.GenerateMoves(b, false, &ml)
				for i := 0; i < ml.Count; i++ {
					m := ml.Moves[i]
					before := b.ToFEN()
					if got, want := b.GivesCheckWith(m, &ci), b.GivesCheck(m); got != want {
						if b.MakeMove(m) { // only legal moves have a defined answer
							b.UnmakeMove(m)
							t.Fatalf("%s, %s: GivesCheckWith %v, GivesCheck %v", before, m, got, want)
						}
					}
					legal := b.MakeMove(m)
					if legal {
						b.UnmakeMove(m)
					}
					if ci.SkipsKingTest(b, m, inCheck) {
						skipped++
						if !legal {
							t.Fatalf("%s, %s: king test skipped for an illegal move", before, m)
						}
						if !b.MakeMoveUnchecked(m) {
							t.Fatalf("%s, %s: MakeMoveUnchecked refused a legal move", before, m)
						}
						b.UnmakeMove(m)
					}
					if b.ToFEN() != before {
						t.Fatalf("%s, %s: board changed", before, m)
					}
					moves++
				}
				legal := movegen.GenerateLegalMoves(b)
				if len(legal) == 0 {
					break
				}
				b.MakeMove(legal[rng.Intn(len(legal))])
			}
		}
	}
	if moves < 50000 || skipped < moves/2 {
		t.Fatalf("%d moves checked, %d with the king test skipped", moves, skipped)
	}
}
