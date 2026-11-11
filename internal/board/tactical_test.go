package board_test

import (
	"testing"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// castleMove reports whether m is a castling move from e1 to the given square.
func castleTo(m types.Move, to types.Square) bool {
	return m.IsCastle() && m.From() == types.E1 && m.To() == to
}

// TestCheckmatePatterns verifies positions where the side to move is in check
// with no legal moves (checkmate), across several common mating patterns
// (Scholar's mate and a back-rank mate).
func TestCheckmatePatterns(t *testing.T) {
	cases := []struct {
		name string
		fen  string
	}{
		{
			name: "Scholar's mate",
			fen:  "r1bqk1nr/pppp1Qpp/2n2n2/4p3/2B1P3/8/PPPP1PPP/RNB1K1NR b KQkq - 0 1",
		},
		{
			name: "Back-rank mate (queen supported)",
			fen:  "Q5k1/5ppp/8/8/8/8/8/6K1 b - - 0 1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := board.ParseFEN(tc.fen)
			if err != nil {
				t.Fatalf("ParseFEN(%q) failed: %v", tc.fen, err)
			}
			if !b.IsInCheck(b.SideToMove) {
				t.Errorf("%s: expected side to move to be in check", tc.fen)
			}
			if legal := movegen.GenerateLegalMoves(b); len(legal) != 0 {
				t.Errorf("%s: expected checkmate (0 legal moves), got %d: %v",
					tc.fen, len(legal), legal)
			}
		})
	}
}

// TestStalematePattern verifies a stalemate: side to move is not in check but
// has no legal moves.
func TestStalematePattern(t *testing.T) {
	b, err := board.ParseFEN("6k1/8/8/8/8/8/5R2/5K1k b - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	if b.IsInCheck(b.SideToMove) {
		t.Errorf("expected side to move to NOT be in check")
	}
	if legal := movegen.GenerateLegalMoves(b); len(legal) != 0 {
		t.Errorf("expected stalemate (0 legal moves, not in check), got %d: %v",
			len(legal), legal)
	}
}

// TestDoubleCheck verifies that a position with two independent checkers reports
// both as checkers (double check), which forces the king to move.
func TestDoubleCheck(t *testing.T) {
	// Queen on h5 and rook on e2 both check the e8 king simultaneously.
	b, err := board.ParseFEN("4k3/8/8/7Q/8/8/4R3/6K1 b - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	if !b.IsInCheck(b.SideToMove) {
		t.Fatalf("expected to be in check")
	}
	checkers := b.Checkers(b.SideToMove)
	if got := bitboard.Count(checkers); got != 2 {
		t.Errorf("expected 2 checkers (double check), got %d (%x)", got, checkers)
	}
	// A double check can only be met by moving the king (cannot block two lines
	// or capture both checkers). All legal moves must be king moves.
	for _, m := range movegen.GenerateLegalMoves(b) {
		if m.From() != b.KingSquare(b.SideToMove) {
			t.Errorf("double check: expected only king moves, got %s", m)
		}
	}
}

// TestAbsolutePin verifies that a piece pinned to its king cannot legally leave
// the pin line, but may move along it, and that removing the pinned piece
// exposes the king.
func TestAbsolutePin(t *testing.T) {
	// White king e1, white queen e2 (pinned), black rook e8, black king h8.
	b, err := board.ParseFEN("4r2k/8/8/8/8/8/4Q3/4K2R w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	if b.IsInCheck(b.SideToMove) {
		t.Fatalf("position should not have the side to move in check")
	}

	// Leaving the e-file exposes the king to the rook => illegal.
	off := types.MakeMove(types.E2, types.A6, types.FlagQuiet)
	if b.MakeMove(off) {
		b.UnmakeMove(off)
		t.Errorf("pinned queen moving off the e-file (e2a6) should be illegal")
	}

	// Moving along the pin line keeps the king safe => legal.
	on := types.MakeMove(types.E2, types.E7, types.FlagQuiet)
	if !b.MakeMove(on) {
		b.UnmakeMove(on)
		t.Errorf("pinned queen moving along the e-file (e2e7) should be legal")
	}
	b.UnmakeMove(on)

	// The pin is real: removing the pinned queen exposes the white king to the
	// e8 rook along the e-file.
	b.Pieces[types.E2] = types.PieceNone
	b.PieceBB[types.WhiteQueen] &^= bitboard.SquareBB[types.E2]
	b.ColorBB[types.White] &^= bitboard.SquareBB[types.E2]
	if !b.IsInCheck(types.White) {
		t.Errorf("removing the pinned queen on e2 should expose the white king to check")
	}
}

// TestCastlingMoves verifies castling move generation, castling through an
// attacked square, and that castling rights are cleared once the king moves.
func TestCastlingMoves(t *testing.T) {
	t.Run("both sides available", func(t *testing.T) {
		b, err := board.ParseFEN("4k2r/8/8/8/8/8/8/R3K2R w KQ - 0 1")
		if err != nil {
			t.Fatalf("ParseFEN failed: %v", err)
		}
		var kingSide, queenSide bool
		for _, m := range movegen.GenerateLegalMoves(b) {
			if castleTo(m, types.G1) {
				kingSide = true
			}
			if castleTo(m, types.C1) {
				queenSide = true
			}
		}
		if !kingSide {
			t.Errorf("expected kingside castle (e1g1) to be available")
		}
		if !queenSide {
			t.Errorf("expected queenside castle (e1c1) to be available")
		}
	})

	t.Run("cannot castle through a checked square", func(t *testing.T) {
		// Black rook on f2 attacks f1, so kingside castling is illegal, but
		// queenside remains legal (d1/c1 are not attacked).
		b, err := board.ParseFEN("4k2r/8/8/8/8/8/5r2/R3K2R w KQ - 0 1")
		if err != nil {
			t.Fatalf("ParseFEN failed: %v", err)
		}
		if !b.IsSquareAttacked(types.F1, types.Black) {
			t.Fatalf("setup: expected black to attack f1")
		}
		var kingSide, queenSide bool
		for _, m := range movegen.GenerateLegalMoves(b) {
			if castleTo(m, types.G1) {
				kingSide = true
			}
			if castleTo(m, types.C1) {
				queenSide = true
			}
		}
		if kingSide {
			t.Errorf("kingside castle should be blocked because f1 is attacked")
		}
		if !queenSide {
			t.Errorf("queenside castle should still be available")
		}
	})

	t.Run("castling rights cleared after king move", func(t *testing.T) {
		b, err := board.ParseFEN("4k2r/8/8/8/8/8/8/R3K2R w KQ - 0 1")
		if err != nil {
			t.Fatalf("ParseFEN failed: %v", err)
		}
		if b.Castling == 0 {
			t.Fatalf("expected castling rights to be set initially")
		}
		// Play a non-castling king move (e1e2); both castling rights must vanish.
		m := types.MakeMove(types.E1, types.E2, types.FlagQuiet)
		if !b.MakeMove(m) {
			t.Fatalf("e1e2 should be legal")
		}
		if b.Castling != 0 {
			t.Errorf("expected castling rights cleared after king move, got %o", b.Castling)
		}
	})
}

// TestEnPassant verifies en-passant move generation, the removal of the
// captured pawn on make, and full board/hash restoration on unmake.
func TestEnPassant(t *testing.T) {
	// White pawn e5, black pawn d5, just-played double push giving EP target d6.
	fen := "8/8/8/3pP3/8/8/8/8 w - d6 0 1"
	b, err := board.ParseFEN(fen)
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	if b.EPSquare != types.D6 {
		t.Fatalf("expected EP square d6, got %s", b.EPSquare)
	}

	var epMove types.Move
	var ml movegen.MoveList
	movegen.GenerateMoves(b, false, &ml)
	for i := 0; i < ml.Count; i++ {
		if ml.Moves[i].Flags() == types.FlagEnPassant {
			epMove = ml.Moves[i]
		}
	}
	if epMove == types.MoveNone {
		t.Fatalf("expected an en-passant move to be generated")
	}
	if epMove.From() != types.E5 || epMove.To() != types.D6 {
		t.Errorf("expected en-passant e5d6, got %s", epMove)
	}

	savedHash := b.Hash
	savedPawnHash := b.PawnHash
	savedEPSquare := b.EPSquare

	if !b.MakeMove(epMove) {
		t.Fatalf("MakeMove(en passant) returned false")
	}
	// The captured black pawn on d5 must be removed.
	if p := b.PieceAt(types.D5); p != types.PieceNone {
		t.Errorf("expected captured pawn on d5 to be removed, found %v", p.Character())
	}
	if b.EPSquare != types.SquareNone {
		t.Errorf("expected EP square cleared after capture, got %s", b.EPSquare)
	}

	b.UnmakeMove(epMove)
	if b.PieceAt(types.D5) != types.BlackPawn {
		t.Errorf("expected black pawn restored on d5 after unmake")
	}
	if b.EPSquare != savedEPSquare {
		t.Errorf("EP square not restored: got %s want %s", b.EPSquare, savedEPSquare)
	}
	if b.Hash != savedHash || b.PawnHash != savedPawnHash {
		t.Errorf("hash not restored after unmake: hash 0x%x->0x%x pawnHash 0x%x->0x%x",
			savedHash, b.Hash, savedPawnHash, b.PawnHash)
	}
	if b.CalculateHash() != savedHash {
		t.Errorf("incremental hash mismatch after unmake")
	}
}

// TestTacticalThreatsHangingPiece verifies that an undefended (hanging) piece
// attacked by an enemy piece is flagged as a tactical threat, while a piece
// adequately defended is not.
func TestTacticalThreatsHangingPiece(t *testing.T) {
	// Black rook on f2 is undefended and attacked by the white king on e1.
	b, err := board.ParseFEN("4k3/8/8/8/4P3/8/5r2/4K3 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	threats := b.TacticalThreatsOnPieces(types.Black)
	if threats&bitboard.SquareBB[types.F2] == 0 {
		t.Errorf("expected black rook on f2 to be flagged under tactical threat")
	}

	// White rook on d1 is attacked by the black rook on d8 but defended by the
	// white king on e2 (a 1v1 trade), so it is not a hanging tactical threat.
	b2, err := board.ParseFEN("3r2k1/5ppp/8/8/8/8/4KPPP/3R4 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	threats2 := b2.TacticalThreatsOnPieces(types.White)
	if threats2&bitboard.SquareBB[types.D1] != 0 {
		t.Errorf("defended rook on d1 should not be flagged as a tactical threat")
	}
}

// TestDiscoveredCheck verifies that moving a piece off a line reveals a check
// from a sliding piece (discovered check).
func TestDiscoveredCheck(t *testing.T) {
	// White rook e1, white knight e4 blocking the e-file, black king e8.
	b, err := board.ParseFEN("4k3/8/8/8/4N3/8/8/4R2K w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN failed: %v", err)
	}
	// A knight move that leaves the e-file discovers check from the rook.
	discovered := types.MakeMove(types.E4, types.D6, types.FlagQuiet)
	if !b.GivesCheck(discovered) {
		t.Errorf("expected %s to give discovered check", discovered)
	}

	// A knight move that stays on the e-file keeps the line blocked.
	blocking := types.MakeMove(types.E4, types.E5, types.FlagQuiet)
	if b.GivesCheck(blocking) {
		t.Errorf("expected %s to NOT give check (e-file still blocked)", blocking)
	}
}
