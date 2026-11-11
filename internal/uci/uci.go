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
	"devilfish/internal/book"
	"devilfish/internal/config"
	"devilfish/internal/eval"
	"devilfish/internal/logger"
	"devilfish/internal/movegen"
	"devilfish/internal/nnue"
	"devilfish/internal/search"
	"devilfish/internal/syzygy"
	"devilfish/internal/types"
)

const (
	EngineName    = "Devilfish"
	EngineVersion = "0.1.1"
	EngineAuthor  = "Peter Tran"
)

type UCI struct {
	board            *board.Board
	engine           *search.Engine
	searching        atomic.Bool
	isPonder         atomic.Bool
	isAutoPonder     atomic.Bool
	autoPonderCancel atomic.Bool
	autoPonderHash   atomic.Uint64
	guiStop          atomic.Bool
	abortSearch      atomic.Bool
	bestMoveSent     atomic.Bool
	ponderEnabled    bool
	moveOverhead     int
	evalFile         string
	bookFile         string
	outMu            sync.Mutex
	in               *bufio.Reader
	out              io.Writer
}

func New(in io.Reader, out io.Writer, cfg config.Config) *UCI {
	b, _ := board.ParseFEN(board.StartFEN)
	threads := cfg.Threads
	if threads < 1 {
		threads = 1
	}
	hash := cfg.Hash
	if hash < 1 {
		hash = 128
	}
	evalFile := cfg.EvalFile
	if evalFile == "" {
		evalFile = "<embedded>"
	}
	bookFile := cfg.BookFile
	if bookFile == "" {
		bookFile = "<embedded>"
	}
	moveOverhead := cfg.MoveOverhead
	if moveOverhead <= 0 {
		moveOverhead = 20
	}
	if cfg.SyzygyPath != "" {
		syzygy.Init(cfg.SyzygyPath)
		syzygy.ProbeDepth = cfg.SyzygyProbeDepth
		syzygy.Use50MoveRule = cfg.Syzygy50MoveRule
		syzygy.ProbeLimit = cfg.SyzygyProbeLimit
	}
	eng := search.NewEngine(hash, threads)
	if cfg.MultiPV >= 1 {
		eng.SetMultiPV(cfg.MultiPV)
	}
	eng.SetUseBook(cfg.OwnBook)
	if bookFile != "<embedded>" {
		if bk, err := book.LoadFile(bookFile); err == nil {
			eng.SetBook(bk)
		} else {
			logger.Warnf("UCI Init: failed to load opening book %s: %v, falling back to embedded book", bookFile, err)
			eng.SetBook(book.DefaultBook())
		}
	} else {
		eng.SetBook(book.DefaultBook())
	}
	return &UCI{
		board:         b,
		engine:        eng,
		ponderEnabled: cfg.Ponder,
		moveOverhead:  moveOverhead,
		evalFile:      evalFile,
		bookFile:      bookFile,
		in:            bufio.NewReader(in),
		out:           out,
	}
}

func (u *UCI) sendLine(line string) {
	u.outMu.Lock()
	defer u.outMu.Unlock()
	logger.LogUCIOut(line)
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

		logger.LogUCIIn(line)
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
			syzygy.Free()
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
	u.sendLine(fmt.Sprintf("option name SyzygyPath type string default %s", syzygy.Path))
	u.sendLine(fmt.Sprintf("option name SyzygyProbeDepth type spin default %d min 1 max 64", syzygy.ProbeDepth))
	u.sendLine(fmt.Sprintf("option name Syzygy50MoveRule type check default %t", syzygy.Use50MoveRule))
	u.sendLine(fmt.Sprintf("option name SyzygyProbeLimit type spin default %d min 0 max 7", syzygy.ProbeLimit))
	u.sendLine(fmt.Sprintf("option name LogFile type string default %s", logger.Path()))
	u.sendLine(fmt.Sprintf("option name Move Overhead type spin default %d min 0 max 5000", u.moveOverhead))
	u.sendLine(fmt.Sprintf("option name MultiPV type spin default %d min 1 max 256", u.engine.MultiPV))
	u.sendLine(fmt.Sprintf("option name OwnBook type check default %t", u.engine.UseBook))
	bookFile := u.bookFile
	if bookFile == "" {
		bookFile = "<embedded>"
	}
	u.sendLine(fmt.Sprintf("option name BookFile type string default %s", bookFile))
	for _, ps := range search.TunableParams() {
		u.sendLine(fmt.Sprintf("option name %s type spin default %d min %d max %d", ps.Name, *ps.Ptr, ps.Min, ps.Max))
	}
	u.sendLine("uciok")
}

var syzygyReported bool

func (u *UCI) cmdIsReady() {
	if syzygy.IsAvailable() && !syzygyReported {
		u.sendLine(fmt.Sprintf("info string found %d-piece Syzygy tablebases", syzygy.MaxPieces()))
		syzygyReported = true
	}
	u.sendLine("readyok")
}

func (u *UCI) cmdSetOption(args []string) {
	if len(args) == 0 {
		return
	}

	nameIdx := -1
	valIdx := -1
	for i := 0; i < len(args); i++ {
		lower := strings.ToLower(args[i])
		if lower == "name" && nameIdx == -1 {
			nameIdx = i
		} else if lower == "value" && valIdx == -1 {
			valIdx = i
		}
	}

	if nameIdx == -1 || nameIdx+1 >= len(args) {
		return
	}

	var optName, valStr string
	if valIdx != -1 && valIdx > nameIdx {
		optName = strings.Join(args[nameIdx+1:valIdx], " ")
		if valIdx+1 < len(args) {
			valStr = strings.Join(args[valIdx+1:], " ")
		}
	} else {
		optName = strings.Join(args[nameIdx+1:], " ")
	}

	normName := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(optName, " ", ""), "_", ""))

	switch normName {
	case "hash":
		sizeMB, err := strconv.Atoi(strings.TrimSpace(valStr))
		if err == nil && sizeMB > 0 {
			u.engine.SetHash(sizeMB)
		}
	case "threads":
		threads, err := strconv.Atoi(strings.TrimSpace(valStr))
		if err == nil && threads > 0 {
			u.engine.SetThreads(threads)
		}
	case "ponder", "autoponder":
		valLower := strings.ToLower(strings.TrimSpace(valStr))
		if valLower == "true" || valLower == "1" {
			u.ponderEnabled = true
		} else if valLower == "false" || valLower == "0" {
			u.ponderEnabled = false
			u.stopSearch(false)
		}
	case "usennue":
		valLower := strings.ToLower(strings.TrimSpace(valStr))
		if valLower == "true" || valLower == "1" {
			nnue.Enabled = true
		} else if valLower == "false" || valLower == "0" {
			nnue.Enabled = false
		}
		logger.Infof("UCI Option Set: UseNNUE=%t", nnue.Enabled)
	case "evalfile":
		trimmedVal := strings.TrimSpace(valStr)
		if trimmedVal != "" && trimmedVal != "<embedded>" {
			err := nnue.LoadFile(trimmedVal)
			if err != nil {
				logger.Warnf("UCI Option Failed: EvalFile=%s err=%v", trimmedVal, err)
				u.sendLine(fmt.Sprintf("info string failed to load nnue file: %v", err))
			} else {
				u.evalFile = trimmedVal
				logger.Infof("UCI Option Set: EvalFile=%s (successfully loaded)", trimmedVal)
				u.sendLine(fmt.Sprintf("info string loaded nnue file %s", trimmedVal))
			}
		} else if trimmedVal == "<embedded>" {
			u.evalFile = "<embedded>"
			nnue.ResetToDefault()
			logger.Infof("UCI Option Set: EvalFile=<embedded> (reset to embedded network)")
			u.sendLine("info string reset to embedded nnue network")
		}
	case "syzygypath":
		valPath := strings.TrimSpace(valStr)
		if valPath == "<empty>" || valPath == "" {
			syzygy.Free()
			u.sendLine("info string syzygy tablebases disabled")
		} else {
			syzygy.Init(valPath)
			if syzygy.IsAvailable() {
				u.sendLine(fmt.Sprintf("info string found %d-piece Syzygy tablebases", syzygy.MaxPieces()))
			} else {
				u.sendLine("info string no Syzygy tablebases found at specified path")
			}
		}
	case "syzygyprobedepth":
		d, err := strconv.Atoi(strings.TrimSpace(valStr))
		if err == nil && d >= 1 && d <= 64 {
			syzygy.ProbeDepth = d
		}
	case "syzygy50moverule":
		valLower := strings.ToLower(strings.TrimSpace(valStr))
		if valLower == "true" || valLower == "1" {
			syzygy.Use50MoveRule = true
		} else if valLower == "false" || valLower == "0" {
			syzygy.Use50MoveRule = false
		}
	case "syzygyprobelimit":
		lim, err := strconv.Atoi(strings.TrimSpace(valStr))
		if err == nil && lim >= 0 && lim <= 7 {
			syzygy.ProbeLimit = lim
		}
	case "logfile", "log":
		valPath := strings.TrimSpace(valStr)
		if valPath == "<empty>" || valPath == "" {
			_ = logger.Close()
			u.sendLine("info string logging disabled")
		} else {
			if err := logger.Init(valPath); err != nil {
				u.sendLine(fmt.Sprintf("info string failed to open log file: %v", err))
			} else {
				u.sendLine(fmt.Sprintf("info string logging to %s", valPath))
			}
		}
	case "moveoverhead", "overhead":
		val, err := strconv.Atoi(strings.TrimSpace(valStr))
		if err == nil && val >= 0 && val <= 5000 {
			u.moveOverhead = val
			logger.Infof("UCI Option Set: Move Overhead=%d ms", val)
		}
	case "multipv":
		val, err := strconv.Atoi(strings.TrimSpace(valStr))
		if err == nil && val >= 1 && val <= 256 {
			u.engine.SetMultiPV(val)
			logger.Infof("UCI Option Set: MultiPV=%d", val)
		}
	case "ownbook", "usebook", "openingbook", "book":
		trimmedVal := strings.TrimSpace(valStr)
		valLower := strings.ToLower(trimmedVal)
		if valLower == "true" || valLower == "1" || valLower == "on" || valLower == "yes" {
			u.engine.SetUseBook(true)
			logger.Infof("UCI Option Set: OwnBook=true")
		} else if valLower == "false" || valLower == "0" || valLower == "off" || valLower == "no" || valLower == "none" || valLower == "<empty>" {
			u.engine.SetUseBook(false)
			logger.Infof("UCI Option Set: OwnBook=false")
		} else if normName == "book" && trimmedVal != "" {
			if trimmedVal == "<embedded>" {
				u.bookFile = "<embedded>"
				u.engine.SetBook(book.DefaultBook())
				u.engine.SetUseBook(true)
				logger.Infof("UCI Option Set: Book=<embedded>")
				u.sendLine("info string reset to embedded opening book")
			} else {
				bk, err := book.LoadFile(trimmedVal)
				if err != nil {
					logger.Warnf("UCI Option Failed: Book=%s err=%v", trimmedVal, err)
					u.sendLine(fmt.Sprintf("info string failed to load book file: %v", err))
				} else {
					u.bookFile = trimmedVal
					u.engine.SetBook(bk)
					u.engine.SetUseBook(true)
					logger.Infof("UCI Option Set: Book=%s (loaded %d entries)", trimmedVal, bk.EntryCount())
					u.sendLine(fmt.Sprintf("info string loaded book file %s (%d entries)", trimmedVal, bk.EntryCount()))
				}
			}
		}
	case "bookfile", "bookpath":
		trimmedVal := strings.TrimSpace(valStr)
		valLower := strings.ToLower(trimmedVal)
		if valLower == "none" || valLower == "<empty>" || valLower == "off" || valLower == "false" {
			u.engine.SetUseBook(false)
			logger.Infof("UCI Option Set: BookFile=%s (disabled opening book)", trimmedVal)
			u.sendLine("info string opening book disabled")
		} else if trimmedVal != "" && trimmedVal != "<embedded>" {
			bk, err := book.LoadFile(trimmedVal)
			if err != nil {
				logger.Warnf("UCI Option Failed: BookFile=%s err=%v", trimmedVal, err)
				u.sendLine(fmt.Sprintf("info string failed to load book file: %v", err))
			} else {
				u.bookFile = trimmedVal
				u.engine.SetBook(bk)
				u.engine.SetUseBook(true)
				logger.Infof("UCI Option Set: BookFile=%s (loaded %d entries)", trimmedVal, bk.EntryCount())
				u.sendLine(fmt.Sprintf("info string loaded book file %s (%d entries)", trimmedVal, bk.EntryCount()))
			}
		} else if trimmedVal == "<embedded>" || trimmedVal == "" {
			u.bookFile = "<embedded>"
			u.engine.SetBook(book.DefaultBook())
			logger.Infof("UCI Option Set: BookFile=<embedded> (reset to embedded book)")
			u.sendLine("info string reset to embedded opening book")
		}
	default:
		// Search tunables (see internal/search/params.go)
		if v, err := strconv.Atoi(strings.TrimSpace(valStr)); err == nil {
			if search.SetParam(optName, int32(v)) {
				logger.Infof("UCI Option Set: %s=%d", optName, v)
			}
		}
	}
}

func (u *UCI) cmdUCINewGame() {
	u.stopSearch(false)
	u.engine.TT.Clear()
	b, _ := board.ParseFEN(board.StartFEN)
	u.board = b

	if opening := book.FindOpening(b); opening.Name != "" {
		logger.Infof("Position Opening: %s", opening)
	}
}

func (u *UCI) stopSearch(requestedByGUI bool) {
	if requestedByGUI {
		u.guiStop.Store(true)
	}
	u.abortSearch.Store(true)
	u.autoPonderCancel.Store(true)
	u.engine.Stop()

	// Wait for official search or auto-ponder to stop cleanly (up to 3 seconds)
	for i := 0; i < 3000 && (u.searching.Load() || u.isAutoPonder.Load()); i++ {
		time.Sleep(1 * time.Millisecond)
	}

	// If GUI sent "stop" and no search was running to emit bestmove, emit best move from TT or position
	if requestedByGUI && !u.bestMoveSent.Load() {
		u.bestMoveSent.Store(true)
		if ttM, _, _, _, found := u.engine.TT.Probe(u.board.Hash); found && ttM != types.MoveNone && isLegal(u.board, ttM) {
			u.sendLine(fmt.Sprintf("bestmove %s", ttM))
		} else {
			legals := movegen.GenerateLegalMoves(u.board)
			if len(legals) > 0 {
				bestIdx := 0
				bestVal := int32(-1000000)
				for i, m := range legals {
					val := int32(0)
					if m.IsCapture() {
						val = 10000 + eval.SEE(u.board, m)
					} else if u.board.GivesCheck(m) {
						val = 5000
					}
					if val > bestVal {
						bestVal = val
						bestIdx = i
					}
				}
				u.sendLine(fmt.Sprintf("bestmove %s", legals[bestIdx]))
			} else {
				u.sendLine("bestmove 0000")
			}
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

	if opening := book.FindOpening(b); opening.Name != "" {
		logger.Infof("Position Opening: %s", opening)
	}
}

func (u *UCI) cmdGo(args []string) {
	// Stop any running search or auto-ponder first
	u.stopSearch(false)
	u.engine.Stopped.Store(false)
	u.guiStop.Store(false)
	u.abortSearch.Store(false)
	u.autoPonderCancel.Store(false)
	u.bestMoveSent.Store(false)

	var limits search.SearchLimits
	limits.MoveOverhead = int64(u.moveOverhead)

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

	// In UCI protocol: "If no parameters are sent, search until stop."
	hasTimeLimit := limits.WhiteTime > 0 || limits.BlackTime > 0 || limits.MoveTime > 0
	hasBoundLimit := limits.MaxDepth > 0 || limits.MaxNodes > 0 || limits.Infinite || limits.Ponder
	if !hasTimeLimit && !hasBoundLimit {
		limits.Infinite = true
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

		// Normal search completed: emit bestmove to GUI
		u.bestMoveSent.Store(true)
		if ponderMove != types.MoveNone {
			u.sendLine(fmt.Sprintf("bestmove %s ponder %s", bestMove, ponderMove))
		} else {
			u.sendLine(fmt.Sprintf("bestmove %s", bestMove))
		}

		// Mark GUI search complete immediately so Loop is never blocked
		u.searching.Store(false)

		// Speculative background calculation (auto-ponder):
		// Runs in a detached goroutine while waiting for opponent move
		if u.ponderEnabled && bestMove != types.MoveNone && !u.abortSearch.Load() && !u.guiStop.Load() && !u.autoPonderCancel.Load() {
			go u.startAutoPonder(currentBoard, bestMove, ponderMove)
		}
	}()
}

func (u *UCI) startAutoPonder(currentBoard *board.Board, bestMove, ponderMove types.Move) {
	if !u.ponderEnabled || bestMove == types.MoveNone || u.abortSearch.Load() || u.guiStop.Load() || u.autoPonderCancel.Load() {
		return
	}

	opponentMove := ponderMove
	if opponentMove == types.MoveNone {
		tempB := currentBoard.Clone()
		if tempB.MakeMove(bestMove) {
			if ttM, _, _, _, found := u.engine.TT.Probe(tempB.Hash); found && ttM != types.MoveNone && isLegal(tempB, ttM) {
				opponentMove = ttM
			} else {
				oppLegals := movegen.GenerateLegalMoves(tempB)
				if len(oppLegals) > 0 {
					opponentMove = oppLegals[0]
				}
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

	if u.abortSearch.Load() || u.guiStop.Load() || u.autoPonderCancel.Load() {
		return
	}

	u.autoPonderHash.Store(hypoBoard.Hash)
	u.isAutoPonder.Store(true)
	defer func() {
		u.isAutoPonder.Store(false)
		u.autoPonderHash.Store(0)
	}()

	autoLimits := search.SearchLimits{Infinite: true, Ponder: true}
	// Silent background search: pre-warms Transposition Table
	u.engine.Search(hypoBoard, autoLimits, nil)
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
	var nnueScore int32
	if nnue.ActiveEightNetwork != nil {
		nnueScore = nnue.EvaluateEight(nnue.ActiveEightNetwork, u.board)
	} else if nnue.ActiveNetwork != nil {
		var acc nnue.Accumulator
		nnue.Refresh(u.board, &acc)
		nnueScore = nnue.Evaluate(&acc, u.board.SideToMove)
	}

	active := "NNUE"
	activeScore := nnueScore
	if !nnue.Enabled {
		active = "Classical"
		activeScore = classicalScore
	}

	netInfo := nnue.GetNetworkInfo()
	u.sendLine(fmt.Sprintf("NNUE architecture     : %s", netInfo.Architecture))
	u.sendLine(fmt.Sprintf("NNUE evaluation       : %+d cp", nnueScore))
	u.sendLine(fmt.Sprintf("Classical evaluation  : %+d cp", classicalScore))
	u.sendLine(fmt.Sprintf("Active evaluation (%s): %+d cp (from %s's perspective)", active, activeScore, u.board.SideToMove))
	logger.Infof("Evaluation Probe | FEN: %s | SideToMove: %s | Active: %s (%+d cp) | NNUE: %+d cp | Classical: %+d cp",
		u.board.ToFEN(), u.board.SideToMove, active, activeScore, nnueScore, classicalScore)
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
	oldBook := u.engine.UseBook
	u.engine.UseBook = false
	defer func() {
		u.engine.UseBook = oldBook
	}()
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
