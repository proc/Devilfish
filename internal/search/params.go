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

	// Depth limits of the pruning rules. Futility, history and quiet SEE
	// pruning compare against the move's reduced depth (depth minus its
	// late-move reduction), the others against the node's depth.
	RFPDepth  int32 // reverse futility pruning
	LMPDepth  int32 // late-move pruning
	FutDepth  int32 // futility pruning (reduced depth)
	HistDepth int32 // history pruning (reduced depth)
	SEEDepth  int32 // SEE pruning
}

// P is the live parameter set used by Search.
var P = Params{
	LMRBase:       40,
	LMRDivisor:    230,
	RFPMargin:     75,
	RFPNonImpr:    35,
	RazorMargin:   350,
	NMPEvalDiv:    200,
	LMPMultImpr:   3,
	LMPMultNonImp: 2,
	FutMargin:     90,
	HistPrImpr:    1024,
	HistPrNonImp:  512,
	SEEQuietMul:   60,
	SEENoisyMul:   200,
	RFPDepth:      8,
	LMPDepth:      8,
	FutDepth:      8,
	HistDepth:     6,
	SEEDepth:      8,
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
		{"RFPDepth", &P.RFPDepth, 1, 14},
		{"LMPDepth", &P.LMPDepth, 1, 14},
		{"FutDepth", &P.FutDepth, 1, 14},
		{"HistDepth", &P.HistDepth, 1, 14},
		{"SEEDepth", &P.SEEDepth, 1, 14},
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
