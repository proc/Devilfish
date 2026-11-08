package board

import (
	"testing"

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

	// Make move e2e4
	m := types.MakeMove(types.E2, types.E4, types.FlagDoublePawnPush)
	b.MakeMove(m)

	if b.Hash != b.CalculateHash() {
		t.Fatalf("hash mismatch after e2e4: 0x%x vs 0x%x", b.Hash, b.CalculateHash())
	}

	// Make move e7e5
	m2 := types.MakeMove(types.E7, types.E5, types.FlagDoublePawnPush)
	b.MakeMove(m2)

	if b.Hash != b.CalculateHash() {
		t.Fatalf("hash mismatch after e7e5: 0x%x vs 0x%x", b.Hash, b.CalculateHash())
	}

	// Unmake e7e5
	b.UnmakeMove(m2)
	if b.Hash != b.CalculateHash() {
		t.Fatalf("hash mismatch after unmake e7e5: 0x%x vs 0x%x", b.Hash, b.CalculateHash())
	}

	// Unmake e2e4
	b.UnmakeMove(m)
	if b.Hash != initialHash {
		t.Fatalf("hash mismatch after full unmake: 0x%x vs 0x%x", b.Hash, initialHash)
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
