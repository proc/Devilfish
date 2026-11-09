package uci

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devilfish/internal/board"
	"devilfish/internal/config"
	"devilfish/internal/eval"
	"devilfish/internal/movegen"
	"devilfish/internal/nnue"
	"devilfish/internal/search"
	"devilfish/internal/types"
)

const (
	EngineName    = "Devilfish"
	EngineVersion = "1.0.0"
	EngineAuthor  = "Antigravity Team"
)

type UCI struct {
	board          *board.Board
	engine         *search.Engine
	searching      atomic.Bool
	isPonder       atomic.Bool
	isAutoPonder   atomic.Bool
	autoPonderHash atomic.Uint64
	guiStop        atomic.Bool
	abortSearch    atomic.Bool
	bestMoveSent   atomic.Bool
	ponderEnabled  bool
	evalFile       string
	outMu          sync.Mutex
	in             *bufio.Reader
	out            io.Writer
}

func New(in io.Reader, out io.Writer, cfg config.Config) *UCI {
	b, _ := board.ParseFEN(board.StartFEN)
	threads := cfg.Threads
	if threads < 1 {
		threads = 1
	}
	hash := cfg.Hash
	if hash < 1 {
		hash = 64
	}
	evalFile := cfg.EvalFile
	if evalFile == "" {
		evalFile = "<embedded>"
	}
	return &UCI{
		board:         b,
		engine:        search.NewEngine(hash, threads),
		ponderEnabled: cfg.Ponder,
		evalFile:      evalFile,
		in:            bufio.NewReader(in),
		out:           out,
	}
}

func (u *UCI) sendLine(line string) {
	u.outMu.Lock()
	defer u.outMu.Unlock()
	fmt.Fprintln(u.out, line)
}

func (u *UCI) Loop() {
	for {
		line, err := u.in.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		parts := strings.Fields(line)
		cmd := parts[0]

		switch cmd {
		case "uci":
			u.cmdUCI()
		case "isready":
			u.cmdIsReady()
		case "ping":
			if len(parts) > 1 {
				u.sendLine(fmt.Sprintf("pong %s", parts[1]))
			} else {
				u.sendLine("pong")
			}
		case "setoption":
			u.cmdSetOption(parts[1:])
		case "ucinewgame":
			u.cmdUCINewGame()
		case "position":
			u.cmdPosition(parts[1:])
		case "go":
			u.cmdGo(parts[1:])
		case "stop":
			u.cmdStop()
		case "ponderhit":
			u.cmdPonderHit()
		case "quit":
			u.stopSearch(false)
			return
		case "perft":
			u.cmdPerft(parts[1:])
		case "eval":
			u.cmdEval()
		case "d", "display":
			u.cmdDisplay()
		case "bench":
			u.cmdBench(parts[1:])
		default:
			// Unknown command
		}
	}
}

func (u *UCI) cmdUCI() {
	u.sendLine(fmt.Sprintf("id name %s %s", EngineName, EngineVersion))
	u.sendLine(fmt.Sprintf("id author %s", EngineAuthor))
	u.sendLine(fmt.Sprintf("option name Hash type spin default %d min 1 max 65536", u.engine.TT.SizeMB()))
	u.sendLine(fmt.Sprintf("option name Threads type spin default %d min 1 max 512", u.engine.NumThreads))
	u.sendLine(fmt.Sprintf("option name Ponder type check default %t", u.ponderEnabled))
	u.sendLine(fmt.Sprintf("option name UseNNUE type check default %t", nnue.Enabled))
	evalFile := u.evalFile
	if evalFile == "" {
		evalFile = "<embedded>"
	}
	u.sendLine(fmt.Sprintf("option name EvalFile type string default %s", evalFile))
	u.sendLine("uciok")
}

func (u *UCI) cmdIsReady() {
	u.sendLine("readyok")
}

func (u *UCI) cmdSetOption(args []string) {
	for i := 0; i < len(args); i++ {
		if strings.ToLower(args[i]) == "name" && i+1 < len(args) {
			optName := strings.ToLower(args[i+1])
			if i+3 < len(args) && strings.ToLower(args[i+2]) == "value" {
				valStr := args[i+3]
				switch optName {
				case "hash":
					sizeMB, err := strconv.Atoi(valStr)
					if err == nil && sizeMB > 0 {
						u.engine.SetHash(sizeMB)
					}
				case "threads":
					threads, err := strconv.Atoi(valStr)
					if err == nil && threads > 0 {
						u.engine.SetThreads(threads)
					}
				case "ponder":
					valLower := strings.ToLower(valStr)
					if valLower == "true" || valLower == "1" {
						u.ponderEnabled = true
					} else if valLower == "false" || valLower == "0" {
						u.ponderEnabled = false
						u.stopSearch(false)
					}
				case "usennue":
					valLower := strings.ToLower(valStr)
					if valLower == "true" || valLower == "1" {
						nnue.Enabled = true
					} else if valLower == "false" || valLower == "0" {
						nnue.Enabled = false
					}
				case "evalfile":
					if valStr != "" && valStr != "<embedded>" {
						err := nnue.LoadFile(valStr)
						if err != nil {
							u.sendLine(fmt.Sprintf("info string failed to load nnue file: %v", err))
						} else {
							u.evalFile = valStr
							u.sendLine(fmt.Sprintf("info string loaded nnue file %s", valStr))
						}
					} else if valStr == "<embedded>" {
						u.evalFile = "<embedded>"
						nnue.ResetToDefault()
						u.sendLine("info string reset to embedded nnue network")
					}
				}
			}
		}
	}
}

func (u *UCI) cmdUCINewGame() {
	u.stopSearch(false)
	u.engine.TT.Clear()
	b, _ := board.ParseFEN(board.StartFEN)
	u.board = b
}

func (u *UCI) stopSearch(requestedByGUI bool) {
	if requestedByGUI {
		u.guiStop.Store(true)
	}
	u.abortSearch.Store(true)
	u.engine.Stop()

	for u.searching.Load() {
		time.Sleep(1 * time.Millisecond)
	}

	// If GUI sent "stop" and no bestmove was emitted, emit one to satisfy ping
	if requestedByGUI && !u.bestMoveSent.Load() {
		u.bestMoveSent.Store(true)
		legals := movegen.GenerateLegalMoves(u.board)
		if len(legals) > 0 {
			u.sendLine(fmt.Sprintf("bestmove %s", legals[0]))
		} else {
			u.sendLine("bestmove 0000")
		}
	}
}

func (u *UCI) cmdPosition(args []string) {
	if len(args) == 0 {
		return
	}

	u.stopSearch(false)

	var b *board.Board
	idx := 0
	if args[0] == "startpos" {
		b, _ = board.ParseFEN(board.StartFEN)
		idx = 1
	} else if args[0] == "fen" {
		var fenParts []string
		for idx = 1; idx < len(args) && args[idx] != "moves"; idx++ {
			fenParts = append(fenParts, args[idx])
		}
		fen := strings.Join(fenParts, " ")
		parsed, err := board.ParseFEN(fen)
		if err != nil {
			u.sendLine(fmt.Sprintf("info string failed to parse FEN: %v", err))
			return
		}
		b = parsed
	}

	if b == nil {
		return
	}

	if idx < len(args) && args[idx] == "moves" {
		for _, moveStr := range args[idx+1:] {
			m, err := parseMove(b, moveStr)
			if err != nil {
				break
			}
			b.MakeMove(m)
		}
	}

	u.board = b
}

func (u *UCI) cmdGo(args []string) {
	// Stop any running search or auto-ponder first
	u.stopSearch(false)
	u.engine.Stopped.Store(false)
	u.guiStop.Store(false)
	u.abortSearch.Store(false)
	u.bestMoveSent.Store(false)

	var limits search.SearchLimits

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "wtime":
			if i+1 < len(args) {
				t, _ := strconv.ParseInt(args[i+1], 10, 64)
				limits.WhiteTime = t
				i++
			}
		case "btime":
			if i+1 < len(args) {
				t, _ := strconv.ParseInt(args[i+1], 10, 64)
				limits.BlackTime = t
				i++
			}
		case "winc":
			if i+1 < len(args) {
				inc, _ := strconv.ParseInt(args[i+1], 10, 64)
				limits.WhiteInc = inc
				i++
			}
		case "binc":
			if i+1 < len(args) {
				inc, _ := strconv.ParseInt(args[i+1], 10, 64)
				limits.BlackInc = inc
				i++
			}
		case "movestogo":
			if i+1 < len(args) {
				mtg, _ := strconv.Atoi(args[i+1])
				limits.MovesToGo = mtg
				i++
			}
		case "depth":
			if i+1 < len(args) {
				d, _ := strconv.Atoi(args[i+1])
				limits.MaxDepth = int8(d)
				i++
			}
		case "nodes":
			if i+1 < len(args) {
				n, _ := strconv.ParseUint(args[i+1], 10, 64)
				limits.MaxNodes = n
				i++
			}
		case "movetime":
			if i+1 < len(args) {
				mt, _ := strconv.ParseInt(args[i+1], 10, 64)
				limits.MoveTime = mt
				i++
			}
		case "infinite":
			limits.Infinite = true
		case "ponder":
			limits.Ponder = true
		}
	}

	u.searching.Store(true)
	u.isPonder.Store(limits.Ponder)
	u.isAutoPonder.Store(false)
	u.autoPonderHash.Store(0)

	currentBoard := u.board.Clone()

	go func() {
		defer func() {
			u.searching.Store(false)
			u.isPonder.Store(false)
			u.isAutoPonder.Store(false)
			u.autoPonderHash.Store(0)
		}()

		infoCallback := func(info search.SearchInfo) {
			if !u.isAutoPonder.Load() {
				u.sendLine(search.FormatInfo(info))
			}
		}

		bestMove, ponderMove := u.engine.Search(currentBoard, limits, infoCallback)

		// If aborted internally before emitting move, do not output bestmove
		if u.abortSearch.Load() && !u.guiStop.Load() {
			return
		}

		// If this was an official ponder ("go ponder")
		if limits.Ponder {
			// UCI protocol: do not exit or send bestmove in ponder mode until "stop" or "ponderhit"
			for u.isPonder.Load() && !u.guiStop.Load() && !u.abortSearch.Load() {
				time.Sleep(2 * time.Millisecond)
			}
			if u.abortSearch.Load() && !u.guiStop.Load() {
				return
			}
			u.bestMoveSent.Store(true)
			if ponderMove != types.MoveNone {
				u.sendLine(fmt.Sprintf("bestmove %s ponder %s", bestMove, ponderMove))
			} else {
				u.sendLine(fmt.Sprintf("bestmove %s", bestMove))
			}
			return
		}

		// Normal search completed
		u.bestMoveSent.Store(true)
		if ponderMove != types.MoveNone {
			u.sendLine(fmt.Sprintf("bestmove %s ponder %s", bestMove, ponderMove))
		} else {
			u.sendLine(fmt.Sprintf("bestmove %s", bestMove))
		}

		// Background calculation (auto-ponder):
		// Check if we were told to stop or abort before starting auto-ponder
		if u.abortSearch.Load() || u.guiStop.Load() || !u.ponderEnabled || bestMove == types.MoveNone {
			return
		}

		opponentMove := ponderMove
		if opponentMove == types.MoveNone {
			tempB := currentBoard.Clone()
			if tempB.MakeMove(bestMove) {
				if ttM, _, _, _, found := u.engine.TT.Probe(tempB.Hash); found && ttM != types.MoveNone && isLegal(tempB, ttM) {
					opponentMove = ttM
				}
			}
		}

		if opponentMove == types.MoveNone {
			return
		}

		// Apply both moves: engine's move and predicted opponent move
		hypoBoard := currentBoard.Clone()
		if !hypoBoard.MakeMove(bestMove) || !hypoBoard.MakeMove(opponentMove) {
			return
		}

		if u.abortSearch.Load() || u.guiStop.Load() {
			return
		}

		u.engine.Stopped.Store(false)
		u.isAutoPonder.Store(true)
		u.autoPonderHash.Store(hypoBoard.Hash)

		autoLimits := search.SearchLimits{Infinite: true, Ponder: true}
		// Silent infoCallback during auto-ponder so GUI is not sent out-of-turn PV lines!
		nextBest, nextPonder := u.engine.Search(hypoBoard, autoLimits, nil)

		u.isAutoPonder.Store(false)
		u.autoPonderHash.Store(0)

		// If GUI sent "stop" during auto-ponder, Cutechess called stopThinking() and is waiting for bestmove!
		if u.guiStop.Load() && !u.bestMoveSent.Load() {
			u.bestMoveSent.Store(true)
			if nextBest != types.MoveNone {
				if nextPonder != types.MoveNone {
					u.sendLine(fmt.Sprintf("bestmove %s ponder %s", nextBest, nextPonder))
				} else {
					u.sendLine(fmt.Sprintf("bestmove %s", nextBest))
				}
			} else {
				legals := movegen.GenerateLegalMoves(hypoBoard)
				if len(legals) > 0 {
					u.sendLine(fmt.Sprintf("bestmove %s", legals[0]))
				} else {
					u.sendLine("bestmove 0000")
				}
			}
		}
	}()
}

func (u *UCI) cmdPonderHit() {
	u.isPonder.Store(false)
	u.engine.PonderHit()
}

func (u *UCI) cmdStop() {
	u.stopSearch(true)
}

func (u *UCI) cmdPerft(args []string) {
	u.stopSearch(false)
	depth := 1
	if len(args) > 0 {
		d, err := strconv.Atoi(args[0])
		if err == nil && d > 0 {
			depth = d
		}
	}
	movegen.PerftDivide(u.board, depth)
}

func (u *UCI) cmdEval() {
	classicalScore := eval.EvaluateClassical(u.board)
	var acc nnue.Accumulator
	nnue.Refresh(u.board, &acc)
	nnueScore := nnue.Evaluate(&acc, u.board.SideToMove)

	active := "NNUE"
	activeScore := nnueScore
	if !nnue.Enabled {
		active = "Classical"
		activeScore = classicalScore
	}

	u.sendLine(fmt.Sprintf("NNUE evaluation       : %+d cp", nnueScore))
	u.sendLine(fmt.Sprintf("Classical evaluation  : %+d cp", classicalScore))
	u.sendLine(fmt.Sprintf("Active evaluation (%s): %+d cp (from %s's perspective)", active, activeScore, u.board.SideToMove))
}

func (u *UCI) cmdDisplay() {
	u.sendLine(u.board.String())
	u.sendLine(fmt.Sprintf("FEN: %s", u.board.ToFEN()))
}

var benchPositions = []string{
	board.StartFEN,
	"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
	"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
	"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
	"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
	"r4rk1/1pp1qppp/p1np1n2/2b1p1B1/2B1P1b1/P1NP1N2/1PP1QPPP/R4RK1 w - - 0 10",
}

func (u *UCI) Bench(depth int8) {
	u.cmdBench([]string{strconv.Itoa(int(depth))})
}

func (u *UCI) cmdBench(args []string) {
	u.stopSearch(false)
	u.abortSearch.Store(false)
	depth := int8(7)
	if len(args) > 0 {
		d, err := strconv.Atoi(args[0])
		if err == nil && d > 0 {
			depth = int8(d)
		}
	}

	u.sendLine(fmt.Sprintf("Running benchmark at depth %d with %d threads...", depth, u.engine.NumThreads))
	totalNodes := uint64(0)
	start := time.Now()

	limits := search.SearchLimits{MaxDepth: depth}

	for i, fen := range benchPositions {
		b, err := board.ParseFEN(fen)
		if err != nil {
			continue
		}
		posStart := time.Now()
		bestMove, _ := u.engine.Search(b, limits, nil)
		posElapsed := time.Since(posStart)
		nodes := u.engine.Nodes.Load()
		totalNodes += nodes
		u.sendLine(fmt.Sprintf("Position %d/6: bestmove %s | nodes %d | time %v", i+1, bestMove, nodes, posElapsed))
	}

	elapsed := time.Since(start)
	nps := float64(totalNodes) / elapsed.Seconds()
	u.sendLine("===============================================")
	u.sendLine(fmt.Sprintf("Total nodes: %d", totalNodes))
	u.sendLine(fmt.Sprintf("Total time : %v", elapsed))
	u.sendLine(fmt.Sprintf("Nodes/sec  : %.0f", nps))
	u.sendLine("===============================================")
}

func isLegal(b *board.Board, m types.Move) bool {
	if m == types.MoveNone {
		return false
	}
	for _, lm := range movegen.GenerateLegalMoves(b) {
		if lm == m {
			return true
		}
	}
	return false
}

func parseMove(b *board.Board, moveStr string) (types.Move, error) {
	legalMoves := movegen.GenerateLegalMoves(b)
	for _, m := range legalMoves {
		if m.String() == moveStr {
			return m, nil
		}
	}
	return types.MoveNone, fmt.Errorf("illegal move: %s", moveStr)
}

func Run(cfg config.Config) {
	u := New(os.Stdin, os.Stdout, cfg)
	u.Loop()
}
