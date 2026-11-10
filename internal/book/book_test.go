package book

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

func TestDefaultBookProbe(t *testing.T) {
	bk := DefaultBook()
	if bk == nil {
		t.Fatal("expected DefaultBook to be non-nil")
	}

	count := bk.EntryCount()
	t.Logf("Builtin book loaded with %d entries", count)
	if count < 100 {
		t.Errorf("expected at least 100 entries, got %d", count)
	}

	// 1. Probe startpos
	bStart, _ := board.ParseFEN(board.StartFEN)
	moveStart := bk.Probe(bStart)
	if moveStart == types.MoveNone {
		t.Fatal("expected book move from startpos, got MoveNone")
	}
	t.Logf("Startpos move probed: %s", moveStart)

	// Verify move is one of standard White opening moves (e4, d4, c4, Nf3)
	moveStr := moveStart.String()
	if moveStr != "e2e4" && moveStr != "d2d4" && moveStr != "c2c4" && moveStr != "g1f3" {
		t.Errorf("unexpected opening move from startpos: %s", moveStr)
	}

	// 2. Probe after 1. e4
	bE4, _ := board.ParseFEN("rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1")
	moveE4 := bk.Probe(bE4)
	if moveE4 == types.MoveNone {
		t.Fatal("expected book response to 1. e4, got MoveNone")
	}
	t.Logf("Response to 1. e4 probed: %s", moveE4)

	// 3. Probe after 1. d4
	bD4, _ := board.ParseFEN("rnbqkbnr/pppppppp/8/8/3P4/8/PPP1PPPP/RNBQKBNR b KQkq d3 0 1")
	moveD4 := bk.Probe(bD4)
	if moveD4 == types.MoveNone {
		t.Fatal("expected book response to 1. d4, got MoveNone")
	}
	t.Logf("Response to 1. d4 probed: %s", moveD4)
}

func TestCastlingBookMove(t *testing.T) {
	bk := DefaultBook()
	// Ruy Lopez: 1. e4 e5 2. Nf3 Nc6 3. Bb5 a6 4. Ba4 Nf6 (White about to castle O-O)
	bCastle, err := board.ParseFEN("r1bqkb1r/1ppp1ppp/p1n2n2/4p3/B3P3/5N2/PPPP1PPP/RNBQK2R w KQkq - 2 5")
	if err != nil {
		t.Fatal(err)
	}
	m := bk.Probe(bCastle)
	if m == types.MoveNone {
		t.Fatal("expected book move in Ruy Lopez, got MoveNone")
	}
	t.Logf("Probed move in Ruy Lopez: %s", m)
	if m.String() != "e1g1" {
		t.Errorf("expected 5. O-O (e1g1), got %s", m.String())
	}
}
