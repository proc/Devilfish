package uci

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"devilfish/internal/search"
	"devilfish/internal/testsuite"
)

// TestSuiteResult summarizes a test suite run.
type TestSuiteResult struct {
	Solved, Total int
	Failed        []string // ids of unsolved positions
	Nodes         uint64
	Elapsed       time.Duration
}

// RunTestSuite runs an EPD test suite (see cmdTestSuite) and prints a report.
func (u *UCI) RunTestSuite(args []string) TestSuiteResult {
	return u.cmdTestSuite(args)
}

// cmdTestSuite handles "testsuite [wac|<file.epd>] [movetime_ms]" and
// "wac [movetime_ms]". Each position is searched from a cleared hash table with
// the book off; it counts as solved if the final best move is a "bm" move (and
// not an "am" move). The time column shows when the engine settled on the
// right move for good.
func (u *UCI) cmdTestSuite(args []string) TestSuiteResult {
	name, movetime := "wac", int64(1000)
	for _, a := range args {
		if ms, err := strconv.ParseInt(a, 10, 64); err == nil && ms > 0 {
			movetime = ms
		} else {
			name = a
		}
	}
	var res TestSuiteResult
	positions, err := testsuite.Load(name)
	if err != nil {
		u.sendLine(fmt.Sprintf("info string testsuite: %v", err))
		return res
	}

	u.stopSearch(false)
	u.abortSearch.Store(false)
	oldBook := u.engine.UseBook
	u.engine.UseBook = false
	defer func() { u.engine.UseBook = oldBook }()

	res.Total = len(positions)
	u.sendLine(fmt.Sprintf("Running test suite %s: %d positions, %d ms each, %d threads, %d MB hash",
		name, len(positions), movetime, u.engine.NumThreads, u.engine.TT.SizeMB()))
	limits := search.SearchLimits{MoveTime: movetime}
	start := time.Now()
	for i := range positions {
		p := &positions[i]
		u.engine.NewGame()

		solvedAt := int64(-1)
		info := func(si search.SearchInfo) {
			if len(si.PV) == 0 || si.MultiPV > 1 {
				return
			}
			if p.Solved(si.PV[0]) {
				if solvedAt < 0 {
					solvedAt = si.TimeMs
				}
			} else {
				solvedAt = -1
			}
		}
		best, _ := u.engine.Search(p.Board.Clone(), limits, info)
		res.Nodes += u.engine.Nodes.Load()

		status, at := "FAIL", ""
		if p.Solved(best) {
			res.Solved++
			status = "ok  "
			if solvedAt >= 0 {
				at = fmt.Sprintf(" (%d ms)", solvedAt)
			}
		} else {
			res.Failed = append(res.Failed, p.ID)
		}
		u.sendLine(fmt.Sprintf("%3d/%d %-8s %s played %-7s expected %s%s",
			i+1, len(positions), p.ID, status, testsuite.SAN(p.Board, best), p.Expected(), at))
	}
	res.Elapsed = time.Since(start)

	u.sendLine("===============================================")
	u.sendLine(fmt.Sprintf("Solved     : %d/%d (%.1f%%)", res.Solved, res.Total, 100*float64(res.Solved)/float64(res.Total)))
	u.sendLine(fmt.Sprintf("Total time : %v", res.Elapsed.Round(time.Millisecond)))
	u.sendLine(fmt.Sprintf("Nodes/sec  : %.0f", float64(res.Nodes)/res.Elapsed.Seconds()))
	if len(res.Failed) > 0 {
		u.sendLine("Failed     : " + strings.Join(res.Failed, " "))
	}
	u.sendLine("===============================================")
	return res
}
