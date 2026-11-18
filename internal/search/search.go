package search

import (
	"math"
	"sync/atomic"
	"unsafe"

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
// (8 bytes each, 16 MB in all). Must be a power of two. A network evaluation
// costs far more than a probe, and the search asks for the same positions
// again and again: with 2 MB of cache 27% of the requests were answered from
// it; 16 MB made the search 6 to 8% faster and 64 MB only 9%.
const evalCacheSize = 1 << 21

const wdlCacheSize = 4096

// evalCacheKey is what an entry stores to recognise its position: the upper
// half of the hash (the index is the lower part), never zero, so that an
// empty entry matches nothing.
func evalCacheKey(h uint64) uint32 { return uint32(h>>32) | 1 }

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
	sharedNodes       atomic.Uint64
	NnueEvals         uint64
	NnueUpdates       uint64
	NnueRefreshes     uint64
	ClassicalEvals    uint64
	SelDepth          int8
	PVArray           [MaxPly][MaxPly]types.Move
	PVLength          [MaxPly]int
	StaticEvalHistory [MaxPly]int32
	rootDepth         int                // depth of the root search in progress
	pickers           [MaxPly]MovePicker // the main search's move picker at each ply
	pickerHC          [MaxPly]histCtx    // and the history rows it reads
	lastCorr          int32              // the correction-history value adjustEval last applied
	done              threadResult       // helper threads: the last finished iteration (see pickThread)
	nmpMinPly         int                // P.NMPGuard 3: no null move above this ply (inside a verification search), 0 otherwise
	plies             [MaxPly]plyInfo    // P.HistUpdate 1: what each node on the path knows about itself and the move it is searching
	// Per-ply scratch for the move loops, kept here so that a node does not
	// zero kilobytes of stack it mostly never reads. The only searches at
	// the same ply as a running node (null-move verification, singular
	// search, razoring) finish before that node's move loop starts.
	searched     [MaxPly][256]types.Move // the main search's moves searched so far
	qMoves       [MaxPly]movegen.MoveList
	qScores      [MaxPly][256]int32
	deferred     [MaxPly][maxDeferred]deferredMove // P.SMPDefer: moves put off at each ply
	busy         *busyTable                        // P.SMPDefer: the search's shared table, nil with one thread
	Accumulators [MaxPly]nnue.Accumulator
	EvalCache    []atomic.Uint64 // shared across Engine workers (or worker-local in standalone NewWorker)
	wdlCache     [wdlCacheSize]struct {
		key uint64
		wdl int8
		ok  bool
	}
	Eight             *nnue.EightState   // incremental 8Chess accumulators (nil unless that net is active)
	useAcc            bool               // the 768->256 accumulator stack is in use this search
	cacheNet          *nnue.EightNetwork // network whose evaluations EvalCache holds
	RootBestMove      types.Move
	RootScore         int32
	RootMoveNodes     [64][64]uint64
	ExcludedRootMoves []types.Move
	Stats             SearchStats

	// lowPrio: the worker's thread is running at low priority (see
	// enterPriority).
	lowPrio bool
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
			e := &w.EvalCache[w.Board.Hash&(evalCacheSize-1)]
			key := evalCacheKey(w.Board.Hash)
			if v := e.Load(); uint32(v>>32) == key {
				staticEval = int32(uint32(v))
			} else {
				if w.Eight != nil {
					staticEval = w.evaluateEight(ply)
				} else {
					staticEval = nnue.EvaluateEight(nnue.ActiveEightNetwork, w.Board)
				}
				e.Store(uint64(key)<<32 | uint64(uint32(staticEval)))
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
	w.lastCorr = w.History.GetCorrection(w.Board)
	v := raw + w.lastCorr
	if P.Optimism != 0 {
		if o := w.TimeMan.Optimism.Load(); o != 0 {
			v += optimismCP(w.Board, raw, o)
		}
	}
	// Fade the eval toward a draw as the fifty-move counter runs up (over 189
	// half-moves, as Stockfish does)
	return v * (189 - int32(w.Board.HalfmoveClock)) / 189
}

// setOptimism records how far ahead the side to move at the root was found
// to be (rootScore, centipawns: the running average of the best move's
// scores, see rootAverage). It is Stockfish's 114*s/(|s|+85), with s in
// the network's internal units: about three to the centipawn.
func setOptimism(tm *TimeManager, rootScore int32, rootSide types.Color) {
	if P.Optimism == 0 {
		tm.Optimism.Store(0)
		return
	}
	x := max(min(rootScore, 2000), -2000) * 3
	o := P.OptimismScale * x / (abs(x) + P.OptimismDiv)
	if rootSide == types.Black {
		o = -o
	}
	tm.Optimism.Store(o)
}

// optimismCP is the optimism term for the side to move, in centipawns, for
// a position whose network evaluation is raw. It is Stockfish's: optimism
// scaled by how far the evaluation and the material balance agree, so the
// side ahead at the root prefers straightforward positions and the other
// side complicated ones. Zero with material level; a few centipawns at most.
func optimismCP(b *board.Board, raw int32, whiteOptimism int32) int32 {
	if b.SideToMove == types.Black {
		whiteOptimism = -whiteOptimism
	}
	return nnue.SFOptimismCP(b, raw, whiteOptimism)
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
	if len(w.EvalCache) == 0 {
		w.EvalCache = make([]atomic.Uint64, evalCacheSize)
	}
	if net != w.cacheNet {
		// The cached values came from another network (or none).
		clear(w.EvalCache)
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
// it keeps its allocations, the static evaluations in EvalCache while the
// network is unchanged (they depend on nothing else), and its history tables
// (see SearchHistory.NewSearch; Engine.NewGame clears them).
func (w *SearchWorker) resetFor(b *board.Board, ttTable *tt.TT, tm *TimeManager, nodes *atomic.Uint64, tbHits *atomic.Uint64) {
	w.Board = b.Clone()
	w.TT = ttTable
	w.TimeMan = tm
	w.Nodes = nodes
	w.TBHits = tbHits
	w.History.NewSearch()
	w.LocalNodes = 0
	w.sharedNodes.Store(0)
	w.NnueEvals = 0
	w.NnueUpdates = 0
	w.NnueRefreshes = 0
	w.ClassicalEvals = 0
	w.SelDepth = 0
	clear(w.PVArray[:])
	clear(w.PVLength[:])
	clear(w.StaticEvalHistory[:])
	clear(w.plies[:])
	w.rootDepth = 0
	w.nmpMinPly = 0
	w.done = threadResult{}
	w.lastCorr = 0
	clear(w.pickers[:])
	clear(w.pickerHC[:])
	clear(w.searched[:])
	clear(w.qMoves[:])
	clear(w.qScores[:])
	clear(w.deferred[:])
	w.busy = nil // Engine.Search hands out the search's table
	clear(w.Accumulators[:])
	w.RootBestMove = types.MoveNone
	w.RootScore = 0
	clear(w.RootMoveNodes[:])
	w.ExcludedRootMoves = nil
	w.Stats = SearchStats{}
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

// checkNodeLimits flushes node counts and checks time/node/stop limits.
// Only the main thread (ID == 0) polls time.Since when MaxNodes == 0; helpers
// just check Stopped.Load(), avoiding cross-core clock syscalls and reducing
// shared atomic node-counter contention.
func (w *SearchWorker) checkNodeLimits() bool {
	if w.lowPrio {
		w.syncPriority()
	}
	if w.TimeMan.MaxNodes > 0 {
		w.Nodes.Add(1024)
		return w.TimeMan.CheckLimits()
	}
	if w.LocalNodes&8191 == 0 {
		w.sharedNodes.Store(w.LocalNodes)
	}
	if w.ID == 0 {
		return w.TimeMan.CheckLimits()
	}
	return w.TimeMan.Stopped.Load()
}

// tryProbCut runs ProbCut out-of-line so Search does not zero probList/probScores
// on the majority of nodes that never enter ProbCut.
//
//go:noinline
func (w *SearchWorker) tryProbCut(
	depth int8, beta int32, ply int, cutNode, inCheck bool,
	excludedMove, ttMove types.Move, ttScore int32, ttDepth int8, ttFound bool,
	staticEval, netEval int32, pvBit uint8, improving bool,
	prevMove, prevMove2, prevMove4 types.Move,
	ci *board.CheckInfo, hc *histCtx,
) (int32, bool) {
	if P.ProbCut == 0 || ply <= 0 || inCheck || excludedMove != types.MoveNone || abs(beta) >= MateThreshold {
		return 0, false
	}
	probMinDepth, probBeta, probDepth := int8(5), beta+P.ProbCutMargin, depth-4
	probTTDepth := depth - 3
	if P.ProbCut == 2 {
		probMinDepth, probBeta, probDepth, probTTDepth = 3, beta+80, depth-3, -128
		if improving {
			probBeta, probDepth = beta+59, depth-5
		}
	}
	if depth < probMinDepth || (ttFound && ttDepth >= probTTDepth && ttScore < probBeta) {
		return 0, false
	}

	var probList movegen.MoveList
	movegen.GenerateMoves(w.Board, true, &probList)

	var probScores [256]int32
	ScoreMoves(w.Board, &probList, probScores[:probList.Count], ttMove, ply, prevMove, prevMove2, prevMove4, w.History)

	st := &w.Stats
	probTried := false
	for i := 0; i < probList.Count; i++ {
		m := PickNextMove(&probList, probScores[:probList.Count], i)
		if !eval.SEEGe(w.Board, m, probBeta-staticEval) {
			continue
		}
		w.prefetchNext(w.Board.HashAfter(m))
		w.pushAccumulator(ply, m)
		w.History.PushMove(ply, w.Board.Pieces[m.From()], m.To())
		w.pushMoveInfo(ply, hc, m, 0)
		if !w.makeMove(m, ci, inCheck) {
			continue
		}
		if !probTried {
			probTried = true
			st.ProbTries++
		}

		probScore := -w.Quiescence(-probBeta, -probBeta+1, ply+1)
		if probScore >= probBeta && probDepth > 0 {
			probScore = -w.Search(probDepth, -probBeta, -probBeta+1, ply+1, false, !cutNode, types.MoveNone, m, prevMove, prevMove2)
		}
		w.Board.UnmakeMove(m)

		if w.TimeMan.Stopped.Load() {
			return 0, true
		}

		if probScore >= probBeta {
			st.ProbCuts++
			storeDepth := depth - 3
			if P.ProbCut == 2 {
				storeDepth = max(probDepth, 0) + 1
			}
			w.TT.StoreEval(w.Board.Hash, m, tt.ScoreToTT(probScore, ply), netEval, storeDepth, tt.LowerBound|pvBit)
			if P.FailSoft&softPruning == 0 {
				return beta, true
			}
			return probScore - (probBeta - beta), true
		}
	}
	return 0, false
}

func (w *SearchWorker) Search(depth int8, alpha, beta int32, ply int, isPVNode bool, cutNode bool, excludedMove types.Move, prevMove types.Move, prevMove2 types.Move, prevMove4 types.Move) int32 {
	if w.TimeMan.Stopped.Load() {
		return 0
	}

	if ply == 0 && w.useAcc {
		w.NnueRefreshes++
		nnue.Refresh(w.Board, &w.Accumulators[0])
	}

	// Periodically check search time/limits. A call at depth <= 0 hands the
	// position to Quiescence below, which counts it and checks the limits
	// itself; counting it here as well reported every horizon node twice.
	if depth > 0 {
		w.LocalNodes++
		if w.LocalNodes&1023 == 0 && w.checkNodeLimits() {
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
			return w.drawScore()
		}
	}

	// P.Cycle 1: the side to move can repeat a position at once, so it has
	// at least a draw (Stockfish's upcoming-repetition rule).
	if draw := w.drawScore(); P.Cycle != 0 && ply > 0 && alpha < draw && w.Board.UpcomingRepetition() {
		alpha = draw
		if alpha >= beta {
			return alpha
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
	st := &w.Stats
	st.Nodes++
	st.TTProbes++
	ttMove := types.MoveNone
	ttEntryMove, ttScore, ttEval, ttDepth, ttType, ttFound := w.TT.ProbeEval(w.Board.Hash)
	// ttPv: this position is, or has been, on a principal variation. pvBit
	// carries that into every hash entry this node writes.
	ttPv := isPVNode || (ttFound && ttType&tt.PVFlag != 0)
	ttType &= tt.BoundMask
	pvBit := uint8(0)
	if ttPv {
		pvBit = tt.PVFlag
	}
	if ply < MaxPly {
		w.plies[ply].ttPv = ttPv
	}
	netEval := tt.EvalNone // raw evaluation of this node, once known
	if ttFound {
		st.TTHits++
		ttMove = ttEntryMove
		ttScore = tt.ScoreFromTT(ttScore, ply)

		// Don't take TT cutoffs if searching with an excluded move
		if excludedMove == types.MoveNone && !isPVNode && ttDepth >= depth {
			switch ttType {
			case tt.Exact:
				st.TTCuts++
				return ttScore
			case tt.LowerBound:
				if ttScore >= beta {
					st.TTCuts++
					if P.HistExtra&8 != 0 && P.HistUpdate != 0 {
						w.rewardHashCutoff(ply, depth, ttMove, prevMove)
					}
					return ttScore
				}
			case tt.UpperBound:
				if ttScore <= alpha {
					st.TTCuts++
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

		key := w.Board.Hash | 1
		ce := &w.wdlCache[w.Board.Hash&(wdlCacheSize-1)]
		var wdl int
		var ok bool
		if ce.key == key {
			wdl, ok = int(ce.wdl), ce.ok
		} else {
			wdl, ok = syzygy.ProbeWDL(w.Board)
			ce.key, ce.wdl, ce.ok = key, int8(wdl), ok
		}
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
		w.plies[ply].inCheck, w.plies[ply].ttHit, w.plies[ply].eval = inCheck, ttFound, rawEval
	}
	if ply+2 < MaxPly {
		w.plies[ply+2].cutoffs = 0
	}
	// corr: how far the correction history moved this node's evaluation.
	// A large value marks a kind of position the evaluation gets wrong.
	corr := int32(0)
	if !inCheck && P.CorrSearch != 0 {
		corr = abs(w.lastCorr)
	}
	// opponentWorsening: the evaluation is better for the side to move than
	// it was one ply ago, seen from the same side. priorReduction: by how
	// many plies the move that led here was reduced.
	opponentWorsening := false
	priorReduction := int32(0)
	if ply > 0 && ply < MaxPly {
		p := &w.plies[ply-1]
		priorReduction, p.reduction = p.reduction, 0
		if !inCheck && !p.inCheck && prevMove != types.MoveNone {
			opponentWorsening = rawEval > -p.eval
			// Hindsight (P.Hindsight 2, as Stockfish): a heavily reduced
			// move after which the evaluation did not turn against the
			// mover gets a ply back; a reduced move after which both
			// sides' evaluations agree it was poor loses one more (166 of
			// Stockfish's units, three to the centipawn).
			if P.Hindsight&2 != 0 && excludedMove == types.MoveNone {
				if priorReduction >= 3 && !opponentWorsening {
					depth++
				}
				if priorReduction >= 2 && depth >= 2 && rawEval+p.eval > 55 {
					depth--
				}
			}
		}
	}
	if P.HistExtra&1 != 0 && P.HistUpdate != 0 && !inCheck && ply > 0 && ply < MaxPly && prevMove != types.MoveNone {
		// The previous move, if quiet, gains or loses history by how the
		// static evaluation moved across it (Stockfish's units: three to
		// the centipawn).
		if p := &w.plies[ply-1]; !p.inCheck && p.captured == types.PieceTypeNone {
			diff := max(-189, min(-3*(p.eval+rawEval), 194)) + 60
			sfGravity(p.main, diff*11, sfMainLimit)
			if !ttFound && !p.promotion && p.piece.Type() != types.Pawn {
				sfGravity(&w.History.PawnHist[w.Board.PawnHash%pawnHistSize][p.piece][p.to], diff*13, sfPawnLimit)
			}
		}
	}
	if ply == 0 {
		w.rootDepth = int(depth)
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
	threatenedPieces := w.Board.TacticalThreatsOnPieces(us)

	hc := w.History.Context(w.Board, ply, threatenedPieces)

	ourQueens := w.Board.PieceBB[types.MakePiece(us, types.Queen)]
	ourRooks := w.Board.PieceBB[types.MakePiece(us, types.Rook)]
	ourMinors := w.Board.PieceBB[types.MakePiece(us, types.Knight)] | w.Board.PieceBB[types.MakePiece(us, types.Bishop)]

	// Guards on reverse futility and null-move pruning (see Params.RFPGuard):
	// a static evaluation says nothing about a mating attack in progress, and
	// little about a piece that is about to be captured.
	kingGuard := kingInDanger || prevIsKingThreat
	rfpAllowed := P.RFPGuard == 0 ||
		(P.RFPGuard == 2 && !kingGuard) ||
		(P.RFPGuard == 1 && !kingGuard && (threatenedPieces&(ourQueens|ourRooks|ourMinors)) == 0)
	nmpAllowed := P.NMPGuard == 0 || (P.NMPGuard == 3 && ply >= w.nmpMinPly) ||
		(P.NMPGuard == 2 && !kingGuard) ||
		(P.NMPGuard == 1 && !kingGuard && (threatenedPieces&ourQueens) == 0)

	// 4. Reverse Futility Pruning (Static Null Move Pruning)
	if ply > 0 && !isPVNode && !inCheck && excludedMove == types.MoveNone && rfpAllowed && !(P.TTPV != 0 && ttPv) && int32(depth) <= P.RFPDepth && abs(beta) < MateThreshold && hasNonPawns {
		margin := int32(depth) * P.RFPMargin
		if !improving {
			margin += int32(depth) * P.RFPNonImpr
		}
		if threatenedPieces != 0 {
			margin += 120 // Safety margin if pawns are contested
		}
		if P.Hindsight&1 != 0 && opponentWorsening {
			margin -= P.RFPMargin / 3 // the opponent's last move made things worse for them
		}
		margin += corr * 2 / 3 // an evaluation that needed correcting is trusted less
		if staticEval-margin >= beta {
			st.RFPCuts++
			if P.FailSoft&softPruning == 0 {
				return beta
			}
			// Fail soft: between beta and the evaluation, which is itself
			// only an estimate.
			return (staticEval + beta) / 2
		}
	} else if ply > 0 && !isPVNode && !inCheck && excludedMove == types.MoveNone && int32(depth) <= P.RFPDepth && abs(beta) < MateThreshold && hasNonPawns {
		// Only the threat and king-safety guards stood in the way.
		margin := int32(depth) * P.RFPMargin
		if !improving {
			margin += int32(depth) * P.RFPNonImpr
		}
		if staticEval-margin >= beta {
			st.RFPGuarded++
		}
	}

	// 5. Razoring
	if ply > 0 && !isPVNode && !inCheck && excludedMove == types.MoveNone && !kingInDanger && !prevIsKingThreat && int32(depth) <= P.RazorDepth {
		razorMargin := P.RazorMargin + P.RazorStep*int32(depth-1)
		if staticEval+razorMargin <= alpha {
			st.RazorTries++
			qScore := w.Quiescence(alpha, beta, ply)
			if qScore <= alpha {
				st.RazorCuts++
				// Always alpha, even when failing soft: the quiescence score
				// says the node fails low, not how low a real search of it
				// would come out. Returning it handed the parents bounds far
				// tighter than anything had been searched to justify.
				return alpha
			}
		}
	}

	// 6. Null Move Pruning (NMP)
	// Never pass if our Queen is under direct tactical threat, and never twice in a row
	// (prevMove is MoveNone below the root only directly after a null move).
	pieceCount := bitboard.Count(w.Board.Occupied())
	if ply > 0 && !isPVNode && !inCheck && excludedMove == types.MoveNone && prevMove != types.MoveNone && nmpAllowed && depth >= 3 && staticEval >= beta+max(P.NMPMargin-P.NMPMarginStep*int32(depth), 0) && hasNonPawns && pieceCount > 6 {
		st.NMPTries++
		r := int8(P.NMPBase) + depth/int8(P.NMPDepthDiv)
		if !improving {
			r += int8(P.NMPNonImpr)
		}
		// Reduce more the further static eval sits above beta
		if evalR := (staticEval - beta) / P.NMPEvalDiv; evalR > 0 {
			if evalR > 3 {
				evalR = 3
			}
			r += int8(evalR)
		}
		w.TT.Prefetch(w.Board.Hash ^ board.ZobristSide)
		if w.useAcc && ply+1 < MaxPly {
			w.Accumulators[ply+1] = w.Accumulators[ply]
		}
		w.History.PushNull(ply)
		w.Board.MakeNullMove()
		nullScore := -w.Search(depth-1-r, -beta, -beta+1, ply+1, false, !cutNode, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
		w.Board.UnmakeNullMove()

		if w.TimeMan.Stopped.Load() {
			return 0
		}
		// P.NMPGuard 3, as Stockfish: at high depth, or with the king under
		// pressure (where the other settings forbid the null move), the
		// cutoff counts only if a reduced search without null moves near
		// this node agrees.
		if nullScore >= beta && P.NMPGuard == 3 && w.nmpMinPly == 0 && (depth >= 16 || kingGuard) {
			vDepth := depth - 1 - r
			w.nmpMinPly = ply + 3*int(max(vDepth, 0))/4 + 1
			v := w.Search(vDepth, beta-1, beta, ply, false, false, types.MoveNone, prevMove, prevMove2, prevMove4)
			w.nmpMinPly = 0
			if w.TimeMan.Stopped.Load() {
				return 0
			}
			if v < beta {
				nullScore = beta - 1
			}
		}
		if nullScore >= beta {
			st.NMPCuts++
			if nullScore >= MateThreshold || P.FailSoft&softPruning == 0 {
				return beta // a mate found with an extra move is not proven
			}
			return nullScore
		}
		if nullScore <= -MateThreshold && ply < 2*w.rootDepth {
			depth++
		}
	} else if ply > 0 && !isPVNode && !inCheck && excludedMove == types.MoveNone && prevMove != types.MoveNone && depth >= 3 && staticEval >= beta && hasNonPawns && pieceCount > 6 {
		st.NMPGuarded++
	}

	// Internal Iterative Reduction (IIR)
	// If at non-PV node at depth >= 6 with no TT move, reduce search depth by 1 ply
	if (!isPVNode || (P.IIRPV != 0 && ply > 0)) && !inCheck && excludedMove == types.MoveNone && int32(depth) >= P.IIRDepth && ttMove == types.MoveNone {
		st.IIR++
		depth--
		if cutNode && depth >= 8 {
			depth -= int8(P.IIRCut)
		}
	}

	extension := int8(0)

	// Singular Extension (SE)
	// If the TT move is significantly better than all other alternative moves,
	// extend the search depth for the TT move.
	if ply > 0 && excludedMove == types.MoveNone && depth >= 5 && ttMove != types.MoveNone &&
		!inCheck && ttFound && ttDepth >= depth-3 && ttType != tt.UpperBound &&
		ttScore > -MateThreshold && ttScore < MateThreshold {

		singularMargin := int32(depth) * 2
		singularBeta := ttScore - singularMargin
		sDepth := (depth - 1) / 2
		if sDepth < 1 {
			sDepth = 1
		}

		st.SETries++
		sScore := w.Search(sDepth, singularBeta-1, singularBeta, ply, false, cutNode, ttMove, prevMove, prevMove2, prevMove4)

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		if sScore >= singularBeta && singularBeta >= beta {
			// Multi-cut: another move also beats beta, even searched shallower
			// and with the TT move excluded, so this node fails high.
			st.SEMultiCut++
			if P.SETriple != 0 {
				sfGravity(&w.History.TTMoveHist, -421-110*int32(depth), sfPawnLimit)
			}
			if P.FailSoft&softPruning == 0 {
				return beta
			}
			return singularBeta
		}
		if sScore < singularBeta {
			st.SEExtends++
			extension = 1
			doubleMargin := P.SEDoubleMargin
			if P.SETriple != 0 {
				// Stockfish: the margin narrows when hash moves have been
				// proving best lately (in its units, three to the
				// centipawn), and where the evaluation needed correcting.
				doubleMargin -= 1175*int32(w.History.TTMoveHist)/114178/3 + corr*2/3
			}
			if !isPVNode && sScore < singularBeta-doubleMargin {
				extension = 2
				// A third ply when nothing else comes close, but not in
				// lines already twice as long as the root search is deep.
				if P.SETriple != 0 && sScore < singularBeta-doubleMargin-P.SETripleMargin && ply < 2*w.rootDepth {
					extension = 3
				}
			}
		} else if P.SENegExt != 0 && (ttScore >= beta || cutNode) {
			// Not singular, and the node is expected to fail high whichever
			// move is played: the hash move does not need its full depth.
			st.SENegExts++
			extension = -1
		}
	}

	// 7. ProbCut: if a good capture, searched to reduced depth, already beats
	// beta by a margin, a full-depth search of this node would almost
	// certainly fail high too. Tried before the hash move's full-depth
	// search, so that a cutoff saves it. Skipped when the hash table already
	// says the position is unlikely to reach the raised bound.
	// Check and pin information for this position, computed once for all of
	// its moves (see makeMove and board.CheckInfo).
	ci := w.Board.NewCheckInfo()

	if !isPVNode {
		if score, ok := w.tryProbCut(depth, beta, ply, cutNode, inCheck, excludedMove, ttMove, ttScore, ttDepth, ttFound, staticEval, netEval, pvBit, improving, prevMove, prevMove2, prevMove4, &ci, &hc); ok {
			return score
		}
	}

	// Stockfish's second ProbCut: a hash entry nearly as deep as this search
	// that already puts the score 428 of its units above beta is believed.
	if P.ProbCut == 2 && ply > 0 && !isPVNode && excludedMove == types.MoveNone && ttFound && ttType == tt.LowerBound &&
		ttDepth >= depth-4 && ttScore >= beta+143 && abs(beta) < MateThreshold && abs(ttScore) < MateThreshold {
		return beta + 143
	}

	// Fail soft: bestScore is the best score actually found, which may lie
	// outside (alpha, beta). It is what the node returns and stores, so that
	// the hash table and the callers get the tightest bound available.
	bestScore := -Infinity
	movesPruned := false // a move was skipped unsearched
	var bestMove types.Move
	var bestRootMove types.Move
	bestRootScore := -Infinity
	searchedMoves := &w.searched[ply]
	movesSearched := 0
	// A capture/promotion TT move suggests the position is tactical: quiet
	// alternatives are less likely to matter, so they get reduced more.
	ttMoveNoisy := ttMove != types.MoveNone && (ttMove.IsCapture() || ttMove.IsPromotion())
	legalMoves := 0
	bestType := tt.UpperBound

	// Staged Move Generation: Search TT move first before generating all moves.
	// Odd helpers and higher-ID helpers at the root defer ttMove if another
	// thread is already on it so helpers spread across different root subtrees.
	deferTTAtRoot := ply == 0 && ((w.ID&1) == 1 || w.ID >= 4) && depth >= 5 && w.busy != nil && ttMove != types.MoveNone && w.busy.busy(busyKey(w.Board.Hash, ttMove))
	if !deferTTAtRoot && ttMove != types.MoveNone && ttMove != excludedMove && (ply > 0 || !w.isRootExcluded(ttMove)) && movegen.IsPseudoLegal(w.Board, ttMove) {
		// Check extension (parent side): only for checks that don't lose
		// material, and never stacked on top of a singular extension.
		if extension == 0 && (P.CheckExt == 2 || (P.CheckExt == 1 && isPVNode)) && w.Board.GivesCheckWith(ttMove, &ci) && eval.SEEGe(w.Board, ttMove, -P.CheckExtSEE) {
			st.CheckExts++
			extension = 1
		}
		w.prefetchNext(w.Board.HashAfter(ttMove)) // before the move is made: see HashAfter
		w.pushAccumulator(ply, ttMove)
		w.History.PushMove(ply, w.Board.Pieces[ttMove.From()], ttMove.To())
		w.pushMoveInfo(ply, &hc, ttMove, 1)
		if w.makeMove(ttMove, &ci, inCheck) {
			legalMoves++
			st.MovesSearched++
			searchedMoves[movesSearched] = ttMove

			var startNodes uint64
			if ply == 0 {
				startNodes = w.LocalNodes
			}

			var ttBusyK uint64
			if ply == 0 && w.busy != nil {
				ttBusyK = busyKey(w.Board.Hash, ttMove)
				w.busy.mark(ttBusyK)
			}
			score := -w.Search(depth-1+extension, -beta, -alpha, ply+1, isPVNode, !isPVNode && !cutNode, types.MoveNone, ttMove, prevMove, prevMove2)
			if ttBusyK != 0 {
				w.busy.unmark(ttBusyK)
			}
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
				st.NodesWithMoves++
				st.BetaCuts++
				st.BetaCutsFirst++
				st.BetaCutsTT++
				w.plies[ply].cutoffs++
				if P.SETriple != 0 && !isPVNode {
					sfGravity(&w.History.TTMoveHist, 918, sfPawnLimit)
				}
				if excludedMove == types.MoveNone && (ply > 0 || len(w.ExcludedRootMoves) == 0) {
					w.TT.StoreEval(w.Board.Hash, ttMove, tt.ScoreToTT(score, ply), netEval, depth, tt.LowerBound|pvBit)
				}

				if !inCheck && excludedMove == types.MoveNone && score > rawEval && score < MateThreshold {
					diff := score - rawEval
					if P.FailSoft&softCorr == 0 {
						diff = min(score, beta) - rawEval
					}
					w.History.UpdateCorrHist(w.Board, diff, depth)
				}

				if ply == 0 {
					bestMove = ttMove
					w.PVArray[0][0] = ttMove
					w.PVLength[0] = 1
				}

				if P.HistUpdate != 0 {
					w.updateStatsSF(&hc, ttMove, nil, depth, true, isPVNode, inCheck, ply, prevMove)
				} else if !ttMove.IsCapture() && !ttMove.IsPromotion() {
					w.History.UpdateKillers(ply, ttMove)
					hc.Update(w.Board.Pieces[ttMove.From()], ttMove, quietStatBonus(depth))
					w.History.UpdateCounterMove(prevMove, ttMove)
				} else if ttMove.IsCapture() {
					attacker := w.Board.Pieces[ttMove.From()].Type()
					victim := w.Board.Pieces[ttMove.To()].Type()
					if ttMove.IsEnPassant() {
						victim = types.Pawn
					}
					w.History.UpdateCaptureHistory(attacker, victim, ttMove.To(), depth)
				}
				if P.FailSoft&softMoves == 0 {
					return beta
				}
				return score
			}

			bestScore = score
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

	// 8. Staged Move Generation & Search Loop
	// One picker per ply, reused: nothing below this point searches at this
	// ply again before the move loop is done (the singular and null-move
	// verification searches come earlier).
	// Compute enemy king ring and our attack pressure only for nodes that enter the move loop.
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

	mp := &w.pickers[ply]
	mp.reset(w.Board, ttMove, excludedMove, ply, prevMove, prevMove2, prevMove4, w.History)
	mp.stage = StageGenCaptures
	mp.SetNodeInfo(threatenedPieces, kingInDanger, hasActiveAttack, themKingRing)
	mp.checkSq = ci.CheckSq
	mp.hasCheckSq = true
	// The picker gets its own copy, kept with it in the worker: pointing it
	// at the local would move that local to the heap at every node.
	w.pickerHC[ply] = hc
	mp.hc = &w.pickerHC[ply]
	mp.depth = int(depth)

	// P.SMPDefer: a move another thread is searching here right now is put
	// off until the picker has nothing else, then searched as usual (by
	// then the other thread's result is often in the hash table). Moves this
	// node searches are marked for the other threads to see.
	deferOK := w.busy != nil && int32(depth) >= P.SMPDeferDepth && excludedMove == types.MoveNone && (ply > 0 || w.ID != 0)
	deferN, deferNext := 0, 0
	pickerDone := false

	for {
		var m types.Move
		var moveSEE int32
		var seeKnown bool
		if !pickerDone {
			m = mp.Next()
			pickerDone = m == types.MoveNone
		}
		if pickerDone {
			if deferNext >= deferN {
				break
			}
			d := &w.deferred[ply][deferNext]
			deferNext++
			m, moveSEE, seeKnown = d.m, d.see, d.seeKnown
		} else {
			if (m == ttMove && !deferTTAtRoot) || m == excludedMove {
				continue
			}
			if ply == 0 && w.isRootExcluded(m) {
				continue
			}
			// SEE: captures and promotions carry the value the picker
			// already computed; quiet moves get one only if a rule below
			// asks for it.
			moveSEE, seeKnown = mp.LastSEE()
			if deferOK && movesSearched > 0 && deferN < maxDeferred && w.busy.busy(busyKey(w.Board.Hash, m)) {
				w.deferred[ply][deferN] = deferredMove{m: m, see: moveSEE, seeKnown: seeKnown}
				deferN++
				st.SMPDeferred++
				continue
			}
		}
		fromSq := m.From()
		toSq := m.To()
		isQuiet := !m.IsCapture() && !m.IsPromotion()
		killer1 := mp.killer1
		killer2 := mp.killer2
		counterMove := mp.counterMove
		movedPiece := w.Board.Pieces[fromSq]
		attacker := movedPiece.Type()
		victim := w.Board.Pieces[toSq].Type()
		if m.IsEnPassant() {
			victim = types.Pawn
		}
		// givesCheck is read by the pruning rules, LMR and the check
		// extension, so it is computed for every move.
		givesCheck := w.Board.GivesCheckWith(m, &ci)

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
		// Moves the pruning rules leave alone: killers and the countermove,
		// and (PruneExempt) threats, threat evasions and advanced pawn pushes.
		pruneExempt := m == killer1 || m == killer2 || m == counterMove ||
			(P.PruneExempt != 0 && (isThreatMove || isThreatEvasion || isAdvancedPawnPush))
		isKingThreat := threatenedPieces == 0 && IsKingAttack(w.Board, m)
		// P.PruneKing: a quiet move that attacks the squares round the enemy
		// king is not pruned either (1: only with an attack already under
		// way, 2: always). A mating attack a queen down is otherwise cut
		// off at its quiet move, where every static rule says "hopeless".
		if isQuiet && isKingThreat && (P.PruneKing == 2 || (P.PruneKing == 1 && hasActiveAttack)) {
			pruneExempt = true
		}
		// 3: as 1, and also when a piece of the mover's own is under threat
		// (after a sacrifice one usually is).
		if P.PruneKing == 3 && isQuiet && hasActiveAttack && (isKingThreat || (threatenedPieces != 0 && IsKingAttack(w.Board, m))) {
			pruneExempt = true
		}
		isKingZoneMove := (bitboard.SquareBB[toSq] & themKingRing) != 0

		// Late Move Pruning (LMP)
		if !isPVNode && !inCheck && int32(depth) <= P.LMPDepth && isQuiet && movesSearched > 0 && !pruneExempt {
			if !givesCheck {
				lmpThreshold := 3 + int(P.LMPMultImpr)*int(depth)*int(depth)
				if !improving {
					lmpThreshold = (3 + int(P.LMPMultNonImp)*int(depth)*int(depth)) / 2
				}
				if legalMoves >= lmpThreshold {
					st.LMPPruned++
					movesPruned = true
					continue
				}
			}
		}

		// Futility Pruning
		if P.HistExtra&4 != 0 && P.HistUpdate != 0 && isQuiet && !isPVNode && movesSearched > 0 {
			// A quiet move's history moves the depth the pruning rules
			// below see it at: later for good moves, earlier for bad ones.
			d := int32(min(depth, 16))
			h := hc.Prune(movedPiece, m) + 69*int32(*hc.mainEntry(m))/32
			lmrDepth = max(lmrDepth+h/(3000+7*(d-8)*(d-8)), 0)
		}
		if !isPVNode && !inCheck && lmrDepth <= P.FutDepth && isQuiet && movesSearched > 0 && !pruneExempt {
			futMargin := P.FutBase + P.FutMargin*max(lmrDepth, 1)
			if staticEval+futMargin <= alpha {
				if !givesCheck {
					st.FutPruned++
					movesPruned = true
					// The pruned move is assumed to reach no more than this.
					if bestScore < staticEval+futMargin {
						bestScore = staticEval + futMargin
					}
					continue
				}
			}
		}

		// History Pruning
		// Prune quiet moves with poor history score at shallow depths
		if !isPVNode && !inCheck && lmrDepth <= P.HistDepth && isQuiet && movesSearched > 0 && !pruneExempt {
			if !givesCheck {
				histScore := hc.Prune(movedPiece, m)
				histMargin := -max(lmrDepth, 1) * P.HistPrImpr
				if !improving {
					histMargin = -max(lmrDepth, 1) * P.HistPrNonImp
				}
				if P.HistUpdate != 0 {
					histMargin = -max(lmrDepth, 1) * 4136 // Stockfish's, for its history scale
				}
				if histScore < histMargin {
					st.HistPruned++
					movesPruned = true
					continue
				}
			}
		}

		// SEE Pruning
		// Do not prune tactical/killer moves, countermoves, advanced pawn pushes, or moves that give check
		if !isPVNode && (!inCheck || (P.CheckSEEPrune != 0 && bestScore > -MateThreshold)) && int32(depth) <= P.SEEDepth && movesSearched > 0 && !pruneExempt {
			if !givesCheck {
				if !seeKnown {
					moveSEE, seeKnown = eval.SEE(w.Board, m), true
				}
				if isQuiet {
					if moveSEE < -max(lmrDepth, 1)*P.SEEQuietMul {
						st.SEEPruned++
						movesPruned = true
						continue
					}
				} else {
					if moveSEE < -int32(depth)*P.SEENoisyMul {
						st.SEEPruned++
						movesPruned = true
						continue
					}
				}
			}
		}

		w.prefetchNext(w.Board.HashAfter(m)) // before the move is made: see HashAfter
		w.pushAccumulator(ply, m)
		w.History.PushMove(ply, movedPiece, toSq)
		w.pushMoveInfo(ply, &hc, m, movesSearched+1)
		var busyK uint64 // P.SMPDefer: this move's mark, taken before the move changes the hash
		if deferOK {
			busyK = busyKey(w.Board.Hash, m)
		}
		if !w.makeMove(m, &ci, inCheck) {
			continue
		}
		if busyK != 0 {
			w.busy.mark(busyK)
		}

		legalMoves++
		st.MovesSearched++
		searchedMoves[movesSearched] = m
		var score int32

		var startNodes uint64
		if ply == 0 {
			startNodes = w.LocalNodes
		}

		// The singular extension belongs to the TT move only; here 7th-rank
		// pushes and checks that don't lose material extend (at most one ply).
		var moveExtension int8
		if is7thRankPawnPush && depth >= 3 && (P.PawnPushExt == 2 || (P.PawnPushExt == 1 && isPVNode)) {
			moveExtension = 1
		} else if givesCheck && (P.CheckExt == 2 || (P.CheckExt == 1 && isPVNode)) {
			if !seeKnown {
				moveSEE, seeKnown = eval.SEE(w.Board, m), true
			}
			if moveSEE >= -P.CheckExtSEE {
				st.CheckExts++
				moveExtension = 1
			}
		}

		var reduction int8
		// Principal Variation Search & LMR
		if movesSearched == 0 {
			// First move: full window (extended if 7th rank pawn push)
			score = -w.Search(depth-1+moveExtension, -beta, -alpha, ply+1, isPVNode, !isPVNode && !cutNode, types.MoveNone, m, prevMove, prevMove2)
		} else if P.LMRMode == 1 {
			// Stockfish's late-move reductions: see sfReduction.
			newDepth := int32(depth) - 1 + int32(moveExtension)
			r := sfReduction(improving, int(depth), movesSearched+1)
			if isPVNode {
				r -= 512 + 1004
			}
			if ttPv {
				r += 929 - 3023
				if ttFound && ttScore > alpha {
					r -= 885
				}
				if ttFound && ttDepth >= depth {
					r -= 816
					if cutNode {
						r -= 940
					}
				}
			}
			r += 697 - 65*int32(movesSearched+1) - corr*15
			if cutNode {
				r += 4026
				if ttMove == types.MoveNone {
					r += 933
				}
			}
			if ttMoveNoisy {
				r += 1079
			}
			if ply+1 < MaxPly {
				if c := w.plies[ply+1].cutoffs; c > 1 {
					r += 264
					if c > 2 {
						r += 1095
					}
					if !isPVNode && !cutNode {
						r += 1138
					}
				}
			}
			if P.HistUpdate != 0 && ply < MaxPly {
				r -= w.plies[ply].statScore * 439 / 4096
			}
			if isQuiet && !inCheck && abs(alpha) < MateThreshold {
				// In Stockfish's units, three to the centipawn.
				r += 3 * max(-64, min(3*(alpha-staticEval), 96))
			}
			if !isPVNode && !cutNode {
				r += r * 276 / (256*int32(depth) + 268)
			}

			if depth >= 2 {
				// A negative reduction extends, by up to two plies, unless
				// the line is already far deeper than the root search.
				ext := int32(0)
				if ply < 2*w.rootDepth {
					ext = 2
				}
				d := max(1, newDepth+min(-r/1024, ext))
				if isPVNode {
					d++
				}
				if d < newDepth {
					reduction = int8(newDepth - d)
					st.LMRSearches++
					st.LMRPlies += uint64(reduction)
				}
				st.LMREligible++
				score = -w.Search(int8(d), -alpha-1, -alpha, ply+1, false, true, types.MoveNone, m, prevMove, prevMove2)

				if score > alpha && !w.TimeMan.Stopped.Load() {
					// The full-depth search goes a ply deeper if the reduced
					// one did well and a ply shallower if it barely passed
					// (53 and 8 in Stockfish's units).
					if d < newDepth && score > bestScore+18 {
						newDepth++
					}
					if score < bestScore+3 {
						newDepth--
					}
					if newDepth > d {
						st.LMRReSearches++
						score = -w.Search(int8(newDepth), -alpha-1, -alpha, ply+1, false, !cutNode, types.MoveNone, m, prevMove, prevMove2)
					}
					if P.HistUpdate != 0 && isQuiet {
						hc.updateContSF(movedPiece, toSq, 1334, inCheck)
					}
				}
			} else {
				if ttMove == types.MoveNone {
					r += 1127
				}
				if r > 5234 {
					newDepth--
				}
				if r > 5487 && newDepth > 2 {
					newDepth--
				}
				score = -w.Search(int8(newDepth), -alpha-1, -alpha, ply+1, false, !cutNode, types.MoveNone, m, prevMove, prevMove2)
			}

			if isPVNode && score > alpha && !w.TimeMan.Stopped.Load() {
				st.PVSReSearches++
				score = -w.Search(int8(newDepth), -beta, -alpha, ply+1, true, false, types.MoveNone, m, prevMove, prevMove2)
			}
		} else {
			// Late Move Reductions (LMR)
			if depth >= 3 && movesSearched >= int(P.LMRMinMove) && (!inCheck || P.CheckLMR != 0) {
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
						reduction++ // Non-PV nodes reduce more
						if P.TTPV != 0 && ttPv {
							reduction-- // unless the position has been on a PV
						}
					}
					if cutNode {
						reduction += int8(P.LMRCutNode) // and expected cut nodes more still
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

					// Advanced pawn pushes, threat evasions and tactical threats:
					// not reduced (LMRExempt), or one ply less.
					lmrExempt := isAdvancedPawnPush || is7thRankPawnPush || isThreatEvasion || isThreatMove
					if lmrExempt && P.LMRExempt == 0 {
						reduction--
					} else if !lmrExempt && (isKingThreat || (hasActiveAttack && isKingZoneMove)) {
						reduction--
					}

					// History-based reduction adjustment
					hist := hc.Stat(movedPiece, m)
					// Continuous history adjustment: one ply per LMRHistDiv
					// of combined history, capped at three plies each way.
					hAdj := hist / P.LMRHistDiv
					if P.HistUpdate != 0 {
						hAdj = hist * 439 / 4096 / 1024 // Stockfish's, for its history scale
					}
					if hAdj > 3 {
						hAdj = 3
					} else if hAdj < -3 {
						hAdj = -3
					}
					reduction -= int8(hAdj)

					if ttMoveNoisy {
						reduction++
					}
					if lmrExempt && P.LMRExempt != 0 {
						reduction = 0
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
						if P.HistUpdate != 0 {
							// Stockfish's, for its history scale: the piece
							// taken counts as well as the capture history.
							reduction -= int8((873*sfPieceValue[victim]/128 + capHist) * 439 / 4096 / 1024)
						} else if capHist < 0 {
							reduction++
						} else if capHist > 2000 {
							reduction--
						}
					} else {
						reduction = 0
					}
				}

				if inCheck {
					reduction-- // evasions: a ply less than other late moves
				}

				// Checks: not reduced at all, or (LMRChecks) one ply less than
				// another move in their place.
				if givesCheck {
					if P.LMRChecks == 0 {
						reduction = 0
					} else {
						reduction--
					}
				}

				// One ply less per 68 cp of evaluation correction (rounded),
				// as Stockfish's |correction| / 26310 in 1024ths of a ply.
				reduction -= int8(min((corr*15+512)/1024, 2))

				if reduction < 0 {
					reduction = 0
				}
				if reduction > depth-2 {
					reduction = depth - 2
				}

				st.LMREligible++
				if reduction == 0 {
					switch {
					case LMRTable[dIdx][mIdx] == 0:
						st.LMRTableZero++
					case givesCheck && P.LMRChecks == 0:
						st.LMRNoCheck++
					case !isQuiet:
						st.LMRNoCapture++
					case isThreatEvasion || isThreatMove:
						st.LMRNoThreat++
					case isAdvancedPawnPush || is7thRankPawnPush:
						st.LMRNoPawn++
					default:
						st.LMRNoAdjust++
					}
				}
			}

			// In-tree diversity for LazySMP helpers: vary late-move reductions
			// slightly by worker ID so threads explore different subtrees.
			if w.ID > 0 && depth >= 4 && movesSearched >= 3 && reduction > 0 {
				switch w.ID & 3 {
				case 1:
					reduction--
				case 3:
					if reduction < depth-2 {
						reduction++
					}
				}
			}

			// Null-window search with reduction
			if reduction > 0 {
				st.LMRSearches++
				st.LMRPlies += uint64(reduction)
			}
			if ply < MaxPly {
				w.plies[ply].reduction = int32(reduction) // read by the node below (P.Hindsight)
			}
			score = -w.Search(depth-1+moveExtension-reduction, -alpha-1, -alpha, ply+1, false, reduction > 0 || !cutNode, types.MoveNone, m, prevMove, prevMove2)
			if ply < MaxPly {
				w.plies[ply].reduction = 0
			}

			// If reduced search fails high, re-search at full depth with null window
			if reduction > 0 && score > alpha && !w.TimeMan.Stopped.Load() {
				full := depth - 1 + moveExtension
				if P.LMRMode == 2 {
					// As Stockfish: a ply deeper if the reduced search did
					// well, a ply shallower if it barely passed.
					if score > bestScore+18 {
						full++
					}
					if score < bestScore+3 {
						full--
					}
					if P.HistUpdate != 0 && isQuiet {
						hc.updateContSF(movedPiece, toSq, 1334, inCheck)
					}
				}
				if full > depth-1+moveExtension-reduction {
					st.LMRReSearches++
					score = -w.Search(full, -alpha-1, -alpha, ply+1, false, !cutNode, types.MoveNone, m, prevMove, prevMove2)
				}
			}

			// If null-window fails high and in PV node, re-search with full PV window
			if isPVNode && score > alpha && !w.TimeMan.Stopped.Load() {
				st.PVSReSearches++
				score = -w.Search(depth-1+moveExtension, -beta, -alpha, ply+1, true, false, types.MoveNone, m, prevMove, prevMove2)
			}
		}

		w.Board.UnmakeMove(m)
		if busyK != 0 {
			w.busy.unmark(busyK)
		}
		movesSearched++

		if ply == 0 {
			w.RootMoveNodes[m.From()][m.To()] += w.LocalNodes - startNodes
			// Only a score above alpha ranks a move. Below it a fail-soft
			// score is just an upper bound, and picking the largest bound
			// would swap the best move for an arbitrary one.
			if bestRootMove == types.MoveNone || (score > bestRootScore && score > alpha) {
				bestRootScore = score
				bestRootMove = m
			}
		}

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		// Beta Cutoff (Fail-High)
		if score >= beta {
			st.NodesWithMoves++
			st.BetaCuts++
			w.plies[ply].cutoffs++
			if P.SETriple != 0 && !isPVNode {
				sfGravity(&w.History.TTMoveHist, -747, sfPawnLimit)
			}
			if movesSearched == 1 {
				st.BetaCutsFirst++
			}
			st.BetaCutIndex += uint64(movesSearched - 1)
			if excludedMove == types.MoveNone && (ply > 0 || len(w.ExcludedRootMoves) == 0) {
				w.TT.StoreEval(w.Board.Hash, m, tt.ScoreToTT(score, ply), netEval, depth, tt.LowerBound|pvBit)
			}

			if !inCheck && excludedMove == types.MoveNone && score > rawEval && score < MateThreshold {
				diff := score - rawEval
				if P.FailSoft&softCorr == 0 {
					diff = min(score, beta) - rawEval
				}
				w.History.UpdateCorrHist(w.Board, diff, depth)
			}

			if ply == 0 {
				bestMove = m
				w.PVArray[0][0] = m
				w.PVLength[0] = 1
			}

			if P.HistUpdate != 0 {
				w.updateStatsSF(&hc, m, searchedMoves[:movesSearched-1], depth, m == ttMove, isPVNode, inCheck, ply, prevMove)
				if P.FailSoft&softMoves == 0 {
					return beta
				}
				return score
			}
			if isQuiet {
				w.History.UpdateKillers(ply, m)
				bonus := quietStatBonus(depth)
				hc.Update(movedPiece, m, bonus)
				w.History.UpdateCounterMove(prevMove, m)

				// Penalize preceding quiet moves that failed to produce a cutoff
				for j := 0; j < movesSearched-1; j++ {
					prevQuiet := searchedMoves[j]
					if !prevQuiet.IsCapture() && !prevQuiet.IsPromotion() {
						hc.Update(w.Board.Pieces[prevQuiet.From()], prevQuiet, -bonus)
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

			if P.FailSoft&softMoves == 0 {
				return beta
			}
			return score
		}

		if score > bestScore {
			bestScore = score
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

	if P.HistExtra&2 != 0 && P.HistUpdate != 0 && bestType == tt.Exact && bestMove != types.MoveNone && movesSearched > 0 {
		w.updateStatsSF(&hc, bestMove, searchedMoves[:movesSearched], depth, bestMove == ttMove, isPVNode, inCheck, ply, prevMove)
	}
	if movesSearched > 0 {
		st.NodesWithMoves++
		if bestType == tt.UpperBound {
			st.AllNodes++
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

	if P.HistUpdate != 0 && bestType == tt.UpperBound && ply > 0 && prevMove != types.MoveNone {
		w.rewardPriorMove(ply, depth, bestScore, alpha, rawEval, inCheck)
	}

	// An upper bound only says the eval was too high when it sits below rawEval
	// Below alpha the best score found is only a bound on the moves that were
	// searched. With moves pruned unsearched it says nothing about those, so
	// the node then claims no more than alpha (unless softUnguarded).
	if bestScore < alpha && (bestScore == -Infinity || P.FailSoft&softMoves == 0 ||
		(movesPruned && P.FailSoft&softUnguarded == 0)) {
		bestScore = alpha
	}
	corrScore := bestScore
	if P.FailSoft&softCorr == 0 && corrScore < alpha {
		corrScore = alpha
	}
	if !inCheck && excludedMove == types.MoveNone && corrScore < MateThreshold && corrScore > -MateThreshold &&
		(bestType == tt.Exact || corrScore < rawEval) {
		diff := corrScore - rawEval
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
		// A node that fails low under a principal-variation position is
		// one the PV may yet pass through.
		if bestType == tt.UpperBound && ply > 0 && w.plies[ply-1].ttPv {
			pvBit = tt.PVFlag
		}
		w.TT.StoreEval(w.Board.Hash, bestMove, tt.ScoreToTT(bestScore, ply), netEval, depth, bestType|pvBit)
	}
	return bestScore
}

func (w *SearchWorker) Quiescence(alpha, beta int32, ply int) int32 {
	return w.quiescence(alpha, beta, ply, 0)
}

// quiescence searches captures (and, at its first ply when P.QSChecks is set,
// quiet checks that don't hang the checking piece). qply counts plies since
// the main search handed over.
func (w *SearchWorker) quiescence(alpha, beta int32, ply, qply int) int32 {
	if w.TimeMan.Stopped.Load() {
		return 0
	}

	w.LocalNodes++
	if w.LocalNodes&1023 == 0 && w.checkNodeLimits() {
		return 0
	}

	if int8(ply) > w.SelDepth {
		w.SelDepth = int8(ply)
	}

	if ply >= MaxPly-1 {
		return w.Evaluate(ply)
	}

	// Draw detection in quiescence
	if w.Board.IsRepetition() || w.Board.IsFiftyMoveDraw() || w.Board.IsInsufficientMaterial() {
		return w.drawScore()
	}

	if draw := w.drawScore(); P.Cycle != 0 && alpha < draw && w.Board.UpcomingRepetition() {
		alpha = draw
		if alpha >= beta {
			return alpha
		}
	}

	st := &w.Stats
	st.QNodes++

	// TT probe: any stored bound is at least as deep as a quiescence search.
	// Stores go in at depth 0; tt.Store never lets those displace a current-generation entry.
	origAlpha := alpha
	ttMove, ttScore, ttEval, _, ttType, ttFound := w.TT.ProbeEval(w.Board.Hash)
	pvBit := ttType & tt.PVFlag
	ttType &= tt.BoundMask
	netEval := tt.EvalNone
	if !ttFound {
		ttMove = types.MoveNone
	}
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
	bestScore := int32(-Infinity)
	if !inCheck {
		if ttFound && ttEval != tt.EvalNone {
			netEval = ttEval
		} else {
			netEval = w.rawEval(ply)
		}
		standPat := w.adjustEval(netEval)
		futBase = standPat + P.QSFutMargin
		bestScore = standPat
		if standPat >= beta {
			st.QStandPatCuts++
			if P.FailSoft&softQS == 0 {
				return beta
			}
			return standPat
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

	ci := w.Board.NewCheckInfo()
	qsChecks := !inCheck && qply == 0 && P.QSChecks != 0
	sfOrder := P.MoveOrder >= 2
	legalMoves := 0
	var bestMove types.Move

	// Try the TT move before generating and scoring all quiescence moves.
	var triedTTMove types.Move
	if ttMove != types.MoveNone && (inCheck || ttMove.IsCapture() || ttMove.IsPromotion() || (qsChecks && w.Board.GivesCheckWith(ttMove, &ci))) && movegen.IsPseudoLegal(w.Board, ttMove) {
		if sfOrder {
			w.History.PushMove(ply, w.Board.Pieces[ttMove.From()], ttMove.To())
		}
		w.prefetchNext(w.Board.HashAfter(ttMove))
		w.pushAccumulator(ply, ttMove)
		if w.makeMove(ttMove, &ci, inCheck) {
			triedTTMove = ttMove
			legalMoves++
			st.QMoves++
			score := -w.quiescence(-beta, -alpha, ply+1, qply+1)
			w.Board.UnmakeMove(ttMove)
			if w.TimeMan.Stopped.Load() {
				return 0
			}
			if score >= beta {
				st.QBetaCuts++
				st.QBetaFirst++
				if P.FailSoft&softQS == 0 {
					return beta
				}
				return score
			}
			if score > bestScore {
				bestScore = score
				if score > alpha {
					alpha = score
					bestMove = ttMove
				}
			}
		}
	}

	ml := &w.qMoves[ply]
	movegen.GenerateMoves(w.Board, !inCheck && !qsChecks, ml)

	var checkers bitboard.Bitboard
	if inCheck {
		checkers = w.Board.Checkers(w.Board.SideToMove)
	}

	// P.MoveOrder 2 orders the moves as Stockfish's quiescence does: the hash
	// move first; captures by capture history plus seven times the value of
	// the piece taken; in check, captures by the piece taken and then the
	// other evasions by main and continuation history. (Queen promotions
	// keep their place at the front when not in check, and the first-ply
	// quiet checks theirs at the back.)
	scores := &w.qScores[ply]
	if sfOrder {
		w.scoreQuiescenceSF(ml, scores, ttMove, ply, inCheck)
	} else {
		clear(scores[:ml.Count]) // quiet evasions keep a score of 0
	}
	for i := 0; i < ml.Count && !sfOrder; i++ {
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

	for i := 0; i < ml.Count; i++ {
		m := PickNextMove(ml, scores[:ml.Count], i)
		if m == triedTTMove {
			continue
		}
		// An underpromotion is rarely better than the queen (P.PromoOrder).
		if !inCheck && P.PromoOrder != 0 && isUnderPromotion(m) {
			continue
		}

		// Quiet moves are only generated for the first-ply checks.
		if qsChecks && !m.IsCapture() && !m.IsPromotion() {
			if !w.Board.GivesCheckWith(m, &ci) || !eval.SEEGe(w.Board, m, 0) {
				continue
			}
		}

		// Per-capture futility pruning: if even winning the captured piece
		// plus a margin cannot lift the stand-pat score above alpha, the
		// capture is futile (non-promotions that don't give check only).
		if !inCheck && m.IsCapture() && !m.IsPromotion() {
			victim := w.Board.Pieces[m.To()].Type()
			if m.IsEnPassant() {
				victim = types.Pawn
			}
			if futValue := futBase + eval.SEEPieceValues[victim]; futValue <= alpha && !w.Board.GivesCheckWith(m, &ci) {
				st.QFutPruned++
				if bestScore < futValue {
					bestScore = futValue
				}
				continue
			}
		}

		if !inCheck && m.IsCapture() && !m.IsPromotion() {
			// Far below alpha, a capture has to win material to matter.
			if P.QSEvenPrune != 0 && futBase <= alpha && !eval.SEEGe(w.Board, m, 1) && !w.Board.GivesCheckWith(m, &ci) {
				st.QFutPruned++
				if bestScore < futBase {
					bestScore = futBase
				}
				continue
			}
			// The captures are ordered by victim: after the first few, the
			// rest rarely change the result.
			if P.QSMaxMoves != 0 && legalMoves >= int(P.QSMaxMoves) && !w.Board.GivesCheckWith(m, &ci) {
				st.QFutPruned++
				continue
			}
		}

		// In check, once one evasion is known not to be mated: quiet evasions
		// beyond the second move and captures that lose material are skipped.
		if inCheck && P.QSEvasionPrune != 0 && legalMoves > 0 && bestScore > -MateThreshold {
			if !m.IsCapture() && !m.IsPromotion() {
				if legalMoves >= 2 {
					continue
				}
			} else if !eval.SEEGe(w.Board, m, 0) {
				continue
			}
		}

		// SEE pruning in quiescence: skip losing captures if not in check and move does not give check
		// SEE first: GivesCheck then only runs for the losing captures.
		if !inCheck && !eval.SEEGe(w.Board, m, 0) && !w.Board.GivesCheckWith(m, &ci) {
			st.QSEEPruned++
			continue
		}

		if sfOrder {
			// For the continuation history of the evasions below.
			w.History.PushMove(ply, w.Board.Pieces[m.From()], m.To())
		}
		w.prefetchNext(w.Board.HashAfter(m)) // before the move is made: see HashAfter
		w.pushAccumulator(ply, m)
		if !w.makeMove(m, &ci, inCheck) {
			continue
		}

		legalMoves++
		st.QMoves++
		score := -w.quiescence(-beta, -alpha, ply+1, qply+1)
		w.Board.UnmakeMove(m)

		if w.TimeMan.Stopped.Load() {
			return 0
		}

		if score >= beta {
			st.QBetaCuts++
			if legalMoves == 1 {
				st.QBetaFirst++
			}
			if !ttFound {
				w.TT.StoreEval(w.Board.Hash, m, tt.ScoreToTT(score, ply), netEval, 0, tt.LowerBound|pvBit)
			}
			if P.FailSoft&softQS == 0 {
				return beta
			}
			return score
		}
		if score > bestScore {
			bestScore = score
		}
		if score > alpha {
			alpha = score
			bestMove = m
		}
	}

	if inCheck && legalMoves == 0 {
		return -MateScore + int32(ply)
	}

	if P.FailSoft&softQS == 0 && bestScore < alpha {
		bestScore = alpha
	}
	if !ttFound {
		bound := tt.UpperBound
		if bestScore > origAlpha {
			bound = tt.Exact
		}
		w.TT.StoreEval(w.Board.Hash, bestMove, tt.ScoreToTT(bestScore, ply), netEval, 0, bound|pvBit)
	}
	return bestScore
}

// updateStatsSF is the history update after best caused a cutoff, as
// Stockfish does it (P.HistUpdate 1): best earns a bonus that grows with
// depth and with the number of moves tried before it; the quiet moves tried
// before it lose a penalty that shrinks down the list, and the captures
// tried before it a fixed one. tried holds the moves searched before best.
func (w *SearchWorker) updateStatsSF(hc *histCtx, best types.Move, tried []types.Move, depth int8, isTTMove, isPV, inCheck bool, ply int, prevMove types.Move) {
	b := w.Board
	captureOf := func(m types.Move) (types.PieceType, types.PieceType) {
		victim := b.Pieces[m.To()].Type()
		if m.IsEnPassant() {
			victim = types.Pawn
		}
		return b.Pieces[m.From()].Type(), victim
	}
	// The move that led here, if any (see plyInfo).
	var prior *plyInfo
	if ply > 0 && ply <= MaxPly && prevMove != types.MoveNone {
		prior = &w.plies[ply-1]
	}
	bonus := sfStatBonus(depth, isTTMove, isPV, len(tried))
	malus := sfStatMalus(depth)

	// The previous move was a quiet move tried early (first, or first after
	// the hash move) and has just been refuted: an extra penalty for it.
	if prior != nil && prior.quiet() {
		early := int32(1)
		if prior.ttHit {
			early = 2
		}
		if prior.moveCount == early {
			c := w.History.contCtx(ply - 1)
			c.updateContSF(prior.piece, prior.to, -malus*713/1024, prior.inCheck)
		}
	}

	if !best.IsCapture() && !best.IsPromotion() {
		w.History.UpdateKillers(ply, best)
		w.History.UpdateCounterMove(prevMove, best)
		hc.UpdateSF(b.Pieces[best.From()], best, bonus*899/1024, inCheck)
		step := malus * 1159 / 1024
		for _, m := range tried {
			if m != best && !m.IsCapture() && !m.IsPromotion() {
				step = step * 921 / 1024
				hc.UpdateSF(b.Pieces[m.From()], m, -step, inCheck)
			}
		}
	} else if best.IsCapture() {
		attacker, victim := captureOf(best)
		w.History.UpdateCaptureSF(attacker, victim, best.To(), bonus*1427/1024)
	}
	for _, m := range tried {
		if m != best && m.IsCapture() {
			attacker, victim := captureOf(m)
			w.History.UpdateCaptureSF(attacker, victim, m.To(), -malus*1489/1024)
		}
	}
}

// rewardHashCutoff is called when the hash table alone cuts a node off with
// a lower bound: its quiet move gains history as if it had been searched,
// and a quiet previous move that was among the first four tried loses some
// (Stockfish).
func (w *SearchWorker) rewardHashCutoff(ply int, depth int8, ttMove, prevMove types.Move) {
	b := w.Board
	if ttMove != types.MoveNone && !ttMove.IsCapture() && !ttMove.IsPromotion() && movegen.IsPseudoLegal(b, ttMove) {
		c := w.History.Context(b, ply, b.TacticalThreatsOnPieces(b.SideToMove))
		c.UpdateSF(b.Pieces[ttMove.From()], ttMove, 131*int32(depth), false)
	}
	if ply > 0 && ply <= MaxPly && prevMove != types.MoveNone {
		if p := &w.plies[ply-1]; p.quiet() && p.moveCount < 5 {
			c := w.History.contCtx(ply - 1)
			c.updateContSF(p.piece, p.to, -2210, p.inCheck)
		}
	}
}

// drawScore is the score of a drawn position: zero, or under P.DrawJitter
// one centipawn either way depending on the node count, as in Stockfish.
// Scoring every draw exactly alike lets the search treat different
// repetitions as interchangeable and miss the way out of one.
func (w *SearchWorker) drawScore() int32 {
	if P.DrawJitter == 0 {
		return 0
	}
	return int32(w.LocalNodes&2) - 1
}

// prefetchNext starts loading what the node with this hash will read first:
// its hash-table bucket and its entry in the evaluation cache. Both tables
// are far larger than the CPU caches, so without this each read waits on
// main memory.
func (w *SearchWorker) prefetchNext(hash uint64) {
	w.TT.Prefetch(hash)
	tt.PrefetchAddr(unsafe.Pointer(&w.EvalCache[hash&(evalCacheSize-1)]))
}

// makeMove plays m, skipping the test for the mover's own king being left in
// check when the position's check information shows it cannot be (the mover
// is not in check, and the piece is neither the king nor pinned). It returns
// false if the move turned out illegal.
func (w *SearchWorker) makeMove(m types.Move, ci *board.CheckInfo, inCheck bool) bool {
	if ci.SkipsKingTest(w.Board, m, inCheck) {
		return w.Board.MakeMoveUnchecked(m)
	}
	return w.Board.MakeMove(m)
}

// plyInfo is what a main-search node records for the nodes below it
// (P.HistUpdate 1): about itself, and about the move it is searching.
type plyInfo struct {
	inCheck, ttHit bool
	ttPv           bool  // the position is or has been on a principal variation
	reduction      int32 // plies the move being searched is reduced by, while its reduced search runs
	cutoffs        int32 // beta cutoffs at this ply since the node two plies up was entered
	eval           int32 // static evaluation, before the hash score replaces it (unset in check)

	moveCount int32 // the move's place among the moves searched, from 1 (0: a ProbCut try)
	statScore int32 // its history score, as late-move reductions use it
	piece     types.Piece
	to        types.Square
	captured  types.PieceType
	promotion bool
	main      *int16 // its main history entry
}

func (p *plyInfo) quiet() bool { return p.captured == types.PieceTypeNone && !p.promotion }

// pushMoveInfo records m, about to be played at ply, for the node below.
func (w *SearchWorker) pushMoveInfo(ply int, hc *histCtx, m types.Move, moveCount int) {
	if P.HistUpdate == 0 || ply >= MaxPly {
		return
	}
	b := w.Board
	p := &w.plies[ply]
	p.moveCount = int32(moveCount)
	p.piece, p.to = b.Pieces[m.From()], m.To()
	p.captured = b.Pieces[m.To()].Type()
	if m.IsEnPassant() {
		p.captured = types.Pawn
	}
	p.promotion = m.IsPromotion()
	p.main = hc.mainEntry(m)
	if p.quiet() {
		p.statScore = hc.Stat(p.piece, m)
	} else {
		p.statScore = 873*sfPieceValue[p.captured]/128 + w.History.CaptureHistory[p.piece.Type()][p.to][p.captured]
	}
}

// rewardPriorMove is called when no move at this node reached alpha, which
// makes the move that led here a good one: its history goes up, as in
// Stockfish. The bonus is larger when that move was tried late, had a poor
// history, or the result is well below what either side's static evaluation
// promised. A prior capture gets a fixed bonus instead.
func (w *SearchWorker) rewardPriorMove(ply int, depth int8, bestScore, alpha, eval int32, inCheck bool) {
	p := &w.plies[ply-1]
	if p.promotion {
		return
	}
	if p.captured != types.PieceTypeNone {
		w.History.UpdateCaptureSF(p.piece.Type(), p.captured, p.to, 892)
		return
	}
	if bestScore == -Infinity {
		bestScore = alpha
	}
	// Stockfish's margins are 106 and 68 in its units, three to the centipawn.
	scale := -241 - p.statScore/98 + min(59*int32(depth), 420)
	if p.moveCount > 9 {
		scale += 186
	}
	if !inCheck && bestScore <= eval-35 {
		scale += 142
	}
	if !p.inCheck && bestScore <= -p.eval-23 {
		scale += 159
	}
	if scale <= 0 {
		return
	}
	bonus := min(150*int32(depth)-85, 1337) * scale

	c := w.History.contCtx(ply - 1)
	c.updateContSF(p.piece, p.to, bonus*263/16384, p.inCheck)
	sfGravity(p.main, bonus*215/32768, sfMainLimit)
	if p.piece.Type() != types.Pawn {
		sfGravity(&w.History.PawnHist[w.Board.PawnHash%pawnHistSize][p.piece][p.to], bonus*324/8192, sfPawnLimit)
	}
}

// sfReductions is Stockfish's table: 2872/128 * ln(i).
var sfReductions = func() (t [256]int32) {
	for i := 1; i < len(t); i++ {
		t[i] = int32(2872.0 / 128.0 * math.Log(float64(i)))
	}
	return
}()

// sfReduction is the base late-move reduction under P.LMRMode 1, in 1024ths
// of a ply, for the moveCount-th move at depth. The search then adjusts it:
// less at PV nodes, for moves with good history and for early moves; more at
// expected cut nodes, when the hash move is a capture, when the node below
// keeps cutting off and when the evaluation is below alpha. Only the whole
// plies of the total are applied. A move whose total is negative is searched
// up to two plies deeper than the first move.
func sfReduction(improving bool, depth, moveCount int) int32 {
	scale := sfReductions[min(depth, 255)] * sfReductions[min(moveCount, 255)]
	r := scale + 948
	if !improving {
		r += scale * 197 / 512
	}
	return r
}

// scoreQuiescenceSF scores the moves of a quiescence node for P.MoveOrder 2.
func (w *SearchWorker) scoreQuiescenceSF(ml *movegen.MoveList, scores *[256]int32, ttMove types.Move, ply int, inCheck bool) {
	const (
		first    = 1 << 30
		captures = 1 << 28
		last     = -(1 << 28)
	)
	b := w.Board
	var hc histCtx
	if inCheck {
		hc = w.History.Context(b, ply, 0)
	}
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		tactical := m.IsCapture() || m.IsPromotion()
		switch {
		case m == ttMove:
			scores[i] = first
		case inCheck && !tactical:
			scores[i] = hc.Evasion(b.Pieces[m.From()], m)
		case !tactical:
			scores[i] = last
		default:
			attacker := b.Pieces[m.From()].Type()
			victim := b.Pieces[m.To()].Type()
			if m.IsEnPassant() {
				victim = types.Pawn
			}
			switch {
			case inCheck:
				scores[i] = captures + sfPieceValue[victim]
			case m.PromotionType() == types.Queen && m.IsPromotion():
				scores[i] = captures + eval.SEE(b, m)
			default:
				scores[i] = w.History.CaptureHistory[attacker][m.To()][victim] + 7*sfPieceValue[victim]
			}
		}
	}
}

func abs(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
