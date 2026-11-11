package book

import (
	"strings"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

func makeMoves(t *testing.T, moveStr string) *board.Board {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatalf("failed to parse StartFEN: %v", err)
	}

	for _, mStr := range strings.Fields(moveStr) {
		legals := movegen.GenerateLegalMoves(b)
		var found types.Move
		for _, lm := range legals {
			if lm.String() == mStr {
				found = lm
				break
			}
		}
		if found == types.MoveNone {
			t.Fatalf("illegal move %s on board: %s", mStr, b.ToFEN())
		}
		b.MakeMove(found)
	}
	return b
}

func TestFindOpeningExact(t *testing.T) {
	// 1. Sicilian Najdorf
	b1 := makeMoves(t, "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6")
	op1 := FindOpening(b1)
	if op1.ECO != "B90" || !strings.Contains(op1.Name, "Najdorf") {
		t.Errorf("expected B90 Najdorf, got: %s", op1)
	}

	// 2. Ruy Lopez
	b2 := makeMoves(t, "e2e4 e7e5 g1f3 b8c6 f1b5")
	op2 := FindOpening(b2)
	if op2.ECO != "C60" || !strings.Contains(op2.Name, "Ruy Lopez") {
		t.Errorf("expected C60 Ruy Lopez, got: %s", op2)
	}

	// 3. Italian Game
	b3 := makeMoves(t, "e2e4 e7e5 g1f3 b8c6 f1c4")
	op3 := FindOpening(b3)
	if op3.ECO != "C50" || !strings.Contains(op3.Name, "Italian") {
		t.Errorf("expected C50 Italian Game, got: %s", op3)
	}

	// 4. French Defense
	b4 := makeMoves(t, "e2e4 e7e6")
	op4 := FindOpening(b4)
	if op4.ECO != "C00" || !strings.Contains(op4.Name, "French") {
		t.Errorf("expected C00 French Defense, got: %s", op4)
	}

	// 5. Queen's Gambit Declined
	b5 := makeMoves(t, "d2d4 d7d5 c2c4 e7e6")
	op5 := FindOpening(b5)
	if op5.ECO != "D30" || !strings.Contains(op5.Name, "Queen's Gambit Declined") {
		t.Errorf("expected D30 Queen's Gambit Declined, got: %s", op5)
	}
}

func TestFindOpeningHistoryWalkback(t *testing.T) {
	// Play Sicilian Najdorf, and then play random novel/quiet moves
	b := makeMoves(t, "e2e4 c7c5 g1f3 d7d6 d2d4 c5d4 f3d4 g8f6 b1c3 a7a6 h2h3 h7h6")
	op := FindOpening(b)
	if op.ECO != "B90" || !strings.Contains(op.Name, "Najdorf") {
		t.Errorf("expected history walkback to find B90 Najdorf, got: %s", op)
	}
}
