package movegen

import (
	"testing"

	"devilfish/internal/board"
)

type perftTestCase struct {
	name     string
	fen      string
	depth    int
	expected uint64
}

func TestPerft(t *testing.T) {
	testCases := []perftTestCase{
		{
			name:     "Startpos Depth 1",
			fen:      board.StartFEN,
			depth:    1,
			expected: 20,
		},
		{
			name:     "Startpos Depth 2",
			fen:      board.StartFEN,
			depth:    2,
			expected: 400,
		},
		{
			name:     "Startpos Depth 3",
			fen:      board.StartFEN,
			depth:    3,
			expected: 8902,
		},
		{
			name:     "Startpos Depth 4",
			fen:      board.StartFEN,
			depth:    4,
			expected: 197281,
		},
		{
			name:     "Position 2 (Kiwipete) Depth 1",
			fen:      "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
			depth:    1,
			expected: 48,
		},
		{
			name:     "Position 2 (Kiwipete) Depth 2",
			fen:      "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
			depth:    2,
			expected: 2039,
		},
		{
			name:     "Position 2 (Kiwipete) Depth 3",
			fen:      "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
			depth:    3,
			expected: 97862,
		},
		{
			name:     "Position 3 Depth 1",
			fen:      "8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
			depth:    1,
			expected: 14,
		},
		{
			name:     "Position 3 Depth 2",
			fen:      "8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
			depth:    2,
			expected: 191,
		},
		{
			name:     "Position 3 Depth 3",
			fen:      "8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
			depth:    3,
			expected: 2812,
		},
		{
			name:     "Position 3 Depth 4",
			fen:      "8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
			depth:    4,
			expected: 43238,
		},
		{
			name:     "Position 4 Depth 1",
			fen:      "r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
			depth:    1,
			expected: 6,
		},
		{
			name:     "Position 4 Depth 2",
			fen:      "r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
			depth:    2,
			expected: 264,
		},
		{
			name:     "Position 4 Depth 3",
			fen:      "r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
			depth:    3,
			expected: 9467,
		},
		{
			name:     "Position 5 Depth 1",
			fen:      "rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
			depth:    1,
			expected: 44,
		},
		{
			name:     "Position 5 Depth 2",
			fen:      "rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
			depth:    2,
			expected: 1486,
		},
		{
			name:     "Position 5 Depth 3",
			fen:      "rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
			depth:    3,
			expected: 62379,
		},
	}

	for _, tc := range testCases {
		b, err := board.ParseFEN(tc.fen)
		if err != nil {
			t.Fatalf("%s: failed to parse FEN: %v", tc.name, err)
		}
		nodes := Perft(b, tc.depth)
		if nodes != tc.expected {
			t.Errorf("%s (depth %d): got %d, expected %d", tc.name, tc.depth, nodes, tc.expected)
		}
	}
}
