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
	searching      bool
	isAutoPonder   bool
	autoPonderHash uint64
	ponderEnabled  bool
	manualStop     atomic.Bool
	searchMu       sync.Mutex
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
	return &UCI{
		board:         b,
		engine:        search.NewEngine(hash, threads),
		ponderEnabled: true,
		in:            bufio.NewReader(in),
		out:           out,
	}
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
			u.cmdStop()
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
	fmt.Fprintf(u.out, "id name %s %s\n", EngineName, EngineVersion)
	fmt.Fprintf(u.out, "id author %s\n", EngineAuthor)
	fmt.Fprintf(u.out, "option name Hash type spin default %d min 1 max 65536\n", u.engine.TT.SizeMB())
	fmt.Fprintf(u.out, "option name Threads type spin default %d min 1 max 512\n", u.engine.NumThreads)
	fmt.Fprintf(u.out, "option name Ponder type check default %t\n", u.ponderEnabled)
	fmt.Fprintf(u.out, "option name UseNNUE type check default %t\n", nnue.Enabled)
	fmt.Fprintf(u.out, "option name EvalFile type string default <embedded>\n")
	fmt.Fprintf(u.out, "uciok\n")
}

func (u *UCI) cmdIsReady() {
	fmt.Fprintf(u.out, "readyok\n")
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
						u.cmdStop()
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
							fmt.Fprintf(u.out, "info string failed to load nnue file: %v\n", err)
						} else {
							fmt.Fprintf(u.out, "info string loaded nnue file %s\n", valStr)
						}
					}
				}
			}
		}
	}
}

func (u *UCI) cmdUCINewGame() {
	u.cmdStop()
	u.engine.TT.Clear()
	b, _ := board.ParseFEN(board.StartFEN)
	u.board = b
}

func (u *UCI) cmdPosition(args []string) {
	if len(args) == 0 {
		return
	}

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
			fmt.Fprintf(u.out, "info string failed to parse FEN: %v\n", err)
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

	u.searchMu.Lock()
	isAuto := u.isAutoPonder
	ponderHash := u.autoPonderHash
	u.searchMu.Unlock()

	// If currently auto-pondering on this exact position, keep thinking!
	// Otherwise stop the background calculation.
	if !isAuto || b.Hash != ponderHash {
		u.cmdStop()
	}

	u.board = b
}

func (u *UCI) cmdGo(args []string) {
	u.cmdStop()
	u.manualStop.Store(false)

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

	u.searchMu.Lock()
	u.searching = true
	u.isAutoPonder = false
	u.autoPonderHash = 0
	u.searchMu.Unlock()

	go func() {
		defer func() {
			u.searchMu.Lock()
			u.searching = false
			u.isAutoPonder = false
			u.autoPonderHash = 0
			u.searchMu.Unlock()
		}()

		infoCallback := func(info search.SearchInfo) {
			fmt.Fprintln(u.out, search.FormatInfo(info))
		}

		currentBoard := u.board
		bestMove, ponderMove := u.engine.Search(currentBoard, limits, infoCallback)
		if ponderMove != types.MoveNone {
			fmt.Fprintf(u.out, "bestmove %s ponder %s\n", bestMove.String(), ponderMove.String())
		} else {
			fmt.Fprintf(u.out, "bestmove %s\n", bestMove.String())
		}

		// Background calculation: look for principal variation and calculate as if opponent played it
		for u.ponderEnabled && !u.manualStop.Load() && bestMove != types.MoveNone {
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
				break
			}

			// Apply both moves: engine's move and predicted opponent move
			hypoBoard := currentBoard.Clone()
			if !hypoBoard.MakeMove(bestMove) || !hypoBoard.MakeMove(opponentMove) {
				break
			}

			u.searchMu.Lock()
			u.isAutoPonder = true
			u.autoPonderHash = hypoBoard.Hash
			u.searchMu.Unlock()

			autoLimits := search.SearchLimits{Infinite: true, Ponder: true}
			nextBest, nextPonder := u.engine.Search(hypoBoard, autoLimits, infoCallback)

			u.searchMu.Lock()
			wasAuto := u.isAutoPonder
			u.isAutoPonder = false
			u.autoPonderHash = 0
			u.searchMu.Unlock()

			// If it transitioned from auto-ponder to a normal search via ponderhit, output and continue
			if !wasAuto && !u.manualStop.Load() && nextBest != types.MoveNone {
				if nextPonder != types.MoveNone {
					fmt.Fprintf(u.out, "bestmove %s ponder %s\n", nextBest.String(), nextPonder.String())
				} else {
					fmt.Fprintf(u.out, "bestmove %s\n", nextBest.String())
				}
				currentBoard = hypoBoard
				bestMove = nextBest
				ponderMove = nextPonder
			} else {
				break
			}
		}
	}()
}

func (u *UCI) cmdPonderHit() {
	u.searchMu.Lock()
	if u.isAutoPonder {
		u.isAutoPonder = false
	}
	u.searchMu.Unlock()
	u.engine.PonderHit()
}

func (u *UCI) cmdStop() {
	u.manualStop.Store(true)
	u.searchMu.Lock()
	searching := u.searching
	u.searchMu.Unlock()

	if searching {
		u.engine.Stop()
		// Wait for search routine to conclude
		for {
			u.searchMu.Lock()
			stillSearching := u.searching
			u.searchMu.Unlock()
			if !stillSearching {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
}

func (u *UCI) cmdPerft(args []string) {
	u.cmdStop()
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

	fmt.Fprintf(u.out, "NNUE evaluation       : %+d cp\n", nnueScore)
	fmt.Fprintf(u.out, "Classical evaluation  : %+d cp\n", classicalScore)
	fmt.Fprintf(u.out, "Active evaluation (%s): %+d cp (from %s's perspective)\n", active, activeScore, u.board.SideToMove)
}

func (u *UCI) cmdDisplay() {
	fmt.Fprintln(u.out, u.board.String())
	fmt.Fprintf(u.out, "FEN: %s\n", u.board.ToFEN())
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
	u.cmdStop()
	depth := int8(7)
	if len(args) > 0 {
		d, err := strconv.Atoi(args[0])
		if err == nil && d > 0 {
			depth = int8(d)
		}
	}

	fmt.Fprintf(u.out, "Running benchmark at depth %d with %d threads...\n", depth, u.engine.NumThreads)
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
		fmt.Fprintf(u.out, "Position %d/6: bestmove %s | nodes %d | time %v\n", i+1, bestMove, nodes, posElapsed)
	}

	elapsed := time.Since(start)
	nps := float64(totalNodes) / elapsed.Seconds()
	fmt.Fprintf(u.out, "===============================================\n")
	fmt.Fprintf(u.out, "Total nodes: %d\n", totalNodes)
	fmt.Fprintf(u.out, "Total time : %v\n", elapsed)
	fmt.Fprintf(u.out, "Nodes/sec  : %.0f\n", nps)
	fmt.Fprintf(u.out, "===============================================\n")
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
