package search

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devilfish/internal/bitboard"
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

	tm.ScaleOptTime(0.8)
	if tm.OptTime >= initialOpt {
		t.Errorf("expected reduced OptTime, got %v >= %v", tm.OptTime, initialOpt)
	}

	tm.ScaleOptTime(1.5)
	if tm.OptTime <= initialOpt {
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
	// ln(1) = 0, so the first entry is the base alone.
	if want := int8(P.LMRBase / 100); LMRTable[1][1] != want {
		t.Errorf("expected LMRTable[1][1] == %d, got %d", want, LMRTable[1][1])
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

// A history update reaches the main, pawn-structure and continuation tables,
// and a penalty takes it back out.
func TestQuietHistoryTables(t *testing.T) {
	b, _ := board.ParseFEN("rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq - 0 2")
	h := NewSearchHistory()
	h.PushMove(0, types.WhitePawn, types.E4)
	h.PushMove(1, types.BlackPawn, types.E5)
	m := types.MakeMove(types.G1, types.F3, types.FlagQuiet)

	c := h.Context(b, 2, 0)
	c.Update(types.WhiteKnight, m, quietStatBonus(4))
	if h.Main[types.White][0][0][types.G1][types.F3] <= 0 {
		t.Errorf("main history not updated")
	}
	if h.Cont[types.BlackPawn][types.E5][types.WhiteKnight][types.F3] <= 0 || h.Cont[types.WhitePawn][types.E4][types.WhiteKnight][types.F3] <= 0 {
		t.Errorf("continuation history not updated for the last two moves")
	}
	if h.PawnHist[b.PawnHash%pawnHistSize][types.WhiteKnight][types.F3] <= 0 {
		t.Errorf("pawn history not updated")
	}
	if got, want := c.Order(types.WhiteKnight, m), 4*quietStatBonus(4); got != want {
		t.Errorf("order score %d, want %d", got, want)
	}
	// The same squares with the knight under threat are a different entry.
	threat := h.Context(b, 2, bitboard.SquareBB[types.G1])
	if threat.Prune(types.WhiteKnight, m) >= c.Prune(types.WhiteKnight, m) {
		t.Errorf("threatened-piece history should not share the unthreatened entry")
	}
	before := c.Stat(types.WhiteKnight, m)
	c.Update(types.WhiteKnight, m, -quietStatBonus(4))
	if c.Stat(types.WhiteKnight, m) >= before {
		t.Errorf("penalty did not lower the history")
	}
	// Values stay bounded however often they are reinforced.
	for i := 0; i < 1000; i++ {
		c.Update(types.WhiteKnight, m, 1600)
	}
	if v := h.Main[types.White][0][0][types.G1][types.F3]; v > histLimit || v < histLimit/2 {
		t.Errorf("saturated history %d not within (%d, %d]", v, histLimit/2, histLimit)
	}
}

func TestCorrectionHistory(t *testing.T) {
	hist := NewSearchHistory()
	b, _ := board.ParseFEN("r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R w KQkq - 2 3")

	if c := hist.GetCorrection(b); c != 0 {
		t.Errorf("expected initial correction 0, got %d", c)
	}

	// Search keeps finding the position better than the eval says
	for i := 0; i < 50; i++ {
		hist.UpdateCorrHist(b, 100, 8)
	}
	c := hist.GetCorrection(b)
	if c <= 0 || c > corrMaxOutput {
		t.Errorf("expected positive correction within ±%d after updates, got %d", corrMaxOutput, c)
	}

	// The same pieces with the other side to move use separate entries
	other, _ := board.ParseFEN("r1bqkbnr/pppp1ppp/2n5/4p3/4P3/5N2/PPPP1PPP/RNBQKB1R b KQkq - 2 3")
	if c := hist.GetCorrection(other); c != 0 {
		t.Errorf("expected black-to-move correction to remain 0, got %d", c)
	}

	// Entries stay bounded however long the same error repeats
	for i := 0; i < 10000; i++ {
		hist.UpdateCorrHist(b, 400, 16)
	}
	if c := hist.GetCorrection(b); c > corrMaxOutput {
		t.Errorf("correction %d exceeds bound %d", c, corrMaxOutput)
	}
	if v := hist.CorrHist[types.White][b.PawnHash%corrSize]; v > corrLimit {
		t.Errorf("pawn entry %d exceeds limit %d", v, corrLimit)
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

	// A quiet mating threat behind a queen sacrifice. Found at depth 9 (about
	// 35k nodes); internal iterative reductions make a nominal ply shallower
	// than it was when depth 7 was enough.
	engine := NewEngine(64, 1)
	bm, _ := engine.Search(b, SearchLimits{MaxDepth: 10}, func(si SearchInfo) {
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
	bm, _ := engine.Search(b, SearchLimits{MaxDepth: 8}, func(si SearchInfo) {
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

	// Test 5: CanStartNewIteration with a stable move. Short of time, a new
	// iteration that would run past OptTime is refused; otherwise the move
	// keeps going until its budget is spent.
	tmStable := NewTimeManager(SearchLimits{WhiteTime: 8000, MoveOverhead: 20}, types.White, &stopped, &nodes)
	tmStable.StartTime = time.Now().Add(-time.Duration(float64(tmStable.OptTime) * 0.85))
	if tmStable.CanStartNewIteration(tmStable.OptTime/4, true) {
		t.Errorf("expected time trouble to reject a new iteration at 85%% of OptTime with a stable move")
	}
	tmRoomy := NewTimeManager(SearchLimits{WhiteTime: 60000, MoveOverhead: 20}, types.White, &stopped, &nodes)
	tmRoomy.StartTime = time.Now().Add(-time.Duration(float64(tmRoomy.OptTime) * 0.85))
	if !tmRoomy.CanStartNewIteration(tmRoomy.OptTime/10, true) {
		t.Errorf("expected a stable move with budget left to start another iteration")
	}
	tmRoomy.StartTime = time.Now().Add(-tmRoomy.OptTime)
	if tmRoomy.CanStartNewIteration(tmRoomy.OptTime/10, true) {
		t.Errorf("expected no new iteration once OptTime is spent")
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

func TestSearchDepthBeyond10(t *testing.T) {
	eng := NewEngine(16, 2)
	eng.SetUseBook(false)

	fen := "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}

	var maxDepthReached int8
	bm, _ := eng.Search(b, SearchLimits{MaxDepth: 11}, func(info SearchInfo) {
		if info.Depth > maxDepthReached {
			maxDepthReached = info.Depth
		}
	})

	if bm == types.MoveNone {
		t.Fatal("expected valid best move")
	}

	if maxDepthReached < 11 {
		t.Errorf("expected search to reach depth 11 without artificial cutoff, got depth %d", maxDepthReached)
	}
}

func TestQueenThreatEvasion(t *testing.T) {
	eng := NewEngine(16, 1)
	eng.SetUseBook(false)

	tests := []struct {
		name    string
		fen     string
		badMove string
	}{
		{
			name:    "Bishop attacks Queen on d2",
			fen:     "r2q1rk1/pp1n1pp1/2p1pn1p/6b1/2PP4/2N5/PP1Q1PPP/R3KB1R w KQ - 0 1",
			badMove: "d2g5",
		},
		{
			name:    "Knight on b4 attacks Queen on d3",
			fen:     "r1bqk2r/pp1n1ppp/2p1p3/3p4/1nPP4/3QPN2/PP3PPP/RNB1KB1R w KQkq - 0 1",
			badMove: "a2a3",
		},
		{
			name:    "Defended Rook attacks Queen",
			fen:     "r2r4/5pk1/8/8/3Q4/8/5PPP/6K1 w - - 0 1",
			badMove: "h2h3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := board.ParseFEN(tc.fen)
			if err != nil {
				t.Fatal(err)
			}
			bm, _ := eng.Search(b, SearchLimits{MaxDepth: 4}, nil)
			t.Logf("[%s] Bestmove: %s", tc.name, bm)
			if bm.String() == tc.badMove {
				t.Errorf("[%s] blundered queen with move %s", tc.name, bm)
			}
		})
	}
}

// The optimism term favours the side found ahead at the root, by the same
// amount against the other side, and is bounded.
func TestOptimism(t *testing.T) {
	old := P.Optimism
	defer func() { P.Optimism = old }()
	// White is a knight up.
	b, err := board.ParseFEN("rnbqkb1r/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	if err != nil {
		t.Fatal(err)
	}
	tm := &TimeManager{}

	P.Optimism = 0
	setOptimism(tm, 150, types.White)
	if tm.Optimism.Load() != 0 {
		t.Fatalf("optimism set with the option off")
	}

	P.Optimism = 1
	setOptimism(tm, 150, types.White)
	stored := tm.Optimism.Load()
	white := optimismCP(b, 300, stored)
	if white <= 0 || white > 10 {
		t.Errorf("White ahead at the root and a knight up: optimism for White to move = %d, want 1..10", white)
	}
	b.SideToMove = types.Black
	if black := optimismCP(b, -300, stored); black != -white {
		t.Errorf("optimism for Black to move = %d, want %d", black, -white)
	}
	setOptimism(tm, 150, types.Black) // Black to move at the root, and ahead
	if got := tm.Optimism.Load(); got != -stored {
		t.Errorf("Black ahead at the root: stored optimism %d, want %d", got, -stored)
	}
	setOptimism(tm, MateScore, types.White)
	b.SideToMove = types.White
	if v := optimismCP(b, 3000, tm.Optimism.Load()); v > 10 {
		t.Errorf("optimism at a mate score = %d, want at most 10", v)
	}
	start, _ := board.ParseFEN(board.StartFEN)
	if v := optimismCP(start, 30, tm.Optimism.Load()); v != 0 {
		t.Errorf("optimism with material level = %d, want 0", v)
	}
}
