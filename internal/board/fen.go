package board

import (
	"fmt"
	"strconv"
	"strings"

	"devilfish/internal/types"
)

const StartFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

func ParseFEN(fen string) (*Board, error) {
	b := NewBoard()
	parts := strings.Fields(fen)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid FEN string: empty")
	}

	// 1. Piece placement
	rows := strings.Split(parts[0], "/")
	if len(rows) != 8 {
		return nil, fmt.Errorf("invalid piece placement in FEN: %s", parts[0])
	}

	for r := 7; r >= 0; r-- {
		row := rows[7-r]
		f := 0
		for i := 0; i < len(row); i++ {
			c := row[i]
			if c >= '1' && c <= '8' {
				f += int(c - '0')
			} else {
				if f > 7 {
					return nil, fmt.Errorf("row %d has too many squares", r+1)
				}
				sq := types.MakeSquare(types.File(f), types.Rank(r))
				var p types.Piece
				switch c {
				case 'P':
					p = types.WhitePawn
				case 'N':
					p = types.WhiteKnight
				case 'B':
					p = types.WhiteBishop
				case 'R':
					p = types.WhiteRook
				case 'Q':
					p = types.WhiteQueen
				case 'K':
					p = types.WhiteKing
				case 'p':
					p = types.BlackPawn
				case 'n':
					p = types.BlackKnight
				case 'b':
					p = types.BlackBishop
				case 'r':
					p = types.BlackRook
				case 'q':
					p = types.BlackQueen
				case 'k':
					p = types.BlackKing
				default:
					return nil, fmt.Errorf("invalid piece character: %c", c)
				}
				b.PutPiece(p, sq)
				f++
			}
		}
		if f != 8 {
			return nil, fmt.Errorf("row %d does not contain 8 squares", r+1)
		}
	}

	// 2. Active color
	b.SideToMove = types.White
	if len(parts) > 1 {
		if parts[1] == "b" {
			b.SideToMove = types.Black
		}
	}

	// 3. Castling rights
	b.Castling = 0
	if len(parts) > 2 {
		for i := 0; i < len(parts[2]); i++ {
			switch parts[2][i] {
			case 'K':
				b.Castling |= types.CastlingWhiteKing
			case 'Q':
				b.Castling |= types.CastlingWhiteQueen
			case 'k':
				b.Castling |= types.CastlingBlackKing
			case 'q':
				b.Castling |= types.CastlingBlackQueen
			case '-':
				// no castling rights
			}
		}
	}

	// 4. En passant target square
	b.EPSquare = types.SquareNone
	if len(parts) > 3 && parts[3] != "-" {
		sq, err := types.ParseSquare(parts[3])
		if err == nil {
			b.EPSquare = sq
		}
	}

	// 5. Halfmove clock
	b.HalfmoveClock = 0
	if len(parts) > 4 {
		hm, err := strconv.Atoi(parts[4])
		if err == nil && hm >= 0 {
			b.HalfmoveClock = uint8(hm)
		}
	}

	// 6. Fullmove number
	b.FullmoveNumber = 1
	if len(parts) > 5 {
		fm, err := strconv.Atoi(parts[5])
		if err == nil && fm > 0 {
			b.FullmoveNumber = uint16(fm)
		}
	}

	b.Hash = b.CalculateHash()
	return b, nil
}

func (b *Board) ToFEN() string {
	var sb strings.Builder

	// 1. Piece placement
	for r := 7; r >= 0; r-- {
		emptyCount := 0
		for f := 0; f < 8; f++ {
			sq := types.MakeSquare(types.File(f), types.Rank(r))
			p := b.Pieces[sq]
			if p == types.PieceNone {
				emptyCount++
			} else {
				if emptyCount > 0 {
					sb.WriteString(strconv.Itoa(emptyCount))
					emptyCount = 0
				}
				sb.WriteByte(p.Character())
			}
		}
		if emptyCount > 0 {
			sb.WriteString(strconv.Itoa(emptyCount))
		}
		if r > 0 {
			sb.WriteByte('/')
		}
	}

	// 2. Active color
	sb.WriteByte(' ')
	if b.SideToMove == types.White {
		sb.WriteByte('w')
	} else {
		sb.WriteByte('b')
	}

	// 3. Castling rights
	sb.WriteByte(' ')
	castlingStr := ""
	if b.Castling&types.CastlingWhiteKing != 0 {
		castlingStr += "K"
	}
	if b.Castling&types.CastlingWhiteQueen != 0 {
		castlingStr += "Q"
	}
	if b.Castling&types.CastlingBlackKing != 0 {
		castlingStr += "k"
	}
	if b.Castling&types.CastlingBlackQueen != 0 {
		castlingStr += "q"
	}
	if castlingStr == "" {
		castlingStr = "-"
	}
	sb.WriteString(castlingStr)

	// 4. En passant target square
	sb.WriteByte(' ')
	if b.EPSquare < types.SquareNone {
		sb.WriteString(b.EPSquare.String())
	} else {
		sb.WriteByte('-')
	}

	// 5. Halfmove clock
	sb.WriteByte(' ')
	sb.WriteString(strconv.Itoa(int(b.HalfmoveClock)))

	// 6. Fullmove number
	sb.WriteByte(' ')
	sb.WriteString(strconv.Itoa(int(b.FullmoveNumber)))

	return sb.String()
}
