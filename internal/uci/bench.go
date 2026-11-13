package uci

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"devilfish/internal/board"
	"devilfish/internal/eval"
	"devilfish/internal/movegen"
	"devilfish/internal/nnue"
	"devilfish/internal/search"
)

const benchRule = "==============================================="

// BenchUsage lists the benchmark modes, one per line.
var BenchUsage = []string{
	"bench [depth]               Search the bench positions to a fixed depth (default 7)",
	"bench time [ms]             Search each position for a fixed time (default 1000 ms): nodes/sec and depth reached",
	"bench threads [ms] [max]    Search speed at every thread count up to max (default: all CPUs), and the fastest setting",
	"bench perft [depth]         Move generator speed (default depth 5)",
	"bench eval [ms]             Static evaluations per second (default 1000 ms per evaluator)",
	"bench all                   The fixed-depth, fixed-time, perft and eval benchmarks in turn",
}

// Bench runs the fixed-depth benchmark.
func (u *UCI) Bench(depth int8) {
	u.cmdBench([]string{strconv.Itoa(int(depth))})
}

// RunBench runs a benchmark by its arguments (see BenchUsage).
func (u *UCI) RunBench(args []string) {
	u.cmdBench(args)
}

// benchInts reads the leading positive integers of args, falling back to
// defaults for those missing or malformed.
func benchInts(args []string, defaults ...int) []int {
	out := append([]int(nil), defaults...)
	for i := range out {
		if i < len(args) {
			if v, err := strconv.Atoi(args[i]); err == nil && v > 0 {
				out[i] = v
			}
		}
	}
	return out
}

func (u *UCI) cmdBench(args []string) {
	u.stopSearch(false)
	u.abortSearch.Store(false)
	oldBook := u.engine.UseBook
	u.engine.UseBook = false
	defer func() {
		u.engine.UseBook = oldBook
	}()

	mode := ""
	if len(args) > 0 {
		mode = strings.ToLower(args[0])
	}
	switch mode {
	case "time", "speed":
		u.benchTime(int64(benchInts(args[1:], 1000)[0]))
	case "threads", "smp":
		v := benchInts(args[1:], 1000, runtime.NumCPU())
		u.benchThreads(int64(v[0]), v[1])
	case "perft", "movegen":
		u.benchPerft(benchInts(args[1:], 5)[0])
	case "eval", "nnue":
		u.benchEval(int64(benchInts(args[1:], 1000)[0]))
	case "all":
		u.benchDepth(7)
		u.benchTime(1000)
		u.benchPerft(5)
		u.benchEval(1000)
	case "help", "-h", "--help":
		for _, l := range BenchUsage {
			u.sendLine(l)
		}
	case "depth":
		u.benchDepth(int8(min(benchInts(args[1:], 7)[0], 64)))
	default:
		u.benchDepth(int8(min(benchInts(args, 7)[0], 64)))
	}
}

// benchResult is the outcome of searching every bench position once.
type benchResult struct {
	nodes   uint64
	elapsed time.Duration
	depth   int // sum of the depths completed
	count   int // positions searched
}

func (r benchResult) nps() float64 {
	if r.elapsed <= 0 {
		return 0
	}
	return float64(r.nodes) / r.elapsed.Seconds()
}

func (r benchResult) avgDepth() float64 {
	if r.count == 0 {
		return 0
	}
	return float64(r.depth) / float64(r.count)
}

// benchSearch searches every bench position with limits, each from a cleared
// hash table and history, and calls report (if not nil) after each one.
func (u *UCI) benchSearch(limits search.SearchLimits, report func(i int, bestMove string, nodes uint64, depth int, elapsed time.Duration)) benchResult {
	var res benchResult
	for i, fen := range benchPositions {
		b, err := board.ParseFEN(fen)
		if err != nil {
			continue
		}
		u.engine.NewGame()
		depth := 0
		start := time.Now()
		bestMove, _ := u.engine.Search(b, limits, func(si search.SearchInfo) {
			if int(si.Depth) > depth {
				depth = int(si.Depth)
			}
		})
		elapsed := time.Since(start)
		nodes := u.engine.Nodes.Load()
		res.nodes += nodes
		res.elapsed += elapsed
		res.depth += depth
		res.count++
		if report != nil {
			report(i, bestMove.String(), nodes, depth, elapsed)
		}
	}
	return res
}

func (u *UCI) benchSummary(res benchResult) {
	u.sendLine(benchRule)
	u.sendLine(fmt.Sprintf("Total nodes: %d", res.nodes))
	u.sendLine(fmt.Sprintf("Total time : %v", res.elapsed.Round(100*time.Microsecond)))
	u.sendLine(fmt.Sprintf("Nodes/sec  : %.0f", res.nps()))
	u.sendLine(benchRule)
}

// benchDepth searches each position to a fixed depth. With one thread the
// node count is reproducible, which makes it a check that a change meant to
// be speed-only did not alter the search.
func (u *UCI) benchDepth(depth int8) {
	u.sendLine(fmt.Sprintf("Running benchmark at depth %d with %d threads...", depth, u.engine.NumThreads))
	n := len(benchPositions)
	res := u.benchSearch(search.SearchLimits{MaxDepth: depth}, func(i int, bestMove string, nodes uint64, _ int, elapsed time.Duration) {
		u.sendLine(fmt.Sprintf("Position %d/%d: bestmove %s | nodes %d | time %v", i+1, n, bestMove, nodes, elapsed))
	})
	u.benchSummary(res)
}

// benchTime searches each position for a fixed time, which is how the engine
// is used in games: the result is the search speed at the current settings.
func (u *UCI) benchTime(ms int64) {
	u.sendLine(fmt.Sprintf("Running benchmark at %d ms per position with %d threads, %d MB hash...", ms, u.engine.NumThreads, u.engine.TT.SizeMB()))
	n := len(benchPositions)
	res := u.benchSearch(search.SearchLimits{MoveTime: ms}, func(i int, bestMove string, nodes uint64, depth int, elapsed time.Duration) {
		u.sendLine(fmt.Sprintf("Position %d/%d: bestmove %s | depth %d | nodes %d | nps %.0f", i+1, n, bestMove, depth, nodes, float64(nodes)/elapsed.Seconds()))
	})
	u.benchSummary(res)
	u.sendLine(fmt.Sprintf("Average depth: %.1f", res.avgDepth()))
}

// benchThreadCounts returns the thread counts to try up to maxThreads: every
// count on ordinary machines, a spread of them on very large ones.
func benchThreadCounts(maxThreads int) []int {
	if maxThreads <= 16 {
		counts := make([]int, maxThreads)
		for i := range counts {
			counts[i] = i + 1
		}
		return counts
	}
	counts := []int{1, 2, 4, 6, 8, 12, 16}
	for n := 24; n < maxThreads; n += n / 3 {
		counts = append(counts, n)
	}
	return append(counts, maxThreads)
}

// benchWithinBest: thread counts whose speed is within this share of the
// fastest count as equally fast, and the smallest of them is recommended.
// Repeated runs of one setting differ by several percent, so a narrower
// band would recommend whichever count happened to get a quiet moment.
const benchWithinBest = 0.95

// benchThreads measures search speed at each thread count and reports the
// fastest. More threads are not always faster: threads beyond the fast cores
// (efficiency cores, or cores other programs are using) can slow all of them.
func (u *UCI) benchThreads(ms int64, maxThreads int) {
	saved := u.engine.NumThreads
	defer u.engine.SetThreads(saved)

	counts := benchThreadCounts(maxThreads)
	u.sendLine(fmt.Sprintf("Thread benchmark: %d positions x %d ms at each of %d thread counts (about %d s), %d MB hash",
		len(benchPositions), ms, len(counts), int64(len(counts)*len(benchPositions))*ms/1000, u.engine.TT.SizeMB()))

	// Warm up: fault in the hash table and start the worker goroutines.
	u.engine.SetThreads(maxThreads)
	u.benchSearch(search.SearchLimits{MoveTime: min(ms, 100)}, nil)

	u.sendLine("")
	u.sendLine("Threads     Nodes/sec   Speedup   Per thread   Avg depth")
	results := make([]benchResult, len(counts))
	best := 0
	for i, n := range counts {
		u.engine.SetThreads(n)
		results[i] = u.benchSearch(search.SearchLimits{MoveTime: ms}, nil)
		nps := results[i].nps()
		base := results[0].nps()
		speedup := 0.0
		if base > 0 {
			speedup = nps / base
		}
		u.sendLine(fmt.Sprintf("%7d  %12.0f  %7.2fx  %10.0f%%  %10.1f", n, nps, speedup, 100*speedup/float64(n)*float64(counts[0]), results[i].avgDepth()))
		if nps > results[best].nps() {
			best = i
		}
	}

	// Prefer the fewest threads that are as fast as the fastest.
	pick := best
	for i := range counts {
		if results[i].nps() >= benchWithinBest*results[best].nps() {
			pick = i
			break
		}
	}
	u.sendLine("")
	u.sendLine(fmt.Sprintf("Fastest: %d threads (%.0f nodes/sec)", counts[best], results[best].nps()))
	if pick != best {
		u.sendLine(fmt.Sprintf("Recommended: %d threads (%.0f nodes/sec, within %.0f%% of the fastest with fewer threads)",
			counts[pick], results[pick].nps(), 100*(1-benchWithinBest)))
	} else {
		u.sendLine(fmt.Sprintf("Recommended: %d threads", counts[pick]))
	}
	u.sendLine("Run to run, results vary by several percent: close other programs, and use a longer time (e.g. bench threads 3000) to separate close settings.")
	u.sendLine("If another engine or a GUI shares this machine during games, leave cores free for it.")
	if counts[pick] != saved {
		u.sendLine(fmt.Sprintf("Currently using %d. To change: setoption name Threads value %d, --threads %d, or \"threads\": %d in config.json",
			saved, counts[pick], counts[pick], counts[pick]))
	} else {
		u.sendLine(fmt.Sprintf("Currently using %d: no change needed", saved))
	}
}

// benchPerft measures the move generator and make/unmake with perft.
func (u *UCI) benchPerft(depth int) {
	u.sendLine(fmt.Sprintf("Running perft benchmark at depth %d...", depth))
	var total uint64
	var elapsed time.Duration
	n := len(benchPositions)
	for i, fen := range benchPositions {
		b, err := board.ParseFEN(fen)
		if err != nil {
			continue
		}
		start := time.Now()
		nodes := movegen.Perft(b, depth)
		d := time.Since(start)
		total += nodes
		elapsed += d
		u.sendLine(fmt.Sprintf("Position %d/%d: nodes %d | time %v", i+1, n, nodes, d))
	}
	u.benchSummary(benchResult{nodes: total, elapsed: elapsed})
}

// benchEval measures static evaluations per second, one thread, each
// evaluation computed from scratch (the search's incremental updates and
// eval cache make its own cost per node lower than this).
func (u *UCI) benchEval(ms int64) {
	var boards []*board.Board
	for _, fen := range benchPositions {
		if b, err := board.ParseFEN(fen); err == nil {
			boards = append(boards, b)
		}
	}
	if len(boards) == 0 {
		return
	}
	run := func(name string, evaluate func(b *board.Board) int32) {
		limit := time.Duration(ms) * time.Millisecond
		var count uint64
		var sink int32
		start := time.Now()
		for time.Since(start) < limit {
			for i := 0; i < 64; i++ {
				sink += evaluate(boards[i%len(boards)])
			}
			count += 64
		}
		elapsed := time.Since(start)
		_ = sink
		u.sendLine(fmt.Sprintf("%-10s %10.0f evals/sec  (%.0f ns each)", name, float64(count)/elapsed.Seconds(), float64(elapsed.Nanoseconds())/float64(count)))
	}

	u.sendLine(fmt.Sprintf("Running evaluation benchmark, %d ms per evaluator, 1 thread...", ms))
	if net := nnue.ActiveEightNetwork; net != nil {
		run("NNUE", func(b *board.Board) int32 { return nnue.EvaluateEight(net, b) })
	} else if nnue.ActiveNetwork != nil {
		run("NNUE", func(b *board.Board) int32 {
			var acc nnue.Accumulator
			nnue.Refresh(b, &acc)
			return nnue.Evaluate(&acc, b.SideToMove)
		})
	}
	run("Classical", eval.EvaluateClassical)
}
