package eval

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

func TestSEE(t *testing.T) {
	tests := []struct {
		name     string
		fen      string
		moveStr  string
		expected int32
	}{
		{
			name:     "Queen takes defended pawn",
			fen:      "rnbqkbnr/ppp1pppp/4p3/3p4/8/3Q4/PPPPPPPP/RNB1KBNR w KQkq - 0 1",
			moveStr:  "d3d5",
			expected: -800, // 100 - 900 = -800
		},
		{
			name:     "Rook takes defended knight",
			fen:      "r1bqkb1r/pppppppp/5n2/8/8/5R2/PPPPPPPP/RNB1KBNR w KQkq - 0 1",
			moveStr:  "f3f6", // f6 is defended by e7 and g7
			expected: -200, // 300 - 500 = -200
		},
		{
			name:     "Pawn takes hanging bishop",
			fen:      "rnbqk1nr/pppp1ppp/8/4b3/3P4/8/PPP1PPPP/RNBQKBNR w KQkq - 0 1",
			moveStr:  "d4e5",
			expected: 300,
		},
		{
			name:     "Knight takes knight, defended by pawn",
			fen:      "r1bqkb1r/pppppppp/2n5/4N3/8/8/PPPPPPPP/RNBQKB1R w KQkq - 0 1",
			moveStr:  "e5c6", // c6 defended by b7 and d7
			expected: 0,    // 300 - 300 = 0
		},
		{
			name:     "Rook takes hanging knight on a7",
			fen:      "r1b2rk1/Np4qp/2n1pp2/3p4/3P1b2/3B1N2/PP3PPP/R2Q1RK1 b - - 0 16",
			moveStr:  "a8a7",
			expected: 300,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := board.ParseFEN(tc.fen)
			if err != nil {
				t.Fatalf("ParseFEN error: %v", err)
			}
			from, _ := types.ParseSquare(tc.moveStr[:2])
			to, _ := types.ParseSquare(tc.moveStr[2:])
			m := types.MakeMove(from, to, 0)
			actual := SEE(b, m)
			if actual != tc.expected {
				t.Errorf("SEE(%s) = %d, expected %d", tc.moveStr, actual, tc.expected)
			}
		})
	}
}
