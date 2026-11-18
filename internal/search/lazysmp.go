package search

import (
	"fmt"
	"runtime"
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
	"devilfish/internal/nnue"
	"devilfish/internal/syzygy"
	"devilfish/internal/threadprio"
	"devilfish/internal/tt"
	"devilfish/internal/types"
)

type SearchInfo struct {
	Depth     int8
	SelDepth  int8
	Score     int32
	IsMate    bool
	MateIn    int
	Nodes     uint64
	NPS       uint64
	TimeMs    int64
	HashFull  int
	TBHits    uint64
	PV        []types.Move
	MultiPV   int
	StringMsg string
	// WDL is the win, draw and loss chances in thousandths for the side to
	// move, printed after the score when HasWDL is set (UCI_ShowWDL).
	WDL    [3]int
	HasWDL bool
}

type Engine struct {
	TT         *tt.TT
	NumThreads int
	MultiPV    int
	Book       *book.Book
	UseBook    bool
	mainDepth  atomic.Int32 // the main thread's last finished depth, for the helpers
	_pad0      [60]byte
	Stopped    atomic.Bool
	_pad1      [63]byte
	Nodes      atomic.Uint64
	_pad2      [56]byte
	TBHits     atomic.Uint64
	_pad3      [56]byte
	currentTM  atomic.Pointer[TimeManager]
	// ponderHit records a ponderhit, so that one arriving before the search
	// has set up its clock (currentTM still nil) is not lost.
	ponderHit atomic.Bool
	searchMu  sync.Mutex

	// workers are kept between searches so their large tables are allocated
	// once. Only Search uses them, under searchMu, and it waits for every
	// helper before returning.
	workers      []*SearchWorker
	helperChs    []chan func()
	evalCache    []atomic.Uint64
	evalCacheNet *nnue.EightNetwork

	busy        *busyTable                  // P.SMPDefer: moves being searched, shared by the threads (see smp.go)
	helperBest  atomic.Uint64               // P.SMPAdopt: the deepest iteration a helper finished (packHelperResult)
	depthCount  [MaxPly]atomic.Int32        // dynamic depth gating: helpers currently on each depth
	multiPVExcl atomic.Pointer[[]types.Move] // MultiPV: snapshot of root moves already found for current depth
}

// worker returns the worker for thread id, ready for a new search of b.
// stabilityScale is the time factor by number of consecutive iterations the
// best move has held: a fresh best move earns extra time, a long-standing one
// gives some back.
var stabilityScale = [...]float64{1.30, 1.15, 1.00, 0.92, 0.85, 0.80}

func (e *Engine) worker(id int, b *board.Board, tm *TimeManager) *SearchWorker {
	if id == 0 {
		if e.evalCache == nil {
			e.evalCache = make([]atomic.Uint64, evalCacheSize)
		}
		if net := eightNet(); net != e.evalCacheNet {
			clear(e.evalCache)
			e.evalCacheNet = net
		}
		if len(e.workers) > e.NumThreads {
			// The thread count went down: let the surplus workers and their
			// parked helper goroutines go.
			for i := e.NumThreads; i < len(e.helperChs); i++ {
				if e.helperChs[i] != nil {
					close(e.helperChs[i])
					e.helperChs[i] = nil
				}
			}
			if e.NumThreads < len(e.helperChs) {
				e.helperChs = e.helperChs[:e.NumThreads]
			}
			clear(e.workers[e.NumThreads:])
			e.workers = e.workers[:e.NumThreads]
		}
	}
	for len(e.workers) <= id {
		e.workers = append(e.workers, nil)
	}
	if w := e.workers[id]; w != nil {
		w.EvalCache = e.evalCache
		w.cacheNet = e.evalCacheNet
		w.resetFor(b, e.TT, tm, &e.Nodes, &e.TBHits)
		return w
	}
	w := NewWorker(id, b, e.TT, tm, &e.Nodes, &e.TBHits)
	w.EvalCache = e.evalCache
	w.cacheNet = e.evalCacheNet
	e.workers[id] = w
	return w
}

func (e *Engine) helperChan(id int) chan func() {
	for len(e.helperChs) <= id {
		e.helperChs = append(e.helperChs, nil)
	}
	if e.helperChs[id] == nil {
		ch := make(chan func(), 1)
		e.helperChs[id] = ch
		go func() {
			for fn := range ch {
				fn()
			}
		}()
	}
	return e.helperChs[id]
}

// Close stops all persistent helper worker goroutines.
func (e *Engine) Close() {
	e.searchMu.Lock()
	defer e.searchMu.Unlock()
	for i, ch := range e.helperChs {
		if ch != nil {
			close(ch)
			e.helperChs[i] = nil
		}
	}
	e.helperChs = nil
}

func (e *Engine) searchNodes(maxNodes uint64, master *SearchWorker) uint64 {
	if maxNodes > 0 {
		return e.Nodes.Load() + (master.LocalNodes % 1024)
	}
	total := master.LocalNodes
	for i := 1; i < e.NumThreads && i < len(e.workers); i++ {
		if w := e.workers[i]; w != nil {
			total += w.sharedNodes.Load()
		}
	}
	return total
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

// NewGame forgets everything learned from earlier positions: the
// transposition table and every worker's history tables.
func (e *Engine) NewGame() {
	e.searchMu.Lock()
	defer e.searchMu.Unlock()
	e.TT.Clear()
	for _, w := range e.workers {
		if w != nil {
			w.History.Clear()
		}
	}
}

// Stats returns the search statistics of the last search, summed over the
// worker threads.
func (e *Engine) Stats() SearchStats {
	e.searchMu.Lock()
	defer e.searchMu.Unlock()
	var total SearchStats
	for _, w := range e.workers {
		if w != nil {
			total.Add(&w.Stats)
		}
	}
	return total
}

func (e *Engine) Stop() {
	e.Stopped.Store(true)
}

// PonderHit switches the pondering search to a normal timed search. It may be
// called at any moment after the search was requested, including before
// Search has got as far as creating its TimeManager.
func (e *Engine) PonderHit() {
	e.ponderHit.Store(true)
	if tm := e.currentTM.Load(); tm != nil {
		tm.PonderHit()
	}
}

// ClearPonderHit forgets an earlier ponderhit. Call it before requesting a
// new search, from the goroutine that delivers ponderhits.
func (e *Engine) ClearPonderHit() {
	e.ponderHit.Store(false)
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

	// Identify opening if available
	opening := book.FindOpening(b)
	if opening.Name != "" {
		logger.Infof("Opening: %s", opening)
		if infoCallback != nil {
			infoCallback(SearchInfo{
				StringMsg: fmt.Sprintf("Opening: %s", opening),
			})
		}
	}

	// Opening book probe
	if e.UseBook && e.Book != nil && !limits.Infinite && !limits.Ponder && e.MultiPV <= 1 {
		if bookMove := e.Book.Probe(b); bookMove != types.MoveNone {
			var ponderMove types.Move
			nextB := b.Clone()
			if nextB.MakeMove(bookMove) {
				ponderMove = e.Book.Probe(nextB)
				if opening.Name == "" {
					if opAfter := book.FindOpening(nextB); opAfter.Name != "" {
						opening = opAfter
						logger.Infof("Opening: %s", opening)
						if infoCallback != nil {
							infoCallback(SearchInfo{
								StringMsg: fmt.Sprintf("Opening: %s", opening),
							})
						}
					}
				}
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
			if opening.Name != "" {
				logger.Infof("Opening: %s | Book Hit: %s (ponder: %s)", opening, bookMove, ponderMove)
			} else {
				logger.Infof("Opening Book Hit: %s (ponder: %s)", bookMove, ponderMove)
			}
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

			wdlStr := syzygy.WDLToString(wdl)
			logger.Infof("Syzygy tablebase determined best position: %s (%s, DTZ: %d, Score: %d, MateIn: %d)", rootMove, wdlStr, dtz, rootScore, mateIn)

			if infoCallback != nil {
				infoCallback(SearchInfo{
					StringMsg: fmt.Sprintf("Syzygy tablebase determined best position: %s (%s, DTZ %d)", rootMove, wdlStr, dtz),
				})
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
	tm.LowPriority = limits.LowPriority && limits.Ponder && threadprio.Supported()
	e.currentTM.Store(tm)
	defer e.currentTM.Store(nil)
	// A ponderhit that arrived before the line above found no TimeManager
	// to tell. (One arriving after it reaches tm directly.)
	if limits.Ponder && e.ponderHit.Load() {
		tm.PonderHit()
	}

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
	var rootAvg rootAverage // drives the optimism term

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
	master := e.worker(0, b, tm)
	if tm.LowPriority {
		master.enterPriority()
		defer master.leavePriority()
	}

	// Thread coordination (smp.go). Marks left by an earlier search, cut
	// off between marking a move and unmarking it, are cleared.
	e.helperBest.Store(0)
	e.multiPVExcl.Store(nil)
	for i := 0; i <= int(maxDepth) && i < len(e.depthCount); i++ {
		e.depthCount[i].Store(0)
	}
	multiPVCount := e.MultiPV
	if multiPVCount > len(legalMoves) {
		multiPVCount = len(legalMoves)
	}
	if multiPVCount < 1 {
		multiPVCount = 1
	}
	var busy *busyTable
	if e.NumThreads > 1 && P.SMPDefer != 0 {
		if e.busy == nil {
			e.busy = newBusyTable()
		} else {
			e.busy.clear()
		}
		busy = e.busy
	}
	master.busy = busy

	// WaitGroup for helper threads
	var wg sync.WaitGroup
	var helpers []*SearchWorker

	// Start helper workers on persistent goroutines
	for id := 1; id < e.NumThreads; id++ {
		wg.Add(1)
		helper := e.worker(id, b, tm)
		helper.busy = busy
		helpers = append(helpers, helper)
		workerID, w := id, helper
		e.helperChan(id) <- func() {
			defer func() {
				w.sharedNodes.Store(w.LocalNodes)
				wg.Done()
			}()
			if tm.LowPriority {
				w.enterPriority()
				defer w.leavePriority()
			}
			// Helper searches with depth variation
			w.done = threadResult{}
			prev := int32(0)
			last := int8(0) // this helper's last finished depth
			for d := int8(1 + (workerID % 2)); d <= maxDepth; d++ {
				if e.Stopped.Load() {
					break
				}
				// P.SMPMode: threads that search the same depth at the same
				// time mostly repeat each other, because none has stored its
				// results yet when the others need them. So the helpers are
				// spread over the depth the main thread is working on and
				// the ones after it (1: every other helper one depth ahead;
				// 2: a third of them two ahead as well), and never search a
				// depth the main thread has already finished. What they
				// store is waiting in the hash table when the main thread
				// gets there.
				if P.SMPMode != 0 {
					if P.SMPMode == 2 {
						d = min(max(int8(e.mainDepth.Load())+1+int8(workerID%3), last+1), maxDepth)
					} else {
						d = max(int8(e.mainDepth.Load())+1, last+1)
						if e.NumThreads > 2 {
							limit := int32(1 + (workerID & 1))
							for d < maxDepth && e.depthCount[d].Load() >= limit {
								d++
							}
						}
						if d > maxDepth {
							d = maxDepth
						}
					}
					if d <= last {
						break // everything up to the depth limit is done
					}
				}
				if multiPVCount > 1 && (workerID&1) == 1 {
					if excl := e.multiPVExcl.Load(); excl != nil {
						w.ExcludedRootMoves = *excl
					} else {
						w.ExcludedRootMoves = nil
					}
				} else {
					w.ExcludedRootMoves = nil
				}
				e.depthCount[d].Add(1)
				score := w.helperIteration(d, prev)
				e.depthCount[d].Add(-1)
				prev = score
				if !e.Stopped.Load() {
					last = d
				}
				// Only a finished iteration counts in the vote for the
				// move to play (pickThread).
				if !e.Stopped.Load() && w.PVLength[0] > 0 {
					w.done = threadResult{depth: d, score: score, move: w.PVArray[0][0]}
					if w.PVLength[0] > 1 {
						w.done.ponder = w.PVArray[0][1]
					}
					if P.SMPAdopt != 0 {
						e.publishHelperResult(d, score)
					}
				}
			}
		}
	}

	startTime := time.Now()
	var prevIterationScore int32
	var prevIterationBestMove types.Move
	var bestMoveStability int
	var bestMoveChanges float64
	var masterDepth int8 // the main thread's last finished iteration
	e.mainDepth.Store(0)
	var rootPV []types.Move

	if multiPVCount > 1 {
		// MultiPV iterative deepening loop
		for depth := int8(1); depth <= maxDepth; depth++ {
			if e.Stopped.Load() {
				break
			}

			iterStartTime := time.Now()
			master.RootMoveNodes = [64][64]uint64{}
			excludedRootMoves := make([]types.Move, 0, multiPVCount)
			e.multiPVExcl.Store(nil)

			for pvIdx := 0; pvIdx < multiPVCount; pvIdx++ {
				if e.Stopped.Load() {
					break
				}
				master.ExcludedRootMoves = excludedRootMoves

				score := master.Search(depth, -Infinity, Infinity, 0, true, false, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
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
					snap := append([]types.Move(nil), excludedRootMoves...)
					e.multiPVExcl.Store(&snap)
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
				totalNodes := e.searchNodes(limits.MaxNodes, master)
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
			// P.SMPAdopt: a depth a helper has already finished is not
			// searched again; its result is in the hash table, and its
			// score centres the aspiration window. The main thread still
			// searches the depth limit itself.
			if P.SMPAdopt != 0 && depth >= 5 {
				if v := e.helperBest.Load(); v != 0 {
					if hd, hs := unpackHelperResult(v); hd >= depth {
						if next := min(hd+1, maxDepth); next > depth {
							logger.Profilef("Depth %d: a helper finished depth %d (score %d), going on at %d", depth, hd, hs, next)
							depth = next
							rootScore = hs
							e.mainDepth.Store(int32(hd))
						}
					}
				}
			}

			iterStartTime := time.Now()
			iterStartNodes := master.LocalNodes
			master.RootMoveNodes = [64][64]uint64{}
			tm.SetRootFailLow(false)
			setOptimism(tm, rootAvg.score, b.SideToMove)

			var score int32
			if depth < 4 {
				score = master.Search(depth, -Infinity, Infinity, 0, true, false, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
				if !e.Stopped.Load() {
					rootAvg.add(master, score)
				}
			} else {
				// Aspiration Windows
				delta := P.AspDelta
				failHighs := int8(0)
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
					searchDepth := depth
					if P.AspReduce != 0 {
						// After a fail high the move is already known to be
						// better; a shallower search settles by how much.
						searchDepth = max(depth-failHighs, 1)
					}
					score = master.Search(searchDepth, alpha, beta, 0, true, false, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)

					if e.Stopped.Load() {
						break
					}
					rootAvg.add(master, score)

					if score <= alpha {
						// The expected move is worse than thought: let this
						// iteration run on to find out how bad, or a better one.
						tm.SetRootFailLow(true)
						beta = (alpha + beta) / 2
						alpha = score - delta
						if alpha < -Infinity {
							alpha = -Infinity
						}
						failedAttempts++
						failHighs = 0
					} else if score >= beta {
						if failHighs < 3 {
							failHighs++
						}
						beta = score + delta
						if beta > Infinity {
							beta = Infinity
						}
						failedAttempts++
					} else {
						break
					}

					delta += delta / 2
					if (P.AspTries != 0 && failedAttempts >= int(P.AspTries)) || delta >= P.AspOpen {
						alpha = -Infinity
						beta = Infinity
					}
				}
			}

			if e.Stopped.Load() {
				// The iteration was cut off, but a root move it finished
				// searching and found better than everything before it
				// (including the previous best move, which is searched
				// first) is a real improvement: play it.
				if m := master.PVArray[0][0]; depth > 1 && master.PVLength[0] > 0 && m != types.MoveNone && m != rootBestMove && isLegalMove(b, m) {
					logger.Profilef("Stopped at depth %d: adopting %s over %s from the unfinished iteration", depth, m, rootBestMove)
					rootBestMove = m
					rootPonderMove = types.MoveNone
					if master.PVLength[0] > 1 {
						rootPonderMove = master.PVArray[0][1]
					}
				}
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
				masterDepth = depth
				e.mainDepth.Store(int32(depth))
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
			totalNodes := e.searchNodes(limits.MaxNodes, master)

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

			// A mate was found. Stop once the search has gone two plies
			// deeper than a mate of that length needs (2n plies for a mate
			// in n): until then a shorter mate may still be hidden behind a
			// reduced or pruned move. Stopping at the first mate seen meant
			// playing a mate in 3 with a mate in 2 on the board (WAC 160),
			// and stopping at 2n plies still missed the mate in 4 of WAC 161
			// behind a mate in 5. Mate trees are small, so the extra
			// iterations cost little clock.
			if isMate && mateIn > 0 && int(depth) >= 2*mateIn+2 {
				if !tm.Ponder.Load() {
					break
				}
			}

			// Dynamic Time Management: best move stability, node dominance and
			// score drops each give a factor; their product scales the base
			// allocation. Recomputed from scratch every iteration, so a run of
			// agreeing shallow iterations cannot shrink the budget for good.
			isStable := false
			hasClockTime := tm.OptTime > 0 && limits.MaxDepth == 0 && limits.MaxNodes == 0 && !tm.Infinite && !tm.Ponder.Load() && !tm.IsMoveTime

			if depth >= 5 && rootBestMove != types.MoveNone {
				bestMoveChanges *= 0.75
				if rootBestMove == prevIterationBestMove {
					bestMoveStability++
					isStable = true

					// Emergency early exit: ONLY in severe time trouble (<2.5s) to prevent flagging
					if hasClockTime && tm.IsEmergency && bestMoveStability >= 2 && depth >= 8 {
						break
					}
				} else {
					bestMoveStability = 0
					if prevIterationBestMove != types.MoveNone {
						bestMoveChanges += 1.0
					}
				}

				if hasClockTime {
					scale := stabilityScale[min(bestMoveStability, len(stabilityScale)-1)]
					if bestMoveChanges > 0 {
						scale *= 1.0 + 0.12*min(bestMoveChanges, 4.0)
					}

					// Share of this iteration's nodes spent on the best move.
					if iterNodes := master.LocalNodes - iterStartNodes; iterNodes > 0 {
						rootNodes := master.RootMoveNodes[rootBestMove.From()][rootBestMove.To()]
						switch nodeRatio := float64(rootNodes) / float64(iterNodes); {
						case nodeRatio >= 0.90:
							scale *= 0.80
						case nodeRatio >= 0.75:
							scale *= 0.90
						case nodeRatio < 0.40:
							// Disputed position: alternatives took most of the search
							scale *= 1.20
						}
					}

					// Score falling or jumping from the previous iteration
					if prevIterationScore != 0 {
						switch drop := prevIterationScore - score; {
						case drop >= 80:
							scale *= 1.50
							isStable = false
						case drop >= 40:
							scale *= 1.30
							isStable = false
						case drop >= 20:
							scale *= 1.15
						case drop <= -50:
							scale *= 1.15
							isStable = false
						}
					}

					// In time trouble the budget may shrink but not grow.
					if tm.IsTimeTrouble {
						scale = min(scale, 1.0)
					}
					tm.ScaleOptTime(scale)
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

	// With several threads, the move comes from the thread the vote picks,
	// which need not be the main one (as in Stockfish; not at a fixed depth,
	// where the result should not depend on thread timing).
	if P.ThreadVote != 0 && len(helpers) > 0 && multiPVCount == 1 && limits.MaxDepth == 0 && rootBestMove != types.MoveNone {
		results := []threadResult{{depth: masterDepth, score: rootScore, move: rootBestMove, ponder: rootPonderMove}}
		for _, h := range helpers {
			if h.done.move != types.MoveNone && isLegalMove(b, h.done.move) {
				results = append(results, h.done)
			}
		}
		if i := pickThread(results); i > 0 && (results[i].move != rootBestMove || results[i].depth > masterDepth) {
			r := results[i]
			changedMove := r.move != rootBestMove
			if changedMove {
				logger.Profilef("Thread vote: %s (depth %d, score %d) from a helper over the main thread's %s (depth %d, score %d)",
					r.move, r.depth, r.score, rootBestMove, masterDepth, rootScore)
			}
			rootBestMove, rootScore = r.move, r.score
			if r.ponder != types.MoveNone {
				rootPonderMove = r.ponder
			} else if changedMove {
				rootPonderMove = types.MoveNone
			}
			if infoCallback != nil {
				pv := []types.Move{r.move}
				if r.ponder != types.MoveNone {
					pv = append(pv, r.ponder)
				}
				elapsed := time.Since(startTime)
				nodes := e.searchNodes(limits.MaxNodes, master)
				var nps uint64
				if elapsed.Seconds() > 0 {
					nps = uint64(float64(nodes) / elapsed.Seconds())
				}
				info := SearchInfo{Depth: r.depth, SelDepth: r.depth, Score: r.score, Nodes: nodes, NPS: nps,
					TimeMs: elapsed.Milliseconds(), HashFull: e.TT.HashFull(), TBHits: e.TBHits.Load(), PV: pv}
				if r.score > MateThreshold {
					info.IsMate, info.MateIn = true, int((MateScore-r.score+1)/2)
				} else if r.score < -MateThreshold {
					info.IsMate, info.MateIn = true, -int((MateScore+r.score+1)/2)
				}
				infoCallback(info)
			}
		}
	}

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

	totalNodes := e.searchNodes(limits.MaxNodes, master)
	e.Nodes.Store(totalNodes)
	totalElapsed := time.Since(startTime)
	var finalNps uint64
	if totalElapsed.Seconds() > 0 {
		finalNps = uint64(float64(totalNodes) / totalElapsed.Seconds())
	}
	logger.Profilef("Search completed: BestMove=%s, PonderMove=%s, Score=%d, TotalNodes=%d, Elapsed=%v, NPS=%d, TBHits=%d",
		rootBestMove, rootPonderMove, rootScore, totalNodes, totalElapsed, finalNps, e.TBHits.Load())

	if e.TBHits.Load() > 0 && rootBestMove != types.MoveNone && syzygy.IsAvailable() {
		tempB := b.Clone()
		if tempB.MakeMove(rootBestMove) && tempB.Castling == 0 && bitboard.Count(tempB.Occupied()) <= syzygy.MaxPieces() {
			if wdl, ok := syzygy.ProbeWDL(tempB); ok {
				resultStr := syzygy.OpponentWDLToOurResult(wdl)
				logger.Infof("Syzygy tablebase determined best position: %s leads to %s (%d pieces remaining)", rootBestMove, resultStr, bitboard.Count(tempB.Occupied()))
				if infoCallback != nil {
					infoCallback(SearchInfo{
						StringMsg: fmt.Sprintf("Syzygy tablebase determined best position: %s leads to %s", rootBestMove, resultStr),
					})
				}
			}
		}
	}

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

	if master.Eight != nil {
		var accs, rowOps, finny, rebuilds uint64
		for _, w := range append([]*SearchWorker{master}, helpers...) {
			if w.Eight != nil {
				accs += w.Eight.Evals
				rowOps += w.Eight.RowOps
				finny += w.Eight.FinnyRefreshes
				rebuilds += w.Eight.ThreatRebuilds
			}
		}
		if accs > 0 {
			logger.Profilef("8Chess Accumulators: Built=%d, RowOps=%d (%.1f per set; ~110 from scratch), KingBucketCache=%d, ThreatRebuilds=%d",
				accs, rowOps, float64(rowOps)/float64(accs), finny, rebuilds)
		}
	}

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
	if info.StringMsg != "" {
		return fmt.Sprintf("info string %s", info.StringMsg)
	}

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
	if info.HasWDL {
		sb.WriteString(fmt.Sprintf("wdl %d %d %d ", info.WDL[0], info.WDL[1], info.WDL[2]))
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

// threadResult is what one search thread concluded in its last finished
// iteration.
type threadResult struct {
	depth  int8
	score  int32
	move   types.Move
	ponder types.Move
}

// pickThread chooses whose move to play, as Stockfish does: every thread
// votes for its move with a weight that grows with its depth and with how
// far its score is above the lowest score any thread reports, and the move
// with the most votes wins, credited to the deepest, best-scoring thread
// that chose it. A thread that sees a forced mate overrides the vote, the
// shortest mate first; a thread that sees itself mated never wins it.
// results[0] is the main thread, which keeps the move on a tie.
func pickThread(results []threadResult) int {
	if len(results) == 0 {
		return 0
	}
	minScore := results[0].score
	for _, r := range results {
		minScore = min(minScore, r.score)
	}
	weight := func(r threadResult) int64 { return int64(r.score-minScore+14) * int64(r.depth) }
	votes := map[types.Move]int64{}
	for _, r := range results {
		votes[r.move] += weight(r)
	}
	best := 0
	for i := 1; i < len(results); i++ {
		r, b := results[i], results[best]
		switch {
		case b.score >= MateThreshold:
			// A mate is already chosen: only a shorter one replaces it.
			if r.score > b.score {
				best = i
			}
		case r.score >= MateThreshold:
			best = i
		case r.score > -MateThreshold && (votes[r.move] > votes[b.move] ||
			(votes[r.move] == votes[b.move] && weight(r) > weight(b))):
			best = i
		}
	}
	return best
}

// helperIteration is one iteration of a helper thread at depth d. Under
// P.SMPMode 1 it searches a window round the helper's previous score, as the
// main thread does, widening it on a failure; a full-window search costs
// several times as much and kept the helpers from ever getting ahead.
// enterPriority ties the calling goroutine to its OS thread and gives the
// thread the priority the search calls for. Only used when the search's
// TimeManager has LowPriority set.
func (w *SearchWorker) enterPriority() {
	runtime.LockOSThread()
	w.lowPrio = false
	w.syncPriority()
}

// syncPriority makes the thread's priority follow the search: low while it
// is pondering, normal from the ponderhit on.
func (w *SearchWorker) syncPriority() {
	if low := w.TimeMan.Ponder.Load(); low != w.lowPrio && threadprio.SetLow(low) {
		w.lowPrio = low
	}
}

// leavePriority undoes enterPriority. A thread whose normal priority could
// not be restored stays tied to the goroutine, so it ends with it instead of
// going back to the runtime's pool at low priority.
func (w *SearchWorker) leavePriority() {
	if w.lowPrio {
		if !threadprio.SetLow(false) {
			return
		}
		w.lowPrio = false
	}
	runtime.UnlockOSThread()
}

func (w *SearchWorker) helperIteration(d int8, prev int32) int32 {
	full := func() int32 {
		return w.Search(d, -Infinity, Infinity, 0, true, false, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
	}
	if P.SMPMode == 0 || d < 4 {
		return full()
	}
	delta := P.AspDelta + int32((w.ID&3)*3)
	alpha, beta := max(prev-delta, -Infinity), min(prev+delta, Infinity)
	for tries := 0; ; tries++ {
		score := w.Search(d, alpha, beta, 0, true, false, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
		if w.TimeMan.Stopped.Load() || (score > alpha && score < beta) {
			return score
		}
		if score <= alpha {
			beta = (alpha + beta) / 2
			alpha = max(score-delta, -Infinity)
		} else {
			beta = min(score+delta, Infinity)
		}
		delta += delta / 2
		if (P.AspTries != 0 && tries+1 >= int(P.AspTries)) || delta >= P.AspOpen {
			alpha, beta = -Infinity, Infinity
		}
	}
}

// rootAverage is a running average of the scores the root searches gave the
// best move: each new score counts for half, as Stockfish's averageScore
// does, and every search counts, the aspiration re-searches included. It is
// steadier than the last score alone. The average starts over when the best
// move changes.
type rootAverage struct {
	score int32
	move  types.Move
}

// add takes in the score of a root search the worker just finished.
func (a *rootAverage) add(w *SearchWorker, score int32) {
	m := types.MoveNone
	if w.PVLength[0] > 0 {
		m = w.PVArray[0][0]
	}
	if m != types.MoveNone && m == a.move {
		a.score = (a.score + score) / 2
		return
	}
	a.score, a.move = score, m
}
