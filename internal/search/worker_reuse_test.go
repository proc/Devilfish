package search

import (
	"reflect"
	"testing"
	"unsafe"

	"devilfish/internal/board"
	"devilfish/internal/nnue"
)

// A worker reused for a new search must start in the same state as a new one,
// apart from its history tables (kept between moves of a game) and
// the eval cache and accumulator storage it is allowed to keep.
func TestReusedWorkerMatchesFresh(t *testing.T) {
	e := NewEngine(16, 2)
	first, _ := board.ParseFEN("r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1")
	e.Search(first, SearchLimits{MaxDepth: 6}, nil)
	if len(e.workers) != 2 || e.workers[0].LocalNodes == 0 {
		t.Fatalf("expected two used workers, have %d", len(e.workers))
	}

	next, _ := board.ParseFEN("rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8")
	tm := NewTimeManager(SearchLimits{MaxDepth: 5}, next.SideToMove, &e.Stopped, &e.Nodes, next)
	for id := 0; id < 2; id++ {
		reused := e.worker(id, next, tm)
		fresh := NewWorker(id, next, e.TT, tm, &e.Nodes, &e.TBHits)

		rc, fc := *reused, *fresh
		rv, fv := reflect.ValueOf(&rc).Elem(), reflect.ValueOf(&fc).Elem()
		for i := 0; i < rv.NumField(); i++ {
			name := rv.Type().Field(i).Name
			switch name {
			case "EvalCache", "Eight", "History":
				continue // kept on purpose; checked below
			}
			// Read through the field's address so unexported fields compare too.
			field := func(v reflect.Value) any {
				f := v.Field(i)
				return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface()
			}
			if !reflect.DeepEqual(field(rv), field(fv)) {
				t.Errorf("worker %d: field %s differs from a new worker's", id, name)
			}
		}
		if reused.History.Killers != fresh.History.Killers {
			t.Errorf("worker %d: killers survived the reset", id)
		}
		if reused.History.Main == fresh.History.Main {
			t.Errorf("worker %d: history was not carried over", id)
		}
		if (reused.Eight == nil) != (fresh.Eight == nil) {
			t.Errorf("worker %d: 8Chess state present %v, new worker %v", id, reused.Eight != nil, fresh.Eight != nil)
		}
		if reused.Eight != nil {
			for ply := 0; ply < MaxPly; ply++ {
				if reused.Eight.Has(ply, next.Hash) || reused.Eight.Has(ply, first.Hash) {
					t.Errorf("worker %d: accumulator at ply %d survived the reset", id, ply)
				}
			}
		}
	}

	// Fewer threads: the surplus worker is released.
	e.SetThreads(1)
	e.Search(next, SearchLimits{MaxDepth: 3}, nil)
	if len(e.workers) != 1 {
		t.Errorf("after SetThreads(1): %d workers kept", len(e.workers))
	}
}

// The eval cache is cleared when the network changes, and kept otherwise.
func TestEvalCacheFollowsNetwork(t *testing.T) {
	if nnue.ActiveEightNetwork == nil {
		t.Skip("8Chess network not embedded")
	}
	e := NewEngine(16, 1)
	b, _ := board.ParseFEN(board.StartFEN)
	e.Search(b, SearchLimits{MaxDepth: 4}, nil)
	w := e.workers[0]
	filled := 0
	for _, c := range w.EvalCache {
		if c.hash != 0 {
			filled++
		}
	}
	if filled == 0 {
		t.Fatal("eval cache unused")
	}
	tm := NewTimeManager(SearchLimits{MaxDepth: 1}, b.SideToMove, &e.Stopped, &e.Nodes, b)
	e.worker(0, b, tm)
	if w.EvalCache[b.Hash&(evalCacheSize-1)].hash != b.Hash {
		t.Error("eval cache dropped with the network unchanged")
	}

	saved := nnue.Enabled
	nnue.Enabled = false
	defer func() { nnue.Enabled = saved }()
	e.worker(0, b, tm)
	for _, c := range w.EvalCache {
		if c.hash != 0 {
			t.Fatal("eval cache kept after the network changed")
		}
	}
}
