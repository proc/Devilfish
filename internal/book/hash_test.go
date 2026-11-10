package book

import (
	"testing"

	"devilfish/internal/board"
)

func TestPolyglotHash(t *testing.T) {
	tests := []struct {
		name     string
		fen      string
		expected uint64
	}{
		{
			name:     "Startpos",
			fen:      board.StartFEN,
			expected: 0x463b96181691fc9c,
		},
		{
			name:     "After 1. e4",
			fen:      "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1",
			expected: 0x823c9b50fd114196,
		},
		{
			name:     "After 1. e4 c5",
			fen:      "rnbqkbnr/pp1ppppp/8/2p5/4P3/8/PPPP1PPP/RNBQKBNR w KQkq c6 0 2",
			expected: 0x644d4afe02564aeb,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := board.ParseFEN(tt.fen)
			if err != nil {
				t.Fatalf("failed to parse FEN: %v", err)
			}
			got := Hash(b)
			if got != tt.expected {
				t.Errorf("Hash() = 0x%016x, expected 0x%016x", got, tt.expected)
			}
		})
	}
}

func TestEnPassantHashing(t *testing.T) {
	// Position where EP square exists and pawn CAN capture it
	fenWithEpCapture := "rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3"
	b1, err := board.ParseFEN(fenWithEpCapture)
	if err != nil {
		t.Fatal(err)
	}

	// Position where EP square exists but NO pawn can capture it
	fenNoEpCapture := "rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq e6 0 2"
	b2, err := board.ParseFEN(fenNoEpCapture)
	if err != nil {
		t.Fatal(err)
	}

	h1 := Hash(b1)
	h2 := Hash(b2)
	if h1 == 0 || h2 == 0 {
		t.Errorf("unexpected zero hash")
	}
}
