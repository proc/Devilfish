package board

import (
	"testing"

	"devilfish/internal/bitboard"
	"devilfish/internal/types"
)

func TestFENRoundtrip(t *testing.T) {
	fens := []string{
		StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1",
		"r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1",
		"rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8",
	}

	for _, fen := range fens {
		b, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("failed to parse FEN %s: %v", fen, err)
		}
		out := b.ToFEN()
		if out != fen {
			t.Errorf("FEN mismatch:\nexpected: %s\ngot:      %s", fen, out)
		}
	}
}

func TestZobristHashIncremental(t *testing.T) {
	b, err := ParseFEN(StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	initialHash := b.Hash
	calculatedHash := b.CalculateHash()
	if initialHash != calculatedHash {
		t.Fatalf("initial hash mismatch: 0x%x vs 0x%x", initialHash, calculatedHash)
	}
	if b.PawnHash != b.CalculatePawnHash() {
		t.Fatalf("initial pawn hash mismatch: 0x%x vs 0x%x", b.PawnHash, b.CalculatePawnHash())
	}

	// Make move e2e4
	m := types.MakeMove(types.E2, types.E4, types.FlagDoublePawnPush)
	b.MakeMove(m)

	if b.Hash != b.CalculateHash() {
		t.Fatalf("hash mismatch after e2e4: 0x%x vs 0x%x", b.Hash, b.CalculateHash())
	}
	if b.PawnHash != b.CalculatePawnHash() {
		t.Fatalf("pawn hash mismatch after e2e4: 0x%x vs 0x%x", b.PawnHash, b.CalculatePawnHash())
	}

	// Make move e7e5
	m2 := types.MakeMove(types.E7, types.E5, types.FlagDoublePawnPush)
	b.MakeMove(m2)

	if b.Hash != b.CalculateHash() {
		t.Fatalf("hash mismatch after e7e5: 0x%x vs 0x%x", b.Hash, b.CalculateHash())
	}
	if b.PawnHash != b.CalculatePawnHash() {
		t.Fatalf("pawn hash mismatch after e7e5: 0x%x vs 0x%x", b.PawnHash, b.CalculatePawnHash())
	}

	// Unmake e7e5
	b.UnmakeMove(m2)
	if b.Hash != b.CalculateHash() {
		t.Fatalf("hash mismatch after unmake e7e5: 0x%x vs 0x%x", b.Hash, b.CalculateHash())
	}
	if b.PawnHash != b.CalculatePawnHash() {
		t.Fatalf("pawn hash mismatch after unmake e7e5: 0x%x vs 0x%x", b.PawnHash, b.CalculatePawnHash())
	}

	// Unmake e2e4
	b.UnmakeMove(m)
	if b.Hash != initialHash {
		t.Fatalf("hash mismatch after full unmake: 0x%x vs 0x%x", b.Hash, initialHash)
	}
	if b.PawnHash != b.CalculatePawnHash() {
		t.Fatalf("pawn hash mismatch after full unmake: 0x%x vs 0x%x", b.PawnHash, b.CalculatePawnHash())
	}
}

func TestRepetitionDraw(t *testing.T) {
	b, err := ParseFEN(StartFEN)
	if err != nil {
		t.Fatal(err)
	}

	// Nf3 Nf6 Ng1 Ng8 (1 repetition)
	// Nf3 Nf6 Ng1 Ng8 (2nd repetition -> 3fold)
	moves := []types.Move{
		types.MakeMove(types.G1, types.F3, types.FlagQuiet),
		types.MakeMove(types.G8, types.F6, types.FlagQuiet),
		types.MakeMove(types.F3, types.G1, types.FlagQuiet),
		types.MakeMove(types.F6, types.G8, types.FlagQuiet),

		types.MakeMove(types.G1, types.F3, types.FlagQuiet),
		types.MakeMove(types.G8, types.F6, types.FlagQuiet),
		types.MakeMove(types.F3, types.G1, types.FlagQuiet),
		types.MakeMove(types.F6, types.G8, types.FlagQuiet),
	}

	for _, m := range moves {
		b.MakeMove(m)
	}

	if !b.IsRepetition() {
		t.Errorf("expected 3-fold repetition draw")
	}
}

func TestRookTacticalThreats(t *testing.T) {
	tests := []struct {
		name       string
		fen        string
		color      types.Color
		expectedSq types.Square
	}{
		{
			name:       "Undefended White Rook on a1 attacked by Black Queen on a8",
			fen:        "q5k1/5ppp/8/8/8/8/5PPP/R5K1 w - - 0 1",
			color:      types.White,
			expectedSq: types.A1,
		},
		{
			name:       "Undefended White Rook on d1 attacked by Black Rook on d8",
			fen:        "3r2k1/5ppp/8/8/8/8/5PPP/3R2K1 w - - 0 1",
			color:      types.White,
			expectedSq: types.D1,
		},
		{
			name:       "Undefended White Rook on a1 attacked by Black Bishop on h8",
			fen:        "7b/5k2/8/8/8/8/5PPP/R5K1 w - - 0 1",
			color:      types.White,
			expectedSq: types.A1,
		},
		{
			name:       "Defended White Rook on d1 attacked by 2 Black Rooks (Rd8, Rd7)",
			fen:        "3r2k1/3r1ppp/8/8/8/8/5PPP/3R1K2 w - - 0 1",
			color:      types.White,
			expectedSq: types.D1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := ParseFEN(tc.fen)
			if err != nil {
				t.Fatalf("failed to parse FEN: %v", err)
			}
			threats := b.TacticalThreatsOnPieces(tc.color)
			if threats&bitboard.SquareBB[tc.expectedSq] == 0 {
				t.Errorf("expected square %s to be flagged under tactical threat, got threats=0x%x",
					tc.expectedSq, threats)
			}
		})
	}

	// Verify that a properly defended Rook attacked only by an equal piece (1v1) is NOT a tactical threat
	t.Run("Defended White Rook on d1 attacked by 1 Black Rook (1v1 trade)", func(t *testing.T) {
		// King on e2 defends d1
		fen := "3r2k1/5ppp/8/8/8/8/4KPPP/3R4 w - - 0 1"
		b, err := ParseFEN(fen)
		if err != nil {
			t.Fatalf("failed to parse FEN: %v", err)
		}
		threats := b.TacticalThreatsOnPieces(types.White)
		if threats&bitboard.SquareBB[types.D1] != 0 {
			t.Errorf("did not expect defended Rook on d1 in 1v1 trade to be flagged as tactical threat")
		}
	})
}
