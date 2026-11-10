package search

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/book"
	"devilfish/internal/eval"
	"devilfish/internal/logger"
	"devilfish/internal/movegen"
	"devilfish/internal/syzygy"
	"devilfish/internal/tt"
	"devilfish/internal/types"
)

type SearchInfo struct {
	Depth    int8
	SelDepth int8
	Score    int32
	IsMate   bool
	MateIn   int
	Nodes    uint64
	NPS      uint64
	TimeMs   int64
	HashFull int
	TBHits   uint64
	PV       []types.Move
	MultiPV  int
}

type Engine struct {
	TT         *tt.TT
	NumThreads int
	MultiPV    int
	Book       *book.Book
	UseBook    bool
	Stopped    atomic.Bool
	Nodes      atomic.Uint64
	TBHits     atomic.Uint64
	currentTM  atomic.Pointer[TimeManager]
	searchMu   sync.Mutex
}

func NewEngine(ttSizeMB int, numThreads int) *Engine {
	if numThreads < 1 {
		numThreads = 1
	}
	return &Engine{
		TT:         tt.New(ttSizeMB),
		NumThreads: numThreads,
		MultiPV:    1,
		Book:       book.DefaultBook(),
		UseBook:    false,
	}
}

func (e *Engine) SetBook(bk *book.Book) {
	e.Book = bk
}

func (e *Engine) SetUseBook(use bool) {
	e.UseBook = use
}

func (e *Engine) SetMultiPV(n int) {
	if n < 1 {
		n = 1
	}
	e.MultiPV = n
}

func (e *Engine) SetThreads(n int) {
	if n < 1 {
		n = 1
	}
	e.NumThreads = n
}

func (e *Engine) SetHash(sizeMB int) {
	e.TT.Resize(sizeMB)
}

func (e *Engine) Stop() {
	e.Stopped.Store(true)
}

func (e *Engine) PonderHit() {
	if tm := e.currentTM.Load(); tm != nil {
		tm.PonderHit()
	}
}

func (e *Engine) Search(
	b *board.Board,
	limits SearchLimits,
	infoCallback func(SearchInfo),
) (types.Move, types.Move) {
	e.searchMu.Lock()
	defer e.searchMu.Unlock()

	e.Stopped.Store(false)
	e.Nodes.Store(0)
	e.TBHits.Store(0)
	e.TT.IncrementAge()

	// Opening book probe
	if e.UseBook && e.Book != nil && !limits.Infinite && !limits.Ponder && e.MultiPV <= 1 {
		if bookMove := e.Book.Probe(b); bookMove != types.MoveNone {
			var ponderMove types.Move
			nextB := b.Clone()
			if nextB.MakeMove(bookMove) {
				ponderMove = e.Book.Probe(nextB)
			}
			pv := []types.Move{bookMove}
			if ponderMove != types.MoveNone {
				pv = append(pv, ponderMove)
			}
			if infoCallback != nil {
				infoCallback(SearchInfo{
					Depth:    1,
					SelDepth: 1,
					Score:    15,
					Nodes:    1,
					PV:       pv,
				})
			}
			logger.Infof("Opening Book Hit: %s (ponder: %s)", bookMove, ponderMove)
			return bookMove, ponderMove
		}
	}

	// Syzygy root probe
	if e.MultiPV <= 1 && syzygy.IsAvailable() && b.Castling == 0 && bitboard.Count(b.Occupied()) <= syzygy.MaxPieces() {
		rootMove, wdl, dtz, ok := syzygy.ProbeRoot(b)
		if ok && rootMove != types.MoveNone {
			e.TBHits.Add(1)
			var rootScore int32
			switch wdl {
			case syzygy.Win:
				rootScore = MateScore - 100 - int32(dtz)
			case syzygy.CursedWin:
				if syzygy.Use50MoveRule {
					rootScore = 0
				} else {
					rootScore = MateScore - 100 - int32(dtz)
				}
			case syzygy.Draw:
				rootScore = 0
			case syzygy.BlessedLoss:
				if syzygy.Use50MoveRule {
					rootScore = 0
				} else {
					rootScore = -MateScore + 100 + int32(dtz)
				}
			case syzygy.Loss:
				rootScore = -MateScore + 100 + int32(dtz)
			}

			isMate := false
			mateIn := 0
			if rootScore > MateThreshold {
				isMate = true
				mateIn = int((MateScore - rootScore + 1) / 2)
			} else if rootScore < -MateThreshold {
				isMate = true
				mateIn = -int((MateScore + rootScore + 1) / 2)
			}

			if infoCallback != nil {
				infoCallback(SearchInfo{
					Depth:    1,
					SelDepth: 1,
					Score:    rootScore,
					IsMate:   isMate,
					MateIn:   mateIn,
					Nodes:    1,
					NPS:      0,
					TimeMs:   0,
					HashFull: e.TT.HashFull(),
					TBHits:   1,
					PV:       []types.Move{rootMove},
				})
			}

			logger.Infof("Syzygy Root Probe Hit: Move=%s, WDL=%d, DTZ=%d, Score=%d, IsMate=%t, MateIn=%d", rootMove, wdl, dtz, rootScore, isMate, mateIn)
			e.TT.Store(b.Hash, rootMove, tt.ScoreToTT(rootScore, 0), 127, tt.Exact)
			return rootMove, types.MoveNone
		}
	}

	// Instant forced-move return: if only 1 legal move exists, play immediately
	legalMoves := movegen.GenerateLegalMoves(b)
	if len(legalMoves) == 0 {
		return types.MoveNone, types.MoveNone
	}
	if len(legalMoves) == 1 && !limits.Infinite && !limits.Ponder && limits.MoveTime == 0 && limits.MaxDepth == 0 && limits.MaxNodes == 0 {
		forcedMove := legalMoves[0]
		logger.Infof("Only 1 legal move available: %s (instant return)", forcedMove)
		if infoCallback != nil {
			info := SearchInfo{
				Depth:    1,
				SelDepth: 1,
				Score:    0,
				Nodes:    1,
				PV:       []types.Move{forcedMove},
			}
			if e.MultiPV > 1 {
				info.MultiPV = 1
			}
			infoCallback(info)
		}
		return forcedMove, types.MoveNone
	}

	tm := NewTimeManager(limits, b.SideToMove, &e.Stopped, &e.Nodes, b)
	e.currentTM.Store(tm)
	defer e.currentTM.Store(nil)

	logger.Infof("Search started | FEN: %s | Limits: depth=%d, nodes=%d, movetime=%v, wtime=%d, btime=%d, winc=%d, binc=%d",
		b.ToFEN(), limits.MaxDepth, limits.MaxNodes, limits.MoveTime, limits.WhiteTime, limits.BlackTime, limits.WhiteInc, limits.BlackInc)
	logger.Profilef("TimeManager Allocation: OptTime=%v, MaxTime=%v, IsEmergency=%t, IsTimeTrouble=%t",
		tm.OptTime, tm.MaxTime, tm.IsEmergency, tm.IsTimeTrouble)

	maxDepth := limits.MaxDepth
	if maxDepth <= 0 || maxDepth > 64 {
		maxDepth = 64
	}

	var rootBestMove types.Move
	var rootPonderMove types.Move
	var rootScore int32

	// Initialize rootBestMove sensibly from TT or best tactical move
	if ttM, _, _, _, found := e.TT.Probe(b.Hash); found && ttM != types.MoveNone && isLegalMove(b, ttM) {
		rootBestMove = ttM
	} else if len(legalMoves) > 0 {
		bestIdx := 0
		bestVal := int32(-1000000)
		for i, m := range legalMoves {
			val := int32(0)
			if m.IsCapture() {
				val = 10000 + eval.SEE(b, m)
			} else if b.GivesCheck(m) {
				val = 5000
			}
			if val > bestVal {
				bestVal = val
				bestIdx = i
			}
		}
		rootBestMove = legalMoves[bestIdx]
	}

	// Master worker
	master := NewWorker(0, b, e.TT, tm, &e.Nodes, &e.TBHits)

	// WaitGroup for helper threads
	var wg sync.WaitGroup
	var helpers []*SearchWorker

	// Start helper workers
	for id := 1; id < e.NumThreads; id++ {
		wg.Add(1)
		helper := NewWorker(id, b, e.TT, tm, &e.Nodes, &e.TBHits)
		helpers = append(helpers, helper)
		go func(workerID int, w *SearchWorker) {
			defer wg.Done()
			// Helper searches with depth variation
			for d := int8(1 + (workerID % 2)); d <= maxDepth; d++ {
				if e.Stopped.Load() {
					break
				}
				w.Search(d, -Infinity, Infinity, 0, true, true, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
			}
		}(id, helper)
	}

	startTime := time.Now()
	var prevIterationScore int32
	var prevIterationBestMove types.Move
	var bestMoveStability int
	var rootPV []types.Move

	multiPVCount := e.MultiPV
	if multiPVCount > len(legalMoves) {
		multiPVCount = len(legalMoves)
	}
	if multiPVCount < 1 {
		multiPVCount = 1
	}

	if multiPVCount > 1 {
		// MultiPV iterative deepening loop
		for depth := int8(1); depth <= maxDepth; depth++ {
			if e.Stopped.Load() {
				break
			}

			iterStartTime := time.Now()
			master.RootMoveNodes = [64][64]uint64{}
			excludedRootMoves := make([]types.Move, 0, multiPVCount)

			for pvIdx := 0; pvIdx < multiPVCount; pvIdx++ {
				if e.Stopped.Load() {
					break
				}
				master.ExcludedRootMoves = excludedRootMoves

				score := master.Search(depth, -Infinity, Infinity, 0, true, true, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
				if e.Stopped.Load() && depth > 1 {
					break
				}

				pvMove := master.PVArray[0][0]
				if pvMove == types.MoveNone || !isLegalMove(b, pvMove) || master.isRootExcluded(pvMove) {
					for _, lm := range legalMoves {
						if !master.isRootExcluded(lm) {
							pvMove = lm
							break
						}
					}
				}

				if pvMove != types.MoveNone {
					excludedRootMoves = append(excludedRootMoves, pvMove)
					if pvIdx == 0 {
						rootBestMove = pvMove
						rootScore = score
					}
				}

				pv := e.extractPV(b, master, depth, pvMove)
				if len(pv) == 0 && pvMove != types.MoveNone {
					pv = []types.Move{pvMove}
				}
				if pvIdx == 0 {
					rootPV = pv
					if len(pv) >= 2 {
						rootPonderMove = pv[1]
					} else {
						rootPonderMove = types.MoveNone
					}
				}

				elapsed := time.Since(startTime)
				totalNodes := e.Nodes.Load() + (master.LocalNodes % 2048)
				var nps uint64
				if elapsed.Seconds() > 0 {
					nps = uint64(float64(totalNodes) / elapsed.Seconds())
				}

				isMate := false
				mateIn := 0
				if score > MateThreshold {
					isMate = true
					mateIn = int((MateScore - score + 1) / 2)
				} else if score < -MateThreshold {
					isMate = true
					mateIn = -int((MateScore + score + 1) / 2)
				}

				if infoCallback != nil {
					infoCallback(SearchInfo{
						Depth:    depth,
						SelDepth: master.SelDepth,
						Score:    score,
						IsMate:   isMate,
						MateIn:   mateIn,
						Nodes:    totalNodes,
						NPS:      nps,
						TimeMs:   elapsed.Milliseconds(),
						HashFull: e.TT.HashFull(),
						TBHits:   e.TBHits.Load(),
						PV:       pv,
						MultiPV:  pvIdx + 1,
					})
				}
			}

			iterDuration := time.Since(iterStartTime)
			if !tm.CanStartNewIteration(iterDuration, false) {
				break
			}
		}
	} else {
		// Master iterative deepening loop (Single PV)
		for depth := int8(1); depth <= maxDepth; depth++ {
			if e.Stopped.Load() {
				break
			}

			iterStartTime := time.Now()
			master.RootMoveNodes = [64][64]uint64{}

			var score int32
			if depth < 4 {
				score = master.Search(depth, -Infinity, Infinity, 0, true, true, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
			} else {
				// Aspiration Windows
				delta := int32(30)
				alpha := rootScore - delta
				beta := rootScore + delta
				if alpha < -Infinity {
					alpha = -Infinity
				}
				if beta > Infinity {
					beta = Infinity
				}

				failedAttempts := 0
				for {
					score = master.Search(depth, alpha, beta, 0, true, true, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)

					if e.Stopped.Load() {
						break
					}

					if score <= alpha {
						beta = (alpha + beta) / 2
						alpha = score - delta
						if alpha < -Infinity {
							alpha = -Infinity
						}
						failedAttempts++
					} else if score >= beta {
						beta = score + delta
						if beta > Infinity {
							beta = Infinity
						}
						failedAttempts++
					} else {
						break
					}

					delta += delta / 2
					if failedAttempts >= 2 || delta >= 300 {
						alpha = -Infinity
						beta = Infinity
					}
				}
			}

			if e.Stopped.Load() {
				break
			}

			// Ensure we extract a valid move
			bestMoveCandidate := master.PVArray[0][0]
			if bestMoveCandidate == types.MoveNone || !isLegalMove(b, bestMoveCandidate) {
				ttM, _, _, _, found := e.TT.Probe(b.Hash)
				if found && ttM != types.MoveNone && isLegalMove(b, ttM) {
					bestMoveCandidate = ttM
				} else {
					bestMoveCandidate = types.MoveNone
				}
			}

			if bestMoveCandidate != types.MoveNone {
				rootBestMove = bestMoveCandidate
				rootScore = score
			}

			// Construct PV line from TT / PVArray
			pv := e.extractPV(b, master, depth, rootBestMove)
			if len(pv) > 0 {
				rootPV = pv
			} else if len(rootPV) > 0 {
				pv = rootPV
			} else if rootBestMove != types.MoveNone {
				pv = []types.Move{rootBestMove}
				if rootPonderMove != types.MoveNone {
					pv = append(pv, rootPonderMove)
				}
			}

			if len(pv) > 0 && rootBestMove == types.MoveNone {
				rootBestMove = pv[0]
			}
			if len(pv) >= 2 {
				rootPonderMove = pv[1]
			} else {
				rootPonderMove = types.MoveNone
			}

			elapsed := time.Since(startTime)
			elapsedMs := elapsed.Milliseconds()
			totalNodes := e.Nodes.Load() + (master.LocalNodes % 2048)

			var nps uint64
			if elapsed.Seconds() > 0 {
				nps = uint64(float64(totalNodes) / elapsed.Seconds())
			}

			// Calculate mate distance
			isMate := false
			mateIn := 0
			if score > MateThreshold {
				isMate = true
				mateIn = int((MateScore - score + 1) / 2)
			} else if score < -MateThreshold {
				isMate = true
				mateIn = -int((MateScore + score + 1) / 2)
			}

			if infoCallback != nil {
				infoCallback(SearchInfo{
					Depth:    depth,
					SelDepth: master.SelDepth,
					Score:    score,
					IsMate:   isMate,
					MateIn:   mateIn,
					Nodes:    totalNodes,
					NPS:      nps,
					TimeMs:   elapsedMs,
					HashFull: e.TT.HashFull(),
					TBHits:   e.TBHits.Load(),
					PV:       pv,
				})
			}

			logger.Profilef("Iteration depth=%d seldepth=%d: Score=%d (IsMate=%t, MateIn=%d), Nodes=%d, NPS=%d, Elapsed=%v, HashFull=%d/1000, TBHits=%d, PV=%v",
				depth, master.SelDepth, score, isMate, mateIn, totalNodes, nps, elapsed, e.TT.HashFull(), e.TBHits.Load(), pv)

			// Check if we found checkmate: exit immediately without burning extra clock
			if isMate && mateIn > 0 {
				if !tm.Ponder.Load() {
					break
				}
			}

			// Dynamic Time Management: Best move stability, node dominance & fail-low recovery
			isStable := false
			if depth >= 5 && rootBestMove != types.MoveNone {
				if rootBestMove == prevIterationBestMove {
					bestMoveStability++
					isStable = true

					// Emergency early exit: ONLY in severe time trouble (<2.5s) to prevent flagging
					if !tm.Infinite && !tm.Ponder.Load() && !tm.IsMoveTime && tm.IsEmergency && bestMoveStability >= 2 && depth >= 8 {
						break
					}

					// High-confidence early exit:
					// Require high stability (>= 4 iterations), adequate depth (>= 12 in endgame, >= 10 otherwise),
					// and that the engine has already consumed >= 75% of its allocated OptTime.
					minExitDepth := int8(10)
					if tm.IsEndgame {
						minExitDepth = 12
					}
					elapsed := time.Since(startTime)
					if !tm.Infinite && !tm.Ponder.Load() && !tm.IsMoveTime &&
						bestMoveStability >= 4 && depth >= minExitDepth &&
						elapsed >= time.Duration(float64(tm.OptTime)*0.75) {
						break
					}

					if bestMoveStability >= 2 {
						tm.AdjustOptTime(0.85)
					}
					if bestMoveStability >= 4 {
						tm.AdjustOptTime(0.75)
					}
				} else {
					bestMoveStability = 0
					isStable = false
				}

				// Node-based time management: evaluate dominance of root move
				rootNodes := master.RootMoveNodes[rootBestMove.From()][rootBestMove.To()]
				if master.LocalNodes > 0 {
					nodeRatio := float64(rootNodes) / float64(master.LocalNodes)
					elapsed := time.Since(startTime)
					minDomDepth := int8(10)
					if tm.IsEndgame {
						minDomDepth = 12
					}
					if nodeRatio >= 0.90 && depth >= minDomDepth && bestMoveStability >= 2 && elapsed >= time.Duration(float64(tm.OptTime)*0.70) {
						// Overwhelming dominance after thorough search: safe to finish
						if !tm.Infinite && !tm.Ponder.Load() && !tm.IsMoveTime {
							break
						}
					} else if nodeRatio >= 0.70 {
						tm.AdjustOptTime(0.85)
					} else if nodeRatio < 0.35 && !tm.IsTimeTrouble {
						// Disputed position: extend allocated time slightly
						tm.AdjustOptTime(1.20)
					}
				}

				// If the score dropped significantly from the previous iteration, extend time to recover (only if not in time trouble)
				if !tm.IsTimeTrouble && prevIterationScore != 0 && score < prevIterationScore-40 {
					isStable = false
					tm.AdjustOptTime(1.25)
				}
			}
			prevIterationBestMove = rootBestMove
			prevIterationScore = rootScore

			iterDuration := time.Since(iterStartTime)
			if !tm.CanStartNewIteration(iterDuration, isStable) {
				break
			}
		}
	}

	// Stop helper threads
	e.Stopped.Store(true)
	wg.Wait()

	// Fallback move if no move was found or if rootBestMove is not legal
	if rootBestMove == types.MoveNone || !isLegalMove(b, rootBestMove) {
		legalMoves := movegen.GenerateLegalMoves(b)
		if len(legalMoves) > 0 {
			bestIdx := 0
			bestVal := int32(-1000000)
			for i, m := range legalMoves {
				val := int32(0)
				if m.IsCapture() {
					val = 10000 + eval.SEE(b, m)
				} else if b.GivesCheck(m) {
					val = 5000
				}
				if val > bestVal {
					bestVal = val
					bestIdx = i
				}
			}
			rootBestMove = legalMoves[bestIdx]
		}
	}

	// Validate rootPonderMove is strictly legal after rootBestMove
	if rootPonderMove != types.MoveNone {
		tempB := b.Clone()
		if tempB.MakeMove(rootBestMove) {
			if !isLegalMove(tempB, rootPonderMove) {
				rootPonderMove = types.MoveNone
			}
		} else {
			rootPonderMove = types.MoveNone
		}
	}

	totalNodes := e.Nodes.Load() + (master.LocalNodes % 2048)
	totalElapsed := time.Since(startTime)
	var finalNps uint64
	if totalElapsed.Seconds() > 0 {
		finalNps = uint64(float64(totalNodes) / totalElapsed.Seconds())
	}
	logger.Profilef("Search completed: BestMove=%s, PonderMove=%s, Score=%d, TotalNodes=%d, Elapsed=%v, NPS=%d, TBHits=%d",
		rootBestMove, rootPonderMove, rootScore, totalNodes, totalElapsed, finalNps, e.TBHits.Load())

	totalNnueEvals := master.NnueEvals
	totalNnueUpdates := master.NnueUpdates
	totalNnueRefreshes := master.NnueRefreshes
	totalClassicalEvals := master.ClassicalEvals

	for _, h := range helpers {
		totalNnueEvals += h.NnueEvals
		totalNnueUpdates += h.NnueUpdates
		totalNnueRefreshes += h.NnueRefreshes
		totalClassicalEvals += h.ClassicalEvals
	}

	evalPct := 0.0
	if totalNodes > 0 {
		evalPct = float64(totalNnueEvals) / float64(totalNodes) * 100.0
	}
	incHitRate := 0.0
	totAccumOps := totalNnueUpdates + totalNnueRefreshes
	if totAccumOps > 0 {
		incHitRate = float64(totalNnueUpdates) / float64(totAccumOps) * 100.0
	}
	logger.Profilef("NNUE Statistics: Evals=%d (%.1f%% of nodes), ClassicalEvals=%d, Accumulator Updates=%d, Accumulator Refreshes=%d, Incremental Hit Rate=%.2f%%",
		totalNnueEvals, evalPct, totalClassicalEvals, totalNnueUpdates, totalNnueRefreshes, incHitRate)

	return rootBestMove, rootPonderMove
}

func (e *Engine) extractPV(b *board.Board, master *SearchWorker, depth int8, rootMove types.Move) []types.Move {
	var pv []types.Move
	visited := make(map[uint64]bool)
	tempB := b.Clone()

	if rootMove != types.MoveNone && isLegalMove(tempB, rootMove) {
		visited[tempB.Hash] = true
		if tempB.MakeMove(rootMove) {
			pv = append(pv, rootMove)
		}
	}

	for d := len(pv); d < int(depth); d++ {
		hash := tempB.Hash
		if visited[hash] {
			break
		}
		visited[hash] = true

		m, _, _, _, found := e.TT.Probe(hash)
		if !found || m == types.MoveNone || !isLegalMove(tempB, m) {
			// Fallback to PVArray
			if d < master.PVLength[0] {
				m = master.PVArray[0][d]
			} else {
				break
			}
		}

		if m == types.MoveNone || !isLegalMove(tempB, m) {
			break
		}

		if !tempB.MakeMove(m) {
			break
		}
		pv = append(pv, m)
	}

	// Fallback to master PVArray if TT-based extraction yielded no moves
	if len(pv) == 0 && master.PVLength[0] > 0 {
		tempB2 := b.Clone()
		for d := 0; d < master.PVLength[0]; d++ {
			m := master.PVArray[0][d]
			if m == types.MoveNone || !isLegalMove(tempB2, m) || !tempB2.MakeMove(m) {
				break
			}
			pv = append(pv, m)
		}
	}

	return pv
}

func isLegalMove(b *board.Board, m types.Move) bool {
	if m == types.MoveNone {
		return false
	}
	legalMoves := movegen.GenerateLegalMoves(b)
	for _, lm := range legalMoves {
		if lm == m {
			return true
		}
	}
	return false
}

func FormatInfo(info SearchInfo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("info depth %d seldepth %d ", info.Depth, info.SelDepth))

	if info.MultiPV > 0 {
		sb.WriteString(fmt.Sprintf("multipv %d ", info.MultiPV))
	}

	if info.IsMate {
		sb.WriteString(fmt.Sprintf("score mate %d ", info.MateIn))
	} else {
		sb.WriteString(fmt.Sprintf("score cp %d ", info.Score))
	}

	sb.WriteString(fmt.Sprintf("nodes %d nps %d time %d",
		info.Nodes, info.NPS, info.TimeMs))
	if info.TBHits > 0 {
		sb.WriteString(fmt.Sprintf(" tbhits %d", info.TBHits))
	}
	sb.WriteString(fmt.Sprintf(" hashfull %d pv", info.HashFull))

	for _, m := range info.PV {
		sb.WriteString(" ")
		sb.WriteString(m.String())
	}

	return sb.String()
}
