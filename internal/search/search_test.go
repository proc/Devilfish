package search

import (
	"sync/atomic"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

func TestTacticsMateInOne(t *testing.T) {
	tests := []struct {
		fen      string
		bestMove string
	}{
		{
			fen:      "6k1/5ppp/8/8/8/8/8/4R1K1 w - - 0 1",
			bestMove: "e1e8",
		},
		{
			fen:      "r1bqkb1r/pppp1ppp/2n5/4p3/2B1n3/5Q2/PPPP1PPP/RNB1K1NR w KQkq - 0 1",
			bestMove: "f3f7",
		},
		{
			fen:      "2r3k1/p4p2/3p2p1/1p1P4/4P1P1/5P2/PP5Q/1K5R w - - 0 1",
			bestMove: "h2h8",
		},
	}

	for _, tt := range tests {
		b, err := board.ParseFEN(tt.fen)
		if err != nil {
			t.Fatalf("failed to parse FEN %s: %v", tt.fen, err)
		}
		engine := NewEngine(64, 1)
		mv, _ := engine.Search(b, SearchLimits{MaxDepth: 4}, nil)
		if mv.String() != tt.bestMove {
			t.Errorf("FEN %s: expected %s, got %s", tt.fen, tt.bestMove, mv.String())
		}
	}
}

func TestLazySMPScaling(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	// Search with 4 threads using Lazy SMP
	engine := NewEngine(64, 4)
	mv, _ := engine.Search(b, SearchLimits{MaxDepth: 6}, nil)
	if mv == types.MoveNone {
		t.Errorf("Lazy SMP search returned MoveNone")
	}
}

func TestHistoryPruning(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(64, 1)
	mv, _ := engine.Search(b, SearchLimits{MaxDepth: 5}, nil)
	if mv == types.MoveNone {
		t.Errorf("Search with History Pruning returned MoveNone")
	}
}

func TestContinuationAndCaptureHistory(t *testing.T) {
	h := NewSearchHistory()
	m1 := types.MakeMove(types.E2, types.E4, types.FlagQuiet)
	m2 := types.MakeMove(types.E7, types.E5, types.FlagQuiet)

	// Continuation history update and penalize
	h.UpdateContHistory(m1, m2, 4)
	if h.ContHistory[m1.To()][m2.To()] <= 0 {
		t.Errorf("expected positive continuation history, got %d", h.ContHistory[m1.To()][m2.To()])
	}

	h.PenalizeContHistory(m1, m2, 4)

	// Capture history update
	h.UpdateCaptureHistory(types.Knight, types.Pawn, types.E5, 4)
	if h.CaptureHistory[types.Knight][types.E5][types.Pawn] <= 0 {
		t.Errorf("expected positive capture history")
	}
}

func TestDynamicTimeAdjustment(t *testing.T) {
	var stopped atomic.Bool
	var nodes atomic.Uint64
	tm := NewTimeManager(SearchLimits{WhiteTime: 10000}, types.White, &stopped, &nodes)
	initialOpt := tm.OptTime

	tm.AdjustOptTime(0.8)
	if tm.OptTime >= initialOpt {
		t.Errorf("expected reduced OptTime, got %v >= %v", tm.OptTime, initialOpt)
	}

	tm.AdjustOptTime(1.5)
	if tm.OptTime <= initialOpt*8/10 {
		t.Errorf("expected increased OptTime, got %v", tm.OptTime)
	}
}

func TestPonderMove(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(64, 1)
	bestMove, ponderMove := engine.Search(b, SearchLimits{MaxDepth: 4}, nil)
	if bestMove == types.MoveNone {
		t.Errorf("expected valid bestMove")
	}
	if ponderMove == types.MoveNone {
		t.Errorf("expected valid ponderMove at depth 4")
	}
}

func TestPonderHit(t *testing.T) {
	var stopped atomic.Bool
	var nodes atomic.Uint64
	tm := NewTimeManager(SearchLimits{WhiteTime: 5000, Ponder: true}, types.White, &stopped, &nodes)
	if !tm.Ponder.Load() {
		t.Errorf("expected Ponder to be true initially")
	}
	if tm.CheckLimits() {
		t.Errorf("CheckLimits should return false while pondering")
	}
	tm.PonderHit()
	if tm.Ponder.Load() {
		t.Errorf("expected Ponder to be false after PonderHit")
	}
}

func TestLMRTable(t *testing.T) {
	// Verify LMRTable values
	if LMRTable[1][1] != 0 {
		t.Errorf("expected LMRTable[1][1] == 0, got %d", LMRTable[1][1])
	}
	if LMRTable[6][6] <= LMRTable[3][3] {
		t.Errorf("expected LMRTable[6][6] > LMRTable[3][3], got %d vs %d", LMRTable[6][6], LMRTable[3][3])
	}
	if LMRTable[10][20] <= LMRTable[5][5] {
		t.Errorf("expected LMRTable[10][20] > LMRTable[5][5], got %d vs %d", LMRTable[10][20], LMRTable[5][5])
	}
}

func TestForwardPruningTacticsIntegrity(t *testing.T) {
	// A tactical position where White has Scholar's mate threat f3f7#
	// Razoring and futility pruning must not prune the winning tactic!
	fen := "r1bqkb1r/pppp1ppp/2n5/4p3/2B1n3/5Q2/PPPP1PPP/RNB1K1NR w KQkq - 0 1"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(64, 1)
	mv, _ := engine.Search(b, SearchLimits{MaxDepth: 5}, nil)
	if mv.String() != "f3f7" {
		t.Errorf("expected tactical mate f3f7 with pruning enabled, got %s", mv.String())
	}
}

func TestBoardGivesCheck(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}
	// e2e4 does not give check
	e2e4 := types.MakeMove(types.E2, types.E4, types.FlagDoublePawnPush)
	if b.GivesCheck(e2e4) {
		t.Errorf("expected e2e4 to not give check")
	}

	// Position where Q checks king
	checkPos := "rnbqkbnr/ppppp1pp/8/5p2/4P3/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 2"
	b2, _ := board.ParseFEN(checkPos)
	d1h5 := types.MakeMove(types.D1, types.H5, types.FlagQuiet)
	if !b2.GivesCheck(d1h5) {
		t.Errorf("expected d1h5 to give check")
	}
}
