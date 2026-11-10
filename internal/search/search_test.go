package search

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
			break
		}
	}
	if !foundQxd7 {
		t.Errorf("expected a4d7 to be generated by MovePicker")
	}
}

func TestWAC055(t *testing.T) {
	fen := "r3r1k1/pp1q1pp1/4b1p1/3p2B1/3Q1R2/8/PPP3PP/4R1K1 w - -"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(64, 1)
	bm, _ := engine.Search(b, SearchLimits{MaxDepth: 7}, func(si SearchInfo) {
		var pvStrs []string
		for _, m := range si.PV {
			pvStrs = append(pvStrs, m.String())
		}
		t.Logf("iter depth %d score %d pv: %s", si.Depth, si.Score, strings.Join(pvStrs, " "))
	})
	if bm.String() != "d4g7" {
		t.Errorf("expected bestmove d4g7, got %s", bm)
	}
}

func TestWAC071(t *testing.T) {
	fen := "2kr3r/pp1q1ppp/5n2/1Nb5/2Pp1B2/7Q/P4PPP/1R3RK1 w - -"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(64, 1)
	bm, _ := engine.Search(b, SearchLimits{MaxDepth: 10}, func(si SearchInfo) {
		var pvStrs []string
		for _, m := range si.PV {
			pvStrs = append(pvStrs, m.String())
		}
		t.Logf("iter depth %d score %d pv: %s", si.Depth, si.Score, strings.Join(pvStrs, " "))
	})
	t.Logf("Engine bestmove: %s", bm.String())
}

func TestWAC071ThreatDetection(t *testing.T) {
	fen := "2kr3r/pp1q1ppp/5n2/1Nb5/2Pp1B2/7Q/P4PPP/1R3RK1 w - -"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	mNxa7 := types.MakeMove(types.B5, types.A7, types.FlagCapture)
	bAfter := b.Clone()
	bAfter.MakeMove(mNxa7)
	bAfter.MakeMove(types.MakeMove(types.C5, types.A7, types.FlagCapture))
	mQa3 := types.MakeMove(types.H3, types.A3, types.FlagQuiet)
	if !IsTacticalThreat(bAfter, mQa3) {
		t.Errorf("expected Qa3 to be recognized as tactical threat against Black's bishop on a7")
	}
}

func TestBulletAndBlitzTimeManagement(t *testing.T) {
	var stopped atomic.Bool
	var nodes atomic.Uint64

	// Test 1: Bullet 1+0 at game start (60,000ms, 0 inc)
	// Must NOT allocate excessive time (previously 2400ms opt, 7200ms max)
	// Now should allocate ~1.3s opt, max ~2.4s
	tmBullet := NewTimeManager(SearchLimits{WhiteTime: 60000, MoveOverhead: 20}, types.White, &stopped, &nodes)
	if tmBullet.OptTime > 1600*time.Millisecond {
		t.Errorf("Bullet 1+0 OptTime too high: got %v, expected <= 1.6s", tmBullet.OptTime)
	}
	if tmBullet.MaxTime > 3000*time.Millisecond {
		t.Errorf("Bullet 1+0 MaxTime too high: got %v, expected <= 3.0s", tmBullet.MaxTime)
	}

	// Test 2: Bullet 1+0 in midgame (20,000ms, 0 inc)
	tmMid := NewTimeManager(SearchLimits{WhiteTime: 20000, MoveOverhead: 20}, types.White, &stopped, &nodes)
	if tmMid.OptTime > 500*time.Millisecond {
		t.Errorf("Bullet 20s OptTime too high: got %v, expected <= 500ms", tmMid.OptTime)
	}
	if tmMid.MaxTime > 900*time.Millisecond {
		t.Errorf("Bullet 20s MaxTime too high: got %v, expected <= 900ms", tmMid.MaxTime)
	}

	// Test 3: Bullet in time trouble (8,000ms, 0 inc)
	tmTrouble := NewTimeManager(SearchLimits{WhiteTime: 8000, MoveOverhead: 20}, types.White, &stopped, &nodes)
	if !tmTrouble.IsTimeTrouble {
		t.Errorf("expected 8s in non-increment bullet to be flagged as TimeTrouble")
	}
	if tmTrouble.MaxTime > 250*time.Millisecond {
		t.Errorf("Bullet 8s MaxTime too high: got %v, expected <= 250ms", tmTrouble.MaxTime)
	}

	// Test 4: Blitz 3+0 at game start (180,000ms, 0 inc)
	// Previously allocated 7.2s opt, 21.6s max (absurdly high)
	// Now should allocate ~4.5s opt, max <= 9.0s
	tmBlitz := NewTimeManager(SearchLimits{WhiteTime: 180000, MoveOverhead: 20}, types.White, &stopped, &nodes)
	if tmBlitz.OptTime > 5000*time.Millisecond {
		t.Errorf("Blitz 3+0 OptTime too high: got %v, expected <= 5.0s", tmBlitz.OptTime)
	}
	if tmBlitz.MaxTime > 10000*time.Millisecond {
		t.Errorf("Blitz 3+0 MaxTime too high: got %v, expected <= 10.0s", tmBlitz.MaxTime)
	}

	// Test 5: CanStartNewIteration early exit when move is stable
	// If move is stable and projected time exceeds OptTime, must return false!
	tmStable := NewTimeManager(SearchLimits{WhiteTime: 60000, MoveOverhead: 20}, types.White, &stopped, &nodes)
	// At 80% elapsed time with stable move, starting new iteration must be denied
	tmStable.StartTime = time.Now().Add(-time.Duration(float64(tmStable.OptTime)*0.85))
	if tmStable.CanStartNewIteration(200*time.Millisecond, true) {
		t.Errorf("expected CanStartNewIteration to reject new iteration when move is stable and elapsed >= 85%% OptTime")
	}
}

func TestForcedSingleLegalMoveInstantReturn(t *testing.T) {
	// King on edge in check with only 1 legal escape (Ka2)
	fen := "8/8/8/8/8/8/8/k1K4R b - - 0 1"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	legalMoves := movegen.GenerateLegalMoves(b)
	if len(legalMoves) != 1 {
		t.Fatalf("expected 1 legal move in test position, got %d", len(legalMoves))
	}

	e := NewEngine(64, 1)
	start := time.Now()
	bm, _ := e.Search(b, SearchLimits{WhiteTime: 60000, BlackTime: 60000}, nil)
	elapsed := time.Since(start)

	if bm != legalMoves[0] {
		t.Errorf("expected forced move %s, got %s", legalMoves[0], bm)
	}
	if elapsed > 10*time.Millisecond {
		t.Errorf("forced move should return instantly (<=10ms), took %v", elapsed)
	}
}

func TestPawnThreatEvasion(t *testing.T) {
	// Position: White Rook on e4 attacked by Black pawn on d5
	fen := "r1bqkbnr/ppp1pppp/2n5/3p4/4R3/3P4/PPP2PPP/RNBQKBN1 w Qkq - 0 5"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	pawnThreats := b.PawnThreatsOnPieces(types.White)
	if pawnThreats == 0 {
		t.Fatalf("expected non-zero pawn threats on White pieces")
	}

	var ml movegen.MoveList
	movegen.GenerateMoves(b, false, &ml)
	scores := make([]int32, ml.Count)
	h := NewSearchHistory()
	ScoreMoves(b, &ml, scores, types.MoveNone, 0, types.MoveNone, types.MoveNone, types.MoveNone, h)

	// Check that quiet evasion e4e3 receives ScorePawnThreatEvasion
	foundEvasion := false
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.From() == types.E4 && m.To() == types.E3 {
			if scores[i] < ScorePawnThreatEvasion {
				t.Errorf("expected move e4e3 to have score >= ScorePawnThreatEvasion (%d), got %d",
					ScorePawnThreatEvasion, scores[i])
			}
			foundEvasion = true
			break
		}
	}
	if !foundEvasion {
		t.Errorf("did not find move e4e3 in move list")
	}

	// Engine search should resolve the threat by moving the attacked piece
	e := NewEngine(64, 1)
	bm, _ := e.Search(b, SearchLimits{MaxDepth: 4}, func(si SearchInfo) {
		var pvStrs []string
		for _, m := range si.PV {
			pvStrs = append(pvStrs, m.String())
		}
		t.Logf("iter depth %d score %d pv: %s", si.Depth, si.Score, strings.Join(pvStrs, " "))
	})
	if bm.From() != types.E4 && !bm.IsCapture() {
		t.Errorf("engine ignored pawn threat on e4! Played %s instead of evading/capturing", bm)
	}
}

func TestNoBadSacrifices(t *testing.T) {
	// White Queen on d4, Black Knight on f6 defended by d7 and g7
	fen := "r1bqkb1r/ppp1pppp/2np1n2/8/3Q4/4P3/PPPP1PPP/RNB1KBNR w KQkq - 0 4"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	h := NewSearchHistory()
	var ml movegen.MoveList
	movegen.GenerateMoves(b, false, &ml)
	scores := make([]int32, ml.Count)
	ScoreMoves(b, &ml, scores, types.MoveNone, 0, types.MoveNone, types.MoveNone, types.MoveNone, h)

	// Verify that Qxf6 (d4f6) is scored as a bad capture, NOT as a good capture
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.String() == "d4f6" {
			if scores[i] >= ScoreGoodCapture {
				t.Errorf("losing queen sacrifice d4f6 was given good capture score %d >= %d",
					scores[i], ScoreGoodCapture)
			}
			break
		}
	}

	// MovePicker must not pick d4f6 as the first move
	mp := NewMovePicker(b, types.MoveNone, types.MoveNone, 0, types.MoveNone, types.MoveNone, types.MoveNone, h)
	firstMove := mp.Next()
	if firstMove.String() == "d4f6" {
		t.Errorf("MovePicker returned bad queen sacrifice d4f6 as the very first move!")
	}
}

func TestKingDefenseMoveOrdering(t *testing.T) {
	// Black pieces Qh4 and Bc5 attacking White king on e1/g1 area
	fen := "r1b1k2r/pppp1ppp/2n5/2b1p3/4P2q/3P1N2/PPP2PPP/RNBQKB1R w KQkq - 1 5"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	attackers, units := b.KingAttackersCount(types.White)
	if attackers < 1 || units < 1 {
		t.Errorf("expected King attackers against White king, got %d attackers, %d units", attackers, units)
	}
}

func TestEngineOpeningBook(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	eng := NewEngine(16, 1)
	// Without book enabled, search performs actual iterative deepening
	bmNoBook, _ := eng.Search(b, SearchLimits{MaxDepth: 1}, nil)
	if bmNoBook == types.MoveNone {
		t.Fatal("expected legal move without book")
	}

	// Enable opening book
	eng.SetUseBook(true)
	var reportedInfo SearchInfo
	bmBook, _ := eng.Search(b, SearchLimits{MaxDepth: 10}, func(si SearchInfo) {
		reportedInfo = si
	})

	if bmBook == types.MoveNone {
		t.Fatal("expected book move from startpos")
	}
	// Startpos book moves are standard e4, d4, c4, Nf3
	bmStr := bmBook.String()
	if bmStr != "e2e4" && bmStr != "d2d4" && bmStr != "c2c4" && bmStr != "g1f3" {
		t.Errorf("unexpected book move from startpos: %s", bmStr)
	}
	if reportedInfo.Nodes != 1 {
		t.Errorf("expected 1 node for book probe hit, got %d", reportedInfo.Nodes)
	}

	// When MultiPV > 1, book probe is bypassed
	eng.SetMultiPV(2)
	nodesBefore := eng.Nodes.Load()
	_ = nodesBefore
	bmMulti, _ := eng.Search(b, SearchLimits{MaxDepth: 1}, nil)
	if bmMulti == types.MoveNone {
		t.Fatal("expected move in multiPV search")
	}
}

func TestThreatenedRookResponses(t *testing.T) {
	eng := NewEngine(16, 1)

	// Case 1: White Rook on d1 attacked by Black Queen on d8 -> White must capture Queen (d1d8)
	t.Run("White Rook captures threatening Queen", func(t *testing.T) {
		fen := "3q2k1/5ppp/8/8/8/8/5PPP/3R2K1 w - - 0 1"
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		bm, _ := eng.Search(b, SearchLimits{MaxDepth: 3}, nil)
		if bm.String() != "d1d8" {
			t.Errorf("expected d1d8, got %s", bm)
		}
	})

	// Case 2: White Rook on a1 attacked by Black Bishop on h8 -> White must move Rook away from danger
	t.Run("White Rook evades Bishop attack", func(t *testing.T) {
		fen := "7b/5k2/8/8/8/8/5PPP/R5K1 w - - 0 1"
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		bm, _ := eng.Search(b, SearchLimits{MaxDepth: 3}, nil)
		if bm.From() != types.A1 {
			t.Errorf("expected White Rook on a1 to evade attack, got move from %s to %s (%s)",
				bm.From(), bm.To(), bm)
		}
	})

	// Case 3: Black Rook on d8 attacks undefended White Rook on d1 -> White captures Rook (d1d8)
	t.Run("White Rook captures threatening enemy Rook", func(t *testing.T) {
		fen := "3r2k1/5ppp/8/8/8/8/5PPP/3R2K1 w - - 0 1"
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatal(err)
		}
		bm, _ := eng.Search(b, SearchLimits{MaxDepth: 3}, nil)
		if bm.String() != "d1d8" {
			t.Errorf("expected d1d8, got %s", bm)
		}
	})
}
