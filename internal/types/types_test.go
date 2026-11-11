package types

import "testing"

func TestColor(t *testing.T) {
	if White.Other() != Black || Black.Other() != White {
		t.Error("Color.Other() should swap white/black")
	}
	if White.Other().Other() != White {
		t.Error("Other() is an involution")
	}
	if White.String() != "white" || Black.String() != "black" || ColorNone.String() != "none" {
		t.Errorf("Color.String() mismatch: %q %q %q", White.String(), Black.String(), ColorNone.String())
	}
}

func TestPieceTypeString(t *testing.T) {
	cases := map[PieceType]string{Pawn: "P", Knight: "N", Bishop: "B", Rook: "R", Queen: "Q", King: "K", PieceTypeNone: " "}
	for pt, want := range cases {
		if pt.String() != want {
			t.Errorf("PieceType %d String() = %q, want %q", pt, pt.String(), want)
		}
	}
}

func TestMakePieceAndAccessors(t *testing.T) {
	if MakePiece(White, Pawn) != WhitePawn || MakePiece(Black, Queen) != BlackQueen {
		t.Error("MakePiece produced wrong piece")
	}
	if MakePiece(White, PieceTypeNone) != PieceNone {
		t.Error("MakePiece(None) should be PieceNone")
	}
	for _, p := range []Piece{WhitePawn, WhiteKnight, WhiteBishop, WhiteRook, WhiteQueen, WhiteKing, BlackPawn, BlackKnight, BlackBishop, BlackRook, BlackQueen, BlackKing} {
		if p.Color() != White && p.Color() != Black {
			t.Errorf("piece %d has wrong color %d", p, p.Color())
		}
	}
	if PieceNone.Color() != ColorNone {
		t.Error("PieceNone.Color() should be ColorNone")
	}
	if PieceNone.Type() != PieceTypeNone {
		t.Error("PieceNone.Type() should be PieceTypeNone")
	}
}

func TestPieceCharacter(t *testing.T) {
	cases := map[Piece]byte{
		WhitePawn: 'P', WhiteKnight: 'N', WhiteBishop: 'B', WhiteRook: 'R', WhiteQueen: 'Q', WhiteKing: 'K',
		BlackPawn: 'p', BlackKnight: 'n', BlackBishop: 'b', BlackRook: 'r', BlackQueen: 'q', BlackKing: 'k',
		PieceNone: '.',
	}
	for p, want := range cases {
		if p.Character() != want {
			t.Errorf("Piece %d Character() = %c, want %c", p, p.Character(), want)
		}
	}
}

func TestSquareMakeFileRank(t *testing.T) {
	// a1 = 0, h1 = 7, a8 = 56, h8 = 63.
	if MakeSquare(FileA, Rank1) != A1 {
		t.Error("MakeSquare(a1) != A1")
	}
	if MakeSquare(FileH, Rank1) != H1 {
		t.Error("MakeSquare(h1) != H1")
	}
	if MakeSquare(FileA, Rank8) != A8 {
		t.Error("MakeSquare(a8) != A8")
	}
	if MakeSquare(FileH, Rank8) != H8 {
		t.Error("MakeSquare(h8) != H8")
	}
	for sq := Square(0); sq < SquareNone; sq++ {
		if got := MakeSquare(sq.File(), sq.Rank()); got != sq {
			t.Errorf("MakeSquare(file,rank) roundtrip failed at %d", sq)
		}
	}
	if A1.File() != FileA || A1.Rank() != Rank1 {
		t.Error("A1.File()/Rank() wrong")
	}
	if H8.File() != FileH || H8.Rank() != Rank8 {
		t.Error("H8.File()/Rank() wrong")
	}
}

func TestSquareStringAndParse(t *testing.T) {
	if A1.String() != "a1" || H8.String() != "h8" || E4.String() != "e4" {
		t.Errorf("Square.String() mismatch: %s %s %s", A1.String(), H8.String(), E4.String())
	}
	if SquareNone.String() != "-" {
		t.Errorf("SquareNone.String() = %q, want -", SquareNone.String())
	}
	for _, tc := range []struct {
		s    string
		want Square
	}{
		{"a1", A1}, {"h8", H8}, {"e4", E4}, {"d7", D7},
	} {
		got, err := ParseSquare(tc.s)
		if err != nil {
			t.Errorf("ParseSquare(%s) err %v", tc.s, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSquare(%s) = %s, want %s", tc.s, got, tc.want)
		}
	}
	// Out-of-range and malformed squares error.
	for _, bad := range []string{"a9", "i1", "aa", "a", "z0"} {
		if _, err := ParseSquare(bad); err == nil {
			t.Errorf("ParseSquare(%q) expected error", bad)
		}
	}
}

func TestCastlingRights(t *testing.T) {
	if CastlingAll != 15 {
		t.Errorf("CastlingAll = %d, want 15", CastlingAll)
	}
	cr := CastlingWhiteKing | CastlingBlackQueen
	if cr&CastlingWhiteKing == 0 || cr&CastlingBlackQueen == 0 {
		t.Error("combined rights missing expected bits")
	}
	if cr&CastlingWhiteQueen != 0 {
		t.Error("unexpected white queen bit")
	}
}

func TestMoveEncoding(t *testing.T) {
	m := MakeMove(E2, E4, FlagDoublePawnPush)
	if m.From() != E2 || m.To() != E4 || m.Flags() != FlagDoublePawnPush {
		t.Errorf("Move roundtrip failed: from=%s to=%s flags=%d", m.From(), m.To(), m.Flags())
	}
	// Full 64-bit range of from/to/flags is preserved.
	for _, tc := range []struct{ from, to Square; flags uint16 }{
		{A1, H8, FlagQueenPromoCap}, {H8, A1, FlagQuiet}, {E2, E4, FlagDoublePawnPush},
		{G1, F3, FlagQuiet}, {E1, G1, FlagKingCastle},
	} {
		mv := MakeMove(tc.from, tc.to, tc.flags)
		if mv.From() != tc.from || mv.To() != tc.to || mv.Flags() != tc.flags {
			t.Errorf("MakeMove(%v,%v,%d) roundtrip failed", tc.from, tc.to, tc.flags)
		}
	}
}

func TestMoveFlags(t *testing.T) {
	if !MakeMove(E2, E4, FlagDoublePawnPush).IsCapture() {
		// double push is not a capture
	}
	if MakeMove(E2, E4, FlagDoublePawnPush).IsCapture() {
		t.Error("double pawn push should not be a capture")
	}
	if !MakeMove(E2, E3, FlagQuiet).IsCapture() == false {
		t.Error("quiet should not be capture")
	}
	if !MakeMove(E7, E8, FlagQueenPromo).IsPromotion() {
		t.Error("queen promo should be promotion")
	}
	if MakeMove(E2, E3, FlagQuiet).IsPromotion() {
		t.Error("quiet should not be promotion")
	}
	if !MakeMove(E2, E3, FlagQuiet).IsPromotion() == false {
	}
	if !MakeMove(E1, G1, FlagKingCastle).IsCastle() {
		t.Error("king castle flag should detect castle")
	}
	if !MakeMove(E1, C1, FlagQueenCastle).IsCastle() {
		t.Error("queen castle flag should detect castle")
	}
	if MakeMove(E2, E3, FlagQuiet).IsCastle() {
		t.Error("quiet should not be castle")
	}
	if !MakeMove(H7, E8, FlagEnPassant).IsEnPassant() {
		t.Error("en passant flag should detect ep")
	}
	if MakeMove(H7, E8, FlagQuiet).IsEnPassant() {
		t.Error("quiet should not be en passant")
	}
}

func TestMovePromotionType(t *testing.T) {
	cases := map[uint16]PieceType{
		FlagKnightPromo: Knight, FlagBishopPromo: Bishop, FlagRookPromo: Rook, FlagQueenPromo: Queen,
		FlagKnightPromoCap: Knight, FlagBishopPromoCap: Bishop, FlagRookPromoCap: Rook, FlagQueenPromoCap: Queen,
	}
	for flags, want := range cases {
		if MakeMove(7, 0, flags).PromotionType() != want {
			t.Errorf("flags %d promo type = %v, want %v", flags, MakeMove(7, 0, flags).PromotionType(), want)
		}
	}
	if MakeMove(E2, E3, FlagQuiet).PromotionType() != PieceTypeNone {
		t.Error("non-promotion should have PieceTypeNone")
	}
}

func TestMoveString(t *testing.T) {
	if MakeMove(E2, E4, FlagDoublePawnPush).String() != "e2e4" {
		t.Errorf("e2e4 string = %s", MakeMove(E2, E4, FlagDoublePawnPush).String())
	}
	if MakeMove(H7, E8, FlagQueenPromo).String() != "h7e8q" {
		t.Errorf("promo string = %s", MakeMove(H7, E8, FlagQueenPromo).String())
	}
	if MakeMove(7, 0, FlagKnightPromo).String() != "h1a1n" {
		t.Errorf("knight promo string = %s", MakeMove(7, 0, FlagKnightPromo).String())
	}
	if MoveNone.String() != "0000" {
		t.Errorf("MoveNone string = %s, want 0000", MoveNone.String())
	}
}
