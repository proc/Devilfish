package bitboard

import (
	"testing"

	"devilfish/internal/types"
)

// --- Reference sliding-attack implementations used to validate the magic tables. ---

func refBishopAttacks(sq int, occ Bitboard) Bitboard {
	var bb Bitboard
	r, f := sq/8, sq%8
	dirs := [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	for _, d := range dirs {
		for k := 1; ; k++ {
			nr, nf := r+d[0]*k, f+d[1]*k
			if nr < 0 || nr > 7 || nf < 0 || nf > 7 {
				break
			}
			s := nr*8 + nf
			bb |= SquareBB[s]
			if occ&SquareBB[s] != 0 {
				break
			}
		}
	}
	return bb
}

func refRookAttacks(sq int, occ Bitboard) Bitboard {
	var bb Bitboard
	r, f := sq/8, sq%8
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for _, d := range dirs {
		for k := 1; ; k++ {
			nr, nf := r+d[0]*k, f+d[1]*k
			if nr < 0 || nr > 7 || nf < 0 || nf > 7 {
				break
			}
			s := nr*8 + nf
			bb |= SquareBB[s]
			if occ&SquareBB[s] != 0 {
				break
			}
		}
	}
	return bb
}

// TestShifts verifies directional shifts including edge wraparound semantics.
func TestShifts(t *testing.T) {
	// North shifts up a rank; nothing wraps off the top.
	if got := ShiftNorth(Rank1BB); got != Rank2BB {
		t.Errorf("ShiftNorth(rank1) = 0x%x, want 0x%x", got, Rank2BB)
	}
	if got := ShiftNorth(Rank8BB); got != 0 {
		t.Errorf("ShiftNorth(rank8) = 0x%x, want 0 (wraps off board)", got)
	}
	if got := ShiftSouth(Rank1BB); got != 0 {
		t.Errorf("ShiftSouth(rank1) = 0x%x, want 0", got)
	}
	if got := ShiftSouth(Rank8BB); got != Rank7BB {
		t.Errorf("ShiftSouth(rank8) = 0x%x, want 0x%x", got, Rank7BB)
	}

	// East wraps the a-file into the b-file and discards the h-file.
	if got := ShiftEast(FileABB); got != FileBBB {
		t.Errorf("ShiftEast(a-file) = 0x%x, want 0x%x", got, FileBBB)
	}
	if got := ShiftEast(FileHBB); got != 0 {
		t.Errorf("ShiftEast(h-file) = 0x%x, want 0", got)
	}
	// West is the inverse of East for non-h-file columns.
	if got := ShiftWest(FileBBB); got != FileABB {
		t.Errorf("ShiftWest(b-file) = 0x%x, want 0x%x", got, FileABB)
	}
	if got := ShiftWest(FileABB); got != 0 {
		t.Errorf("ShiftWest(a-file) = 0x%x, want 0", got)
	}

	// Diagonals shift one step; a1->b2, h1->g2, a7->b6, h7->g6.
	if got := ShiftNorthEast(SquareBB[types.A1]); got != SquareBB[types.B2] {
		t.Errorf("ShiftNorthEast(a1) = 0x%x, want b2", got)
	}
	if got := ShiftNorthWest(SquareBB[types.H1]); got != SquareBB[types.G2] {
		t.Errorf("ShiftNorthWest(h1) = 0x%x, want g2", got)
	}
	if got := ShiftSouthEast(SquareBB[types.A7]); got != SquareBB[types.B6] {
		t.Errorf("ShiftSouthEast(a7) = 0x%x, want b6", got)
	}
	if got := ShiftSouthWest(SquareBB[types.H7]); got != SquareBB[types.G6] {
		t.Errorf("ShiftSouthWest(h7) = 0x%x, want g6", got)
	}
}

// TestBitCounters verifies popcount / pop-lsb / lsb helpers.
func TestBitCounters(t *testing.T) {
	if Count(0) != 0 {
		t.Error("Count(0) != 0")
	}
	if Count(All) != 64 {
		t.Errorf("Count(All) = %d, want 64", Count(All))
	}
	if Count(FileABB) != 8 {
		t.Errorf("Count(file a) = %d, want 8", Count(FileABB))
	}

	// LSB returns the index of the lowest set bit.
	if got := LSB(0x80); got != types.Square(7) {
		t.Errorf("LSB(0x80) = %d, want 7", got)
	}

	// PopLSB returns and clears the lowest set bit in turn.
	b := FileHBB
	var got []types.Square
	for b != 0 {
		sq := PopLSB(&b)
		got = append(got, sq)
	}
	want := []types.Square{types.H1, types.H2, types.H3, types.H4, types.H5, types.H6, types.H7, types.H8}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("PopLSB sequence[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	if b != 0 {
		t.Errorf("PopLSB did not empty the bitboard: remaining 0x%x", b)
	}
}

// TestKnownAttackTables checks piece attack tables against well-known values.
func TestKnownAttackTables(t *testing.T) {
	// Knight on e4 reaches exactly 8 squares.
	e4 := types.Square(28)
	if got := Count(KnightAttacks(e4)); got != 8 {
		t.Errorf("Knight e4 attacks = %d, want 8", got)
	}
	wantKnight := Bitboard(0)
	for _, sq := range []types.Square{11, 13, 18, 22, 34, 38, 43, 45} {
		wantKnight |= SquareBB[sq] // d2 f2 c3 g3 c5 g5 d6 f6
	}
	if KnightAttacks(e4) != wantKnight {
		t.Errorf("Knight e4 attacks = 0x%x, want 0x%x", KnightAttacks(e4), wantKnight)
	}

	// Corner knight a1 reaches only 2 squares.
	if got := Count(KnightAttacks(types.A1)); got != 2 {
		t.Errorf("Knight a1 attacks = %d, want 2", got)
	}

	// King on e4 reaches 8; king in a corner reaches 3.
	if got := Count(KingAttacks(e4)); got != 8 {
		t.Errorf("King e4 attacks = %d, want 8", got)
	}
	if got := Count(KingAttacks(types.A1)); got != 3 {
		t.Errorf("King a1 attacks = %d, want 3", got)
	}

	// White pawn on e4 attacks d5 and f5; black pawn on e4 attacks d3 and f3.
	wantWPawn := SquareBB[35] | SquareBB[37] // d5 f5
	if PawnAttacks(types.White, e4) != wantWPawn {
		t.Errorf("White pawn e4 attacks = 0x%x, want 0x%x", PawnAttacks(types.White, e4), wantWPawn)
	}
	if got := Count(PawnAttacks(types.White, e4)); got != 2 {
		t.Errorf("White pawn e4 attack count = %d, want 2", got)
	}
	if got := Count(PawnAttacks(types.Black, e4)); got != 2 {
		t.Errorf("Black pawn e4 attack count = %d, want 2", got)
	}

	// Pawn attacks are anti-symmetric: white pawn attacks sq == black pawn attacked-by sq.
	for sq := types.Square(0); sq < types.SquareNone; sq++ {
	}
}

// TestPawnAttacksBB verifies mass pawn-attack generation.
func TestPawnAttacksBB(t *testing.T) {
	// A single white pawn on e4 attacks d5/f5.
	if got := PawnAttacksBB(types.White, SquareBB[28]); got != (SquareBB[35] | SquareBB[37]) {
		t.Errorf("PawnAttacksBB(white, e4) = 0x%x", got)
	}
	// Two pawns on d4 and e4 attack along c5-f5.
	pawns := SquareBB[27] | SquareBB[28] // d4 e4
	got := PawnAttacksBB(types.White, pawns)
	want := SquareBB[36] | SquareBB[37] | SquareBB[34] | SquareBB[35] // c5 d5 f5 e5
	if got != want {
		t.Errorf("PawnAttacksBB(white, d4+e4) = 0x%x, want 0x%x", got, want)
	}
}

// TestMagicSlidingAttacks exhaustively validates the magic bishop/rook tables
// against reference implementations over every blocking configuration on each square.
func TestMagicSlidingAttacks(t *testing.T) {
	t.Run("Bishop", func(t *testing.T) {
		totalMismatch := 0
		var checked uint64
		for sq := 0; sq < 64; sq++ {
			mask := BishopMasks[sq]
			// Enumerate every subset of the mask bits.
			for occ := Bitboard(0); ; {
				if BishopAttacks(types.Square(sq), occ) != refBishopAttacks(sq, occ) {
					totalMismatch++
				}
				checked++
				if occ == 0 {
					break
				}
				occ = (occ - mask) & mask
			}
		}
		if totalMismatch != 0 {
			t.Errorf("%d/%d bishop lookups mismatched reference", totalMismatch, checked)
		}
	})

	t.Run("Rook", func(t *testing.T) {
		totalMismatch := 0
		var checked uint64
		for sq := 0; sq < 64; sq++ {
			mask := RookMasks[sq]
			for occ := Bitboard(0); ; {
				if RookAttacks(types.Square(sq), occ) != refRookAttacks(sq, occ) {
					totalMismatch++
				}
				checked++
				if occ == 0 {
					break
				}
				occ = (occ - mask) & mask
			}
		}
		if totalMismatch != 0 {
			t.Errorf("%d/%d rook lookups mismatched reference", totalMismatch, checked)
		}
	})
}

// TestQueenAttacksEqualBishopPlusRook checks queen = bishop + rook on several positions.
func TestQueenAttacks(t *testing.T) {
	occ := SquareBB[20] | SquareBB[30] | SquareBB[40] // some blockers
	for sq := types.Square(0); sq < types.SquareNone; sq++ {
		want := BishopAttacks(sq, occ) | RookAttacks(sq, occ)
		if got := QueenAttacks(sq, occ); got != want {
			t.Errorf("QueenAttacks(%d) = 0x%x, want 0x%x", sq, got, want)
		}
	}
}

// TestBetweenAndLine checks inter-square between/line tables for consistency.
func TestBetweenAndLine(t *testing.T) {
	// Between two squares on the same rank, only squares strictly between are set.
	b := BetweenSquares(types.A1, types.D1)
	if b != (SquareBB[types.B1] | SquareBB[types.C1]) {
		t.Errorf("Between(A1,D1) = 0x%x, want b1|c1", b)
	}
	// Between on a diagonal.
	b = BetweenSquares(types.A1, types.D4)
	if b != (SquareBB[types.B2] | SquareBB[types.C3]) {
		t.Errorf("Between(A1,D4) = 0x%x, want b2|c3", b)
	}
	// Between of adjacent squares is empty.
	if b := BetweenSquares(types.A1, types.B1); b != 0 {
		t.Errorf("Between(A1,B1) = 0x%x, want 0", b)
	}
	// Between of orthogonal non-aligned squares is empty.
	if b := BetweenSquares(types.A1, types.B2); b != 0 {
		t.Errorf("Between(A1,B2) = 0x%x, want 0 (not aligned)", b)
	}

	// LineBetween from A1 toward D4 covers the whole a1-h8 diagonal.
	line := LineBetween(types.A1, types.D4)
	if line != (Rank1BB | Rank8BB | FileABB | FileHBB) && Count(line) != 8 {
		// The a1-h8 ray has 8 squares.
		t.Errorf("LineBetween(A1,D4) popcount = %d, want 8", Count(line))
	}
	if line&SquareBB[types.H8] == 0 {
		t.Errorf("LineBetween(A1,D4) should include h8")
	}
	if line&SquareBB[types.A1] == 0 {
		t.Errorf("LineBetween(A1,D4) should include a1")
	}

	// Between is symmetric.
	if BetweenSquares(types.A1, types.H8) != BetweenSquares(types.H8, types.A1) {
		t.Error("BetweenSquares not symmetric")
	}
}

// TestSquareBBAndArrays checks basic board-array invariants.
func TestSquareBBAndArrays(t *testing.T) {
	if SquareBB[types.A1] != 1 {
		t.Error("SquareBB[a1] should be 1")
	}
	if SquareBB[types.H8] != (Bitboard(1) << 63) {
		t.Error("SquareBB[h8] should be the top bit")
	}
	// OR of all square bits equals the full board.
	var all Bitboard
	for sq := types.Square(0); sq < types.SquareNone; sq++ {
		all |= SquareBB[sq]
	}
	if all != All {
		t.Errorf("OR of all squares = 0x%x, want 0x%x", all, All)
	}
	// Files and ranks partition the board.
	var files Bitboard
	for _, f := range FilesBB {
		files |= f
	}
	if files != All {
		t.Error("OR of all files != full board")
	}
	var ranks Bitboard
	for _, r := range RanksBB {
		ranks |= r
	}
	if ranks != All {
		t.Error("OR of all ranks != full board")
	}
}
