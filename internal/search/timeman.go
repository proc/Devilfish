package search

import (
	"sync/atomic"
	"time"

	"devilfish/internal/types"
)

type SearchLimits struct {
	WhiteTime    int64 // ms
	BlackTime    int64
	WhiteInc     int64
	BlackInc     int64
	MovesToGo    int
	MoveTime     int64
	MaxDepth     int8
	MaxNodes     uint64
	Infinite     bool
	Ponder       bool
}

type TimeManager struct {
	StartTime   time.Time
	OptTime     time.Duration
	MaxTime     time.Duration
	MaxDepth    int8
	MaxNodes    uint64
	Infinite    bool
	Ponder      atomic.Bool
	Stopped     *atomic.Bool
	Nodes       *atomic.Uint64
}

func NewTimeManager(limits SearchLimits, side types.Color, stopped *atomic.Bool, nodes *atomic.Uint64) *TimeManager {
	tm := &TimeManager{
		StartTime: time.Now(),
		MaxDepth:  limits.MaxDepth,
		MaxNodes:  limits.MaxNodes,
		Infinite:  limits.Infinite,
		Stopped:   stopped,
		Nodes:     nodes,
	}
	tm.Ponder.Store(limits.Ponder)

	if limits.MoveTime > 0 {
		dur := time.Duration(limits.MoveTime) * time.Millisecond
		tm.OptTime = dur
		tm.MaxTime = dur
		return tm
	}

	if limits.Infinite {
		return tm
	}

	var myTime, myInc int64
	if side == types.White {
		myTime = limits.WhiteTime
		myInc = limits.WhiteInc
	} else {
		myTime = limits.BlackTime
		myInc = limits.BlackInc
	}

	if myTime <= 0 {
		// No time specified; default to infinite or depth limit
		return tm
	}

	var optTimeMs, maxTimeMs int64
	if limits.MovesToGo > 0 {
		moves := int64(limits.MovesToGo)
		if moves > 50 {
			moves = 50
		}
		optTimeMs = (myTime / (moves + 2)) + (myInc * 3 / 4)
		maxTimeMs = (myTime / (moves / 2 + 1)) + myInc
	} else {
		optTimeMs = (myTime / 25) + (myInc / 2)
		maxTimeMs = (myTime / 8) + myInc
	}

	// Safety margins & Move Overhead
	moveOverhead := int64(25) // 25ms buffer for GUI / pipe latency
	if myTime > moveOverhead*2 {
		if maxTimeMs > myTime-moveOverhead {
			maxTimeMs = myTime - moveOverhead
		}
	} else {
		maxTimeMs = myTime / 2
		if maxTimeMs < 5 {
			maxTimeMs = 5
		}
	}

	if optTimeMs > maxTimeMs {
		optTimeMs = maxTimeMs
	}
	if optTimeMs < 5 {
		optTimeMs = 5
	}

	tm.OptTime = time.Duration(optTimeMs) * time.Millisecond
	tm.MaxTime = time.Duration(maxTimeMs) * time.Millisecond

	return tm
}

func (tm *TimeManager) PonderHit() {
	tm.StartTime = time.Now()
	tm.Ponder.Store(false)
}

func (tm *TimeManager) CheckLimits() bool {
	if tm.Stopped.Load() {
		return true
	}

	if tm.MaxNodes > 0 && tm.Nodes.Load() >= tm.MaxNodes {
		tm.Stopped.Store(true)
		return true
	}

	if tm.Ponder.Load() {
		return false
	}

	if tm.Infinite {
		return false
	}

	if tm.MaxTime > 0 && time.Since(tm.StartTime) >= tm.MaxTime {
		tm.Stopped.Store(true)
		return true
	}

	return false
}

func (tm *TimeManager) CanStartNewIteration() bool {
	if tm.Stopped.Load() {
		return false
	}
	if tm.Ponder.Load() {
		return true
	}
	if tm.Infinite {
		return true
	}
	if tm.OptTime > 0 && time.Since(tm.StartTime) >= tm.OptTime {
		return false
	}
	return true
}

// AdjustOptTime dynamically scales the optimal search time based on move stability and score drops.
func (tm *TimeManager) AdjustOptTime(factor float64) {
	if tm.OptTime <= 0 || tm.Infinite {
		return
	}
	newOptMs := int64(float64(tm.OptTime.Milliseconds()) * factor)
	if newOptMs < 10 {
		newOptMs = 10
	}
	maxMs := tm.MaxTime.Milliseconds()
	if maxMs > 0 && newOptMs > maxMs {
		newOptMs = maxMs
	}
	tm.OptTime = time.Duration(newOptMs) * time.Millisecond
}
