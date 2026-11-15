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
	NMPGuard  int32 // no null move: 2 with the king under pressure, 1 also with the queen under threat, 0 no guard; 3: no guard, but a cutoff with the king under pressure or at depth >= 16 needs a verification search
	CheckExt  int32 // check extension: 2 at every node, 1 at PV nodes only, 0 never
	LMRChecks int32 // 1: moves that give check take late-move reductions (one ply less)

	// Strength of the reductions and of the pruning before the move loop.
	LMRMinMove     int32 // late-move reductions start once this many moves have been searched
	LMRCutNode     int32 // extra plies of reduction at expected cut nodes
	LMRExempt      int32 // 1: threat moves, threat evasions and advanced pawn pushes are never reduced; 0: they are reduced one ply less
	PruneExempt    int32 // 1: those moves are also exempt from late-move, futility, history and SEE pruning; 0: only killers and the countermove are
	NMPBase        int32 // null-move reduction = NMPBase + depth/NMPDepthDiv (+ eval and improving terms)
	NMPDepthDiv    int32
	NMPMargin      int32 // null move only with the evaluation this far above beta, less NMPMarginStep per ply of depth
	NMPMarginStep  int32
	NMPNonImpr     int32 // extra plies of null-move reduction when not improving
	FutBase        int32 // futility margin = FutBase + FutMargin per ply of reduced depth
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

	// Quiescence and in-check pruning.
	QSEvenPrune    int32 // 1: with the stand-pat score well below alpha, captures that do not win material are skipped
	QSMaxMoves     int32 // captures searched per quiescence node before the rest (unless they give check) are skipped; 0: no limit
	QSEvasionPrune int32 // 1: in check in quiescence, once one evasion holds, quiet evasions after the second move and losing captures are skipped
	CheckLMR       int32 // 1: late evasions are reduced like other late moves (one ply less)
	CheckSEEPrune  int32 // 1: SEE pruning also applies to evasions

	// The options from here to HistUpdate were added together. The WAC
	// figures in their comments were measured with the network the unit
	// tests run on (8Chess), not the Stockfish network. With the Stockfish
	// network, in 100k / 400k nodes: everything as set here 273 / 288;
	// ProbCut, Cycle and HistExtra off 279 / 286; LMRMode 0 277 / 289;
	// LMRMode 1 253 / 269; HistUpdate 0 281 / 287; MoveOrder 0 275 / 290;
	// NMPGuard 3 270 / 283; TTPV 1 271 / 285. Differences of four or five
	// positions are within the suite's noise.
	//
	// MoveOrder 1: Stockfish-style move ordering in the main search (see
	// picker_sf.go); 2: in quiescence as well (scoreQuiescenceSF), which
	// then also tries the hash move first (against 1, to depth 18 on 60
	// positions: 96% of the nodes, 1.4% fewer nodes per second).
	// 0: the staged picker with killers, counter-move and
	// hand-written quiet-move classes. Against 0, to depth 18 on 60
	// positions: the same tree size (101%), 9% more nodes per second,
	// first-move cutoffs 84.0% against 83.0%. Not match-tested.
	MoveOrder int32

	// LMRMode 2: the late-move reductions are unchanged, but a reduced
	// search that beats alpha is searched again a ply deeper than normal if
	// it beat the best score so far by 18 cp, and a ply shallower (or not
	// at all) if by less than 3 cp, as in Stockfish; the move also gains
	// continuation history. Against 0, to depth 18 on 60 positions: 93% of
	// the nodes; WAC positions solved in 100k / 400k nodes: 267 and 276
	// against 269 and 276. Not match-tested.
	//
	// LMRMode 1: Stockfish's whole reduction formula as well (sfReduction
	// in search.go), in place of the LMR table, the killer, threat,
	// pawn-push and check exemptions and the separate rule for captures.
	// Measured worse here: 120% of the nodes (73% without its two
	// extension rules), and 238 and 257 WAC positions. Its history term
	// assumes HistUpdate 1.
	LMRMode int32

	// TTPV 1: positions that have been on a principal variation (a flag kept
	// in the hash table, as in Stockfish) are treated as PV nodes by two
	// rules even when searched with a null window: no reverse futility
	// pruning, and no extra ply of late-move reduction. The flag is stored
	// either way. Measured worse: 112% of the nodes to depth 18, WAC 264 /
	// 277 against 266 / 282. LMRMode 1 reads the flag too and is no better
	// for it (156%, 239 / 259).
	TTPV int32

	// Cycle 1: a node where the side to move can repeat an earlier position
	// with one move is worth at least a draw to it, before anything is
	// searched (Stockfish's upcoming-repetition detection). No effect on
	// the opening and tactical test sets (101% of the nodes, WAC 261 / 278
	// against 266 / 282, within their noise); it is meant for drawn
	// endgames, which those sets do not contain.
	Cycle int32

	// HistExtra: further Stockfish history rules, a sum of
	//   1  after a quiet move, its history moves by how the static
	//      evaluation changed across it;
	//   2  the history update also runs when a move raised alpha without
	//      a cutoff, not only on cutoffs;
	//   4  a quiet move's history shifts the depth its futility, history
	//      and exchange pruning are judged at;
	//   8  a hash-table cutoff rewards its quiet move and penalizes an
	//      early quiet previous move.
	// All need HistUpdate 1. Nodes to depth 18 on 60 positions against 0:
	// 93%, 90%, 98% and 95% one at a time, 85% together; WAC in 100k /
	// 400k nodes 263 / 281 together against 261 / 278. Not match-tested.
	HistExtra int32

	// PruneKing: quiet moves that attack the squares round the enemy king
	// are exempt from late-move, futility, history and exchange pruning
	// (1: only when two pieces or more already attack the king, 2: always).
	// Written for WAC 55 (a queen sacrifice whose point is a quiet rook
	// move threatening mate): with 1 it is found at depth 4 instead of 9
	// (Stockfish network; 11 instead of 16 on the test network). Over the
	// whole suite it does not help: to depth 18 on 60 positions 96% of the
	// nodes with 1 and 102% with 2; WAC with the Stockfish network in
	// 100k / 400k nodes 271 / 283 and 265 / 280 against 273 / 288. 1 is
	// on at the user's request; not match-tested.
	// 3: as 1, also when a piece of the mover's own is under threat.
	// Whole WAC suite at one million nodes a position (default network):
	// 0 292, 1 289, 3 286 of 300, which is within the suite's noise.
	PruneKing int32

	// HistUpdate 1: the history tables are updated with Stockfish's bonuses,
	// per-table bounds and weights (see sfGravity and updateStatsSF in
	// history.go and search.go), and history pruning and the history part
	// of late-move reductions use its formulas for those scales. 0: one
	// bonus, 160*depth-80, for every table. Against 0, to depth 18 on 60
	// positions: 58% of the nodes; first-move cutoffs 88.1% against 85.0%;
	// reduced moves 2.45 plies on average against 2.34; 264 against 259 of
	// 300 WAC positions solved in 100k nodes. Not match-tested.
	// It includes the bonus for the previous move when a node fails low
	// and the extra penalty for an early quiet move that gets refuted
	// (rewardPriorMove, updateStatsSF): with them, the same nodes to depth
	// 18 and 269 WAC positions.
	HistUpdate int32

	// Optimism 1: the evaluation leans toward the side the last iteration
	// found ahead at the root, by a few centipawns where material is
	// unbalanced (Stockfish's optimism term; see optimismCP).
	Optimism int32

	// Aspiration windows at the root.
	AspDelta  int32 // half-width of the first window
	AspTries  int32 // open the window fully after this many failures (0: only once it is AspOpen wide)
	AspOpen   int32 // open the window fully once its half-width reaches this
	AspReduce int32 // 1: after a fail high, search again one ply shallower (up to three plies)

	IIRCut int32 // extra plies of internal iterative reduction at expected cut nodes of depth >= 8 with no hash move

	// ProbCut 1: before the hash move, at every non-PV node of depth >= 5.
	// 2: Stockfish's depths and margins, plus its hash-entry shortcut.
	// 0: off. An earlier self-play test of 1 showed no gain. With the
	// Stockfish move ordering and history, to depth 18 on 60 positions and
	// on WAC in 100k / 400k nodes: 0 100%, 267 / 276; 1 91%, 266 / 281;
	// 2 93%, 255 / 277. 1 is on; not match-tested since.
	ProbCut       int32
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
	//
	// NMPGuard 3 (no guard, Stockfish's verification search instead): 93%
	// of the nodes to depth 18, but WAC 253 / 274 against 266 / 281.
	RFPGuard: 0,
	NMPGuard: 2,
	// Every check extended and never reduced. Extending at PV nodes only and
	// reducing checks also won in self-play, but misses more tactics.
	CheckExt:  2,
	LMRChecks: 0,

	LMRMinMove:  1,
	LMRCutNode:  1,
	LMRExempt:   0,
	PruneExempt: 1,
	// Null move. With NMPBase 4 and a further ply when not improving the
	// null search was a bare quiescence search up to depth 9 or so, and only
	// 24% of them cut off. Within 40 centipawns of beta fewer than 5% did:
	// the side to move is worth about that much, and the null move gives it
	// away. Not yet match-tested.
	NMPBase:        3,
	NMPDepthDiv:    3,
	NMPMargin:      40,
	NMPMarginStep:  3,
	NMPNonImpr:     0,
	FutBase:        0,
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

	// On at the user's request, not yet match-tested. None of them made the
	// tree smaller (depth 16, 24 positions: between 100% and 122% of the
	// nodes without them; all five together 103%).
	QSEvenPrune:    1,
	QSMaxMoves:     3,
	QSEvasionPrune: 1,
	CheckLMR:       1,
	CheckSEEPrune:  1,
	MoveOrder:      2,
	HistUpdate:     1,
	LMRMode:        2,
	PruneKing:      1,
	HistExtra:      15,
	Cycle:          1,
	TTPV:           0,

	Optimism: 0, // not match-tested

	// A narrower window that widens gradually and searches a ply shallower
	// after a fail high (10, 0, 1000, 1) cut the tree to depth 19 by 14% but
	// scored -1 Elo +-15 over 604 games at 8s+0.08s. IIRCut 1 cut it by 8%
	// and scored +1 +-16 over 604. Both are left off.
	AspDelta:  30,
	AspTries:  2,
	AspOpen:   300,
	AspReduce: 0,

	IIRCut: 0,

	ProbCut:       1,
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
		{"NMPGuard", &P.NMPGuard, 0, 3},
		{"CheckExt", &P.CheckExt, 0, 2},
		{"LMRChecks", &P.LMRChecks, 0, 1},
		{"LMRMinMove", &P.LMRMinMove, 1, 6},
		{"LMRCutNode", &P.LMRCutNode, 0, 3},
		{"LMRExempt", &P.LMRExempt, 0, 1},
		{"PruneExempt", &P.PruneExempt, 0, 1},
		{"NMPBase", &P.NMPBase, 2, 6},
		{"NMPDepthDiv", &P.NMPDepthDiv, 2, 8},
		{"NMPMargin", &P.NMPMargin, 0, 400},
		{"NMPMarginStep", &P.NMPMarginStep, 0, 50},
		{"NMPNonImpr", &P.NMPNonImpr, 0, 1},
		{"FutBase", &P.FutBase, 0, 300},
		{"IIRDepth", &P.IIRDepth, 2, 12},
		{"IIRPV", &P.IIRPV, 0, 1},
		{"RazorDepth", &P.RazorDepth, 1, 6},
		{"RazorStep", &P.RazorStep, 0, 600},
		{"SEDoubleMargin", &P.SEDoubleMargin, 0, 10000},
		{"SENegExt", &P.SENegExt, 0, 1},
		{"PawnPushExt", &P.PawnPushExt, 0, 2},
		{"FailSoft", &P.FailSoft, 0, 31},
		{"QSEvenPrune", &P.QSEvenPrune, 0, 1},
		{"QSMaxMoves", &P.QSMaxMoves, 0, 16},
		{"QSEvasionPrune", &P.QSEvasionPrune, 0, 1},
		{"CheckLMR", &P.CheckLMR, 0, 1},
		{"CheckSEEPrune", &P.CheckSEEPrune, 0, 1},
		{"MoveOrder", &P.MoveOrder, 0, 2},
		{"HistUpdate", &P.HistUpdate, 0, 1},
		{"LMRMode", &P.LMRMode, 0, 2},
		{"PruneKing", &P.PruneKing, 0, 3},
		{"HistExtra", &P.HistExtra, 0, 15},
		{"Cycle", &P.Cycle, 0, 1},
		{"TTPV", &P.TTPV, 0, 1},
		{"Optimism", &P.Optimism, 0, 1},
		{"AspDelta", &P.AspDelta, 5, 100},
		{"AspTries", &P.AspTries, 0, 10},
		{"AspOpen", &P.AspOpen, 50, 5000},
		{"AspReduce", &P.AspReduce, 0, 1},
		{"IIRCut", &P.IIRCut, 0, 2},
		{"ProbCut", &P.ProbCut, 0, 2},
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
