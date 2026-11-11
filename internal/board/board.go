package board

import (
	"fmt"
	"strings"

	"devilfish/internal/bitboard"
	"devilfish/internal/types"
)

var CastlingMask [64]types.CastlingRights

func init() {
	for i := 0; i < 64; i++ {
		CastlingMask[i] = 15
	}
	CastlingMask[types.A1] = ^types.CastlingWhiteQueen & 15
	CastlingMask[types.E1] = ^(types.CastlingWhiteKing | types.CastlingWhiteQueen) & 15
	CastlingMask[types.H1] = ^types.CastlingWhiteKing & 15

	CastlingMask[types.A8] = ^types.CastlingBlackQueen & 15
	CastlingMask[types.E8] = ^(types.CastlingBlackKing | types.CastlingBlackQueen) & 15
	CastlingMask[types.H8] = ^types.CastlingBlackKing & 15
}

type UndoState struct {
	Move          types.Move
	Captured      types.Piece
	Castling      types.CastlingRights
	EPSquare      types.Square
	HalfmoveClock uint8
	Hash          uint64
	PawnHash      uint64
}

type Board struct {
	Pieces   [64]types.Piece
	PieceBB  [15]bitboard.Bitboard
	ColorBB  [2]bitboard.Bitboard

	SideToMove     types.Color
	Castling       types.CastlingRights
	EPSquare       types.Square
	HalfmoveClock  uint8
	FullmoveNumber uint16
	Hash           uint64
	PawnHash       uint64

	UndoStack []UndoState
}

func NewBoard() *Board {
	b := &Board{
		EPSquare:  types.SquareNone,
		UndoStack: make([]UndoState, 0, 256),
	}
	return b
}

func (b *Board) Occupied() bitboard.Bitboard {
	return b.ColorBB[types.White] | b.ColorBB[types.Black]
}

func (b *Board) OccupiedBy(c types.Color) bitboard.Bitboard {
	return b.ColorBB[c]
}

func (b *Board) PieceAt(sq types.Square) types.Piece {
	return b.Pieces[sq]
}

// Attackers returns a bitboard of all pieces (both colors) attacking the given square with occupied bitboard occ.
func (b *Board) Attackers(sq types.Square, occ bitboard.Bitboard) bitboard.Bitboard {
	return (bitboard.PawnAttacks(types.White, sq) & b.PieceBB[types.BlackPawn]) |
		(bitboard.PawnAttacks(types.Black, sq) & b.PieceBB[types.WhitePawn]) |
		(bitboard.KnightAttacks(sq) & (b.PieceBB[types.WhiteKnight] | b.PieceBB[types.BlackKnight])) |
		(bitboard.BishopAttacks(sq, occ) & (b.PieceBB[types.WhiteBishop] | b.PieceBB[types.BlackBishop] | b.PieceBB[types.WhiteQueen] | b.PieceBB[types.BlackQueen])) |
		(bitboard.RookAttacks(sq, occ) & (b.PieceBB[types.WhiteRook] | b.PieceBB[types.BlackRook] | b.PieceBB[types.WhiteQueen] | b.PieceBB[types.BlackQueen])) |
		(bitboard.KingAttacks(sq) & (b.PieceBB[types.WhiteKing] | b.PieceBB[types.BlackKing]))
}

// TacticalThreatsOnPieces returns a bitboard of friendly pieces (Knight, Bishop, Rook, Queen)
// that are under tactical threat:
// 1. Lower-value enemy pieces attacking higher-value friendly pieces:
//    - Enemy Pawns attacking friendly Minors, Rooks, Queens
//    - Enemy Knights/Bishops attacking friendly Rooks, Queens
//    - Enemy Rooks attacking friendly Queens
// 2. Enemy pieces attacking undefended friendly pieces (hanging piece / en prise).
// 3. Enemy pieces attacking friendly pieces of equal/higher value where enemy attackers outnumber friendly defenders.
func (b *Board) TacticalThreatsOnPieces(us types.Color) bitboard.Bitboard {
	them := us.Other()
	themPawns := b.PieceBB[types.MakePiece(them, types.Pawn)]
	themKnights := b.PieceBB[types.MakePiece(them, types.Knight)]
	themBishops := b.PieceBB[types.MakePiece(them, types.Bishop)]
	themRooks := b.PieceBB[types.MakePiece(them, types.Rook)]
	themQueens := b.PieceBB[types.MakePiece(them, types.Queen)]

	ourKnights := b.PieceBB[types.MakePiece(us, types.Knight)]
	ourBishops := b.PieceBB[types.MakePiece(us, types.Bishop)]
	ourRooks := b.PieceBB[types.MakePiece(us, types.Rook)]
	ourQueens := b.PieceBB[types.MakePiece(us, types.Queen)]
	ourMinors := ourKnights | ourBishops
	ourPieces := ourMinors | ourRooks | ourQueens

	var threats bitboard.Bitboard

	// 1. Pawn threats on minors, rooks, queens
	if themPawns != 0 {
		pawnAttacks := bitboard.PawnAttacksBB(them, themPawns)
		threats |= ourPieces & pawnAttacks
	}

	occ := b.Occupied()

	// 2. Minor piece threats on rooks and queens
	if (themKnights|themBishops) != 0 && (ourRooks|ourQueens) != 0 {
		tk := themKnights
		for tk != 0 {
			sq := bitboard.PopLSB(&tk)
			threats |= (ourRooks | ourQueens) & bitboard.KnightAttacks(sq)
		}
		tb := themBishops
		for tb != 0 {
			sq := bitboard.PopLSB(&tb)
			threats |= (ourRooks | ourQueens) & bitboard.BishopAttacks(sq, occ)
		}
	}

	// 3. Rook threats on queens
	if themRooks != 0 && ourQueens != 0 {
		tr := themRooks
		for tr != 0 {
			sq := bitboard.PopLSB(&tr)
			threats |= ourQueens & bitboard.RookAttacks(sq, occ)
		}
	}

	// 3b. Queen threats on queens
	if themQueens != 0 && ourQueens != 0 {
		tq := themQueens
		for tq != 0 {
			sq := bitboard.PopLSB(&tq)
			threats |= ourQueens & bitboard.QueenAttacks(sq, occ)
		}
	}

	// 4. Undefended friendly pieces attacked by ANY enemy piece (hanging / en prise),
	// or where enemy attackers outnumber friendly defenders.
	unflagged := ourPieces &^ threats
	for unflagged != 0 {
		sq := bitboard.PopLSB(&unflagged)
		if !b.IsSquareAttacked(sq, them) {
			continue
		}
		if !b.IsSquareAttacked(sq, us) {
			threats |= bitboard.SquareBB[sq]
			continue
		}
		allAtt := b.Attackers(sq, occ)
		themAtt := allAtt & b.ColorBB[them]
		usDef := allAtt & b.ColorBB[us]
		if bitboard.Count(themAtt) > bitboard.Count(usDef) {
			threats |= bitboard.SquareBB[sq]
		}
	}

	return threats
}

// PawnThreatsOnPieces returns a bitboard of friendly pieces attacked by enemy pieces (aliased to TacticalThreatsOnPieces).
func (b *Board) PawnThreatsOnPieces(us types.Color) bitboard.Bitboard {
	return b.TacticalThreatsOnPieces(us)
}

// KingAttackersCount returns the number of enemy pieces attacking the King zone of color `us`.
func (b *Board) KingAttackersCount(us types.Color) (int, int32) {
	kSq := b.KingSquare(us)
	if kSq >= types.SquareNone {
		return 0, 0
	}
	them := us.Other()
	kZone := bitboard.KingAttacks(kSq) | bitboard.SquareBB[kSq]
	occ := b.Occupied()

	attackersCount := 0
	attackUnits := int32(0)

	// Queens
	tq := b.PieceBB[types.MakePiece(them, types.Queen)]
	for tq != 0 {
		sq := bitboard.PopLSB(&tq)
		att := bitboard.QueenAttacks(sq, occ) & kZone
		if att != 0 {
			attackersCount++
			attackUnits += 4 * int32(bitboard.Count(att))
		}
	}

	// Rooks
	tr := b.PieceBB[types.MakePiece(them, types.Rook)]
	for tr != 0 {
		sq := bitboard.PopLSB(&tr)
		att := bitboard.RookAttacks(sq, occ) & kZone
		if att != 0 {
			attackersCount++
			attackUnits += 3 * int32(bitboard.Count(att))
		}
	}

	// Bishops
	tb := b.PieceBB[types.MakePiece(them, types.Bishop)]
	for tb != 0 {
		sq := bitboard.PopLSB(&tb)
		att := bitboard.BishopAttacks(sq, occ) & kZone
		if att != 0 {
			attackersCount++
			attackUnits += 2 * int32(bitboard.Count(att))
		}
	}

	// Knights
	tk := b.PieceBB[types.MakePiece(them, types.Knight)]
	for tk != 0 {
		sq := bitboard.PopLSB(&tk)
		att := bitboard.KnightAttacks(sq) & kZone
		if att != 0 {
			attackersCount++
			attackUnits += 2 * int32(bitboard.Count(att))
		}
	}

	return attackersCount, attackUnits
}

func (b *Board) KingSquare(c types.Color) types.Square {
	kPiece := types.MakePiece(c, types.King)
	bb := b.PieceBB[kPiece]
	if bb == 0 {
		return types.SquareNone
	}
	return bitboard.LSB(bb)
}

func (b *Board) PutPiece(p types.Piece, sq types.Square) {
	if p == types.PieceNone || p.Color() >= types.ColorNone || sq >= types.SquareNone {
		return
	}
	b.Pieces[sq] = p
	bb := bitboard.SquareBB[sq]
	b.PieceBB[p] |= bb
	b.ColorBB[p.Color()] |= bb
	b.Hash ^= ZobristPiece[p][sq]
	if p.Type() == types.Pawn {
		b.PawnHash ^= ZobristPiece[p][sq]
	}
}

func (b *Board) RemovePiece(sq types.Square) types.Piece {
	if sq >= types.SquareNone {
		return types.PieceNone
	}
	p := b.Pieces[sq]
	if p == types.PieceNone || p.Color() >= types.ColorNone {
		return types.PieceNone
	}
	b.Pieces[sq] = types.PieceNone
	bb := bitboard.SquareBB[sq]
	b.PieceBB[p] &= ^bb
	b.ColorBB[p.Color()] &= ^bb
	b.Hash ^= ZobristPiece[p][sq]
	if p.Type() == types.Pawn {
		b.PawnHash ^= ZobristPiece[p][sq]
	}
	return p
}

func (b *Board) MovePiece(p types.Piece, from, to types.Square) {
	if p == types.PieceNone || p.Color() >= types.ColorNone || from >= types.SquareNone || to >= types.SquareNone {
		return
	}
	fromBB := bitboard.SquareBB[from]
	toBB := bitboard.SquareBB[to]
	fromToBB := fromBB | toBB

	b.Pieces[from] = types.PieceNone
	b.Pieces[to] = p

	b.PieceBB[p] ^= fromToBB
	b.ColorBB[p.Color()] ^= fromToBB

	b.Hash ^= ZobristPiece[p][from] ^ ZobristPiece[p][to]
	if p.Type() == types.Pawn {
		b.PawnHash ^= ZobristPiece[p][from] ^ ZobristPiece[p][to]
	}
}

func (b *Board) IsSquareAttacked(sq types.Square, byColor types.Color) bool {
	if sq >= types.SquareNone {
		return false
	}
	// Pawn attacks
	them := byColor
	us := them.Other()
	pawn := types.MakePiece(them, types.Pawn)
	if bitboard.PawnAttacks(us, sq)&b.PieceBB[pawn] != 0 {
		return true
	}

	// Knight attacks
	knight := types.MakePiece(them, types.Knight)
	if bitboard.KnightAttacks(sq)&b.PieceBB[knight] != 0 {
		return true
	}

	// King attacks
	king := types.MakePiece(them, types.King)
	if bitboard.KingAttacks(sq)&b.PieceBB[king] != 0 {
		return true
	}

	occ := b.Occupied()

	// Bishop and Queen diagonal attacks
	bishop := types.MakePiece(them, types.Bishop)
	queen := types.MakePiece(them, types.Queen)
	diagPieces := b.PieceBB[bishop] | b.PieceBB[queen]
	if diagPieces != 0 && (bitboard.BishopAttacks(sq, occ)&diagPieces != 0) {
		return true
	}

	// Rook and Queen straight attacks
	rook := types.MakePiece(them, types.Rook)
	straightPieces := b.PieceBB[rook] | b.PieceBB[queen]
	if straightPieces != 0 && (bitboard.RookAttacks(sq, occ)&straightPieces != 0) {
		return true
	}

	return false
}

func (b *Board) IsInCheck(c types.Color) bool {
	kSq := b.KingSquare(c)
	if kSq >= types.SquareNone {
		return false
	}
	return b.IsSquareAttacked(kSq, c.Other())
}

// Checkers returns the bitboard of enemy pieces directly giving check to king of color c.
func (b *Board) Checkers(c types.Color) bitboard.Bitboard {
	kSq := b.KingSquare(c)
	if kSq >= types.SquareNone {
		return 0
	}
	them := c.Other()
	occ := b.Occupied()
	return (bitboard.PawnAttacks(c, kSq) & b.PieceBB[types.MakePiece(them, types.Pawn)]) |
		(bitboard.KnightAttacks(kSq) & b.PieceBB[types.MakePiece(them, types.Knight)]) |
		(bitboard.BishopAttacks(kSq, occ) & (b.PieceBB[types.MakePiece(them, types.Bishop)] | b.PieceBB[types.MakePiece(them, types.Queen)])) |
		(bitboard.RookAttacks(kSq, occ) & (b.PieceBB[types.MakePiece(them, types.Rook)] | b.PieceBB[types.MakePiece(them, types.Queen)]))
}

// GivesCheck reports whether pseudo-legal move m, played by the side to move,
// attacks the enemy king. It works from attack tables without making the move.
// It does not test whether m is legal (i.e. whether it leaves our own king in
// check); callers find that out when they make the move.
func (b *Board) GivesCheck(m types.Move) bool {
	from := m.From()
	to := m.To()
	if m == types.MoveNone || from >= types.SquareNone || to >= types.SquareNone {
		return false
	}
	us := b.SideToMove
	them := us.Other()
	moving := b.Pieces[from]
	if moving == types.PieceNone || moving.Color() != us {
		return false
	}
	kSq := b.KingSquare(them)
	if kSq >= types.SquareNone {
		return false
	}
	kBB := bitboard.SquareBB[kSq]
	fromBB := bitboard.SquareBB[from]
	toBB := bitboard.SquareBB[to]

	// Occupancy after the move.
	occ := (b.Occupied() &^ fromBB) | toBB
	special := false
	if m.IsEnPassant() {
		if us == types.White {
			if to < 8 {
				return false
			}
			occ &^= bitboard.SquareBB[to-8]
		} else {
			if to >= 56 {
				return false
			}
			occ &^= bitboard.SquareBB[to+8]
		}
		special = true
	}

	ourDiag := b.PieceBB[types.MakePiece(us, types.Bishop)] | b.PieceBB[types.MakePiece(us, types.Queen)]
	ourStraight := b.PieceBB[types.MakePiece(us, types.Rook)] | b.PieceBB[types.MakePiece(us, types.Queen)]

	if m.IsCastle() {
		var rFrom, rTo types.Square
		if m.Flags() == types.FlagKingCastle {
			if us == types.White {
				rFrom, rTo = types.H1, types.F1
			} else {
				rFrom, rTo = types.H8, types.F8
			}
		} else {
			if us == types.White {
				rFrom, rTo = types.A1, types.D1
			} else {
				rFrom, rTo = types.A8, types.D8
			}
		}
		occ = (occ &^ bitboard.SquareBB[rFrom]) | bitboard.SquareBB[rTo]
		ourStraight = (ourStraight &^ bitboard.SquareBB[rFrom]) | bitboard.SquareBB[rTo]
		special = true
	} else {
		// Direct check from the destination square.
		pt := moving.Type()
		if m.IsPromotion() {
			pt = m.PromotionType()
		}
		switch pt {
		case types.Pawn:
			if bitboard.PawnAttacks(us, to)&kBB != 0 {
				return true
			}
		case types.Knight:
			if bitboard.KnightAttacks(to)&kBB != 0 {
				return true
			}
		case types.Bishop:
			if bitboard.BishopAttacks(to, occ)&kBB != 0 {
				return true
			}
		case types.Rook:
			if bitboard.RookAttacks(to, occ)&kBB != 0 {
				return true
			}
		case types.Queen:
			if bitboard.QueenAttacks(to, occ)&kBB != 0 {
				return true
			}
		}
	}

	// Discovered check: one of our sliders now sees the king. Only possible if
	// the vacated square is on a line with the king, or occupancy changed in
	// more than the usual two squares (en passant, castling).
	if !special && bitboard.LineTable[from][kSq] == 0 {
		return false
	}
	ourDiag &^= fromBB
	ourStraight &^= fromBB
	if ourDiag != 0 && bitboard.BishopAttacks(kSq, occ)&ourDiag != 0 {
		return true
	}
	if ourStraight != 0 && bitboard.RookAttacks(kSq, occ)&ourStraight != 0 {
		return true
	}
	return false
}

// GivesCheckSlow is the reference implementation of GivesCheck: it makes the
// move and looks. It returns false for illegal moves. Kept for tests.
func (b *Board) GivesCheckSlow(m types.Move) bool {
	if !b.MakeMove(m) {
		return false
	}
	check := b.IsInCheck(b.SideToMove)
	b.UnmakeMove(m)
	return check
}

func (b *Board) CalculateHash() uint64 {
	var hash uint64
	for sq := 0; sq < 64; sq++ {
		p := b.Pieces[sq]
		if p != types.PieceNone {
			hash ^= ZobristPiece[p][sq]
		}
	}
	hash ^= ZobristCastling[b.Castling]
	if b.EPSquare < types.SquareNone {
		hash ^= ZobristEP[b.EPSquare.File()]
	}
	if b.SideToMove == types.Black {
		hash ^= ZobristSide
	}
	return hash
}

func (b *Board) CalculatePawnHash() uint64 {
	var hash uint64
	for sq := 0; sq < 64; sq++ {
		p := b.Pieces[sq]
		if p != types.PieceNone && p.Type() == types.Pawn {
			hash ^= ZobristPiece[p][sq]
		}
	}
	return hash
}

func (b *Board) Clone() *Board {
	newB := *b
	newB.UndoStack = make([]UndoState, len(b.UndoStack), len(b.UndoStack)+64)
	copy(newB.UndoStack, b.UndoStack)
	return &newB
}

// IsRepetition returns true if the current position has appeared at least once before
// in the game history within the halfmove clock limit (2-fold repetition / cycle).
// In tree search, any repeated position along the search line is a cycle and treated as a draw.
func (b *Board) IsRepetition() bool {
	currHash := b.Hash
	// Search back until the last reversible boundary (or up to halfmove clock)
	limit := len(b.UndoStack) - int(b.HalfmoveClock)
	if limit < 0 {
		limit = 0
	}
	for i := len(b.UndoStack) - 2; i >= limit; i -= 2 {
		if b.UndoStack[i].Hash == currHash {
			return true
		}
	}
	return false
}

// IsThreefoldRepetition returns true if the current position has appeared at least twice before
// in the game history (3-fold repetition), satisfying the FIDE draw claim rule.
func (b *Board) IsThreefoldRepetition() bool {
	count := 0
	currHash := b.Hash
	limit := len(b.UndoStack) - int(b.HalfmoveClock)
	if limit < 0 {
		limit = 0
	}
	for i := len(b.UndoStack) - 2; i >= limit; i -= 2 {
		if b.UndoStack[i].Hash == currHash {
			count++
			if count >= 2 {
				return true
			}
		}
	}
	return false
}

func (b *Board) IsFiftyMoveDraw() bool {
	return b.HalfmoveClock >= 100
}

func (b *Board) IsInsufficientMaterial() bool {
	// If any pawns, rooks, or queens exist, not insufficient
	if (b.PieceBB[types.WhitePawn] | b.PieceBB[types.BlackPawn] |
		b.PieceBB[types.WhiteRook] | b.PieceBB[types.BlackRook] |
		b.PieceBB[types.WhiteQueen] | b.PieceBB[types.BlackQueen]) != 0 {
		return false
	}

	wKnights := bitboard.Count(b.PieceBB[types.WhiteKnight])
	bKnights := bitboard.Count(b.PieceBB[types.BlackKnight])
	wBishops := bitboard.Count(b.PieceBB[types.WhiteBishop])
	bBishops := bitboard.Count(b.PieceBB[types.BlackBishop])

	// King vs King
	if wKnights == 0 && bKnights == 0 && wBishops == 0 && bBishops == 0 {
		return true
	}

	// King and Minor vs King
	if (wKnights+wBishops <= 1 && bKnights+bBishops == 0) ||
		(bKnights+bBishops <= 1 && wKnights+wBishops == 0) {
		return true
	}

	// King and Bishop vs King and Bishop on same color
	if wKnights == 0 && bKnights == 0 && wBishops == 1 && bBishops == 1 {
		wBishopSq := bitboard.LSB(b.PieceBB[types.WhiteBishop])
		bBishopSq := bitboard.LSB(b.PieceBB[types.BlackBishop])
		wIsDark := (int(wBishopSq.Rank())+int(wBishopSq.File()))%2 == 0
		bIsDark := (int(bBishopSq.Rank())+int(bBishopSq.File()))%2 == 0
		if wIsDark == bIsDark {
			return true
		}
	}

	return false
}

func (b *Board) String() string {
	var sb strings.Builder
	sb.WriteString("\n +---+---+---+---+---+---+---+---+\n")
	for r := 7; r >= 0; r-- {
		sb.WriteString(fmt.Sprintf("%d|", r+1))
		for f := 0; f < 8; f++ {
			sq := types.MakeSquare(types.File(f), types.Rank(r))
			p := b.Pieces[sq]
			sb.WriteString(fmt.Sprintf(" %c |", p.Character()))
		}
		sb.WriteString("\n +---+---+---+---+---+---+---+---+\n")
	}
	sb.WriteString("   a   b   c   d   e   f   g   h\n\n")
	sb.WriteString(fmt.Sprintf("Side to move: %s\n", b.SideToMove))
	sb.WriteString(fmt.Sprintf("Castling: %04b\n", b.Castling))
	sb.WriteString(fmt.Sprintf("En Passant: %s\n", b.EPSquare))
	sb.WriteString(fmt.Sprintf("Halfmove: %d, Fullmove: %d\n", b.HalfmoveClock, b.FullmoveNumber))
	sb.WriteString(fmt.Sprintf("Zobrist Hash: 0x%016x\n", b.Hash))
	return sb.String()
}
