package book

import (
	"strings"
	"testing"
	"time"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

var embeddedSuiteTests = []struct {
	name    string
	aliases []string
	min     int
}{
	{PohlBookName, []string{"pohl", "<POHL>"}, 220_000},
}

func TestEmbeddedSuitesLoad(t *testing.T) {
	for _, tc := range embeddedSuiteTests {
		start := time.Now()
		bk, err := LoadFile(tc.name)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		t.Logf("%s: %d positions, loaded in %v", tc.name, bk.EntryCount(), time.Since(start))
		if bk.EntryCount() < tc.min {
			t.Errorf("%s: expected at least %d positions, got %d", tc.name, tc.min, bk.EntryCount())
		}
		for _, a := range tc.aliases {
			if again, _ := LoadFile(a); again != bk {
				t.Errorf("%q should resolve to %s", a, tc.name)
			}
		}
	}
}

// inSuite reports whether b, at its game ply, is one of the suite's positions.
func inSuite(eb *epdBook, b *board.Board) bool {
	ply := gamePly(b)
	wp, bp := uint64(b.PieceBB[types.WhitePawn]), uint64(b.PieceBB[types.BlackPawn])
	for i := range eb.meta {
		if eb.wp[i] != wp || eb.bp[i] != bp || eb.plies(i) != ply || eb.castling(i) != b.Castling {
			continue
		}
		tb, err := eb.board(i)
		if err == nil && tb.Pieces == b.Pieces {
			return true
		}
	}
	return false
}

// Playing book moves for both sides from the start position must land exactly
// on a position from the suite.
func TestEmbeddedSuitesReachSuitePositions(t *testing.T) {
	for _, tc := range embeddedSuiteTests {
		bk, err := LoadFile(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		reached := 0
		for game := 0; game < 4; game++ {
			b, _ := board.ParseFEN(board.StartFEN)
			var moves []string
			var slowest time.Duration
			for len(moves) < 40 {
				start := time.Now()
				m := bk.Probe(b)
				slowest = max(slowest, time.Since(start))
				if m == types.MoveNone {
					break
				}
				moves = append(moves, m.String())
				if !b.MakeMove(m) {
					t.Fatalf("%s: illegal book move %s after %v", tc.name, m, moves)
				}
			}
			t.Logf("%s game %d: %d plies, slowest probe %v: %s", tc.name, game, len(moves), slowest, strings.Join(moves, " "))
			if len(moves) == 0 {
				continue
			}
			if !inSuite(bk.epd, b) {
				t.Fatalf("%s: line ended outside the suite: %s", tc.name, b.ToFEN())
			}
			reached++
		}
		if reached == 0 {
			t.Fatalf("%s: no game reached a suite position", tc.name)
		}
	}
}

// When the opponent leaves the suite, the book must stop rather than misplay.
func TestPohlBookOutOfBook(t *testing.T) {
	bk, _ := PohlBook()
	// 1. a4 h5 2. Ra3 Rh6: nothing in the suite looks like this.
	b, _ := board.ParseFEN("rnbqkbn1/ppppppp1/7r/7p/P7/R7/1PPPPPPP/1NBQKBNR w Kq - 2 3")
	if m := bk.Probe(b); m != types.MoveNone {
		t.Errorf("expected no book move, got %s", m)
	}
}

// A text EPD file round-trips through the packed format.
func TestPackedRoundTrip(t *testing.T) {
	const epd = "rnbqkbnr/pp2pppp/2p5/8/2pP4/4P3/PP3PPP/RNBQKBNR w KQkq - 0 4\n" +
		"rnbqk1nr/pppp1ppp/3b4/4p3/3PP3/5N2/PPP2PPP/RNBQKB1R b KQkq - 0 3\n"
	bk, err := ReadEPD(strings.NewReader(epd))
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := bk.WritePacked(&buf); err != nil {
		t.Fatal(err)
	}
	back, err := ReadPackedEPD(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(epd), "\n") {
		want[strings.Join(strings.Fields(l)[:3], " ")] = true
	}
	for i := 0; i < back.EntryCount(); i++ {
		b, err := back.epd.board(i)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(strings.Fields(b.ToFEN())[:3], " "); !want[got] {
			t.Errorf("unexpected position after round trip: %s", got)
		}
	}
}
