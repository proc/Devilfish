package search

import (
	"sync/atomic"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
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

func TestAggressiveMoveOrdering(t *testing.T) {
	// fen has:
	// - Pawn on e7 promoting to e8: e7e8=Q
	// - Undefended Black Rook on d3: e2xd3 (winning capture)
	// - Killers: e2e3, e2e4
	fen := "k7/4P3/8/8/8/3r4/4Q3/4K3 w - - 0 1"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	hist := NewSearchHistory()
	// Set killers
	k1 := types.MakeMove(types.E2, types.E3, types.FlagQuiet)
	k2 := types.MakeMove(types.E2, types.E4, types.FlagQuiet)
	hist.Killers[0][0] = k1
	hist.Killers[0][1] = k2

	var ml movegen.MoveList
	movegen.GenerateMoves(b, false, &ml)
	scores := make([]int32, ml.Count)

	ScoreMoves(b, &ml, scores, types.MoveNone, 0, types.MoveNone, types.MoveNone, types.MoveNone, hist)

	var promoScore, winCapScore, k1Score, k2Score, checkScore, quietScore, badCapScore int32
	foundPromo, foundBadCap, foundK1, foundK2 := false, false, false, false

	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.IsPromotion() && m.PromotionType() == types.Queen {
			promoScore = scores[i]
			foundPromo = true
		} else if m.From() == types.E2 && m.To() == types.E3 {
			k1Score = scores[i]
			foundK1 = true
		} else if m.From() == types.E2 && m.To() == types.E4 {
			k2Score = scores[i]
			foundK2 = true
		} else if b.GivesCheck(m) && !m.IsCapture() && !m.IsPromotion() {
			checkScore = scores[i]
		} else if !m.IsCapture() && !m.IsPromotion() && m != k1 && m != k2 {
			quietScore = scores[i]
		}
	}

	if !foundPromo || !foundK1 || !foundK2 {
		t.Fatalf("expected to find promo and killer moves in test position")
	}

	winCapScore = -1
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.From() == types.E2 && m.To() == types.D3 {
			winCapScore = scores[i]
			break
		}
	}

	// Verify promotion > winning capture > killers > check > quiet
	if promoScore <= winCapScore {
		t.Errorf("expected Queen promotion (%d) > winning capture (%d)", promoScore, winCapScore)
	}
	if winCapScore <= k1Score {
		t.Errorf("expected winning capture (%d) > killer 1 (%d)", winCapScore, k1Score)
	}
	if k1Score <= k2Score {
		t.Errorf("expected killer 1 (%d) > killer 2 (%d)", k1Score, k2Score)
	}
	if checkScore <= quietScore {
		t.Errorf("expected quiet check (%d) > normal quiet (%d)", checkScore, quietScore)
	}

	// Test bad capture in position where piece is defended:
	// Black has pawn on d5 defended by c6 pawn, White Queen on d3 captures d5: Q x P is losing capture
	badCapFEN := "rnbqkbnr/ppp1pppp/2p5/3p4/8/3Q4/PPPPPPPP/RNB1KBNR w KQkq - 0 1"
	bBad, _ := board.ParseFEN(badCapFEN)
	var mlBad movegen.MoveList
	movegen.GenerateMoves(bBad, false, &mlBad)
	scoresBad := make([]int32, mlBad.Count)
	ScoreMoves(bBad, &mlBad, scoresBad, types.MoveNone, 0, types.MoveNone, types.MoveNone, types.MoveNone, hist)

	for i := 0; i < mlBad.Count; i++ {
		m := mlBad.Moves[i]
		// Queen takes d5: d3d5
		if m.From() == types.D3 && m.To() == types.D5 {
			badCapScore = scoresBad[i]
			foundBadCap = true
			break
		}
	}

	if !foundBadCap {
		t.Fatalf("d1xd5 not generated")
	}

	if badCapScore >= 0 {
		t.Errorf("expected losing capture d1xd5 to have negative score (<0), got %d", badCapScore)
	}
	if badCapScore >= quietScore {
		t.Errorf("expected losing capture (%d) to be demoted below quiet move (%d)", badCapScore, quietScore)
	}
}

func TestContinuationHistory2(t *testing.T) {
	hist := NewSearchHistory()
	prev2 := types.MakeMove(types.E2, types.E4, types.FlagDoublePawnPush)
	m := types.MakeMove(types.G1, types.F3, types.FlagQuiet)

	hist.UpdateContHistory2(prev2, m, 4)
	if hist.ContHistory2[prev2.To()][m.To()] <= 0 {
		t.Errorf("expected positive ContHistory2 after update")
	}

	scoreBefore := hist.ContHistory2[prev2.To()][m.To()]
	hist.PenalizeContHistory2(prev2, m, 4)
	scoreAfter := hist.ContHistory2[prev2.To()][m.To()]
	if scoreAfter >= scoreBefore {
		t.Errorf("expected ContHistory2 to decrease after penalty")
	}
}

func TestContinuationHistory4(t *testing.T) {
	hist := NewSearchHistory()
	prev4 := types.MakeMove(types.D2, types.D4, types.FlagDoublePawnPush)
	m := types.MakeMove(types.C1, types.F4, types.FlagQuiet)

	hist.UpdateContHistory4(prev4, m, 5)
	if hist.ContHistory4[prev4.To()][m.To()] <= 0 {
		t.Errorf("expected positive ContHistory4 after update")
	}

	scoreBefore := hist.ContHistory4[prev4.To()][m.To()]
	hist.PenalizeContHistory4(prev4, m, 5)
	scoreAfter := hist.ContHistory4[prev4.To()][m.To()]
	if scoreAfter >= scoreBefore {
		t.Errorf("expected ContHistory4 to decrease after penalty")
	}
}

func TestCorrectionHistory(t *testing.T) {
	hist := NewSearchHistory()
	pawnHash := uint64(0x123456789ABCDEF0)

	corrInit := hist.GetCorrection(types.White, pawnHash)
	if corrInit != 0 {
		t.Errorf("expected initial correction 0, got %d", corrInit)
	}

	// Update with positive error diff (search score was higher than static eval)
	hist.UpdateCorrHist(types.White, pawnHash, 100, 4)
	corrAfter := hist.GetCorrection(types.White, pawnHash)
	if corrAfter <= 0 {
		t.Errorf("expected positive correction after update, got %d", corrAfter)
	}

	// White and Black indices are distinct
	corrBlack := hist.GetCorrection(types.Black, pawnHash)
	if corrBlack != 0 {
		t.Errorf("expected black correction to remain 0, got %d", corrBlack)
	}
}

func TestMovePicker(t *testing.T) {
	b, _ := board.ParseFEN(board.StartFEN)
	hist := NewSearchHistory()
	ttMove := types.MakeMove(types.E2, types.E4, types.FlagDoublePawnPush)

	mp := NewMovePicker(b, ttMove, types.MoveNone, 0, types.MoveNone, types.MoveNone, types.MoveNone, hist)

	// First move should be the TT move
	first := mp.Next()
	if first != ttMove {
		t.Errorf("expected first move to be ttMove %s, got %s", ttMove, first)
	}

	// Subsequent moves should be generated and not repeat ttMove
	var moves []types.Move
	for {
		m := mp.Next()
		if m == types.MoveNone {
			break
		}
		if m == ttMove {
			t.Errorf("MovePicker yielded ttMove %s again", ttMove)
		}
		moves = append(moves, m)
	}

	// Standard start position has 20 moves, minus 1 TT move = 19 moves
	if len(moves) != 19 {
		t.Errorf("expected 19 remaining moves, got %d", len(moves))
	}
}

func TestBK21MoveOrdering(t *testing.T) {
	fen := "r2q1r1k/1b1nN2p/pp3pp1/8/Q7/PP5P/1BP2RPN/7K w - -"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	hist := NewSearchHistory()
	mp := NewMovePicker(b, types.MoveNone, types.MoveNone, 0, types.MoveNone, types.MoveNone, types.MoveNone, hist)
	foundQxd7 := false
	order := 0
	for {
		m := mp.Next()
		if m == types.MoveNone {
			break
		}
		order++
		if m.String() == "a4d7" {
			foundQxd7 = true
			if order > 5 {
				t.Errorf("expected a4d7 to be among the top 5 candidate moves, got order #%d", order)
			}
			break
		}
	}
	if !foundQxd7 {
		t.Errorf("expected a4d7 to be generated by MovePicker")
	}
}
