package uci

import (
	"sync"
	"time"
)

// lagTracker measures how much clock time the GUI charges us per move beyond
// our own thinking: the time between our bestmove and the start of our next
// clock that the GUI counts against us (its own processing, display, pipes).
// BanksiaGUI was found charging about 200 ms a move this way while the engine
// answered in a millisecond; in a long one-minute game that is half the
// clock. The GUI's own clock readings show it: our clock at one move, plus
// the increment, less the time we thought, should equal our clock at the
// next move; whatever is missing is the lag.
type lagTracker struct {
	mu sync.Mutex

	// The move in progress.
	game    string    // the position command's start (FEN or startpos)
	ply     int       // plies played in the position searched
	clock   int64     // our clock in its go command, ms
	inc     int64     // our increment, ms
	started time.Time // our clock started (go, or ponderhit); zero while pondering
	think   time.Duration
	done    bool // bestmove sent for it, with started set

	est     float64 // smoothed lag per move, ms
	samples int
}

// lagAlpha weights a new measurement in the smoothed lag.
const lagAlpha = 0.25

// maxLagSample discards measurements above it (ms): a paused or adjudicated
// game, not lag.
const maxLagSample = 2000

// onGo records a new search, first measuring the lag of the previous move
// when this one follows it in the same game (two plies on: our move and the
// reply; with "go ponder" the reply is the predicted one, and the clocks are
// the same). game and ply describe the position searched.
func (t *lagTracker) onGo(game string, ply int, clock, inc int64, ponder bool, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done && clock > 0 && game == t.game && ply == t.ply+2 {
		lag := float64(t.clock + t.inc - t.think.Milliseconds() - clock)
		if lag >= 0 && lag <= maxLagSample {
			if t.samples == 0 {
				t.est = lag
			} else {
				t.est += lagAlpha * (lag - t.est)
			}
			t.samples++
		}
	}
	t.game, t.ply, t.clock, t.inc = game, ply, clock, inc
	t.started, t.think, t.done = time.Time{}, 0, false
	if !ponder {
		t.started = now
	}
}

// onPonderHit starts our clock for a pondered search.
func (t *lagTracker) onPonderHit(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started.IsZero() && !t.done {
		t.started = now
	}
}

// onBestMove records the end of our thinking for the move in progress. A
// bestmove ending a ponder search (a ponder miss) has no start and is not
// measured.
func (t *lagTracker) onBestMove(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started.IsZero() && !t.done {
		t.think = now.Sub(t.started)
		t.done = true
	}
}

// overhead returns the move overhead to plan with: the configured one, or
// the measured lag when that is larger. Two measurements are needed first.
func (t *lagTracker) overhead(configured int64) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.samples < 2 {
		return configured
	}
	return max(configured, int64(t.est+0.5))
}
