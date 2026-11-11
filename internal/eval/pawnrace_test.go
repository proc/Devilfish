package eval

import (
	"testing"

	"devilfish/internal/board"
)

func TestPawnRaceBonus(t *testing.T) {
	cases := []struct {
		name string
		fen  string
		want int // sign of the bonus, relative to side to move
	}{
		{"white pawn outside the square, white to move", "8/8/8/P7/8/8/8/3K3k w - - 0 1", +1},
		{"same, black to move", "8/8/8/P7/8/8/8/3K3k b - - 0 1", -1},
		{"black king inside the square", "8/8/2k5/P7/8/8/8/7K w - - 0 1", 0},
		{"tempo decides: black to move steps into the square", "8/8/8/P7/4k3/8/8/7K b - - 0 1", 0},
		{"tempo decides: white to move escapes", "8/8/8/P7/4k3/8/8/7K w - - 0 1", +1},
		{"double push from the 2nd rank counts", "8/8/8/8/8/8/P7/4k2K w - - 0 1", +1},
		{"not passed", "8/1p6/8/P7/8/8/8/k6K w - - 0 1", 0},
		{"pieces on board: no bonus", "8/8/8/P7/8/8/8/n6k w - - 0 1", 0},
		{"black passer runs", "K7/8/8/8/7p/8/8/7k b - - 0 1", +1},
		{"both queen, black much faster", "7k/8/8/8/P7/8/7p/K7 w - - 0 1", -1},
	}
	for _, tc := range cases {
		b, err := board.ParseFEN(tc.fen)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := PawnRaceBonus(b)
		sign := 0
		if got > 0 {
			sign = 1
		} else if got < 0 {
			sign = -1
		}
		if sign != tc.want {
			t.Errorf("%s (%s): PawnRaceBonus = %d, want sign %d", tc.name, tc.fen, got, tc.want)
		}
	}
}
