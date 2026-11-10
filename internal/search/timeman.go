package search

import (
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

	var myTime, myInc int64
	if side == types.White {
		myTime = limits.WhiteTime
		myInc = limits.WhiteInc
	} else {
		myTime = limits.BlackTime
		myInc = limits.BlackInc
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
		optTimeMs = (usableTime / (moves + 2)) + (myInc * 3 / 4)
		if tm.IsEmergency {
			maxMultiplier = 1.15
		} else if tm.IsTimeTrouble {
			maxMultiplier = 1.3
		} else {
			maxMultiplier = 1.8
		}
	} else {
		if myTime < 2000 {
			divisor = 80
			maxMultiplier = 1.15
		} else if myTime < 5000 {
			divisor = 65
			maxMultiplier = 1.25
		} else if myTime < 15000 {
			divisor = 55
			maxMultiplier = 1.4
		} else if myTime < 30000 {
			divisor = 48
			maxMultiplier = 1.6
		} else if myTime <= 60000 { // Bullet (<= 1 min)
			divisor = 44
			maxMultiplier = 1.8
		} else if myTime <= 180000 { // Blitz (<= 3 min)
			divisor = 40
			maxMultiplier = 2.0
		} else { // Rapid / Classical
			divisor = 36
			maxMultiplier = 2.2
		}

		// Dynamic Endgame Time Scaling:
		// When fewer pieces remain on the board, fewer moves remain in the game,
		// and tactical precision in king/pawn endgames is critical.
		// Allocate more time per move by reducing the divisor.
		if tm.IsEndgame && !tm.IsEmergency {
			if tm.PieceCount <= 6 {
				// Deep endgame / pawn race / king opposition: ~50% more time
				divisor = divisor * 55 / 100
				if divisor < 16 {
					divisor = 16
				}
				maxMultiplier += 0.4
			} else if tm.PieceCount <= 10 {
				// Minor piece / rook endgame: ~35% more time
				divisor = divisor * 68 / 100
				if divisor < 18 {
					divisor = 18
				}
				maxMultiplier += 0.3
			} else {
				// Early endgame / transition: ~20% more time
				divisor = divisor * 82 / 100
				if divisor < 22 {
					divisor = 22
				}
				maxMultiplier += 0.2
			}
		}

		optTimeMs = (usableTime / divisor) + (myInc * 3 / 4)
	}

	maxTimeMs = int64(float64(optTimeMs) * maxMultiplier)

	// Hard safety caps: the engine must never spend an excessive fraction of its clock on a single move
	maxFraction := 0.20
	if tm.IsEndgame && !tm.IsTimeTrouble {
		maxFraction = 0.25
	}
	if tm.IsTimeTrouble {
		maxFraction = 0.12
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
		if projected > tm.MaxTime {
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
	if newOptMs < 10 {
		newOptMs = 10
	}
	maxMs := tm.MaxTime.Milliseconds()
	if maxMs > 0 && newOptMs > maxMs {
		newOptMs = maxMs
	}
	tm.OptTime = time.Duration(newOptMs) * time.Millisecond
}
