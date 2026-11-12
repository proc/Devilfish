package search

import (
	"sync/atomic"
	"testing"
	"time"

	"devilfish/internal/types"
)

func newTM(l SearchLimits, side types.Color) *TimeManager {
	var stopped atomic.Bool
	var nodes atomic.Uint64
	return NewTimeManager(l, side, &stopped, &nodes)
}

// A clock lead grows the allocation, more for a bigger lead, up to a ceiling,
// for either color.
func TestClockLeadThinksMore(t *testing.T) {
	even := newTM(SearchLimits{WhiteTime: 120000, BlackTime: 120000, WhiteInc: 2000, BlackInc: 2000}, types.White)
	ahead := newTM(SearchLimits{WhiteTime: 120000, BlackTime: 60000, WhiteInc: 2000, BlackInc: 2000}, types.White)
	farAhead := newTM(SearchLimits{WhiteTime: 120000, BlackTime: 20000, WhiteInc: 2000, BlackInc: 2000}, types.White)
	if !(farAhead.OptTime > ahead.OptTime && ahead.OptTime > even.OptTime) {
		t.Errorf("expected opt to grow with the lead: even %v, ahead %v, far ahead %v", even.OptTime, ahead.OptTime, farAhead.OptTime)
	}
	if farAhead.OptTime > time.Duration(float64(even.OptTime)*leadMaxScale)+time.Millisecond {
		t.Errorf("lead scaling went above the ceiling: %v vs %v", farAhead.OptTime, even.OptTime)
	}
	black := newTM(SearchLimits{WhiteTime: 60000, BlackTime: 120000, WhiteInc: 2000, BlackInc: 2000}, types.Black)
	if black.OptTime != ahead.OptTime {
		t.Errorf("black with the same lead should get the same budget: %v vs %v", black.OptTime, ahead.OptTime)
	}

	// Being behind, or an unknown opponent clock, leaves the budget alone.
	behind := newTM(SearchLimits{WhiteTime: 120000, BlackTime: 240000, WhiteInc: 2000, BlackInc: 2000}, types.White)
	unknown := newTM(SearchLimits{WhiteTime: 120000, WhiteInc: 2000}, types.White)
	if behind.OptTime != even.OptTime || unknown.OptTime != even.OptTime {
		t.Errorf("behind %v / unknown %v should equal even %v", behind.OptTime, unknown.OptTime, even.OptTime)
	}

	// A small lead (within the threshold) changes nothing.
	slight := newTM(SearchLimits{WhiteTime: 130000, BlackTime: 120000, WhiteInc: 2000, BlackInc: 2000}, types.White)
	alone := newTM(SearchLimits{WhiteTime: 130000, WhiteInc: 2000}, types.White)
	if slight.OptTime != alone.OptTime {
		t.Errorf("slight lead changed the budget: %v vs %v", slight.OptTime, alone.OptTime)
	}

	// In time trouble a lead buys nothing extra.
	trouble := newTM(SearchLimits{WhiteTime: 2500, BlackTime: 500, WhiteInc: 100, BlackInc: 100}, types.White)
	troubleAlone := newTM(SearchLimits{WhiteTime: 2500, WhiteInc: 100}, types.White)
	if !trouble.IsTimeTrouble || trouble.OptTime != troubleAlone.OptTime {
		t.Errorf("time trouble with a lead: %v vs %v", trouble.OptTime, troubleAlone.OptTime)
	}
}

// More time on the clock means more time per move.
func TestLongClockThinksMore(t *testing.T) {
	prev := time.Duration(0)
	for _, ms := range []int64{30000, 60000, 180000, 300000, 900000, 3600000} {
		tm := newTM(SearchLimits{WhiteTime: ms, BlackTime: ms}, types.White)
		if tm.OptTime <= prev {
			t.Errorf("%d ms clock: opt %v not above %v for less time", ms, tm.OptTime, prev)
		}
		prev = tm.OptTime
	}
}

// In-search adjustments stay within bounds of the base allocation, and the
// ceiling is higher with a clock lead or a long clock.
func TestAdjustOptTimeIsBounded(t *testing.T) {
	check := func(name string, tm *TimeManager, ceiling float64) {
		base := tm.OptTime
		for i := 0; i < 20; i++ {
			tm.AdjustOptTime(1.25)
		}
		limit := min(time.Duration(float64(base)*ceiling), tm.MaxTime)
		if tm.OptTime > limit+time.Millisecond {
			t.Errorf("%s: extensions compounded to %v, limit %v", name, tm.OptTime, limit)
		}
		for i := 0; i < 20; i++ {
			tm.AdjustOptTime(0.75)
		}
		if floor := time.Duration(float64(base) * 0.35); tm.OptTime < floor-time.Millisecond {
			t.Errorf("%s: reductions compounded to %v, floor %v", name, tm.OptTime, floor)
		}
	}
	even := newTM(SearchLimits{WhiteTime: 300000, BlackTime: 300000}, types.White)
	lead := newTM(SearchLimits{WhiteTime: 300000, BlackTime: 100000}, types.White)
	long := newTM(SearchLimits{WhiteTime: 900000, BlackTime: 900000}, types.White)
	if even.MaxExtend != 2.0 || lead.MaxExtend != 2.5 || long.MaxExtend != 2.5 {
		t.Errorf("ceilings: even %.2f, lead %.2f, long %.2f", even.MaxExtend, lead.MaxExtend, long.MaxExtend)
	}
	check("even", even, 2.0)
	check("lead", lead, 2.5)
	check("long", long, 2.5)
}

// No single move may take more than 15% of the clock (18% in endgames, +3%
// with a big clock lead).
func TestHardCap(t *testing.T) {
	for _, ms := range []int64{30000, 180000, 900000, 3600000} {
		for _, opp := range []int64{ms, ms / 4} {
			tm := newTM(SearchLimits{WhiteTime: ms, BlackTime: opp, WhiteInc: ms / 60, BlackInc: ms / 60}, types.White)
			if tm.MaxTime > time.Duration(float64(ms)*0.18)*time.Millisecond {
				t.Errorf("%d ms vs %d ms: max %v exceeds 18%%", ms, opp, tm.MaxTime)
			}
		}
	}
}

// An iteration that runs well past OptTime is stopped before MaxTime.
func TestSoftStop(t *testing.T) {
	tm := newTM(SearchLimits{WhiteTime: 300000, BlackTime: 300000}, types.White)
	tm.StartTime = time.Now().Add(-time.Duration(float64(tm.OptTime) * 1.1))
	if tm.CheckLimits() {
		t.Fatal("stopped too early, at 1.1x OptTime")
	}
	if tm.MaxTime > time.Duration(float64(tm.OptTime)*softStopFactor) {
		tm.StartTime = time.Now().Add(-time.Duration(float64(tm.OptTime) * (softStopFactor + 0.05)))
		if !tm.CheckLimits() {
			t.Fatal("expected a stop past the soft limit")
		}
	}

	// movetime searches use their full time.
	var stopped atomic.Bool
	var nodes atomic.Uint64
	mt := NewTimeManager(SearchLimits{MoveTime: 1000}, types.White, &stopped, &nodes)
	mt.StartTime = time.Now().Add(-900 * time.Millisecond)
	if mt.CheckLimits() {
		t.Fatal("movetime search stopped early")
	}
}
