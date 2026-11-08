package types

import "fmt"

type Move uint16

const (
	MoveNone Move = 0

	FlagQuiet          uint16 = 0
	FlagDoublePawnPush uint16 = 1
	FlagKingCastle     uint16 = 2
	FlagQueenCastle    uint16 = 3
	FlagCapture        uint16 = 4
	FlagEnPassant      uint16 = 5
	FlagKnightPromo    uint16 = 8
	FlagBishopPromo    uint16 = 9
	FlagRookPromo      uint16 = 10
	FlagQueenPromo     uint16 = 11
	FlagKnightPromoCap uint16 = 12
	FlagBishopPromoCap uint16 = 13
	FlagRookPromoCap   uint16 = 14
	FlagQueenPromoCap  uint16 = 15
)

func MakeMove(from, to Square, flags uint16) Move {
	return Move(uint16(from) | (uint16(to) << 6) | (flags << 12))
}

func (m Move) From() Square {
	return Square(m & 0x3F)
}

func (m Move) To() Square {
	return Square((m >> 6) & 0x3F)
}

func (m Move) Flags() uint16 {
	return uint16(m >> 12)
}

func (m Move) IsCapture() bool {
	return (m.Flags() & 0x4) != 0
}

func (m Move) IsPromotion() bool {
	return (m.Flags() & 0x8) != 0
}

func (m Move) IsEnPassant() bool {
	return m.Flags() == FlagEnPassant
}

func (m Move) IsCastle() bool {
	f := m.Flags()
	return f == FlagKingCastle || f == FlagQueenCastle
}

func (m Move) PromotionType() PieceType {
	if !m.IsPromotion() {
		return PieceTypeNone
	}
	switch m.Flags() & 0x3 {
	case 0:
		return Knight
	case 1:
		return Bishop
	case 2:
		return Rook
	case 3:
		return Queen
	default:
		return PieceTypeNone
	}
}

func (m Move) String() string {
	if m == MoveNone {
		return "0000"
	}
	res := fmt.Sprintf("%s%s", m.From().String(), m.To().String())
	if m.IsPromotion() {
		switch m.PromotionType() {
		case Knight:
			res += "n"
		case Bishop:
			res += "b"
		case Rook:
			res += "r"
		case Queen:
			res += "q"
		}
	}
	return res
}
