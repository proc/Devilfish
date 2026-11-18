package uci

import (
	"testing"
	"time"
)

// The lag is what the GUI's clocks show missing beyond our own thinking, for
// plain searches and pondered ones; a ponder miss, a new game or a jump in
// the move count is not measured.
func TestLagTrackerMeasuresGUICharge(t *testing.T) {
	var lt lagTracker
	t0 := time.Unix(0, 0)
	ms := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }

	// Move at ply 10: 60 s on the clock, 1 s thinking, the GUI charges 1.2 s.
	lt.onGo("startpos", 10, 60000, 0, false, ms(0))
	lt.onBestMove(ms(1000))
	if got := lt.overhead(100); got != 100 {
		t.Fatalf("no measurement yet: overhead %d", got)
	}
	lt.onGo("startpos", 12, 58800, 0, false, ms(5000))
	lt.onBestMove(ms(5500))
	// Pondered next move: the go ponder carries the clock after our move.
	lt.onGo("startpos", 14, 58100, 1000, true, ms(6000))
	if got := lt.overhead(100); got != 200 {
		t.Fatalf("two moves charged 200 ms beyond thinking: overhead %d", got)
	}
	lt.onPonderHit(ms(7000))
	lt.onBestMove(ms(7300))
	// With increment: 58100 + 1000 - 300 thought - 250 lag.
	lt.onGo("startpos", 16, 58550, 1000, false, ms(8000))
	if got := lt.overhead(100); got != 213 {
		t.Fatalf("after a 250 ms lag: overhead %d, want 213", got)
	}

	// A clock 9 s short is not lag (a pause, an adjudication): discarded.
	lt.onBestMove(ms(8100))
	lt.onGo("startpos", 18, 50000, 0, true, ms(9000))
	if lt.samples != 3 {
		t.Fatalf("samples %d, want 3 (the 9 s gap is not one)", lt.samples)
	}
	// A ponder miss: no start, so no thinking time, and the next go (same
	// ply) is not measured against it.
	lt.onBestMove(ms(9500))
	lt.onGo("startpos", 18, 50000, 0, false, ms(9600))
	if lt.samples != 3 {
		t.Fatalf("samples %d, want 3 (the ponder miss is not one)", lt.samples)
	}
	// A new game, or a skipped move, is not measured either.
	lt.onBestMove(ms(9700))
	lt.onGo("startpos", 0, 60000, 0, false, ms(10000))
	lt.onBestMove(ms(10100))
	lt.onGo("startpos", 4, 1000, 0, false, ms(11000))
	if lt.samples != 3 {
		t.Fatalf("samples %d after a new game and a skipped move", lt.samples)
	}
	// The configured overhead wins when it is larger.
	if got := lt.overhead(500); got != 500 {
		t.Fatalf("overhead %d, want the configured 500", got)
	}
}
