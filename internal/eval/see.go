package eval

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

func SEE(b *board.Board, m types.Move) int32 {
	from := m.From()
	to := m.To()
	piece := b.Pieces[from]
	target := b.Pieces[to]

	if m.IsEnPassant() {
		target = types.MakePiece(b.SideToMove.Other(), types.Pawn)
	}

	var gain [32]int32
	d := 0

	gain[d] = SEEPieceValues[target.Type()]
	if m.IsPromotion() {
		gain[d] += SEEPieceValues[m.PromotionType()] - SEEPieceValues[types.Pawn]
	}

	fromBB := bitboard.SquareBB[from]
	occ := b.Occupied() ^ fromBB

	side := b.SideToMove.Other()
	attadef := getAttackersTo(b, to, occ)

	currPieceType := piece.Type()
	if m.IsPromotion() {
		currPieceType = m.PromotionType()
	}

	for {
		d++
		gain[d] = SEEPieceValues[currPieceType] - gain[d-1]

		if max32(-gain[d-1], gain[d]) < 0 {
			break
		}

		attadef &= occ
		myAttackers := attadef & b.OccupiedBy(side)
		if myAttackers == 0 {
			break
		}

		// Find least valuable attacker
		nextAttackerSq, nextType := findLVA(b, myAttackers, side)
		if nextAttackerSq >= types.SquareNone {
			break
		}

		currPieceType = nextType
		occ ^= bitboard.SquareBB[nextAttackerSq]

		// Add x-ray attackers if sliding piece moved
		if nextType == types.Pawn || nextType == types.Bishop || nextType == types.Rook || nextType == types.Queen {
			attadef |= getXRayAttackers(b, to, occ)
		}

		side = side.Other()
	}

	for d > 1 {
		d--
		gain[d-1] = -max32(-gain[d-1], gain[d])
	}

	return gain[0]
}

func getAttackersTo(b *board.Board, sq types.Square, occ bitboard.Bitboard) bitboard.Bitboard {
	return (bitboard.PawnAttacks(types.White, sq) & b.PieceBB[types.BlackPawn]) |
		(bitboard.PawnAttacks(types.Black, sq) & b.PieceBB[types.WhitePawn]) |
		(bitboard.KnightAttacks(sq) & (b.PieceBB[types.WhiteKnight] | b.PieceBB[types.BlackKnight])) |
		(bitboard.BishopAttacks(sq, occ) & (b.PieceBB[types.WhiteBishop] | b.PieceBB[types.BlackBishop] | b.PieceBB[types.WhiteQueen] | b.PieceBB[types.BlackQueen])) |
		(bitboard.RookAttacks(sq, occ) & (b.PieceBB[types.WhiteRook] | b.PieceBB[types.BlackRook] | b.PieceBB[types.WhiteQueen] | b.PieceBB[types.BlackQueen])) |
		(bitboard.KingAttacks(sq) & (b.PieceBB[types.WhiteKing] | b.PieceBB[types.BlackKing]))
}

func getXRayAttackers(b *board.Board, sq types.Square, occ bitboard.Bitboard) bitboard.Bitboard {
	return (bitboard.BishopAttacks(sq, occ) & (b.PieceBB[types.WhiteBishop] | b.PieceBB[types.BlackBishop] | b.PieceBB[types.WhiteQueen] | b.PieceBB[types.BlackQueen])) |
		(bitboard.RookAttacks(sq, occ) & (b.PieceBB[types.WhiteRook] | b.PieceBB[types.BlackRook] | b.PieceBB[types.WhiteQueen] | b.PieceBB[types.BlackQueen]))
}

func findLVA(b *board.Board, attackers bitboard.Bitboard, side types.Color) (types.Square, types.PieceType) {
	for pt := types.Pawn; pt <= types.King; pt++ {
		p := types.MakePiece(side, pt)
		bb := attackers & b.PieceBB[p]
		if bb != 0 {
			return bitboard.LSB(bb), pt
		}
	}
	return types.SquareNone, types.PieceTypeNone
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
