package search

import (
	"math"
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

var LMRTable [128][128]int8

func init() {
	for d := 1; d < 128; d++ {
		for m := 1; m < 128; m++ {
			r := 0.4 + math.Log(float64(d))*math.Log(float64(m))/2.3
			if r < 0 {
				r = 0
			}
			LMRTable[d][m] = int8(r)
		}
	}
}

type SearchWorker struct {
	ID                int
	Board             *board.Board
	TT                *tt.TT
	TimeMan           *TimeManager
	History           *SearchHistory
	Nodes             *atomic.Uint64
	TBHits            *atomic.Uint64
	LocalNodes        uint64
	SelDepth          int8
	PVArray           [MaxPly][MaxPly]types.Move
	PVLength          [MaxPly]int
	StaticEvalHistory [MaxPly]int32
	Accumulators      [MaxPly]nnue.Accumulator
	RootBestMove      types.Move
	RootScore         int32
	RootMoveNodes     [64][64]uint64
}

func (w *SearchWorker) Evaluate(ply int) int32 {
	var staticEval int32
	if nnue.Enabled && nnue.ActiveNetwork != nil && ply < MaxPly {
		staticEval = nnue.Evaluate(&w.Accumulators[ply], w.Board.SideToMove)
	} else {
		staticEval = eval.EvaluateClassical(w.Board)
	}
	corr := w.History.GetCorrection(w.Board.SideToMove, w.Board.PawnHash)
	return staticEval + corr
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
	nnue.Refresh(w.Board, &w.Accumulators[0])
	return w
}

func (w *SearchWorker) Search(depth int8, alpha, beta int32, ply int, isPVNode bool, canMultiCut bool, excludedMove types.Move, prevMove types.Move, prevMove2 types.Move, prevMove4 types.Move) int32 {
	if w.TimeMan.Stopped.Load() {
		return 0
	}

	if ply == 0 {
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

	// Check extension
	if inCheck {
		depth++
	}

	// Leaf / Quiescence search
	if depth <= 0 {
		return w.Quiescence(alpha, beta, ply)
	}

	if ply >= MaxPly-1 {
		return w.Evaluate(ply)
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

	var staticEval int32
	if inCheck {
		staticEval = -Infinity
	} else {
		staticEval = w.Evaluate(ply)
		if ttFound {
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

	// 4. Reverse Futility Pruning (Static Null Move Pruning)
	if ply > 0 && !isPVNode && !inCheck && depth <= 6 && abs(beta) < MateThreshold {
		margin := int32(depth) * 80
		if !improving {
			margin = int32(depth) * 60
		}
		if staticEval-margin >= beta {
			return staticEval - margin
		}
	}

	// 5. Razoring
	if ply > 0 && !isPVNode && !inCheck && depth <= 3 {
		razorMargin := int32(150 + int(depth)*125)
		if !improving {
			razorMargin -= 50
		}
		if staticEval+razorMargin <= alpha {
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

	if ply > 0 && !isPVNode && !inCheck && depth >= 3 && staticEval >= beta && hasNonPawns {
		r := int8(3 + depth/4)
		if !improving {
			r++
		}
		if ply+1 < MaxPly {
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
	// If at depth >= 4 with no TT move, reduce search depth by 1 ply
	if excludedMove == types.MoveNone && depth >= 4 && ttMove == types.MoveNone {
		depth--
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

		sScore := w.Search(sDepth, singularBeta-1, singularBeta, ply, false, false, ttMove, prevMove, prevMove2, prevMove4)

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
			return beta
		}
	}

	var bestMove types.Move
	var bestRootMove types.Move
	bestRootScore := -Infinity
	var searchedMoves [256]types.Move
	movesSearched := 0
	legalMoves := 0
	bestType := tt.UpperBound

	// Staged Move Generation: Search TT move first before generating all moves
	if ttMove != types.MoveNone && ttMove != excludedMove && movegen.IsPseudoLegal(w.Board, ttMove) {
		if ply+1 < MaxPly {
			nnue.Update(&w.Accumulators[ply], &w.Accumulators[ply+1], ttMove, w.Board)
		}
		if w.Board.MakeMove(ttMove) {
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
				w.TT.Store(w.Board.Hash, ttMove, tt.ScoreToTT(score, ply), depth, tt.LowerBound)

				if !inCheck && score < MateThreshold && score > -MateThreshold {
					diff := score - staticEval
					w.History.UpdateCorrHist(us, w.Board.PawnHash, diff, depth)
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
	if ply > 0 && canMultiCut && !isPVNode && !inCheck && depth >= 5 && abs(beta) < MateThreshold {
		probBeta := beta + 200
		probDepth := depth - 4
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
			if ply+1 < MaxPly {
				nnue.Update(&w.Accumulators[ply], &w.Accumulators[ply+1], m, w.Board)
			}
			if !w.Board.MakeMove(m) {
				continue
			}

			probScore := -w.Search(probDepth, -probBeta, -probBeta+1, ply+1, false, false, types.MoveNone, m, prevMove, prevMove2)
			w.Board.UnmakeMove(m)

			if w.TimeMan.Stopped.Load() {
				return 0
			}

			if probScore >= probBeta {
				w.TT.Store(w.Board.Hash, m, tt.ScoreToTT(beta, ply), probDepth, tt.LowerBound)
				return beta
			}
		}
	}

	// 8. Multi-Cut Pruning (MPC)
	// If at least M=3 out of the first C=8 candidate moves produce a beta cutoff
	// at reduced depth (depth - 1 - R, R=3), the position is almost certainly a CUT node.
	if ply > 0 && canMultiCut && !isPVNode && !inCheck && depth >= 6 && hasNonPawns && staticEval >= beta-100 {
		var mcList movegen.MoveList
		movegen.GenerateMoves(w.Board, false, &mcList)

		var mcScores [256]int32
		ScoreMoves(w.Board, &mcList, mcScores[:mcList.Count], ttMove, ply, prevMove, prevMove2, prevMove4, w.History)

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
			if ply+1 < MaxPly {
				nnue.Update(&w.Accumulators[ply], &w.Accumulators[ply+1], m, w.Board)
			}
			if !w.Board.MakeMove(m) {
				continue
			}

			mcScore := -w.Search(mcDepth, -beta, -beta+1, ply+1, false, false, types.MoveNone, m, prevMove, prevMove2)
			w.Board.UnmakeMove(m)

			if w.TimeMan.Stopped.Load() {
				return 0
			}

			if mcScore >= beta {
				cutoffs++
				if cutoffs >= 3 {
					w.TT.Store(w.Board.Hash, m, tt.ScoreToTT(beta, ply), mcDepth, tt.LowerBound)
					return beta
				}
			}
		}
	}

	// 9. Staged Move Generation & Search Loop
	mp := NewMovePicker(w.Board, ttMove, excludedMove, ply, prevMove, prevMove2, prevMove4, w.History)
	mp.stage = StageGenCaptures

	for {
		m := mp.Next()
		if m == types.MoveNone {
			break
		}
		if m == ttMove || m == excludedMove {
			continue
		}
		isQuiet := !m.IsCapture() && !m.IsPromotion()
		killer1 := mp.killer1
		killer2 := mp.killer2
		counterMove := mp.counterMove

		// Late Move Pruning (LMP)
		if !isPVNode && !inCheck && depth <= 5 && isQuiet && movesSearched > 0 && m != killer1 && m != killer2 {
			lmpThreshold := 3 + 2*int(depth)*int(depth)
			if !improving {
				lmpThreshold /= 2
			}
			if legalMoves >= lmpThreshold {
				continue
			}
		}

		// Futility Pruning
		if !isPVNode && !inCheck && depth <= 6 && isQuiet && movesSearched > 0 && m != killer1 && m != killer2 && m != counterMove {
			futMargin := int32(80) * int32(depth)
			if !improving {
				futMargin -= int32(20) * int32(depth)
			}
			if staticEval+futMargin <= alpha {
				if !w.Board.GivesCheck(m) {
					continue
				}
			}
		}

		// History Pruning
		// Prune quiet moves with poor history score at shallow depths
		if !isPVNode && !inCheck && depth <= 4 && isQuiet && movesSearched > 0 && m != killer1 && m != killer2 && m != counterMove {
			histScore := w.History.HistoryTable[us][m.From()][m.To()]
			if prevMove != types.MoveNone && prevMove.To() < 64 && m.To() < 64 {
				histScore += w.History.ContHistory[prevMove.To()][m.To()]
			}
			if prevMove2 != types.MoveNone && prevMove2.To() < 64 && m.To() < 64 {
				histScore += w.History.ContHistory2[prevMove2.To()][m.To()]
			}
			histMargin := -int32(depth) * 512
			if !improving {
				histMargin = -int32(depth) * 256
			}
			if histScore < histMargin {
				continue
			}
		}

		// SEE Pruning
		if !isPVNode && !inCheck && depth <= 4 && m != killer1 && m != killer2 {
			seeThreshold := -int32(depth) * 35
			if !isQuiet {
				seeThreshold = -int32(depth) * 90
			}
			if eval.SEE(w.Board, m) < seeThreshold {
				continue
			}
		}

		if ply+1 < MaxPly {
			nnue.Update(&w.Accumulators[ply], &w.Accumulators[ply+1], m, w.Board)
		}
		if !w.Board.MakeMove(m) {
			continue
		}

		legalMoves++
		searchedMoves[movesSearched] = m
		givesCheck := w.Board.IsInCheck(w.Board.SideToMove)
		var score int32

		var startNodes uint64
		if ply == 0 {
			startNodes = w.LocalNodes
		}

		// Principal Variation Search & LMR
		if movesSearched == 0 {
			// First move: full window (extended if singular)
			score = -w.Search(depth-1+extension, -beta, -alpha, ply+1, isPVNode, true, types.MoveNone, m, prevMove, prevMove2)
		} else {
			// Late Move Reductions (LMR)
			reduction := int8(0)
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

					// History-based reduction adjustment
					hist := w.History.HistoryTable[us][m.From()][m.To()]
					if prevMove != types.MoveNone && prevMove.To() < 64 && m.To() < 64 {
						hist += w.History.ContHistory[prevMove.To()][m.To()]
					}
					if prevMove2 != types.MoveNone && prevMove2.To() < 64 && m.To() < 64 {
						hist += w.History.ContHistory2[prevMove2.To()][m.To()]
					}
					if prevMove4 != types.MoveNone && prevMove4.To() < 64 && m.To() < 64 {
						hist += w.History.ContHistory4[prevMove4.To()][m.To()]
					}
					if hist > 4000 {
						reduction--
						if hist > 8000 {
							reduction--
						}
					} else if hist < -4000 {
						reduction++
						if hist < -8000 {
							reduction++
						}
					}
				} else {
					// Tactical moves / Captures (Noisy LMR)
					if depth >= 4 && movesSearched >= 4 {
						reduction /= 2

						attacker := w.Board.Pieces[m.From()].Type()
						victim := w.Board.Pieces[m.To()].Type()
						if m.IsEnPassant() {
							victim = types.Pawn
						}
						capHist := w.History.CaptureHistory[attacker][m.To()][victim]
						if capHist < 0 {
							reduction++
						} else if capHist > 2000 {
							reduction--
						}

						see := eval.SEE(w.Board, m)
						if see < 0 {
							reduction++
						} else if see > 100 {
							reduction--
						}
					} else {
						reduction = 0
					}
				}

				// Check if move gives check: checks should be reduced less
				if givesCheck {
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
			score = -w.Search(depth-1-reduction, -alpha-1, -alpha, ply+1, false, false, types.MoveNone, m, prevMove, prevMove2)

			// If reduced search fails high, re-search at full depth with null window
			if reduction > 0 && score > alpha && !w.TimeMan.Stopped.Load() {
				score = -w.Search(depth-1, -alpha-1, -alpha, ply+1, false, true, types.MoveNone, m, prevMove, prevMove2)
			}

			// If null-window fails high and in PV node, re-search with full PV window
			if isPVNode && score > alpha && !w.TimeMan.Stopped.Load() {
				score = -w.Search(depth-1, -beta, -alpha, ply+1, true, true, types.MoveNone, m, prevMove, prevMove2)
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
			w.TT.Store(w.Board.Hash, m, tt.ScoreToTT(score, ply), depth, tt.LowerBound)

			if !inCheck && score < MateThreshold && score > -MateThreshold {
				diff := score - staticEval
				w.History.UpdateCorrHist(us, w.Board.PawnHash, diff, depth)
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

	if !inCheck && alpha < MateThreshold && alpha > -MateThreshold {
		diff := alpha - staticEval
		w.History.UpdateCorrHist(us, w.Board.PawnHash, diff, depth)
	}

	if ply == 0 && bestMove == types.MoveNone && bestRootMove != types.MoveNone {
		bestMove = bestRootMove
		w.PVArray[0][0] = bestRootMove
		w.PVLength[0] = 1
	}

	w.TT.Store(w.Board.Hash, bestMove, tt.ScoreToTT(alpha, ply), depth, bestType)
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

	inCheck := w.Board.IsInCheck(w.Board.SideToMove)

	// Stand-pat score
	if !inCheck {
		standPat := w.Evaluate(ply)
		if standPat >= beta {
			return beta
		}
		// Delta pruning
		if standPat < alpha-950 {
			return alpha
		}
		if standPat > alpha {
			alpha = standPat
		}
	}

	var ml movegen.MoveList
	movegen.GenerateMoves(w.Board, !inCheck, &ml)

	var scores [256]int32
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.IsPromotion() && m.PromotionType() == types.Queen {
			scores[i] = 100000 + eval.SEE(w.Board, m)
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
		}
	}

	legalMoves := 0
	for i := 0; i < ml.Count; i++ {
		m := PickNextMove(&ml, scores[:ml.Count], i)

		// SEE pruning in quiescence: skip losing captures if not in check
		if !inCheck && eval.SEE(w.Board, m) < 0 {
			continue
		}

		if ply+1 < MaxPly {
			nnue.Update(&w.Accumulators[ply], &w.Accumulators[ply+1], m, w.Board)
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

func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
