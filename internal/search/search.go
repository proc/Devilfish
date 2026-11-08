package search

import (
	"math"
	"sync/atomic"

	"devilfish/internal/board"
	"devilfish/internal/eval"
	"devilfish/internal/movegen"
	"devilfish/internal/tt"
	"devilfish/internal/types"
)

const (
	Infinity      int32 = 32000
	MateScore     int32 = 30000
	MateThreshold int32 = 29000
)

var LMRTable [128][128]int8

func init() {
	for d := 1; d < 128; d++ {
		for m := 1; m < 128; m++ {
			r := 0.75 + math.Log(float64(d))*math.Log(float64(m))/2.25
			if r < 1 {
				r = 1
			}
			LMRTable[d][m] = int8(r)
		}
	}
}

type SearchWorker struct {
	ID          int
	Board       *board.Board
	TT          *tt.TT
	TimeMan     *TimeManager
	History     *SearchHistory
	Nodes       *atomic.Uint64
	LocalNodes  uint64
	SelDepth    int8
	PVArray     [MaxPly][MaxPly]types.Move
	PVLength    [MaxPly]int
	RootBestMove types.Move
	RootScore   int32
}

func NewWorker(id int, b *board.Board, ttTable *tt.TT, tm *TimeManager, nodes *atomic.Uint64) *SearchWorker {
	return &SearchWorker{
		ID:      id,
		Board:   b.Clone(),
		TT:      ttTable,
		TimeMan: tm,
		History: NewSearchHistory(),
		Nodes:   nodes,
	}
}

func (w *SearchWorker) Search(depth int8, alpha, beta int32, ply int, isPVNode bool, canMultiCut bool, excludedMove types.Move, prevMove types.Move) int32 {
	// Periodically check search time/limits
	w.LocalNodes++
	if w.LocalNodes%2048 == 0 {
		w.Nodes.Add(2048)
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

	// Check extension
	if inCheck {
		depth++
	}

	// Leaf / Quiescence search
	if depth <= 0 {
		return w.Quiescence(alpha, beta, ply)
	}

	if ply >= MaxPly-1 {
		return eval.Evaluate(w.Board)
	}

	// 3. Transposition Table Probe
	ttMove := types.MoveNone
	ttEntryMove, ttScore, ttDepth, ttType, ttFound := w.TT.Probe(w.Board.Hash)
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

	staticEval := eval.Evaluate(w.Board)

	// 4. Reverse Futility Pruning (Static Null Move Pruning)
	if !isPVNode && !inCheck && depth <= 4 {
		margin := int32(depth) * 80
		if staticEval-margin >= beta {
			return staticEval
		}
	}

	// 5. Razoring
	if !isPVNode && !inCheck && depth <= 3 {
		if staticEval+int32(depth)*400 <= alpha {
			qScore := w.Quiescence(alpha, beta, ply)
			if qScore <= alpha {
				return qScore
			}
		}
	}

	// 6. Null Move Pruning (NMP)
	us := w.Board.SideToMove
	hasNonPawns := (w.Board.PieceBB[types.MakePiece(us, types.Knight)] |
		w.Board.PieceBB[types.MakePiece(us, types.Bishop)] |
		w.Board.PieceBB[types.MakePiece(us, types.Rook)] |
		w.Board.PieceBB[types.MakePiece(us, types.Queen)]) != 0

	if !isPVNode && !inCheck && depth >= 3 && staticEval >= beta && hasNonPawns {
		r := int8(3 + depth/4)
		w.Board.MakeNullMove()
		nullScore := -w.Search(depth-1-r, -beta, -beta+1, ply+1, false, false, types.MoveNone, types.MoveNone)
		w.Board.UnmakeNullMove()

		if w.TimeMan.Stopped.Load() {
			return 0
		}
		if nullScore >= beta {
			if nullScore > MateThreshold {
				nullScore = beta
			}
			return nullScore
		}
	}

	extension := int8(0)

	// Singular Extension (SE)
	// If the TT move is significantly better than all other alternative moves,
	// extend the search depth for the TT move.
	if excludedMove == types.MoveNone && depth >= 6 && ttMove != types.MoveNone &&
		!inCheck && ttFound && ttDepth >= depth-3 && ttType != tt.UpperBound &&
		ttScore > -MateThreshold && ttScore < MateThreshold {

		singularMargin := int32(depth) * 2
		singularBeta := ttScore - singularMargin
		sDepth := (depth - 1) / 2
		if sDepth < 1 {
			sDepth = 1
		}

		sScore := w.Search(sDepth, singularBeta-1, singularBeta, ply, false, false, ttMove, prevMove)

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		if sScore < singularBeta {
			extension = 1
			if !isPVNode && sScore < singularBeta-25 {
				extension = 2
			}
		} else if singularBeta >= beta {
			// Multi-cut fail high: an alternative move beats beta at reduced depth
			return singularBeta
		}
	}

	var bestMove types.Move
	var searchedMoves [256]types.Move
	movesSearched := 0
	legalMoves := 0
	bestType := tt.UpperBound

	// Staged Move Generation: Search TT move first before generating all moves
	if ttMove != types.MoveNone && ttMove != excludedMove && movegen.IsPseudoLegal(w.Board, ttMove) {
		if w.Board.MakeMove(ttMove) {
			legalMoves++
			searchedMoves[movesSearched] = ttMove

			score := -w.Search(depth-1+extension, -beta, -alpha, ply+1, isPVNode, true, types.MoveNone, ttMove)
			w.Board.UnmakeMove(ttMove)
			movesSearched++

			if w.TimeMan.Stopped.Load() {
				return 0
			}

			// Beta Cutoff (Fail-High)
			if score >= beta {
				w.TT.Store(w.Board.Hash, ttMove, tt.ScoreToTT(score, ply), depth, tt.LowerBound)

				if ply == 0 {
					bestMove = ttMove
					w.PVArray[0][0] = ttMove
					w.PVLength[0] = 1
				}

				if !ttMove.IsCapture() && !ttMove.IsPromotion() {
					w.History.UpdateKillers(ply, ttMove)
					w.History.UpdateHistory(us, ttMove, depth)
					w.History.UpdateCounterMove(prevMove, ttMove)
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

	// 7. Multi-Cut Pruning (MPC)
	// If at least M=3 out of the first C=8 candidate moves produce a beta cutoff
	// at reduced depth (depth - 1 - R, R=3), the position is almost certainly a CUT node.
	if canMultiCut && !isPVNode && !inCheck && depth >= 6 && hasNonPawns && staticEval >= beta-100 {
		var mcList movegen.MoveList
		movegen.GenerateMoves(w.Board, false, &mcList)

		var mcScores [256]int32
		ScoreMoves(w.Board, &mcList, mcScores[:mcList.Count], ttMove, ply, prevMove, w.History)

		c := 8
		if mcList.Count < c {
			c = mcList.Count
		}

		mcR := int8(3)
		mcDepth := depth - 1 - mcR
		cutoffs := 0

		for i := 0; i < c; i++ {
			m := PickNextMove(&mcList, mcScores[:mcList.Count], i)
			if m == ttMove || m == excludedMove {
				continue
			}
			if !w.Board.MakeMove(m) {
				continue
			}

			mcScore := -w.Search(mcDepth, -beta, -beta+1, ply+1, false, false, types.MoveNone, m)
			w.Board.UnmakeMove(m)

			if w.TimeMan.Stopped.Load() {
				return 0
			}

			if mcScore >= beta {
				cutoffs++
				if cutoffs >= 3 {
					w.TT.Store(w.Board.Hash, m, tt.ScoreToTT(beta, ply), depth, tt.LowerBound)
					return beta
				}
			}
		}
	}

	// 8. Move Generation & Scoring (only needed if TT move and Multi-Cut did not cut off)
	var ml movegen.MoveList
	movegen.GenerateMoves(w.Board, false, &ml)

	var scores [256]int32
	ScoreMoves(w.Board, &ml, scores[:ml.Count], types.MoveNone, ply, prevMove, w.History)

	for i := 0; i < ml.Count; i++ {
		m := PickNextMove(&ml, scores[:ml.Count], i)
		if m == ttMove || m == excludedMove {
			continue
		}
		isQuiet := !m.IsCapture() && !m.IsPromotion()

		// Late Move Pruning (LMP)
		if !isPVNode && !inCheck && depth <= 4 && isQuiet && legalMoves >= (3+2*int(depth)*int(depth)) {
			continue
		}

		// Futility Pruning
		if !isPVNode && !inCheck && depth <= 3 && isQuiet && (staticEval+int32(depth)*100 <= alpha) {
			continue
		}

		// SEE Pruning
		if !inCheck && depth <= 3 && eval.SEE(w.Board, m) < -int32(depth)*60 {
			continue
		}

		if !w.Board.MakeMove(m) {
			continue
		}

		legalMoves++
		searchedMoves[movesSearched] = m
		var score int32

		// Principal Variation Search & LMR
		if movesSearched == 0 {
			// First move: full window (extended if singular)
			score = -w.Search(depth-1+extension, -beta, -alpha, ply+1, isPVNode, true, types.MoveNone, m)
		} else {
			// Late Move Reductions (LMR)
			reduction := int8(0)
			if depth >= 3 && movesSearched >= 4 && isQuiet && !inCheck {
				dIdx := depth
				if dIdx > 127 {
					dIdx = 127
				}
				mIdx := movesSearched
				if mIdx >= 128 {
					mIdx = 127
				}
				reduction = LMRTable[dIdx][mIdx]
				if isPVNode {
					reduction--
				}
				if reduction < 0 {
					reduction = 0
				}
				if reduction > depth-2 {
					reduction = depth - 2
				}
			}

			// Null-window search with reduction
			score = -w.Search(depth-1-reduction, -alpha-1, -alpha, ply+1, false, false, types.MoveNone, m)

			// If reduced search fails high, re-search at full depth
			if reduction > 0 && score > alpha {
				score = -w.Search(depth-1, -alpha-1, -alpha, ply+1, false, true, types.MoveNone, m)
			}

			// If null-window fails high and in PV node, re-search with full window
			if isPVNode && score > alpha && score < beta {
				score = -w.Search(depth-1, -beta, -alpha, ply+1, true, true, types.MoveNone, m)
			}
		}

		w.Board.UnmakeMove(m)
		movesSearched++

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		// Beta Cutoff (Fail-High)
		if score >= beta {
			w.TT.Store(w.Board.Hash, m, tt.ScoreToTT(score, ply), depth, tt.LowerBound)

			if ply == 0 {
				bestMove = m
				w.PVArray[0][0] = m
				w.PVLength[0] = 1
			}

			if isQuiet {
				w.History.UpdateKillers(ply, m)
				w.History.UpdateHistory(us, m, depth)
				w.History.UpdateCounterMove(prevMove, m)

				// Penalize preceding quiet moves that failed to produce a cutoff
				for j := 0; j < movesSearched-1; j++ {
					prevQuiet := searchedMoves[j]
					if !prevQuiet.IsCapture() && !prevQuiet.IsPromotion() {
						w.History.PenalizeHistory(us, prevQuiet, depth)
					}
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

	w.TT.Store(w.Board.Hash, bestMove, tt.ScoreToTT(alpha, ply), depth, bestType)
	return alpha
}

func (w *SearchWorker) Quiescence(alpha, beta int32, ply int) int32 {
	w.LocalNodes++
	if w.LocalNodes%2048 == 0 {
		w.Nodes.Add(2048)
		if w.TimeMan.CheckLimits() {
			return 0
		}
	}

	if int8(ply) > w.SelDepth {
		w.SelDepth = int8(ply)
	}

	if ply >= MaxPly-1 {
		return eval.Evaluate(w.Board)
	}

	// Draw detection in quiescence
	if w.Board.IsRepetition() || w.Board.IsFiftyMoveDraw() || w.Board.IsInsufficientMaterial() {
		return 0
	}

	inCheck := w.Board.IsInCheck(w.Board.SideToMove)

	// Stand-pat score
	if !inCheck {
		standPat := eval.Evaluate(w.Board)
		if standPat >= beta {
			return beta
		}
		if standPat > alpha {
			alpha = standPat
		}
		// Delta pruning
		if standPat < alpha-950 {
			return alpha
		}
	}

	var ml movegen.MoveList
	movegen.GenerateMoves(w.Board, !inCheck, &ml)

	var scores [256]int32
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.IsCapture() || m.IsPromotion() {
			attacker := w.Board.Pieces[m.From()].Type()
			victim := w.Board.Pieces[m.To()].Type()
			if m.IsEnPassant() {
				victim = types.Pawn
			}
			scores[i] = PieceMVV[victim]*10 - PieceMVV[attacker]
		}
	}

	legalMoves := 0
	for i := 0; i < ml.Count; i++ {
		m := PickNextMove(&ml, scores[:ml.Count], i)

		// SEE pruning in quiescence: skip losing captures if not in check
		if !inCheck && eval.SEE(w.Board, m) < 0 {
			continue
		}

		if !w.Board.MakeMove(m) {
			continue
		}

		legalMoves++
		score := -w.Quiescence(-beta, -alpha, ply+1)
		w.Board.UnmakeMove(m)

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		if score >= beta {
			return beta
		}
		if score > alpha {
			alpha = score
		}
	}

	if inCheck && legalMoves == 0 {
		return -MateScore + int32(ply)
	}

	return alpha
}
