package board

import (
	"testing"

	"devilfish/internal/types"
)

func TestCuckooTableSize(t *testing.T) {
	n := 0
	for _, k := range cuckoo {
		if k != 0 {
			n++
		}
	}
	if n != 3668 {
		t.Errorf("%d reversible moves in the cuckoo table, want 3668", n)
	}
}

func TestUpcomingRepetition(t *testing.T) {
	b, _ := ParseFEN("4k3/8/8/8/8/8/8/4K2R w - - 0 1")
	play := func(s string) {
		t.Helper()
		from := types.Square(int(s[0]-'a') + 8*int(s[1]-'1'))
		to := types.Square(int(s[2]-'a') + 8*int(s[3]-'1'))
		if !b.MakeMove(types.MakeMove(from, to, 0)) {
			t.Fatalf("illegal %s", s)
		}
	}
	if b.UpcomingRepetition() {
		t.Error("repetition reported at the start")
	}
	play("h1h2")
	play("e8d8")
	if b.UpcomingRepetition() {
		t.Error("repetition reported after two plies")
	}
	play("h2h1")
	// Black to move: Kd8-e8 returns to the starting position.
	if !b.UpcomingRepetition() {
		t.Error("Ke8 repeats the starting position, not reported")
	}
	play("d8c8")
	// White to move: nothing white can play repeats (the black king is on c8).
	if b.UpcomingRepetition() {
		t.Error("repetition reported with the king elsewhere")
	}
}
