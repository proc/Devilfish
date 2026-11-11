package movegen

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

// TestLegalMoveCounts asserts exact legal-move counts for a range of tactical
// positions. These are regression guards for the move generator.
func TestLegalMoveCounts(t *testing.T) {
	cases := []struct {
		name     string
		fen      string
		expected int
	}{
		{"Scholar's mate (black to move, mated)", "r1bqk1nr/pppp1Qpp/2n2n2/4p3/2B1P3/8/PPPP1PPP/RNB1K1NR b KQkq - 0 1", 0},
		{"Rook+king vs cornered king", "6k1/5ppp/8/8/8/8/8/4R1K1 w - - 0 1", 17},
		{"Rook+king vs rook+king", "4k2r/8/8/8/8/8/8/4R3 w - - 0 1", 14},
		{"King vs king", "3k4/8/8/8/8/8/8/4K3 w - - 0 1", 5},
		{"Queen vs king", "4k3/8/8/8/3Q4/8/8/4K3 w - - 0 1", 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := board.ParseFEN(tc.fen)
			if err != nil {
				t.Fatalf("ParseFEN(%q) failed: %v", tc.fen, err)
			}
			if got := len(GenerateLegalMoves(b)); got != tc.expected {
				t.Errorf("%s: got %d legal moves, want %d", tc.fen, got, tc.expected)
			}
		})
	}
}

// TestMoveFlags verifies that generated moves carry the correct flag bits for
// captures, en passant, castling, and quiet moves.
func TestMoveFlags(t *testing.T) {
	// Kiwipete: a rich middlegame position with captures and castling on both sides.
	b, err := board.ParseFEN("r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	var captures, quiets, castles int
	var ml MoveList
	GenerateMoves(b, false, &ml)
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		switch {
		case m.IsCastle():
			castles++
		case m.IsCapture():
			captures++
		default:
			quiets++
		}
	}
	if captures == 0 {
		t.Errorf("expected some capture moves in this position")
	}
	if quiets == 0 {
		t.Errorf("expected some quiet moves in this position")
	}
	if castles == 0 {
		t.Errorf("expected at least one castling move")
	}
	// A quiet move must have FlagQuiet (0) and neither capture/castle/promotion bits.
	quietFound := false
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.Flags() == types.FlagQuiet && !m.IsCapture() && !m.IsCastle() && !m.IsPromotion() {
			quietFound = true
		}
	}
	if !quietFound {
		t.Errorf("expected a pure quiet move (FlagQuiet) in the list")
	}
}

// TestPromotionMovesGenerated verifies that a pawn about to promotes generates
// all four promotion types (quiet and capture) plus the underlying pawn moves.
func TestPromotionMovesGenerated(t *testing.T) {
	// White pawn e7 heading to e8; knight on a8 for non-promotion moves.
	b, err := board.ParseFEN("N6k/4P3/8/8/8/8/8/6K1 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	var ml MoveList
	GenerateMoves(b, false, &ml)

	seen := map[types.PieceType]bool{}
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.From() == types.E7 && m.To() == types.E8 && m.IsPromotion() {
			seen[m.PromotionType()] = true
		}
	}
	for _, pt := range []types.PieceType{types.Knight, types.Bishop, types.Rook, types.Queen} {
		if !seen[pt] {
			t.Errorf("expected e7e8 %s promotion to be generated", pt)
		}
	}

	// Capture-promotion: d7xc8 (queen) with a black rook on c8 and king on e8.
	bc, err := board.ParseFEN("2r1k3/3P4/8/8/8/8/8/6K1 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	var mlc MoveList
	GenerateMoves(bc, false, &mlc)
	capPromo := 0
	for i := 0; i < mlc.Count; i++ {
		m := mlc.Moves[i]
		if m.IsPromotion() && m.IsCapture() && m.From() == types.D7 {
			capPromo++
		}
	}
	if capPromo == 0 {
		t.Errorf("expected capture-promotion moves from d7 (e.g. d7c8) to be generated")
	}
}

// TestEnPassantGeneration verifies the en-passant capture is generated for a
// pawn adjacent to a just-moved double-push pawn.
func TestEnPassantGeneration(t *testing.T) {
	b, err := board.ParseFEN("8/8/8/3pP3/8/8/8/8 w - d6 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	var ml MoveList
	GenerateMoves(b, false, &ml)
	found := false
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if m.Flags() == types.FlagEnPassant {
			if m.From() != types.E5 || m.To() != types.D6 {
				t.Errorf("unexpected EP move %s, want e5d6", m)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("expected en-passant e5d6 to be generated with EP target d6")
	}

	// Without an EP target square, no EP capture should be generated.
	b2, err := board.ParseFEN("8/8/8/3pP3/8/8/8/8 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	var ml2 MoveList
	GenerateMoves(b2, false, &ml2)
	for i := 0; i < ml2.Count; i++ {
		if ml2.Moves[i].Flags() == types.FlagEnPassant {
			t.Errorf("did not expect an EP move without an EP target, got %s", ml2.Moves[i])
		}
	}
}

// TestCastlingOnlyWhenKingNotInCheck verifies castling moves are only generated
// when the king is not currently in check.
func TestCastlingOnlyWhenKingNotInCheck(t *testing.T) {
	// Black rook e8 gives check along the e-file; white cannot castle.
	b, err := board.ParseFEN("4r2k/8/8/8/8/8/8/R3K2R w KQ - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	var ml MoveList
	GenerateMovesByType(b, GenAll, &ml)
	for i := 0; i < ml.Count; i++ {
		if ml.Moves[i].IsCastle() {
			t.Errorf("expected no castling moves when king is in check, got %s", ml.Moves[i])
		}
	}

	// With no check, castling moves are generated.
	b2, err := board.ParseFEN("4k2r/8/8/8/8/8/8/R3K2R w KQ - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	var ml2 MoveList
	GenerateMovesByType(b2, GenAll, &ml2)
	castleCount := 0
	for i := 0; i < ml2.Count; i++ {
		if ml2.Moves[i].IsCastle() {
			castleCount++
		}
	}
	if castleCount == 0 {
		t.Errorf("expected castling moves when king is not in check")
	}
}

// TestIsPseudoLegal verifies the pseudo-legality predicate per piece type and
// for common illegal cases, using boards sized to isolate each piece.
func TestIsPseudoLegal(t *testing.T) {
	knightBoard, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	pawnBoard, err := board.ParseFEN("4k3/8/8/4P3/8/8/4P3/4K3 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	rookBoard, err := board.ParseFEN("4k3/8/8/8/8/8/8/R3K3 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	bishopBoard, err := board.ParseFEN("4k3/8/8/8/8/8/8/4B2K w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}

	cases := []struct {
		name   string
		b      *board.Board
		move   types.Move
		pseudo bool
	}{
		// Knight
		{"Knight g1 to f3 (L-shape)", knightBoard, types.MakeMove(types.G1, types.F3, types.FlagQuiet), true},
		{"Knight g1 to e2 (not L-shape)", knightBoard, types.MakeMove(types.G1, types.E2, types.FlagQuiet), false},
		// Pawns
		{"Pawn e2 to e4 (double push)", pawnBoard, types.MakeMove(types.E2, types.E4, types.FlagDoublePawnPush), true},
		{"Pawn e2 to e3 (single push)", pawnBoard, types.MakeMove(types.E2, types.E3, types.FlagQuiet), true},
		{"Pawn e2 to e5 (double push onto occupied)", pawnBoard, types.MakeMove(types.E2, types.E5, types.FlagDoublePawnPush), false},
		{"Pawn e5 to e6 (single push)", pawnBoard, types.MakeMove(types.E5, types.E6, types.FlagQuiet), true},
		// Rook
		{"Rook a1 to a2 (along file)", rookBoard, types.MakeMove(types.A1, types.A2, types.FlagQuiet), true},
		{"Rook a1 to c1 (along rank)", rookBoard, types.MakeMove(types.A1, types.C1, types.FlagQuiet), true},
		{"Rook a1 to c3 (not a straight line)", rookBoard, types.MakeMove(types.A1, types.C3, types.FlagQuiet), false},
		// King
		{"King e1 to e2 (adjacent)", rookBoard, types.MakeMove(types.E1, types.E2, types.FlagQuiet), true},
		{"King e1 to e3 (two squares)", rookBoard, types.MakeMove(types.E1, types.E3, types.FlagQuiet), false},
		{"King e1 to f1 (adjacent)", rookBoard, types.MakeMove(types.E1, types.F1, types.FlagQuiet), true},
		// Bishop
		{"Bishop e1 to a5 (diagonal)", bishopBoard, types.MakeMove(types.E1, types.A5, types.FlagQuiet), true},
		{"Bishop e1 to e2 (not diagonal)", bishopBoard, types.MakeMove(types.E1, types.E2, types.FlagQuiet), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPseudoLegal(tc.b, tc.move); got != tc.pseudo {
				t.Errorf("IsPseudoLegal(%s) = %v, want %v", tc.move, got, tc.pseudo)
			}
		})
	}
}

// TestIsPseudoLegalBlockedByFriendly verifies a move landing on a friendly piece
// is not pseudo-legal, and castling is never pseudo-legal per the predicate.
func TestIsPseudoLegalBlockedByFriendly(t *testing.T) {
	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	// e2 pawn cannot pseudo-legally move to e1 (occupied by own king).
	if IsPseudoLegal(b, types.MakeMove(types.E2, types.E1, types.FlagQuiet)) {
		t.Errorf("expected pawn e2e1 (onto own king) to be non-pseudo-legal")
	}
	// Castling moves are reported as non-pseudo-legal by the predicate.
	if IsPseudoLegal(b, types.MakeMove(types.E1, types.G1, types.FlagKingCastle)) {
		t.Errorf("expected kingside castle to be non-pseudo-legal per predicate")
	}
}

// TestCaptureQuietPartition verifies that captures and quiets partition the full
// move list for several tactical positions.
func TestCaptureQuietPartition(t *testing.T) {
	fens := []string{
		board.StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"4k2r/8/8/8/8/8/8/R3K2R w KQ - 0 1",
		"6k1/5ppp/8/8/8/8/8/4R1K1 w - - 0 1",
	}
	for _, fen := range fens {
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatalf("ParseFEN(%q) failed: %v", fen, err)
		}
		var all, caps, quiets MoveList
		GenerateMovesByType(b, GenAll, &all)
		GenerateMovesByType(b, GenCaptures, &caps)
		GenerateMovesByType(b, GenQuiets, &quiets)
		if caps.Count+quiets.Count != all.Count {
			t.Errorf("FEN %s: captures(%d) + quiets(%d) != all(%d)",
				fen, caps.Count, quiets.Count, all.Count)
		}
		// Every captured move must actually be a capture, every quiet a non-capture.
		for i := 0; i < caps.Count; i++ {
			if !caps.Moves[i].IsCapture() {
				t.Errorf("FEN %s: GenCaptures returned non-capture %s", fen, caps.Moves[i])
			}
		}
		for i := 0; i < quiets.Count; i++ {
			if quiets.Moves[i].IsCapture() {
				t.Errorf("FEN %s: GenQuiets returned capture %s", fen, quiets.Moves[i])
			}
		}
	}
}
