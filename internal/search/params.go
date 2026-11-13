package search

import (
	"math"
	"strings"
)

// Params holds the pruning and reduction constants that are exposed as UCI
// options so they can be tuned (see scripts/spsa.py) without a rebuild.
type Params struct {
	LMRBase       int32 // LMR table base, in hundredths (40 = 0.40)
	LMRDivisor    int32 // LMR table divisor, in hundredths (230 = 2.30)
	LMRHistDiv    int32 // LMR: one ply less/more reduction per this much history
	RFPMargin     int32 // reverse futility margin per ply of depth
	RFPNonImpr    int32 // extra RFP margin per ply when not improving
	RazorMargin   int32
	NMPEvalDiv    int32 // extra null-move reduction per this much eval above beta
	LMPMultImpr   int32 // LMP threshold = base + mult*depth^2 (improving)
	LMPMultNonImp int32 // same, not improving
	FutMargin     int32 // futility margin per ply of depth
	HistPrImpr    int32 // history pruning margin per ply (improving)
	HistPrNonImp  int32 // same, not improving
	SEEQuietMul   int32 // SEE pruning margin per ply, quiet moves
	SEENoisyMul   int32 // SEE pruning margin per ply, captures
	QSFutMargin   int32 // quiescence per-capture futility margin
	CheckExtSEE   int32 // extend checks whose SEE is at least -CheckExtSEE (0: only non-losing checks)
	QSChecks      int32 // 1: try quiet checks (SEE >= 0) at the first quiescence ply

	// Depth limits of the pruning rules. Futility, history and quiet SEE
	// pruning compare against the move's reduced depth (depth minus its
	// late-move reduction), the others against the node's depth.
	RFPDepth  int32 // reverse futility pruning
	LMPDepth  int32 // late-move pruning
	FutDepth  int32 // futility pruning (reduced depth)
	HistDepth int32 // history pruning (reduced depth)
	SEEDepth  int32 // SEE pruning

	// Switches for rules that hold the pruning back.
	RFPGuard  int32 // no reverse futility pruning: 2 with the king under pressure, 1 also with a piece under threat, 0 no guard
	NMPGuard  int32 // no null move: 2 with the king under pressure, 1 also with the queen under threat, 0 no guard
	CheckExt  int32 // check extension: 2 at every node, 1 at PV nodes only, 0 never
	LMRChecks int32 // 1: moves that give check take late-move reductions (one ply less)

	// Strength of the reductions and of the pruning before the move loop.
	LMRMinMove     int32 // late-move reductions start once this many moves have been searched
	LMRCutNode     int32 // extra plies of reduction at expected cut nodes
	LMRExempt      int32 // 1: threat moves, threat evasions and advanced pawn pushes are never reduced; 0: they are reduced one ply less
	PruneExempt    int32 // 1: those moves are also exempt from late-move, futility, history and SEE pruning; 0: only killers and the countermove are
	NMPBase        int32 // null-move reduction = NMPBase + depth/NMPDepthDiv (+ eval and improving terms)
	NMPDepthDiv    int32
	IIRDepth       int32 // internal iterative reduction from this depth, when there is no hash move
	IIRPV          int32 // 1: also at PV nodes
	RazorDepth     int32 // razoring up to this depth
	RazorStep      int32 // razoring margin grows by this per ply of depth above 1
	SEDoubleMargin int32 // a singular move is extended two plies when the alternatives fall this far below the singular bound (fail-soft only: a fail-hard search never reports how far)
	SENegExt       int32 // 1: a hash move that is not singular is searched a ply shallower when the node is expected to fail high anyway
	PawnPushExt    int32 // extension for pawn pushes to the 7th rank: 2 at every node, 1 at PV nodes only, 0 never

	// FailSoft selects where the search returns the score it found rather
	// than the bound it was searching against (a sum of the soft* bits).
	FailSoft int32

	ProbCut       int32 // 1: ProbCut before the hash move, at every non-PV node of depth >= 5; 0: off (no gain measured in self-play)
	ProbCutMargin int32 // ProbCut searches against beta plus this
}

// Bits of Params.FailSoft.
const (
	softMoves     = 1 << iota // the move loop returns and stores the best score found
	softQS                    // so does quiescence
	softPruning               // reverse futility, null move, multi-cut and ProbCut return their own score, not beta
	softUnguarded             // a fail-low node reports its best score even when moves were pruned unsearched
	softCorr                  // correction history learns from scores outside the window
)

// P is the live parameter set used by Search.
var P = Params{
	LMRBase:       100,
	LMRDivisor:    200,
	LMRHistDiv:    5000,
	RFPMargin:     75,
	RFPNonImpr:    35,
	RazorMargin:   350,
	NMPEvalDiv:    200,
	LMPMultImpr:   1,
	LMPMultNonImp: 1,
	FutMargin:     90,
	HistPrImpr:    1024,
	HistPrNonImp:  512,
	SEEQuietMul:   60,
	SEENoisyMul:   200,
	QSFutMargin:   175,
	CheckExtSEE:   0,
	QSChecks:      0,
	RFPDepth:      8,
	LMPDepth:      8,
	FutDepth:      8,
	HistDepth:     6,
	SEEDepth:      8,
	// Reverse futility pruning unguarded; null move guarded only by king
	// safety. With both guards at 1 the tree was three times the size. With
	// both at 0 it is no smaller than this, but a quiet mating threat behind
	// a sacrifice (WAC 55, mate in 4) goes unseen until depth 16: the null
	// move hands the attacker the extra tempo the mate needs.
	RFPGuard: 0,
	NMPGuard: 2,
	// Every check extended and never reduced. Extending at PV nodes only and
	// reducing checks also won in self-play, but misses more tactics.
	CheckExt:  2,
	LMRChecks: 0,

	LMRMinMove:     1,
	LMRCutNode:     1,
	LMRExempt:      0,
	PruneExempt:    1,
	NMPBase:        4,
	NMPDepthDiv:    3,
	IIRDepth:       4,
	IIRPV:          1,
	RazorDepth:     3,
	RazorStep:      250,
	SEDoubleMargin: 25,
	SENegExt:       1,
	PawnPushExt:    1,

	// Fail hard. Fail-soft has not shown a gain in self-play. With razoring
	// also returning its quiescence score it lost (everything on -35 Elo
	// +-23; move loop and quiescence only -21 +-18). With razoring returning
	// alpha, as it now always does, move loop and quiescence scored -8 +-16
	// over 606 games: no worse than level, no better either.
	FailSoft: 0,

	ProbCut:       0,
	ProbCutMargin: 200,
}

// ParamSpec describes one tunable for UCI option listing and the tuner.
type ParamSpec struct {
	Name     string
	Ptr      *int32
	Min, Max int32
}

// TunableParams lists every tunable with its bounds, in a stable order.
func TunableParams() []ParamSpec {
	return []ParamSpec{
		{"LMRBase", &P.LMRBase, 0, 200},
		{"LMRDivisor", &P.LMRDivisor, 100, 500},
		{"LMRHistDiv", &P.LMRHistDiv, 1000, 20000},
		{"RFPMargin", &P.RFPMargin, 20, 200},
		{"RFPNonImpr", &P.RFPNonImpr, 0, 150},
		{"RazorMargin", &P.RazorMargin, 100, 800},
		{"NMPEvalDiv", &P.NMPEvalDiv, 50, 600},
		{"LMPMultImpr", &P.LMPMultImpr, 1, 8},
		{"LMPMultNonImp", &P.LMPMultNonImp, 1, 8},
		{"FutMargin", &P.FutMargin, 30, 250},
		{"HistPrImpr", &P.HistPrImpr, 128, 4096},
		{"HistPrNonImp", &P.HistPrNonImp, 64, 4096},
		{"SEEQuietMul", &P.SEEQuietMul, 10, 200},
		{"SEENoisyMul", &P.SEENoisyMul, 50, 500},
		{"QSFutMargin", &P.QSFutMargin, 0, 600},
		{"CheckExtSEE", &P.CheckExtSEE, 0, 2000},
		{"QSChecks", &P.QSChecks, 0, 1},
		{"RFPDepth", &P.RFPDepth, 1, 14},
		{"LMPDepth", &P.LMPDepth, 1, 14},
		{"FutDepth", &P.FutDepth, 1, 14},
		{"HistDepth", &P.HistDepth, 1, 14},
		{"SEEDepth", &P.SEEDepth, 1, 14},
		{"RFPGuard", &P.RFPGuard, 0, 2},
		{"NMPGuard", &P.NMPGuard, 0, 2},
		{"CheckExt", &P.CheckExt, 0, 2},
		{"LMRChecks", &P.LMRChecks, 0, 1},
		{"LMRMinMove", &P.LMRMinMove, 1, 6},
		{"LMRCutNode", &P.LMRCutNode, 0, 3},
		{"LMRExempt", &P.LMRExempt, 0, 1},
		{"PruneExempt", &P.PruneExempt, 0, 1},
		{"NMPBase", &P.NMPBase, 2, 6},
		{"NMPDepthDiv", &P.NMPDepthDiv, 2, 8},
		{"IIRDepth", &P.IIRDepth, 2, 12},
		{"IIRPV", &P.IIRPV, 0, 1},
		{"RazorDepth", &P.RazorDepth, 1, 6},
		{"RazorStep", &P.RazorStep, 0, 600},
		{"SEDoubleMargin", &P.SEDoubleMargin, 0, 10000},
		{"SENegExt", &P.SENegExt, 0, 1},
		{"PawnPushExt", &P.PawnPushExt, 0, 2},
		{"FailSoft", &P.FailSoft, 0, 31},
		{"ProbCut", &P.ProbCut, 0, 1},
		{"ProbCutMargin", &P.ProbCutMargin, 50, 500},
	}
}

// SetParam sets a tunable by (case-insensitive) name. Returns false if the
// name is unknown. Values are clamped to the declared bounds.
func SetParam(name string, v int32) bool {
	for _, ps := range TunableParams() {
		if strings.EqualFold(ps.Name, name) {
			if v < ps.Min {
				v = ps.Min
			}
			if v > ps.Max {
				v = ps.Max
			}
			*ps.Ptr = v
			if ps.Ptr == &P.LMRBase || ps.Ptr == &P.LMRDivisor {
				rebuildLMRTable()
			}
			return true
		}
	}
	return false
}

func rebuildLMRTable() {
	base := float64(P.LMRBase) / 100
	div := float64(P.LMRDivisor) / 100
	for d := 1; d < 128; d++ {
		for m := 1; m < 128; m++ {
			r := base + math.Log(float64(d))*math.Log(float64(m))/div
			if r < 0 {
				r = 0
			}
			LMRTable[d][m] = int8(r)
		}
	}
}

func init() {
	rebuildLMRTable()
}
