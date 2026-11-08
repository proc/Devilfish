package search

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

func TestTacticsMateInOne(t *testing.T) {
	tests := []struct {
		fen      string
		bestMove string
	}{
		{
			fen:      "6k1/5ppp/8/8/8/8/8/4R1K1 w - - 0 1",
			bestMove: "e1e8",
		},
		{
			fen:      "r1bqkb1r/pppp1ppp/2n5/4p3/2B1n3/5Q2/PPPP1PPP/RNB1K1NR w KQkq - 0 1",
			bestMove: "f3f7",
		},
		{
			fen:      "2r3k1/p4p2/3p2p1/1p1P4/4P1P1/5P2/PP5Q/1K5R w - - 0 1",
			bestMove: "h2h8",
		},
	}

	for _, tt := range tests {
		b, err := board.ParseFEN(tt.fen)
		if err != nil {
			t.Fatalf("failed to parse FEN %s: %v", tt.fen, err)
		}
		engine := NewEngine(64, 1)
		mv := engine.Search(b, SearchLimits{MaxDepth: 4}, nil)
		if mv.String() != tt.bestMove {
			t.Errorf("FEN %s: expected %s, got %s", tt.fen, tt.bestMove, mv.String())
		}
	}
}

func TestLazySMPScaling(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	// Search with 4 threads using Lazy SMP
	engine := NewEngine(64, 4)
	mv := engine.Search(b, SearchLimits{MaxDepth: 6}, nil)
	if mv == types.MoveNone {
		t.Errorf("Lazy SMP search returned MoveNone")
	}
}

func TestHistoryPruning(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(64, 1)
	mv := engine.Search(b, SearchLimits{MaxDepth: 5}, nil)
	if mv == types.MoveNone {
		t.Errorf("Search with History Pruning returned MoveNone")
	}
}
