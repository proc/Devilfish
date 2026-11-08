package types

import "fmt"

type Color uint8

const (
	White Color = 0
	Black Color = 1
	ColorNone Color = 2
)

func (c Color) Other() Color {
	return c ^ 1
}

func (c Color) String() string {
	switch c {
	case White:
		return "white"
	case Black:
		return "black"
	default:
		return "none"
	}
}

type PieceType uint8

const (
	PieceTypeNone PieceType = 0
	Pawn          PieceType = 1
	Knight        PieceType = 2
	Bishop        PieceType = 3
	Rook          PieceType = 4
	Queen         PieceType = 5
	King          PieceType = 6
)

func (pt PieceType) String() string {
	switch pt {
	case Pawn:
		return "P"
	case Knight:
		return "N"
	case Bishop:
		return "B"
	case Rook:
		return "R"
	case Queen:
		return "Q"
	case King:
		return "K"
	default:
		return " "
	}
}

type Piece uint8

const (
	PieceNone   Piece = 0
	WhitePawn   Piece = 1
	WhiteKnight Piece = 2
	WhiteBishop Piece = 3
	WhiteRook   Piece = 4
	WhiteQueen  Piece = 5
	WhiteKing   Piece = 6

	BlackPawn   Piece = 9
	BlackKnight Piece = 10
	BlackBishop Piece = 11
	BlackRook   Piece = 12
	BlackQueen  Piece = 13
	BlackKing   Piece = 14
)

func MakePiece(c Color, pt PieceType) Piece {
	if pt == PieceTypeNone {
		return PieceNone
	}
	return Piece((uint8(c) << 3) | uint8(pt))
}

func (p Piece) Color() Color {
	if p == PieceNone {
		return ColorNone
	}
	return Color(p >> 3)
}

func (p Piece) Type() PieceType {
	return PieceType(p & 7)
}

func (p Piece) Character() byte {
	switch p {
	case WhitePawn:
		return 'P'
	case WhiteKnight:
		return 'N'
	case WhiteBishop:
		return 'B'
	case WhiteRook:
		return 'R'
	case WhiteQueen:
		return 'Q'
	case WhiteKing:
		return 'K'
	case BlackPawn:
		return 'p'
	case BlackKnight:
		return 'n'
	case BlackBishop:
		return 'b'
	case BlackRook:
		return 'r'
	case BlackQueen:
		return 'q'
	case BlackKing:
		return 'k'
	default:
		return '.'
	}
}

type Square uint8

const (
	A1 Square = iota; B1; C1; D1; E1; F1; G1; H1
	A2; B2; C2; D2; E2; F2; G2; H2
	A3; B3; C3; D3; E3; F3; G3; H3
	A4; B4; C4; D4; E4; F4; G4; H4
	A5; B5; C5; D5; E5; F5; G5; H5
	A6; B6; C6; D6; E6; F6; G6; H6
	A7; B7; C7; D7; E7; F7; G7; H7
	A8; B8; C8; D8; E8; F8; G8; H8
	SquareNone Square = 64
)

func MakeSquare(file File, rank Rank) Square {
	return Square(uint8(rank)*8 + uint8(file))
}

func (s Square) File() File {
	return File(s % 8)
}

func (s Square) Rank() Rank {
	return Rank(s / 8)
}

func (s Square) String() string {
	if s >= SquareNone {
		return "-"
	}
	return fmt.Sprintf("%c%c", 'a'+s.File(), '1'+s.Rank())
}

func ParseSquare(s string) (Square, error) {
	if len(s) != 2 {
		return SquareNone, fmt.Errorf("invalid square string: %s", s)
	}
	f := s[0] - 'a'
	r := s[1] - '1'
	if f > 7 || r > 7 {
		return SquareNone, fmt.Errorf("out of bounds square: %s", s)
	}
	return MakeSquare(File(f), Rank(r)), nil
}

type File uint8

const (
	FileA File = iota
	FileB
	FileC
	FileD
	FileE
	FileF
	FileG
	FileH
)

type Rank uint8

const (
	Rank1 Rank = iota
	Rank2
	Rank3
	Rank4
	Rank5
	Rank6
	Rank7
	Rank8
)

type CastlingRights uint8

const (
	CastlingWhiteKing  CastlingRights = 1 << 0
	CastlingWhiteQueen CastlingRights = 1 << 1
	CastlingBlackKing  CastlingRights = 1 << 2
	CastlingBlackQueen CastlingRights = 1 << 3
	CastlingAll        CastlingRights = 15
)
