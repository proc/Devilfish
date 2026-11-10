package nnue

import (
	"os"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

func TestFeatureIndex(t *testing.T) {
	wpE2 := FeatureIndex(types.White, types.WhitePawn, types.E2)
	bpE7 := FeatureIndex(types.Black, types.BlackPawn, types.E7)

	if wpE2 < 0 || wpE2 >= NumFeatures {
		t.Fatalf("invalid feature index for White Pawn on E2: %d", wpE2)
	}

	// Due to vertical symmetry (sq ^ 56), White Pawn on E2 should match Black Pawn on E7
	if wpE2 != bpE7 {
		t.Errorf("expected vertical symmetry between White P on E2 (%d) and Black P on E7 (%d)", wpE2, bpE7)
	}
}

func TestTarraschPieceBlunder(t *testing.T) {
	fenAfter2 := "r1b2rk1/rp4qp/2n1pp2/3p4/3P1b2/3B1N2/PP3PPP/R2Q1R1K b - - 1 17"
	bAfter2, _ := board.ParseFEN(fenAfter2)
	var accAfter2 Accumulator
	Refresh(bAfter2, &accAfter2)
	scoreNNUE := Evaluate(&accAfter2, bAfter2.SideToMove)

	t.Logf("Position after 1...Rxa7 2.Kh1 (Black to move):")
	t.Logf("  NNUE score from Black perspective: %d cp", scoreNNUE)
}

func TestAccumulatorRefreshAndEvaluate(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	var acc Accumulator
	Refresh(b, &acc)

	score := Evaluate(&acc, b.SideToMove)
	// Startpos score should be balanced around zero (within tempo margin)
	if score < -100 || score > 100 {
		t.Errorf("expected balanced startpos score, got %d", score)
	}

	// White is up a queen
	bQueenUp, _ := board.ParseFEN("rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	var accQueen Accumulator
	Refresh(bQueenUp, &accQueen)
	scoreQueen := Evaluate(&accQueen, bQueenUp.SideToMove)

	if scoreQueen <= score+300 {
		t.Errorf("expected substantial White advantage when up a queen, got %d vs %d", scoreQueen, score)
	}
}

func TestPrintEvaluations(t *testing.T) {
	fens := []struct {
		desc string
		fen  string
	}{
		{"Startpos (White)", board.StartFEN},
		{"After 1. e4 (Black)", "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1"},
		{"White up Queen (White)", "rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"},
		{"Black down Queen (Black)", "rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR b KQkq - 0 1"},
		{"White up Knight (White)", "r1bqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"},
		{"White +Rook (White)", "4k3/8/8/8/8/8/8/R3K3 w - - 0 1"},
		{"Black -Rook (Black)", "4k3/8/8/8/8/8/8/R3K3 b - - 0 1"},
		{"Endgame +1 pawn (White)", "8/8/8/4k3/8/4P3/4K3/8 w - - 0 1"},
	}

	for _, tc := range fens {
		b, err := board.ParseFEN(tc.fen)
		if err != nil {
			t.Fatal(err)
		}
		var acc Accumulator
		Refresh(b, &acc)
		score := Evaluate(&acc, b.SideToMove)
		t.Logf("%-25s: score=%+5d cp (stm=%s)", tc.desc, score, b.SideToMove)
	}
}

func TestIncrementalUpdateConsistency(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	var accRoot, accIncremental, accFresh Accumulator
	Refresh(b, &accRoot)

	// Test across all legal moves from start position
	legalMoves := movegen.GenerateLegalMoves(b)
	for _, m := range legalMoves {
		// 1. Incremental update
		Update(&accRoot, &accIncremental, m, b)

		// 2. Make move on board and compute fresh accumulator
		bCopy := b.Clone()
		bCopy.MakeMove(m)
		Refresh(bCopy, &accFresh)

		// 3. Verify White accumulator matches exactly
		for i := 0; i < HiddenSize; i++ {
			if accIncremental.White[i] != accFresh.White[i] {
				t.Fatalf("move %s: mismatch in White accumulator at index %d: incremental %d vs fresh %d",
					m, i, accIncremental.White[i], accFresh.White[i])
			}
			if accIncremental.Black[i] != accFresh.Black[i] {
				t.Fatalf("move %s: mismatch in Black accumulator at index %d: incremental %d vs fresh %d",
					m, i, accIncremental.Black[i], accFresh.Black[i])
			}
		}

		// 4. Verify evaluation scores match
		incScore := Evaluate(&accIncremental, bCopy.SideToMove)
		freshScore := Evaluate(&accFresh, bCopy.SideToMove)
		if incScore != freshScore {
			t.Fatalf("move %s: eval score mismatch: incremental %d vs fresh %d", m, incScore, freshScore)
		}
	}
}

func TestSequenceUpdate(t *testing.T) {
	b, _ := board.ParseFEN(board.StartFEN)
	moveStrs := []string{
		"e2e4", "e7e6", "g1f3", "b8c6", "f1b5", "f8b4", "b1c3", "g8e7",
		"e4e5", "e7g6", "b5c6", "d7c6", "e1e2", "c8d7", "h1e1", "g6f4", "e2e3",
	}
	var acc [25]Accumulator
	Refresh(b, &acc[0])

	for ply, mStr := range moveStrs {
		legalMoves := movegen.GenerateLegalMoves(b)
		var targetMove types.Move
		for _, m := range legalMoves {
			if m.String() == mStr {
				targetMove = m
				break
			}
		}
		if targetMove == types.MoveNone {
			t.Fatalf("illegal move %s", mStr)
		}

		Update(&acc[ply], &acc[ply+1], targetMove, b)
		b.MakeMove(targetMove)

		var fresh Accumulator
		Refresh(b, &fresh)

		incEval := Evaluate(&acc[ply+1], b.SideToMove)
		freshEval := Evaluate(&fresh, b.SideToMove)
		t.Logf("ply %d move %s: incEval %d freshEval %d FEN %s", ply+1, mStr, incEval, freshEval, b.ToFEN())
		if incEval != freshEval {
			t.Errorf("mismatch at ply %d move %s: inc %d vs fresh %d", ply+1, mStr, incEval, freshEval)
		}
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpFile := "test_network.nnue"
	defer os.Remove(tmpFile)

	b, _ := board.ParseFEN(board.StartFEN)
	var accBefore Accumulator
	Refresh(b, &accBefore)
	scoreBefore := Evaluate(&accBefore, b.SideToMove)

	// Save active network
	if err := SaveFile(tmpFile); err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	// Load it back
	if err := LoadFile(tmpFile); err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	var accAfter Accumulator
	Refresh(b, &accAfter)
	scoreAfter := Evaluate(&accAfter, b.SideToMove)

	if scoreBefore != scoreAfter {
		t.Errorf("eval changed after load: before %d, after %d", scoreBefore, scoreAfter)
	}
}

func BenchmarkVectorOpsNEON(b *testing.B) {
	var acc [HiddenSize]int16
	var w [HiddenSize]int16
	for i := 0; i < HiddenSize; i++ {
		acc[i] = int16(i)
		w[i] = int16(i * 2)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addVectorNEON(&acc, &w)
		subVectorNEON(&acc, &w)
	}
}

func BenchmarkVectorOpsScalar(b *testing.B) {
	var acc [HiddenSize]int16
	var w [HiddenSize]int16
	for i := 0; i < HiddenSize; i++ {
		acc[i] = int16(i)
		w[i] = int16(i * 2)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < HiddenSize; j++ {
			acc[j] += w[j]
		}
		for j := 0; j < HiddenSize; j++ {
			acc[j] -= w[j]
		}
	}
}
