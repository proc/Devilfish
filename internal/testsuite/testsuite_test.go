package testsuite

import (
	"strings"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
)

func TestWACParses(t *testing.T) {
	ps, err := Load("wac")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 300 {
		t.Fatalf("expected 300 positions, got %d", len(ps))
	}
	if ps[0].ID != "WAC.001" || ps[299].ID != "WAC.300" {
		t.Errorf("unexpected ids %q, %q", ps[0].ID, ps[299].ID)
	}
	if got := SAN(ps[0].Board, ps[0].Best[0]); got != "Qg6" {
		t.Errorf("WAC.001 best move: got %s, want Qg6", got)
	}
	// Every suite move must format back to the SAN it was written as.
	for _, p := range ps {
		for i, m := range p.Best {
			if got, want := SAN(p.Board, m), normalizeSAN(p.BestSAN[i]); normalizeSAN(got) != want {
				t.Errorf("%s: %s formats as %s", p.ID, p.BestSAN[i], got)
			}
		}
	}
}

func TestSANAllLegalMovesRoundTrip(t *testing.T) {
	ps, _ := Load("wac")
	for _, p := range ps {
		for _, m := range movegen.GenerateLegalMoves(p.Board) {
			back, err := ParseSAN(p.Board, SAN(p.Board, m))
			if err != nil || back != m {
				t.Fatalf("%s: %s (%s) did not round-trip", p.ID, SAN(p.Board, m), m)
			}
		}
	}
}

func TestSANCases(t *testing.T) {
	cases := []struct{ fen, uci, san string }{
		{"r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", "e1g1", "O-O"},
		{"r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", "e1c1", "O-O-O"},
		{"r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", "a1d1", "Rd1"},
		{"4k3/8/8/8/8/8/K7/R6R w - - 0 1", "a1d1", "Rad1"},
		{"4k3/8/8/8/8/8/K7/R6R w - - 0 1", "h1f1", "Rhf1"},
		{"4k3/8/8/R7/8/8/8/R3K3 w - - 0 1", "a1a3", "R1a3"},
		{"4k3/8/8/3pP3/8/8/8/4K3 w - d6 0 2", "e5d6", "exd6"},
		{"1n2k3/P7/8/8/8/8/8/4K3 w - - 0 1", "a7b8q", "axb8=Q"},
		{"4k3/P7/8/8/8/8/8/4K3 w - - 0 1", "a7a8n", "a8=N"},
		{"4k3/8/8/8/8/2N3N1/8/4K3 w - - 0 1", "c3e4", "Nce4"},
		{"4k3/8/8/8/8/2N5/8/2N1K3 w - - 0 1", "c3e2", "N3e2"},
		{"4k3/8/8/8/8/Q1Q5/8/Q3K3 w - - 0 1", "a3b2", "Qa3b2"},
	}
	for _, c := range cases {
		b, err := board.ParseFEN(c.fen)
		if err != nil {
			t.Fatal(err)
		}
		m, err := ParseSAN(b, c.uci)
		if err != nil {
			t.Fatalf("%s: %v", c.uci, err)
		}
		if got := SAN(b, m); got != c.san {
			t.Errorf("%s %s: got %s, want %s", c.fen, c.uci, got, c.san)
		}
		if again, err := ParseSAN(b, c.san+"+"); err != nil || again != m {
			t.Errorf("ParseSAN(%s+) failed", c.san)
		}
	}
	b, _ := board.ParseFEN("r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1")
	if _, err := ParseSAN(b, "0-0"); err != nil {
		t.Error("0-0 should parse as castling")
	}
}

func TestParseOps(t *testing.T) {
	ps, err := Parse(strings.NewReader(
		`r1b1k2r/ppppnppp/2n2q2/2b5/3NP3/2P1B3/PP3PPP/RN1QKB1R w KQkq - bm Qd2 Be2; am Nxc6; id "x;y";` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	p := ps[0]
	if p.ID != "x;y" || len(p.Best) != 2 || len(p.Avoid) != 1 {
		t.Fatalf("got id %q, %d bm, %d am", p.ID, len(p.Best), len(p.Avoid))
	}
	if p.Solved(p.Avoid[0]) || !p.Solved(p.Best[1]) {
		t.Error("Solved() disagrees with bm/am")
	}
	if _, err := Parse(strings.NewReader("8/8/8/8/8/8/8/K6k w - - bm Qh1;\n")); err == nil {
		t.Error("an unresolvable bm move should be an error")
	}
}
