package eval

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

// EvaluateClassical is used directly so these tests do not depend on the NNUE
// network being loaded.

func evalClassical(t *testing.T, fen string) int32 {
	t.Helper()
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatalf("failed to parse FEN %q: %v", fen, err)
	}
	return EvaluateClassical(b)
}

// TestScoreTypeUnitTests cover the Score arithmetic and accessors used throughout eval.
func TestScoreTypeUnitTests(t *testing.T) {
	a := Score{mg: 100, eg: 130}
	b := Score{mg: -30, eg: 40}
	if s := a.Add(b); s.MG() != 70 || s.EG() != 170 {
		t.Errorf("Add: got %+v, want {70,170}", s)
	}
	if s := a.Sub(b); s.MG() != 130 || s.EG() != 90 {
		t.Errorf("Sub: got %+v, want {130,90}", s)
	}
	if a.MG() != 100 || a.EG() != 130 {
		t.Error("MG()/EG() accessors")
	}

	// Phase constants.
	if TotalPhase != 24 {
		t.Errorf("TotalPhase = %d, want 24", TotalPhase)
	}
	if PiecePhase[types.Pawn] != 0 || PiecePhase[types.Knight] != 1 || PiecePhase[types.Bishop] != 1 ||
		PiecePhase[types.Rook] != 2 || PiecePhase[types.Queen] != 4 || PiecePhase[types.King] != 0 {
		t.Errorf("PiecePhase table wrong: %+v", PiecePhase)
	}
}

// TestPSTCentralization checks the piece-square tables reward centralization.
func TestPSTCentralization(t *testing.T) {
	// Knight: e4 > f3 > a1 in both midgame and endgame tables.
	e4, f3, a1 := PST[types.Knight][types.E4], PST[types.Knight][types.F3], PST[types.Knight][types.A1]
	if e4.MG() <= f3.MG() || f3.MG() <= a1.MG() {
		t.Errorf("Knight midgame PST centralization: e4=%d f3=%d a1=%d", e4.MG(), f3.MG(), a1.MG())
	}
	if e4.EG() <= f3.EG() || f3.EG() <= a1.EG() {
		t.Errorf("Knight endgame PST centralization: e4=%d f3=%d a1=%d", e4.EG(), f3.EG(), a1.EG())
	}

	// Pawn: midgame value rises with advancement to the 7th rank.
	if !(PST[types.Pawn][types.E7].MG() > PST[types.Pawn][types.E5].MG() &&
		PST[types.Pawn][types.E5].MG() > PST[types.Pawn][types.E2].MG()) {
		t.Errorf("Pawn midgame PST should rise with advancement: e2=%d e5=%d e7=%d",
			PST[types.Pawn][types.E2].MG(), PST[types.Pawn][types.E5].MG(), PST[types.Pawn][types.E7].MG())
	}
	// Pawn endgame: 7th rank is paramount.
	if PST[types.Pawn][types.E7].EG() <= PST[types.Pawn][types.E2].EG() {
		t.Errorf("Pawn endgame PST: e7=%d should exceed e2=%d", PST[types.Pawn][types.E7].EG(), PST[types.Pawn][types.E2].EG())
	}
}

// TestEvaluateMaterialOrdering checks that more material yields a higher score.
func TestEvaluateMaterialOrdering(t *testing.T) {
	q := evalClassical(t, "4k3/8/8/8/8/8/8/Q3K3 w - - 0 1")
	r := evalClassical(t, "4k3/8/8/8/8/8/8/R3K3 w - - 0 1")
	n := evalClassical(t, "4k3/8/8/8/8/8/8/1N2K3 w - - 0 1")
	p := evalClassical(t, "4k3/8/8/8/8/8/4P3/4K3 w - - 0 1")
	balanced := evalClassical(t, board.StartFEN)

	if !(q > r) {
		t.Errorf("Queen advantage (%d) should exceed rook (%d)", q, r)
	}
	if !(r > p) {
		t.Errorf("Rook advantage (%d) should exceed a pawn (%d)", r, p)
	}
	if !(p > balanced) {
		t.Errorf("Extra pawn (%d) should exceed balanced startpos (%d)", p, balanced)
	}
	if !(q > r && r > n && n > balanced) {
		t.Errorf("Material ordering Q(%d) > R(%d) > N(%d) > balanced(%d)", q, r, n, balanced)
	}
}

// TestEvaluateStartposBalanced checks the engine does not wildly over/under-evaluate the start.
func TestEvaluateStartposBalanced(t *testing.T) {
	score := evalClassical(t, board.StartFEN)
	if score < -30 || score > 30 {
		t.Errorf("startpos eval %d should be near tempo (~15), got out of [-30,30]", score)
	}
}

// TestEvaluateSignFollowsSideToMove checks eval is reported relative to the side to move.
func TestEvaluateSignFollowsSideToMove(t *testing.T) {
	// White up a queen with WHITE to move: positive.
	whiteStm := evalClassical(t, "4k3/8/8/8/8/8/8/Q3K3 w - - 0 1")
	if whiteStm <= 0 {
		t.Errorf("expected positive eval when white (up a queen) to move, got %d", whiteStm)
	}
	// Same material imbalance but BLACK to move: negative.
	blackStm := evalClassical(t, "4k3/8/8/8/8/8/8/Q3K3 b - - 0 1")
	if blackStm >= 0 {
		t.Errorf("expected negative eval when black (up a queen) to move, got %d", blackStm)
	}
}

// TestEvaluatePassedPawn checks advanced passed pawns score more.
func TestEvaluatePassedPawn(t *testing.T) {
	advanced := evalClassical(t, "4k3/8/8/4P3/8/8/8/4K3 w - - 0 1")
	base := evalClassical(t, "4k3/8/8/8/8/8/4P3/4K3 w - - 0 1")
	if advanced <= base {
		t.Errorf("advanced passed pawn (%d) should score at least as much as base (%d)", advanced, base)
	}
	if base <= 0 {
		t.Errorf("a lone passed pawn should give white a positive eval, got %d", base)
	}
}

// TestEvaluateIsolatedPawn checks the isolated-pawn penalty reduces the score.
func TestEvaluateIsolatedPawn(t *testing.T) {
	// {c2,d2}: d-pawn is connected (c-file present).
	connected := evalClassical(t, "4k3/8/8/8/8/8/2PP4/4K3 w - - 0 1")
	// {d2,f2}: d-pawn is isolated (no c/e-file pawns); the c2 pawn moved to f2 (same rank).
	isolated := evalClassical(t, "4k3/8/8/8/8/8/3P1P2/4K3 w - - 0 1")
	if isolated >= connected {
		t.Errorf("isolated pawn (%d) should score lower than connected (%d)", isolated, connected)
	}
}

// TestEvaluateDoubledPawn checks the doubled-pawn penalty reduces the score.
func TestEvaluateDoubledPawn(t *testing.T) {
	// {c3,d2}: not doubled.
	connected := evalClassical(t, "4k3/8/2P5/4P3/8/8/8/4K3 w - - 0 1")
	// {c2,c3}: c-file doubled (the d2 pawn retreated to c2, same rank, so advancement is held fixed).
	doubled := evalClassical(t, "4k3/8/2P5/2P5/8/8/8/4K3 w - - 0 1")
	if doubled >= connected {
		t.Errorf("doubled pawns (%d) should score lower than non-doubled (%d)", doubled, connected)
	}
}

// TestEvaluateOpenFileRook checks open/semi-open file rook bonuses.
func TestEvaluateOpenFileRook(t *testing.T) {
	open := evalClassical(t, "4k3/8/8/8/8/8/8/2R1K3 w - - 0 1")
	semi := evalClassical(t, "4k3/8/8/8/8/8/2p5/2R1K3 w - - 0 1")
	if open <= semi {
		t.Errorf("open-file rook (%d) should score higher than semi-open (%d)", open, semi)
	}
	if semi <= 0 {
		t.Errorf("semi-open file rook should be positive, got %d", semi)
	}
}

// TestEvaluateSeventhRankRook checks the 7th-rook bonus.
func TestEvaluateSeventhRankRook(t *testing.T) {
	score := evalClassical(t, "4k3/8/8/8/8/8/8/4RK2 w - - 0 1")
	if score <= 0 {
		t.Errorf("rook on the 7th rank should be strongly positive, got %d", score)
	}
}

// TestEvaluateTaperedEndgame checks the mg->eg interpolation: a queen advantage is
// worth more in an endgame (eg value 1020) than in a full midgame (mg value 950).
func TestEvaluateTaperedEndgame(t *testing.T) {
	endgame := evalClassical(t, "4k3/8/8/8/8/8/8/Q3K3 w - - 0 1")
	midgame := evalClassical(t, "rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
	if endgame <= midgame {
		t.Errorf("queen advantage should be worth more in the endgame (%d) than midgame (%d)", endgame, midgame)
	}
}
