package tt

import (
	"testing"

	"devilfish/internal/types"
)

func TestTTStoreEval(t *testing.T) {
	table := New(1)
	mv := types.MakeMove(types.G1, types.F3, 0)
	cases := []struct{ score, eval int32 }{
		{150, -73}, {-29990, 1234}, {29990, -2000}, {0, 0}, {-1, 32767},
	}
	for i, c := range cases {
		h := uint64(0x9E3779B97F4A7C15) * uint64(i+1)
		table.StoreEval(h, mv, c.score, c.eval, 7, LowerBound)
		m, s, e, d, nt, ok := table.ProbeEval(h)
		if !ok || m != mv || s != c.score || e != c.eval || d != 7 || nt != LowerBound {
			t.Fatalf("case %d: got m=%v s=%d e=%d d=%d nt=%d ok=%v", i, m, s, e, d, nt, ok)
		}
	}

	// Store without eval keeps the previously stored eval.
	h := uint64(0xABCDEF0123456789)
	table.StoreEval(h, mv, 10, 42, 3, UpperBound)
	table.Store(h, mv, 20, 5, Exact)
	if _, s, e, d, _, ok := table.ProbeEval(h); !ok || s != 20 || e != 42 || d != 5 {
		t.Fatalf("keep eval: s=%d e=%d d=%d ok=%v", s, e, d, ok)
	}

	// Entries stored without any eval report EvalNone.
	h2 := uint64(0x1111222233334444)
	table.Store(h2, mv, 5, 2, Exact)
	if _, _, e, _, _, ok := table.ProbeEval(h2); !ok || e != EvalNone {
		t.Fatalf("missing eval: e=%d ok=%v", e, ok)
	}
}
