package search

import (
	"math"
	"sync/atomic"
	"time"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

type SearchLimits struct {
	WhiteTime    int64 // ms
	BlackTime    int64
	WhiteInc     int64
	BlackInc     int64
	MovesToGo    int
	MoveTime     int64
	MoveOverhead int64
	MaxDepth     int8
	MaxNodes     uint64
	Infinite     bool
	Ponder       bool
}

type TimeManager struct {
	StartTime     time.Time
	OptTime       time.Duration
	MaxTime       time.Duration
	MaxDepth      int8
	MaxNodes      uint64
	Infinite      bool
	Ponder        atomic.Bool
	Stopped       *atomic.Bool
	Nodes         *atomic.Uint64
	RemainingTime int64
	Increment     int64
	MoveOverhead  int64
	IsTimeTrouble bool
	IsEmergency   bool
	IsMoveTime    bool
	PieceCount    int
	IsEndgame     bool

	// BaseOptTime is OptTime as first allocated; AdjustOptTime keeps OptTime
	// within [MinExtend, MaxExtend] times it, so repeated in-search
	// extensions cannot compound up to MaxTime.
	BaseOptTime time.Duration
	MinExtend   float64
	MaxExtend   float64
	// ClockLead is our expected thinking time over the next moves divided by
	// the opponent's (1 when equal or unknown).
	ClockLead float64
}

// Tuning for the clock-lead adjustment. Each side's budget is its clock plus
// the increments of the next leadHorizon moves. Above leadThreshold the
// allocation is scaled up by sqrt(lead/leadThreshold), at most leadMaxScale:
// spare time is turned into deeper search instead of being left on the clock.
const (
	leadHorizon   = 20
	leadThreshold = 1.2
	leadMaxScale  = 1.5

	// softStopFactor: an iteration still running at this multiple of OptTime
	// is stopped, rather than being allowed to run on to MaxTime.
	softStopFactor = 1.6

	// longClockMs: from this much time left, extensions may go further.
	longClockMs = 600000
)

// clockLeadScale returns the factor to scale our budget by for a given lead.
func clockLeadScale(lead float64) float64 {
	if lead <= leadThreshold {
		return 1
	}
	return min(math.Sqrt(lead/leadThreshold), leadMaxScale)
}

func NewTimeManager(limits SearchLimits, side types.Color, stopped *atomic.Bool, nodes *atomic.Uint64, boards ...*board.Board) *TimeManager {
	tm := &TimeManager{
		StartTime:  time.Now(),
		MaxDepth:   limits.MaxDepth,
		MaxNodes:   limits.MaxNodes,
		Infinite:   limits.Infinite,
		Stopped:    stopped,
		Nodes:      nodes,
		PieceCount: 32,
	}
	tm.Ponder.Store(limits.Ponder)

	if len(boards) > 0 && boards[0] != nil {
		b := boards[0]
		tm.PieceCount = bitboard.Count(b.Occupied())
		tm.IsEndgame = tm.PieceCount <= 14
	}

	if limits.MoveTime > 0 {
		dur := time.Duration(limits.MoveTime) * time.Millisecond
		tm.OptTime = dur
		tm.MaxTime = dur
		tm.IsMoveTime = true
		return tm
	}

	if limits.Infinite {
		return tm
	}

	var myTime, myInc, oppTime, oppInc int64
	if side == types.White {
		myTime, myInc = limits.WhiteTime, limits.WhiteInc
		oppTime, oppInc = limits.BlackTime, limits.BlackInc
	} else {
		myTime, myInc = limits.BlackTime, limits.BlackInc
		oppTime, oppInc = limits.WhiteTime, limits.WhiteInc
	}
	tm.ClockLead = 1
	if myTime > 0 && oppTime > 0 {
		tm.ClockLead = float64(myTime+leadHorizon*myInc) / float64(oppTime+leadHorizon*oppInc)
	}

	moveOverhead := limits.MoveOverhead
	if moveOverhead <= 0 {
		moveOverhead = 20
	}
	tm.MoveOverhead = moveOverhead
	tm.RemainingTime = myTime
	tm.Increment = myInc

	if myTime <= 0 {
		// No time specified; default to infinite or depth limit
		return tm
	}

	// Calculate usable time after subtracting safety move overhead (prevents lag loss)
	usableTime := myTime - moveOverhead
	if usableTime < 5 {
		usableTime = 5
	}

	// Realistic time trouble thresholds for bullet & blitz games
	if myInc == 0 {
		tm.IsTimeTrouble = myTime < 12000
		tm.IsEmergency = myTime < 2500
	} else {
		tm.IsTimeTrouble = myTime < 3000+myInc*2
		tm.IsEmergency = myTime < 1000+myInc
	}

	// Dynamic divisor & maxMultiplier allocation:
	// Allocates appropriate move budgets for Bullet (<60s), Blitz (<180s), and Rapid
	var divisor int64 = 40
	var maxMultiplier float64 = 2.0

	var optTimeMs, maxTimeMs int64
	if limits.MovesToGo > 0 {
		moves := int64(limits.MovesToGo)
		if moves > 50 {
			moves = 50
		}
		if tm.IsTimeTrouble && moves < 10 {
			moves = 10
		}
		optTimeMs = (usableTime / (moves + 3)) + (myInc * 3 / 4)
		if tm.IsEmergency {
			maxMultiplier = 1.15
		} else if tm.IsTimeTrouble {
			maxMultiplier = 1.3
		} else {
			maxMultiplier = 1.6
		}
	} else {
		if myTime < 2000 {
			divisor = 80
			maxMultiplier = 1.15
		} else if myTime < 5000 {
			divisor = 65
			maxMultiplier = 1.25
		} else if myTime < 15000 {
			divisor = 50
			maxMultiplier = 1.5
		} else if myTime < 30000 {
			divisor = 46
			maxMultiplier = 1.6
		} else if myTime <= 60000 { // Bullet (<= 1 min)
			divisor = 42
			maxMultiplier = 1.8
		} else if myTime <= 180000 { // Blitz (<= 3 min)
			divisor = 38
			maxMultiplier = 2.0
		} else if myTime < longClockMs { // Rapid
			divisor = 34
			maxMultiplier = 2.2
		} else { // Long clock: plenty of time to think
			divisor = 30
			maxMultiplier = 2.5
		}

		// Dynamic Endgame Time Scaling:
		// When fewer pieces remain on the board, fewer moves remain in the game,
		// and tactical precision in king/pawn endgames is critical.
		// Allocate more time per move by reducing the divisor.
		if tm.IsEndgame && !tm.IsEmergency {
			if tm.PieceCount <= 6 {
				// Deep endgame / pawn race / king opposition: ~40% more time
				divisor = divisor * 70 / 100
				if divisor < 22 {
					divisor = 22
				}
				maxMultiplier += 0.25
			} else if tm.PieceCount <= 10 {
				// Minor piece / rook endgame: ~25% more time
				divisor = divisor * 80 / 100
				if divisor < 26 {
					divisor = 26
				}
				maxMultiplier += 0.15
			} else {
				// Early endgame / transition: ~10% more time
				divisor = divisor * 90 / 100
				if divisor < 30 {
					divisor = 30
				}
				maxMultiplier += 0.1
			}
		}

		optTimeMs = (usableTime / divisor) + (myInc * 3 / 4)
	}

	maxTimeMs = int64(float64(optTimeMs) * maxMultiplier)

	// Ahead on the clock: invest the lead in deeper search. Not in time
	// trouble, where every millisecond still counts.
	if s := clockLeadScale(tm.ClockLead); s > 1 && !tm.IsTimeTrouble {
		optTimeMs = int64(float64(optTimeMs) * s)
		maxTimeMs = int64(float64(maxTimeMs) * s)
	}

	// Hard safety caps: the engine must never spend an excessive fraction of its clock on a single move
	maxFraction := 0.15
	if tm.IsEndgame && !tm.IsTimeTrouble {
		maxFraction = 0.18
	}
	if tm.ClockLead >= 1.5 && !tm.IsTimeTrouble {
		maxFraction += 0.03
	}
	if tm.IsTimeTrouble {
		maxFraction = 0.10
	}
	if tm.IsEmergency {
		maxFraction = 0.08
	}
	hardCap := int64(float64(usableTime) * maxFraction)
	if hardCap < 10 {
		hardCap = 10
	}
	if maxTimeMs > hardCap {
		maxTimeMs = hardCap
	}
	if optTimeMs > maxTimeMs {
		optTimeMs = maxTimeMs
	}
	if optTimeMs < 5 {
		optTimeMs = 5
	}

	tm.OptTime = time.Duration(optTimeMs) * time.Millisecond
	tm.MaxTime = time.Duration(maxTimeMs) * time.Millisecond
	tm.BaseOptTime = tm.OptTime
	// In-search adjustments may grow the budget further when there is time
	// to spare: a clock lead or a long clock.
	tm.MinExtend, tm.MaxExtend = 0.35, 2.0
	if (tm.ClockLead >= 1.5 || myTime >= longClockMs) && !tm.IsTimeTrouble {
		tm.MaxExtend = 2.5
	}

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

	if tm.MaxTime > 0 {
		elapsed := time.Since(tm.StartTime)
		limit := tm.MaxTime
		if !tm.IsMoveTime && tm.OptTime > 0 {
			limit = min(limit, time.Duration(float64(tm.OptTime)*softStopFactor))
		}
		if elapsed >= limit {
			tm.Stopped.Store(true)
			return true
		}
	}

	return false
}

func (tm *TimeManager) CanStartNewIteration(lastIterDuration time.Duration, isStable bool) bool {
	if tm.Stopped.Load() {
		return false
	}
	if tm.Ponder.Load() || tm.Infinite {
		return true
	}
	if tm.OptTime <= 0 {
		return true
	}

	elapsed := time.Since(tm.StartTime)
	if elapsed >= tm.OptTime {
		return false
	}

	if tm.IsMoveTime {
		// In explicit movetime mode (tactical test suites, analysis),
		// make full use of the requested search time.
		if lastIterDuration > 0 && elapsed+lastIterDuration/2 > tm.MaxTime {
			return false
		}
		return elapsed < tm.OptTime-10*time.Millisecond
	}

	// In fast time controls or time trouble, if we have already spent >= 60-75% of OptTime,
	// starting another iteration will almost certainly blow past OptTime due to branching factor.
	threshold := 0.75
	if tm.IsEmergency {
		threshold = 0.50
	} else if tm.IsTimeTrouble {
		threshold = 0.65
	} else if tm.IsEndgame {
		// In endgames, branching factor is lower (~1.4-1.6x), so it is safe to start up to 80% of OptTime
		threshold = 0.80
	} else if tm.RemainingTime > 0 && tm.RemainingTime < 60000 {
		threshold = 0.68
	} else if tm.RemainingTime > 0 && tm.RemainingTime < 180000 {
		threshold = 0.72
	}
	if elapsed >= time.Duration(float64(tm.OptTime)*threshold) && isStable {
		return false
	}

	// Project next iteration time:
	// Branching factor in endgames is significantly smaller (1.4-1.6x) than middlegame (2.0-2.5x)
	branchingFactor := 2.0
	if tm.PieceCount > 0 && tm.PieceCount <= 8 {
		branchingFactor = 1.45
	} else if tm.PieceCount > 0 && tm.PieceCount <= 14 {
		branchingFactor = 1.65
	}

	if lastIterDuration > 0 {
		projected := elapsed + time.Duration(float64(lastIterDuration)*branchingFactor)
		// If the move is already stable, don't overshoot OptTime
		if isStable && projected > tm.OptTime {
			return false
		}
		if tm.IsTimeTrouble && projected > tm.OptTime {
			return false
		}
		// Unsettled moves may run past OptTime, but only to halfway to MaxTime.
		if projected > tm.OptTime+(tm.MaxTime-tm.OptTime)/2 {
			return false
		}
	}
	return true
}

// AdjustOptTime dynamically scales the optimal search time based on move stability and score drops.
func (tm *TimeManager) AdjustOptTime(factor float64) {
	if tm.OptTime <= 0 || tm.Infinite || tm.IsMoveTime {
		return
	}
	newOptMs := int64(float64(tm.OptTime.Milliseconds()) * factor)
	// Keep the cumulative effect of all adjustments within bounds of the
	// original allocation.
	if base := tm.BaseOptTime.Milliseconds(); base > 0 && tm.MaxExtend > 0 {
		newOptMs = min(max(newOptMs, int64(float64(base)*tm.MinExtend)), int64(float64(base)*tm.MaxExtend))
	}
	if newOptMs < 10 {
		newOptMs = 10
	}
	maxMs := tm.MaxTime.Milliseconds()
	if maxMs > 0 && newOptMs > maxMs {
		newOptMs = maxMs
	}
	tm.OptTime = time.Duration(newOptMs) * time.Millisecond
}
