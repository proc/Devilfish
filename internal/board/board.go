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

func (b *Board) KingSquare(c types.Color) types.Square {
	kPiece := types.MakePiece(c, types.King)
	bb := b.PieceBB[kPiece]
	if bb == 0 {
		return types.SquareNone
	}
	return bitboard.LSB(bb)
}

func (b *Board) PutPiece(p types.Piece, sq types.Square) {
	b.Pieces[sq] = p
	bb := bitboard.SquareBB[sq]
	b.PieceBB[p] |= bb
	b.ColorBB[p.Color()] |= bb
	b.Hash ^= ZobristPiece[p][sq]
}

func (b *Board) RemovePiece(sq types.Square) types.Piece {
	p := b.Pieces[sq]
	if p == types.PieceNone {
		return types.PieceNone
	}
	b.Pieces[sq] = types.PieceNone
	bb := bitboard.SquareBB[sq]
	b.PieceBB[p] &= ^bb
	b.ColorBB[p.Color()] &= ^bb
	b.Hash ^= ZobristPiece[p][sq]
	return p
}

func (b *Board) MovePiece(p types.Piece, from, to types.Square) {
	fromBB := bitboard.SquareBB[from]
	toBB := bitboard.SquareBB[to]
	fromToBB := fromBB | toBB

	b.Pieces[from] = types.PieceNone
	b.Pieces[to] = p

	b.PieceBB[p] ^= fromToBB
	b.ColorBB[p.Color()] ^= fromToBB

	b.Hash ^= ZobristPiece[p][from] ^ ZobristPiece[p][to]
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

func (b *Board) Clone() *Board {
	newB := *b
	newB.UndoStack = make([]UndoState, len(b.UndoStack), len(b.UndoStack)+64)
	copy(newB.UndoStack, b.UndoStack)
	return &newB
}

func (b *Board) IsRepetition() bool {
	count := 0
	currHash := b.Hash
	// Search back until the last reversible boundary (or up to halfmove clock)
	limit := len(b.UndoStack) - int(b.HalfmoveClock)
	if limit < 0 {
		limit = 0
	}
	for i := len(b.UndoStack) - 1; i >= limit; i-- {
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
