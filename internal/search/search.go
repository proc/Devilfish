package search

import (
	"sync/atomic"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/eval"
	"devilfish/internal/movegen"
	"devilfish/internal/nnue"
	"devilfish/internal/syzygy"
	"devilfish/internal/tt"
	"devilfish/internal/types"
)

const (
	Infinity      int32 = 32000
	MateScore     int32 = 30000
	MateThreshold int32 = 29000
)

// evalCacheSize is the number of entries in each worker's static-eval cache
// (16 bytes each). Must be a power of two.
const evalCacheSize = 1 << 17

// LMRTable is built from P.LMRBase / P.LMRDivisor by rebuildLMRTable (params.go).
var LMRTable [128][128]int8

type SearchWorker struct {
	ID                int
	Board             *board.Board
	TT                *tt.TT
	TimeMan           *TimeManager
	History           *SearchHistory
	Nodes             *atomic.Uint64
	TBHits            *atomic.Uint64
	LocalNodes        uint64
	NnueEvals         uint64
	NnueUpdates       uint64
	NnueRefreshes     uint64
	ClassicalEvals    uint64
	SelDepth          int8
	PVArray           [MaxPly][MaxPly]types.Move
	PVLength          [MaxPly]int
	StaticEvalHistory [MaxPly]int32
	Accumulators      [MaxPly]nnue.Accumulator
	EvalCache         [evalCacheSize]struct {
		hash uint64
		eval int32
	}
	Eight             *nnue.EightState   // incremental 8Chess accumulators (nil unless that net is active)
	useAcc            bool               // the 768->256 accumulator stack is in use this search
	cacheNet          *nnue.EightNetwork // network whose evaluations EvalCache holds
	RootBestMove      types.Move
	RootScore         int32
	RootMoveNodes     [64][64]uint64
	ExcludedRootMoves []types.Move
}

func (w *SearchWorker) isRootExcluded(m types.Move) bool {
	for _, em := range w.ExcludedRootMoves {
		if em == m {
			return true
		}
	}
	return false
}

// Evaluate returns the static evaluation of the current position: the
// network (or classical) evaluation with the search's adjustments applied.
func (w *SearchWorker) Evaluate(ply int) int32 {
	return w.adjustEval(w.rawEval(ply))
}

// rawEval is the network (or classical) evaluation before adjustment. It
// depends only on the position and the evaluation settings, so it is also
// kept in the transposition table and reused from there.
func (w *SearchWorker) rawEval(ply int) int32 {
	var staticEval int32
	if nnue.Enabled {
		if nnue.ActiveEightNetwork != nil {
			w.NnueEvals++
			h := w.Board.Hash
			idx := h & (evalCacheSize - 1)
			if w.EvalCache[idx].hash == h {
				staticEval = w.EvalCache[idx].eval
			} else if w.Eight != nil {
				staticEval = w.evaluateEight(ply)
				w.EvalCache[idx].hash = h
				w.EvalCache[idx].eval = staticEval
			} else {
				staticEval = nnue.EvaluateEight(nnue.ActiveEightNetwork, w.Board)
				w.EvalCache[idx].hash = h
				w.EvalCache[idx].eval = staticEval
			}
		} else if nnue.ActiveNetwork != nil && ply < MaxPly {
			w.NnueEvals++
			staticEval = nnue.Evaluate(&w.Accumulators[ply], w.Board.SideToMove)
		} else {
			w.ClassicalEvals++
			staticEval = eval.EvaluateClassical(w.Board)
		}
	} else {
		w.ClassicalEvals++
		staticEval = eval.EvaluateClassical(w.Board)
	}
	return staticEval
}

// adjustEval applies the correction history and the fifty-move fade to a
// raw evaluation of the current position.
func (w *SearchWorker) adjustEval(raw int32) int32 {
	v := raw + w.History.GetCorrection(w.Board)
	// Fade the eval toward a draw as the fifty-move counter runs up
	return v * (200 - int32(w.Board.HalfmoveClock)) / 200
}

func NewWorker(id int, b *board.Board, ttTable *tt.TT, tm *TimeManager, nodes *atomic.Uint64, tbHits *atomic.Uint64) *SearchWorker {
	w := &SearchWorker{
		ID:      id,
		Board:   b.Clone(),
		TT:      ttTable,
		TimeMan: tm,
		History: NewSearchHistory(),
		Nodes:   nodes,
		TBHits:  tbHits,
	}
	w.setupEval()
	return w
}

// eightNet returns the 8Chess network evaluations will use, or nil.
func eightNet() *nnue.EightNetwork {
	if nnue.Enabled {
		return nnue.ActiveEightNetwork
	}
	return nil
}

// setupEval prepares the evaluation state for a search of w.Board.
func (w *SearchWorker) setupEval() {
	net := eightNet()
	if net != w.cacheNet {
		// The cached values came from another network (or none).
		clear(w.EvalCache[:])
		w.cacheNet = net
	}
	if net == nil {
		w.Eight = nil
	} else if w.Eight == nil {
		w.Eight = nnue.NewEightState(MaxPly)
	} else {
		w.Eight.Reset()
	}
	w.useAcc = nnue.UsesAccumulator()
	if w.useAcc {
		w.NnueRefreshes++
		nnue.Refresh(w.Board, &w.Accumulators[0])
	}
}

// resetFor readies a worker used by an earlier search for a new search of b.
// Afterwards it is in the same state as NewWorker would build, except that
// it keeps its allocations, and the static evaluations in EvalCache while the
// network is unchanged (they depend on nothing else).
func (w *SearchWorker) resetFor(b *board.Board, ttTable *tt.TT, tm *TimeManager, nodes *atomic.Uint64, tbHits *atomic.Uint64) {
	w.Board = b.Clone()
	w.TT = ttTable
	w.TimeMan = tm
	w.Nodes = nodes
	w.TBHits = tbHits
	w.History.Clear()
	w.LocalNodes = 0
	w.NnueEvals = 0
	w.NnueUpdates = 0
	w.NnueRefreshes = 0
	w.ClassicalEvals = 0
	w.SelDepth = 0
	clear(w.PVArray[:])
	clear(w.PVLength[:])
	clear(w.StaticEvalHistory[:])
	clear(w.Accumulators[:])
	w.RootBestMove = types.MoveNone
	w.RootScore = 0
	clear(w.RootMoveNodes[:])
	w.ExcludedRootMoves = nil
	w.setupEval()
}

// parentHash returns the hash of the position one move up the search path.
func (w *SearchWorker) parentHash(ply int) (uint64, bool) {
	if n := len(w.Board.UndoStack); ply > 0 && n > 0 {
		return w.Board.UndoStack[n-1].Hash, true
	}
	return 0, false
}

// evaluateEight runs the 8Chess network on the current position using the
// incremental accumulator stack.
//
// The cheapest way to build this position's accumulator is from its parent's,
// one move away. The parent's is normally there, left behind by the parent's
// own evaluation, but not if the parent was in check (never evaluated) or its
// eval came from the cache. In that case the last move is taken back for a
// moment so the parent's accumulator can be built first: that costs about the
// same for this child as building it from a less related position would, and
// every later sibling then gets the one-move path too.
func (w *SearchWorker) evaluateEight(ply int) int32 {
	net := nnue.ActiveEightNetwork
	parentHash, hasParent := w.parentHash(ply)
	if hasParent && !w.Eight.Has(ply-1, parentHash) && !w.Eight.Has(ply, w.Board.Hash) {
		b := w.Board
		last := b.UndoStack[len(b.UndoStack)-1].Move
		var grandHash uint64
		hasGrand := false
		if n := len(b.UndoStack); ply > 1 && n > 1 {
			grandHash, hasGrand = b.UndoStack[n-2].Hash, true
		}
		if last == types.MoveNone {
			b.UnmakeNullMove()
			w.Eight.Prepare(net, b, ply-1, grandHash, hasGrand)
			b.MakeNullMove()
		} else {
			b.UnmakeMove(last)
			w.Eight.Prepare(net, b, ply-1, grandHash, hasGrand)
			b.MakeMove(last)
		}
	}
	return w.Eight.Evaluate(net, w.Board, ply, parentHash, hasParent)
}

// pushAccumulator prepares the 768->256 accumulator for the position after m.
// It does nothing when that network is not the one being evaluated.
func (w *SearchWorker) pushAccumulator(ply int, m types.Move) {
	if w.useAcc && ply+1 < MaxPly {
		w.NnueUpdates++
		nnue.Update(&w.Accumulators[ply], &w.Accumulators[ply+1], m, w.Board)
	}
}

func (w *SearchWorker) Search(depth int8, alpha, beta int32, ply int, isPVNode bool, canMultiCut bool, excludedMove types.Move, prevMove types.Move, prevMove2 types.Move, prevMove4 types.Move) int32 {
	if w.TimeMan.Stopped.Load() {
		return 0
	}

	if ply == 0 && w.useAcc {
		w.NnueRefreshes++
		nnue.Refresh(w.Board, &w.Accumulators[0])
	}

	// Periodically check search time/limits
	w.LocalNodes++
	if w.LocalNodes%1024 == 0 {
		w.Nodes.Add(1024)
		if w.TimeMan.CheckLimits() {
			return 0
		}
	}

	if int8(ply) > w.SelDepth {
		w.SelDepth = int8(ply)
	}

	w.PVLength[ply] = 0

	// 1. Draw detection
	if ply > 0 {
		if w.Board.IsRepetition() || w.Board.IsFiftyMoveDraw() || w.Board.IsInsufficientMaterial() {
			return 0
		}
	}

	// 2. Mate distance pruning
	if alpha < -MateScore+int32(ply) {
		alpha = -MateScore + int32(ply)
	}
	if beta > MateScore-int32(ply) {
		beta = MateScore - int32(ply)
	}
	if alpha >= beta {
		return alpha
	}

	inCheck := w.Board.IsInCheck(w.Board.SideToMove)

	// Leaf / Quiescence search
	if depth <= 0 {
		return w.Quiescence(alpha, beta, ply)
	}

	if ply >= MaxPly-1 {
		return w.Evaluate(ply)
	}

	// 3. Transposition Table Probe
	ttMove := types.MoveNone
	ttEntryMove, ttScore, ttEval, ttDepth, ttType, ttFound := w.TT.ProbeEval(w.Board.Hash)
	netEval := tt.EvalNone // raw evaluation of this node, once known
	if ttFound {
		ttMove = ttEntryMove
		ttScore = tt.ScoreFromTT(ttScore, ply)

		// Don't take TT cutoffs if searching with an excluded move
		if excludedMove == types.MoveNone && !isPVNode && ttDepth >= depth {
			switch ttType {
			case tt.Exact:
				return ttScore
			case tt.LowerBound:
				if ttScore >= beta {
					return ttScore
				}
			case tt.UpperBound:
				if ttScore <= alpha {
					return ttScore
				}
			}
		}
	}

	// 3.5. Syzygy Tablebase WDL probe
	if ply > 0 && depth >= int8(syzygy.ProbeDepth) &&
		syzygy.IsAvailable() &&
		w.Board.Castling == 0 &&
		bitboard.Count(w.Board.Occupied()) <= syzygy.MaxPieces() {

		wdl, ok := syzygy.ProbeWDL(w.Board)
		if ok {
			if w.TBHits != nil {
				w.TBHits.Add(1)
			}

			var tbScore int32
			var boundType uint8

			switch wdl {
			case syzygy.Win:
				tbScore = MateScore - 100 - int32(ply)
				boundType = tt.LowerBound
			case syzygy.CursedWin:
				if syzygy.Use50MoveRule {
					tbScore = 0
					boundType = tt.Exact
				} else {
					tbScore = MateScore - 100 - int32(ply)
					boundType = tt.LowerBound
				}
			case syzygy.Draw:
				tbScore = 0
				boundType = tt.Exact
			case syzygy.BlessedLoss:
				if syzygy.Use50MoveRule {
					tbScore = 0
					boundType = tt.Exact
				} else {
					tbScore = -MateScore + 100 + int32(ply)
					boundType = tt.UpperBound
				}
			case syzygy.Loss:
				tbScore = -MateScore + 100 + int32(ply)
				boundType = tt.UpperBound
			}

			w.TT.Store(w.Board.Hash, types.MoveNone, tt.ScoreToTT(tbScore, ply), depth, boundType)

			if boundType == tt.Exact {
				return tbScore
			}
			if boundType == tt.LowerBound && tbScore >= beta {
				return tbScore
			}
			if boundType == tt.UpperBound && tbScore <= alpha {
				return tbScore
			}
		}
	}

	var staticEval, rawEval int32
	if inCheck {
		staticEval = -Infinity
		rawEval = -Infinity
	} else {
		if ttFound && ttEval != tt.EvalNone {
			netEval = ttEval
		} else {
			netEval = w.rawEval(ply)
		}
		staticEval = w.adjustEval(netEval)
		rawEval = staticEval
		if ttFound && abs(ttScore) < MateThreshold {
			if ttType == tt.Exact || (ttType == tt.LowerBound && ttScore > staticEval) || (ttType == tt.UpperBound && ttScore < staticEval) {
				staticEval = ttScore
			}
		}
	}
	if ply < MaxPly {
		w.StaticEvalHistory[ply] = staticEval
	}

	// Improving flag: static evaluation is higher than 2 plies ago (same player's last turn)
	improving := false
	if !inCheck && ply >= 2 && ply < MaxPly && w.StaticEvalHistory[ply-2] != -Infinity {
		improving = staticEval > w.StaticEvalHistory[ply-2]
	}

	us := w.Board.SideToMove
	hasNonPawns := (w.Board.PieceBB[types.MakePiece(us, types.Knight)] |
		w.Board.PieceBB[types.MakePiece(us, types.Bishop)] |
		w.Board.PieceBB[types.MakePiece(us, types.Rook)] |
		w.Board.PieceBB[types.MakePiece(us, types.Queen)]) != 0

	kAttackers, _ := w.Board.KingAttackersCount(us)
	kingInDanger := kAttackers >= 2
	prevIsKingThreat := IsKingThreatAfterMove(w.Board, prevMove)

	them := us.Other()
	themKingSq := w.Board.KingSquare(them)
	themKingRing := bitboard.Bitboard(0)
	if themKingSq < types.SquareNone {
		themKingRing = bitboard.KingAttacks(themKingSq) | bitboard.SquareBB[themKingSq]
		if them == types.White {
			themKingRing |= (themKingRing << 8) | (themKingRing << 16)
		} else {
			themKingRing |= (themKingRing >> 8) | (themKingRing >> 16)
		}
	}
	myAttackers, myAttackUnits := w.Board.KingAttackersCount(them)
	hasActiveAttack := myAttackers >= 2 || myAttackUnits >= 4

	threatenedPieces := w.Board.TacticalThreatsOnPieces(us)

	ourQueens := w.Board.PieceBB[types.MakePiece(us, types.Queen)]
	ourRooks := w.Board.PieceBB[types.MakePiece(us, types.Rook)]
	ourMinors := w.Board.PieceBB[types.MakePiece(us, types.Knight)] | w.Board.PieceBB[types.MakePiece(us, types.Bishop)]

	// 4. Reverse Futility Pruning (Static Null Move Pruning)
	// Never RFP if high-value pieces (Queen, Rook, Minor) are under tactical threat,
	// because opponent capture would immediately wipe out the static advantage.
	if ply > 0 && !isPVNode && !inCheck && excludedMove == types.MoveNone && !kingInDanger && !prevIsKingThreat && (threatenedPieces&(ourQueens|ourRooks|ourMinors)) == 0 && int32(depth) <= P.RFPDepth && abs(beta) < MateThreshold && hasNonPawns {
		margin := int32(depth) * P.RFPMargin
		if !improving {
			margin += int32(depth) * P.RFPNonImpr
		}
		if threatenedPieces != 0 {
			margin += 120 // Safety margin if pawns are contested
		}
		if staticEval-margin >= beta {
			return beta
		}
	}

	// 5. Razoring
	if ply > 0 && !isPVNode && !inCheck && excludedMove == types.MoveNone && !kingInDanger && !prevIsKingThreat && depth <= 1 {
		razorMargin := P.RazorMargin
		if staticEval+razorMargin <= alpha {
			qScore := w.Quiescence(alpha, beta, ply)
			if qScore <= alpha {
				return qScore
			}
		}
	}

	// 6. Null Move Pruning (NMP)
	// Never pass if our Queen is under direct tactical threat, and never twice in a row
	// (prevMove is MoveNone below the root only directly after a null move).
	pieceCount := bitboard.Count(w.Board.Occupied())
	if ply > 0 && !isPVNode && !inCheck && excludedMove == types.MoveNone && prevMove != types.MoveNone && !kingInDanger && !prevIsKingThreat && (threatenedPieces&ourQueens) == 0 && depth >= 3 && staticEval >= beta && hasNonPawns && pieceCount > 6 {
		r := int8(3 + depth/4)
		if !improving {
			r++
		}
		// Reduce more the further static eval sits above beta
		if evalR := (staticEval - beta) / P.NMPEvalDiv; evalR > 0 {
			if evalR > 3 {
				evalR = 3
			}
			r += int8(evalR)
		}
		if w.useAcc && ply+1 < MaxPly {
			w.Accumulators[ply+1] = w.Accumulators[ply]
		}
		w.Board.MakeNullMove()
		nullScore := -w.Search(depth-1-r, -beta, -beta+1, ply+1, false, false, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
		w.Board.UnmakeNullMove()

		if w.TimeMan.Stopped.Load() {
			return 0
		}
		if nullScore >= beta {
			return beta
		}
	}

	// Internal Iterative Reduction (IIR)
	// If at non-PV node at depth >= 6 with no TT move, reduce search depth by 1 ply
	if !isPVNode && !inCheck && excludedMove == types.MoveNone && depth >= 6 && ttMove == types.MoveNone {
		depth--
	}

	extension := int8(0)

	// Singular Extension (SE)
	// If the TT move is significantly better than all other alternative moves,
	// extend the search depth for the TT move.
	if ply > 0 && excludedMove == types.MoveNone && depth >= 6 && ttMove != types.MoveNone &&
		!inCheck && ttFound && ttDepth >= depth-3 && ttType != tt.UpperBound &&
		ttScore > -MateThreshold && ttScore < MateThreshold {

		singularMargin := int32(depth) * 2
		singularBeta := ttScore - singularMargin
		sDepth := (depth - 1) / 2
		if sDepth < 1 {
			sDepth = 1
		}

		sScore := w.Search(sDepth, singularBeta-1, singularBeta, ply, false, false, ttMove, prevMove, prevMove2, prevMove4)

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		if sScore >= singularBeta && singularBeta >= beta {
			// Multi-cut: another move also beats beta, even searched shallower
			// and with the TT move excluded, so this node fails high.
			return beta
		}
		if sScore < singularBeta {
			extension = 1
			if !isPVNode && sScore < singularBeta-25 {
				extension = 2
			}
		}
	}

	var bestMove types.Move
	var bestRootMove types.Move
	bestRootScore := -Infinity
	var searchedMoves [256]types.Move
	movesSearched := 0
	// A capture/promotion TT move suggests the position is tactical: quiet
	// alternatives are less likely to matter, so they get reduced more.
	ttMoveNoisy := ttMove != types.MoveNone && (ttMove.IsCapture() || ttMove.IsPromotion())
	legalMoves := 0
	bestType := tt.UpperBound

	// Staged Move Generation: Search TT move first before generating all moves
	if ttMove != types.MoveNone && ttMove != excludedMove && (ply > 0 || !w.isRootExcluded(ttMove)) && movegen.IsPseudoLegal(w.Board, ttMove) {
		// Check extension (parent side): only for checks that don't lose
		// material, and never stacked on top of a singular extension.
		if extension == 0 && w.Board.GivesCheck(ttMove) && eval.SEE(w.Board, ttMove) >= 0 {
			extension = 1
		}
		w.pushAccumulator(ply, ttMove)
		if w.Board.MakeMove(ttMove) {
			w.TT.Prefetch(w.Board.Hash)
			legalMoves++
			searchedMoves[movesSearched] = ttMove

			var startNodes uint64
			if ply == 0 {
				startNodes = w.LocalNodes
			}

			score := -w.Search(depth-1+extension, -beta, -alpha, ply+1, isPVNode, true, types.MoveNone, ttMove, prevMove, prevMove2)
			w.Board.UnmakeMove(ttMove)
			movesSearched++

			if ply == 0 {
				w.RootMoveNodes[ttMove.From()][ttMove.To()] += w.LocalNodes - startNodes
				bestRootMove = ttMove
				bestRootScore = score
			}

			if w.TimeMan.Stopped.Load() {
				return 0
			}

			// Beta Cutoff (Fail-High)
			if score >= beta {
				if excludedMove == types.MoveNone && (ply > 0 || len(w.ExcludedRootMoves) == 0) {
					w.TT.StoreEval(w.Board.Hash, ttMove, tt.ScoreToTT(score, ply), netEval, depth, tt.LowerBound)
				}

				if !inCheck && excludedMove == types.MoveNone && score > rawEval && score < MateThreshold {
					diff := score - rawEval
					w.History.UpdateCorrHist(w.Board, diff, depth)
				}

				if ply == 0 {
					bestMove = ttMove
					w.PVArray[0][0] = ttMove
					w.PVLength[0] = 1
				}

				if !ttMove.IsCapture() && !ttMove.IsPromotion() {
					w.History.UpdateKillers(ply, ttMove)
					w.History.UpdateHistory(us, ttMove, depth)
					w.History.UpdateContHistory(prevMove, ttMove, depth)
					w.History.UpdateContHistory2(prevMove2, ttMove, depth)
					w.History.UpdateContHistory4(prevMove4, ttMove, depth)
					w.History.UpdateCounterMove(prevMove, ttMove)
				} else if ttMove.IsCapture() {
					attacker := w.Board.Pieces[ttMove.From()].Type()
					victim := w.Board.Pieces[ttMove.To()].Type()
					if ttMove.IsEnPassant() {
						victim = types.Pawn
					}
					w.History.UpdateCaptureHistory(attacker, victim, ttMove.To(), depth)
				}
				return beta
			}

			// Alpha improvement
			if score > alpha {
				alpha = score
				bestMove = ttMove
				bestType = tt.Exact

				w.PVArray[ply][0] = ttMove
				copy(w.PVArray[ply][1:1+w.PVLength[ply+1]], w.PVArray[ply+1][:w.PVLength[ply+1]])
				w.PVLength[ply] = 1 + w.PVLength[ply+1]
			}
		}
	}

	// 7. ProbCut (Probability Cut)
	// If a shallow capture search with elevated beta fails high, the node is almost certainly a CUT node.
	if ply > 0 && canMultiCut && !isPVNode && !inCheck && depth >= 5 && abs(beta) < MateThreshold && staticEval >= beta+150 {
		probBeta := beta + 200
		probDepth := depth - 3
		if probDepth < 1 {
			probDepth = 1
		}

		var probList movegen.MoveList
		movegen.GenerateMoves(w.Board, true, &probList)

		var probScores [256]int32
		ScoreMoves(w.Board, &probList, probScores[:probList.Count], ttMove, ply, prevMove, prevMove2, prevMove4, w.History)

		for i := 0; i < probList.Count; i++ {
			m := PickNextMove(&probList, probScores[:probList.Count], i)
			if m == ttMove || m == excludedMove {
				continue
			}
			// Skip captures that can't plausibly reach probBeta
			if eval.SEE(w.Board, m) < probBeta-staticEval {
				continue
			}
			w.pushAccumulator(ply, m)
			if !w.Board.MakeMove(m) {
				continue
			}

			probScore := -w.Search(probDepth, -probBeta, -probBeta+1, ply+1, false, false, types.MoveNone, m, prevMove, prevMove2)
			w.Board.UnmakeMove(m)

			if w.TimeMan.Stopped.Load() {
				return 0
			}

			if probScore >= probBeta {
				w.TT.StoreEval(w.Board.Hash, m, tt.ScoreToTT(beta, ply), netEval, probDepth, tt.LowerBound)
				return beta
			}
		}
	}

	// 8. Staged Move Generation & Search Loop
	mp := NewMovePicker(w.Board, ttMove, excludedMove, ply, prevMove, prevMove2, prevMove4, w.History)
	mp.stage = StageGenCaptures
	mp.SetNodeInfo(threatenedPieces, kingInDanger, hasActiveAttack, themKingRing)

	for {
		m := mp.Next()
		if m == types.MoveNone {
			break
		}
		if m == ttMove || m == excludedMove {
			continue
		}
		if ply == 0 && w.isRootExcluded(m) {
			continue
		}
		fromSq := m.From()
		toSq := m.To()
		isQuiet := !m.IsCapture() && !m.IsPromotion()
		killer1 := mp.killer1
		killer2 := mp.killer2
		counterMove := mp.counterMove
		attacker := w.Board.Pieces[fromSq].Type()
		victim := w.Board.Pieces[toSq].Type()
		if m.IsEnPassant() {
			victim = types.Pawn
		}
		// SEE: captures and promotions carry the value the picker already
		// computed; quiet moves get one only if a rule below asks for it.
		moveSEE, seeKnown := mp.LastSEE()
		// givesCheck is read by the pruning rules, LMR and the check
		// extension, so it is computed for every move.
		givesCheck := w.Board.GivesCheck(m)

		// The depth this move would be searched at after its late-move
		// reduction; the futility, history and quiet SEE rules judge it there.
		lmrDepth := int32(depth) - int32(LMRTable[min(int(depth), 127)][min(movesSearched+1, 127)])
		if lmrDepth < 0 {
			lmrDepth = 0
		}
		isAdvancedPawnPush := isQuiet && attacker == types.Pawn &&
			((us == types.White && toSq >= types.A6) || (us == types.Black && toSq <= types.H3))
		is7thRankPawnPush := attacker == types.Pawn &&
			((us == types.White && toSq.Rank() == types.Rank7) || (us == types.Black && toSq.Rank() == types.Rank2))

		isThreatEvasion := false
		if isQuiet && threatenedPieces != 0 && (bitboard.SquareBB[fromSq]&threatenedPieces) != 0 {
			if !seeKnown {
				moveSEE, seeKnown = eval.SEE(w.Board, m), true
			}
			isThreatEvasion = moveSEE >= 0
		}
		isThreatMove := isQuiet && threatenedPieces == 0 && IsTacticalThreat(w.Board, m)
		isKingThreat := threatenedPieces == 0 && IsKingAttack(w.Board, m)
		isKingZoneMove := (bitboard.SquareBB[toSq] & themKingRing) != 0

		// Late Move Pruning (LMP)
		if !isPVNode && !inCheck && int32(depth) <= P.LMPDepth && isQuiet && movesSearched > 0 &&
			m != killer1 && m != killer2 && m != counterMove && !isThreatMove && !isThreatEvasion && !isAdvancedPawnPush {
			if !givesCheck {
				lmpThreshold := 5 + int(P.LMPMultImpr)*int(depth)*int(depth)
				if !improving {
					lmpThreshold = 3 + int(P.LMPMultNonImp)*int(depth)*int(depth)
				}
				if legalMoves >= lmpThreshold {
					continue
				}
			}
		}

		// Futility Pruning
		if !isPVNode && !inCheck && lmrDepth <= P.FutDepth && isQuiet && movesSearched > 0 &&
			m != killer1 && m != killer2 && m != counterMove && !isThreatMove && !isThreatEvasion && !isAdvancedPawnPush {
			futMargin := P.FutMargin * max(lmrDepth, 1)
			if staticEval+futMargin <= alpha {
				if !givesCheck {
					continue
				}
			}
		}

		// History Pruning
		// Prune quiet moves with poor history score at shallow depths
		if !isPVNode && !inCheck && lmrDepth <= P.HistDepth && isQuiet && movesSearched > 0 &&
			m != killer1 && m != killer2 && m != counterMove && !isThreatMove && !isThreatEvasion && !isAdvancedPawnPush {
			if !givesCheck {
				histScore := w.History.HistoryTable[us][m.From()][m.To()]
				if prevMove != types.MoveNone && prevMove.To() < 64 && m.To() < 64 {
					histScore += w.History.ContHistory[prevMove.To()][m.To()]
				}
				if prevMove2 != types.MoveNone && prevMove2.To() < 64 && m.To() < 64 {
					histScore += w.History.ContHistory2[prevMove2.To()][m.To()]
				}
				histMargin := -max(lmrDepth, 1) * P.HistPrImpr
				if !improving {
					histMargin = -max(lmrDepth, 1) * P.HistPrNonImp
				}
				if histScore < histMargin {
					continue
				}
			}
		}

		// SEE Pruning
		// Do not prune tactical/killer moves, countermoves, advanced pawn pushes, or moves that give check
		if !isPVNode && !inCheck && int32(depth) <= P.SEEDepth && movesSearched > 0 && m != killer1 && m != killer2 && m != counterMove && !isAdvancedPawnPush && !isThreatMove && !isThreatEvasion {
			if !givesCheck {
				if !seeKnown {
					moveSEE, seeKnown = eval.SEE(w.Board, m), true
				}
				if isQuiet {
					if moveSEE < -max(lmrDepth, 1)*P.SEEQuietMul {
						continue
					}
				} else {
					if moveSEE < -int32(depth)*P.SEENoisyMul {
						continue
					}
				}
			}
		}

		w.pushAccumulator(ply, m)
		if !w.Board.MakeMove(m) {
			continue
		}
		w.TT.Prefetch(w.Board.Hash)

		legalMoves++
		searchedMoves[movesSearched] = m
		var score int32

		var startNodes uint64
		if ply == 0 {
			startNodes = w.LocalNodes
		}

		// The singular extension belongs to the TT move only; here 7th-rank
		// pushes and checks that don't lose material extend (at most one ply).
		var moveExtension int8
		if is7thRankPawnPush && depth >= 3 {
			moveExtension = 1
		} else if givesCheck {
			if !seeKnown {
				moveSEE, seeKnown = eval.SEE(w.Board, m), true
			}
			if moveSEE >= 0 {
				moveExtension = 1
			}
		}

		var reduction int8
		// Principal Variation Search & LMR
		if movesSearched == 0 {
			// First move: full window (extended if 7th rank pawn push)
			score = -w.Search(depth-1+moveExtension, -beta, -alpha, ply+1, isPVNode, true, types.MoveNone, m, prevMove, prevMove2)
		} else {
			// Late Move Reductions (LMR)
			if depth >= 3 && movesSearched >= 2 && !inCheck {
				dIdx := depth
				if dIdx > 127 {
					dIdx = 127
				}
				mIdx := movesSearched
				if mIdx >= 128 {
					mIdx = 127
				}
				reduction = LMRTable[dIdx][mIdx]

				if isQuiet {
					// Base reduction for quiet moves
					if !isPVNode {
						reduction++ // Non-PV nodes (cut nodes) reduce more
					}
					if !improving {
						reduction++ // Position getting worse: reduce more
					} else {
						reduction-- // Position improving: reduce less
					}

					// Killer & Countermove reductions
					if m == killer1 || m == killer2 {
						reduction -= 2
					}
					if m == counterMove {
						reduction--
					}

					// Advanced passed pawn push: don't reduce
					if isAdvancedPawnPush || is7thRankPawnPush {
						reduction = 0
					}

					// Threat evasion or tactical threat: don't reduce
					if isThreatEvasion || isThreatMove {
						reduction = 0
					} else if isKingThreat || (hasActiveAttack && isKingZoneMove) {
						reduction--
					}

					// History-based reduction adjustment
					hist := w.History.HistoryTable[us][fromSq][toSq]
					if prevMove != types.MoveNone && prevMove.To() < 64 && toSq < 64 {
						hist += w.History.ContHistory[prevMove.To()][toSq]
					}
					if prevMove2 != types.MoveNone && prevMove2.To() < 64 && toSq < 64 {
						hist += w.History.ContHistory2[prevMove2.To()][toSq]
					}
					if prevMove4 != types.MoveNone && prevMove4.To() < 64 && toSq < 64 {
						hist += w.History.ContHistory4[prevMove4.To()][toSq]
					}
					// Continuous history adjustment: one ply per LMRHistDiv
					// of combined history, capped at three plies each way.
					hAdj := hist / P.LMRHistDiv
					if hAdj > 3 {
						hAdj = 3
					} else if hAdj < -3 {
						hAdj = -3
					}
					reduction -= int8(hAdj)

					if ttMoveNoisy {
						reduction++
					}
				} else {
					// Tactical moves / Captures (Noisy LMR)
					if ply == 0 {
						reduction = 0
					} else if depth >= 4 && movesSearched >= 4 {
						reduction /= 2

						if !seeKnown {
							moveSEE, seeKnown = eval.SEE(w.Board, m), true
						}
						if moveSEE < 0 {
							reduction++
						} else if moveSEE > 100 {
							reduction--
						}

						capHist := w.History.CaptureHistory[attacker][toSq][victim]
						if capHist < 0 {
							reduction++
						} else if capHist > 2000 {
							reduction--
						}
					} else {
						reduction = 0
					}
				}

				// Check if move gives check: NEVER reduce checks
				if givesCheck {
					reduction = 0
				}

				if reduction < 0 {
					reduction = 0
				}
				if reduction > depth-2 {
					reduction = depth - 2
				}
			}

			// Null-window search with reduction
			score = -w.Search(depth-1+moveExtension-reduction, -alpha-1, -alpha, ply+1, false, false, types.MoveNone, m, prevMove, prevMove2)

			// If reduced search fails high, re-search at full depth with null window
			if reduction > 0 && score > alpha && !w.TimeMan.Stopped.Load() {
				score = -w.Search(depth-1+moveExtension, -alpha-1, -alpha, ply+1, false, true, types.MoveNone, m, prevMove, prevMove2)
			}

			// If null-window fails high and in PV node, re-search with full PV window
			if isPVNode && score > alpha && !w.TimeMan.Stopped.Load() {
				score = -w.Search(depth-1+moveExtension, -beta, -alpha, ply+1, true, true, types.MoveNone, m, prevMove, prevMove2)
			}
		}

		w.Board.UnmakeMove(m)
		movesSearched++

		if ply == 0 {
			w.RootMoveNodes[m.From()][m.To()] += w.LocalNodes - startNodes
			if bestRootMove == types.MoveNone || score > bestRootScore {
				bestRootScore = score
				bestRootMove = m
			}
		}

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		// Beta Cutoff (Fail-High)
		if score >= beta {
			if excludedMove == types.MoveNone && (ply > 0 || len(w.ExcludedRootMoves) == 0) {
				w.TT.StoreEval(w.Board.Hash, m, tt.ScoreToTT(score, ply), netEval, depth, tt.LowerBound)
			}

			if !inCheck && excludedMove == types.MoveNone && score > rawEval && score < MateThreshold {
				diff := score - rawEval
				w.History.UpdateCorrHist(w.Board, diff, depth)
			}

			if ply == 0 {
				bestMove = m
				w.PVArray[0][0] = m
				w.PVLength[0] = 1
			}

			if isQuiet {
				w.History.UpdateKillers(ply, m)
				w.History.UpdateHistory(us, m, depth)
				w.History.UpdateContHistory(prevMove, m, depth)
				w.History.UpdateContHistory2(prevMove2, m, depth)
				w.History.UpdateContHistory4(prevMove4, m, depth)
				w.History.UpdateCounterMove(prevMove, m)

				// Penalize preceding quiet moves that failed to produce a cutoff
				for j := 0; j < movesSearched-1; j++ {
					prevQuiet := searchedMoves[j]
					if !prevQuiet.IsCapture() && !prevQuiet.IsPromotion() {
						w.History.PenalizeHistory(us, prevQuiet, depth)
						w.History.PenalizeContHistory(prevMove, prevQuiet, depth)
						w.History.PenalizeContHistory2(prevMove2, prevQuiet, depth)
						w.History.PenalizeContHistory4(prevMove4, prevQuiet, depth)
					}
				}
			} else if m.IsCapture() {
				attacker := w.Board.Pieces[m.From()].Type()
				victim := w.Board.Pieces[m.To()].Type()
				if m.IsEnPassant() {
					victim = types.Pawn
				}
				w.History.UpdateCaptureHistory(attacker, victim, m.To(), depth)
			}

			// Penalize preceding captures that failed to produce a cutoff
			for j := 0; j < movesSearched-1; j++ {
				prevCap := searchedMoves[j]
				if prevCap.IsCapture() {
					att := w.Board.Pieces[prevCap.From()].Type()
					vic := w.Board.Pieces[prevCap.To()].Type()
					if prevCap.IsEnPassant() {
						vic = types.Pawn
					}
					w.History.PenalizeCaptureHistory(att, vic, prevCap.To(), depth)
				}
			}

			return beta
		}

		// Alpha improvement
		if score > alpha {
			alpha = score
			bestMove = m
			bestType = tt.Exact

			// Update PV array
			w.PVArray[ply][0] = m
			copy(w.PVArray[ply][1:1+w.PVLength[ply+1]], w.PVArray[ply+1][:w.PVLength[ply+1]])
			w.PVLength[ply] = 1 + w.PVLength[ply+1]
		}
	}

	// 8. Checkmate or Stalemate detection
	if legalMoves == 0 {
		if excludedMove != types.MoveNone {
			return alpha
		}
		if inCheck {
			return -MateScore + int32(ply)
		}
		return 0
	}

	// An upper bound only says the eval was too high when it sits below rawEval
	if !inCheck && excludedMove == types.MoveNone && alpha < MateThreshold && alpha > -MateThreshold &&
		(bestType == tt.Exact || alpha < rawEval) {
		diff := alpha - rawEval
		w.History.UpdateCorrHist(w.Board, diff, depth)
	}

	if ply == 0 && bestMove == types.MoveNone && bestRootMove != types.MoveNone {
		bestMove = bestRootMove
		w.PVArray[0][0] = bestRootMove
		w.PVLength[0] = 1
	}

	if w.TimeMan.Stopped.Load() {
		return 0
	}

	if excludedMove == types.MoveNone && (ply > 0 || len(w.ExcludedRootMoves) == 0) {
		w.TT.StoreEval(w.Board.Hash, bestMove, tt.ScoreToTT(alpha, ply), netEval, depth, bestType)
	}
	return alpha
}

func (w *SearchWorker) Quiescence(alpha, beta int32, ply int) int32 {
	if w.TimeMan.Stopped.Load() {
		return 0
	}

	w.LocalNodes++
	if w.LocalNodes%1024 == 0 {
		w.Nodes.Add(1024)
		if w.TimeMan.CheckLimits() {
			return 0
		}
	}

	if int8(ply) > w.SelDepth {
		w.SelDepth = int8(ply)
	}

	if ply >= MaxPly-1 {
		return w.Evaluate(ply)
	}

	// Draw detection in quiescence
	if w.Board.IsRepetition() || w.Board.IsFiftyMoveDraw() || w.Board.IsInsufficientMaterial() {
		return 0
	}

	// TT probe: any stored bound is at least as deep as a quiescence search.
	// Stores go in at depth 0; tt.Store never lets those displace a current-generation entry.
	origAlpha := alpha
	_, ttScore, ttEval, _, ttType, ttFound := w.TT.ProbeEval(w.Board.Hash)
	netEval := tt.EvalNone
	if ttFound {
		ttScore = tt.ScoreFromTT(ttScore, ply)
		switch {
		case ttType == tt.Exact,
			ttType == tt.LowerBound && ttScore >= beta,
			ttType == tt.UpperBound && ttScore <= alpha:
			return ttScore
		}
	}

	inCheck := w.Board.IsInCheck(w.Board.SideToMove)

	// Stand-pat score
	futBase := int32(-Infinity) // standPat + QS futility margin
	if !inCheck {
		if ttFound && ttEval != tt.EvalNone {
			netEval = ttEval
		} else {
			netEval = w.rawEval(ply)
		}
		standPat := w.adjustEval(netEval)
		futBase = standPat + P.QSFutMargin
		if standPat >= beta {
			return beta
		}
		// Delta pruning: do not delta-prune if pawns are close to promotion
		pawnsNearPromo := (w.Board.PieceBB[types.WhitePawn]&bitboard.Rank7BB != 0) || (w.Board.PieceBB[types.BlackPawn]&bitboard.Rank2BB != 0)
		if !pawnsNearPromo && standPat < alpha-1100 {
			return alpha
		}
		if standPat > alpha {
			alpha = standPat
		}
	}

	var ml movegen.MoveList
	movegen.GenerateMoves(w.Board, !inCheck, &ml)

	var checkers bitboard.Bitboard
	if inCheck {
		checkers = w.Board.Checkers(w.Board.SideToMove)
	}

	var scores [256]int32
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.IsPromotion() && m.PromotionType() == types.Queen {
			scores[i] = 100000 + eval.SEE(w.Board, m)
			if inCheck && (bitboard.SquareBB[m.To()]&checkers) != 0 {
				scores[i] += 500000
			}
			continue
		}
		if m.IsCapture() || m.IsPromotion() {
			attacker := w.Board.Pieces[m.From()].Type()
			victim := w.Board.Pieces[m.To()].Type()
			if m.IsEnPassant() {
				victim = types.Pawn
			}
			capHist := int32(0)
			if attacker < 7 && victim < 7 && m.To() < 64 {
				capHist = w.History.CaptureHistory[attacker][m.To()][victim]
			}
			scores[i] = PieceMVV[victim]*1000 - PieceMVV[attacker]*100 + capHist/10
			if inCheck && (bitboard.SquareBB[m.To()]&checkers) != 0 {
				scores[i] += 500000
			}
		}
	}

	legalMoves := 0
	for i := 0; i < ml.Count; i++ {
		m := PickNextMove(&ml, scores[:ml.Count], i)

		// Per-capture futility pruning: if even winning the captured piece
		// plus a margin cannot lift the stand-pat score above alpha, the
		// capture is futile (non-promotions that don't give check only).
		if !inCheck && m.IsCapture() && !m.IsPromotion() {
			victim := w.Board.Pieces[m.To()].Type()
			if m.IsEnPassant() {
				victim = types.Pawn
			}
			if futBase+eval.SEEPieceValues[victim] <= alpha && !w.Board.GivesCheck(m) {
				continue
			}
		}

		// SEE pruning in quiescence: skip losing captures if not in check and move does not give check
		// SEE first: GivesCheck then only runs for the losing captures.
		if !inCheck && eval.SEE(w.Board, m) < 0 && !w.Board.GivesCheck(m) {
			continue
		}

		w.pushAccumulator(ply, m)
		if !w.Board.MakeMove(m) {
			continue
		}
		w.TT.Prefetch(w.Board.Hash)

		legalMoves++
		score := -w.Quiescence(-beta, -alpha, ply+1)
		w.Board.UnmakeMove(m)

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		if score >= beta {
			if !ttFound {
				w.TT.StoreEval(w.Board.Hash, m, tt.ScoreToTT(beta, ply), netEval, 0, tt.LowerBound)
			}
			return beta
		}
		if score > alpha {
			alpha = score
		}
	}

	if inCheck && legalMoves == 0 {
		return -MateScore + int32(ply)
	}

	if !ttFound {
		bound := tt.UpperBound
		if alpha > origAlpha {
			bound = tt.Exact
		}
		w.TT.StoreEval(w.Board.Hash, types.MoveNone, tt.ScoreToTT(alpha, ply), netEval, 0, bound)
	}
	return alpha
}

func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
