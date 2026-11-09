package search

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
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
	PV       []types.Move
}

type Engine struct {
	TT         *tt.TT
	NumThreads int
	Stopped    atomic.Bool
	Nodes      atomic.Uint64
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
	}
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
	e.TT.IncrementAge()

	tm := NewTimeManager(limits, b.SideToMove, &e.Stopped, &e.Nodes)
	e.currentTM.Store(tm)
	defer e.currentTM.Store(nil)

	maxDepth := limits.MaxDepth
	if maxDepth <= 0 || maxDepth > 64 {
		maxDepth = 64
	}

	var rootBestMove types.Move
	var rootPonderMove types.Move
	var rootScore int32

	// Master worker
	master := NewWorker(0, b, e.TT, tm, &e.Nodes)

	// WaitGroup for helper threads
	var wg sync.WaitGroup

	// Start helper workers
	for id := 1; id < e.NumThreads; id++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			helper := NewWorker(workerID, b, e.TT, tm, &e.Nodes)
			// Helper searches with depth variation
			for d := int8(1 + (workerID % 2)); d <= maxDepth; d++ {
				if e.Stopped.Load() {
					break
				}
				helper.Search(d, -Infinity, Infinity, 0, true, true, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
			}
		}(id)
	}

	startTime := time.Now()
	var prevIterationScore int32
	var prevIterationBestMove types.Move
	var bestMoveStability int
	var rootPV []types.Move

	// Master iterative deepening loop
	for depth := int8(1); depth <= maxDepth; depth++ {
		if e.Stopped.Load() {
			break
		}

		master.RootMoveNodes = [64][64]uint64{}

		var score int32
		if depth < 4 {
			score = master.Search(depth, -Infinity, Infinity, 0, true, true, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)
		} else {
			// Aspiration Windows
			delta := int32(30)
			alpha := rootScore - delta
			beta := rootScore + delta

			for {
				if alpha < -Infinity {
					alpha = -Infinity
				}
				if beta > Infinity {
					beta = Infinity
				}

				score = master.Search(depth, alpha, beta, 0, true, true, types.MoveNone, types.MoveNone, types.MoveNone, types.MoveNone)

				if e.Stopped.Load() {
					break
				}

				if score <= alpha {
					alpha -= delta
					delta += delta / 2
				} else if score >= beta {
					beta += delta
					delta += delta / 2
				} else {
					break
				}
			}
		}

		if e.Stopped.Load() && depth > 1 {
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
		pv := e.extractPV(b, master, depth)
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
				PV:       pv,
			})
		}

		// Check if we found checkmate
		if isMate && mateIn > 0 && mateIn <= int(depth)/2 {
			if !tm.Ponder.Load() {
				break
			}
		}

		// Dynamic Time Management: Best move stability, node dominance & fail-low recovery
		if depth >= 5 && rootBestMove != types.MoveNone {
			if rootBestMove == prevIterationBestMove {
				bestMoveStability++
				// If the best move has been rock-solid across 4+ iterations, reduce time to save clock
				if bestMoveStability >= 4 {
					tm.AdjustOptTime(0.85)
				}
			} else {
				bestMoveStability = 0
			}

			// Node-based time management: evaluate dominance of root move
			rootNodes := master.RootMoveNodes[rootBestMove.From()][rootBestMove.To()]
			if master.LocalNodes > 0 {
				nodeRatio := float64(rootNodes) / float64(master.LocalNodes)
				if nodeRatio >= 0.65 {
					// Clear dominance: reduce allocated time
					tm.AdjustOptTime(0.75)
				} else if nodeRatio < 0.35 {
					// Disputed position: extend allocated time
					tm.AdjustOptTime(1.30)
				}
			}

			// If the score dropped significantly from the previous iteration, extend time to recover
			if prevIterationScore != 0 && score < prevIterationScore-40 {
				tm.AdjustOptTime(1.35)
			}
		}
		prevIterationBestMove = rootBestMove
		prevIterationScore = rootScore

		if !tm.CanStartNewIteration() {
			break
		}
	}

	// Stop helper threads
	e.Stopped.Store(true)
	wg.Wait()

	// Fallback move if no move was found or if rootBestMove is not legal
	if rootBestMove == types.MoveNone || !isLegalMove(b, rootBestMove) {
		legalMoves := movegen.GenerateLegalMoves(b)
		if len(legalMoves) > 0 {
			rootBestMove = legalMoves[0]
		}
	}

	return rootBestMove, rootPonderMove
}

func (e *Engine) extractPV(b *board.Board, master *SearchWorker, depth int8) []types.Move {
	var pv []types.Move
	visited := make(map[uint64]bool)
	tempB := b.Clone()

	for d := 0; d < int(depth); d++ {
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

	if info.IsMate {
		sb.WriteString(fmt.Sprintf("score mate %d ", info.MateIn))
	} else {
		sb.WriteString(fmt.Sprintf("score cp %d ", info.Score))
	}

	sb.WriteString(fmt.Sprintf("nodes %d nps %d time %d hashfull %d pv",
		info.Nodes, info.NPS, info.TimeMs, info.HashFull))

	for _, m := range info.PV {
		sb.WriteString(" ")
		sb.WriteString(m.String())
	}

	return sb.String()
}
