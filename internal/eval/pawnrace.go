package eval

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

// UnstoppablePasserBonus is the score for having a passed pawn the enemy king
// cannot catch (rule of the square) in a king-and-pawn ending. It is reduced
// by UnstoppablePasserPerMove for each move the pawn still needs to queen.
const (
	UnstoppablePasserBonus   int32 = 550
	UnstoppablePasserPerMove int32 = 25
	noRace                         = 99
)

// IsPawnEnding reports whether only kings and pawns remain.
func IsPawnEnding(b *board.Board) bool {
	pieces := b.Occupied() &^ (b.PieceBB[types.WhitePawn] | b.PieceBB[types.BlackPawn] |
		b.PieceBB[types.WhiteKing] | b.PieceBB[types.BlackKing])
	return pieces == 0
}

// PawnRaceBonus applies the rule of the square in king-and-pawn endings.
// The network judges pawn races poorly; this adds a large bonus for a passed
// pawn the defending king cannot reach in time. When both sides have one,
// the bonus goes only to a side that queens at least two moves earlier,
// since otherwise the race comes down to checks and captures the rule
// can't see. Returns a score relative to the side to move, 0 if not a pawn
// ending or no side has an unstoppable pawn.
func PawnRaceBonus(b *board.Board) int32 {
	if !IsPawnEnding(b) {
		return 0
	}
	stm := b.SideToMove
	us := fastestUnstoppable(b, stm)
	them := fastestUnstoppable(b, stm.Other())

	switch {
	case us == noRace && them == noRace:
		return 0
	case them == noRace:
		return UnstoppablePasserBonus - UnstoppablePasserPerMove*int32(us)
	case us == noRace:
		return -(UnstoppablePasserBonus - UnstoppablePasserPerMove*int32(them))
	}
	// Both queen. The side to move gets there first on equal counts; only
	// call it when one side is clearly ahead.
	if us+2 <= them+1 {
		return UnstoppablePasserBonus - UnstoppablePasserPerMove*int32(us)
	}
	if them+2 <= us {
		return -(UnstoppablePasserBonus - UnstoppablePasserPerMove*int32(them))
	}
	return 0
}

// fastestUnstoppable returns how many moves c's quickest unstoppable passed
// pawn needs to queen, or noRace if c has none.
func fastestUnstoppable(b *board.Board, c types.Color) int {
	them := c.Other()
	ourPawns := b.PieceBB[types.MakePiece(c, types.Pawn)]
	theirPawns := b.PieceBB[types.MakePiece(them, types.Pawn)]
	ourKing := b.KingSquare(c)
	theirKing := b.KingSquare(them)
	if ourKing >= types.SquareNone || theirKing >= types.SquareNone {
		return noRace
	}

	best := noRace
	for pawns := ourPawns; pawns != 0; {
		sq := bitboard.PopLSB(&pawns)
		file, rank := int(sq.File()), int(sq.Rank())

		// Squares ahead of the pawn on its own file and the two adjacent ones
		var front, path bitboard.Bitboard
		for r := rank + 1; r <= 7 && c == types.White; r++ {
			path |= bitboard.SquareBB[types.MakeSquare(types.File(file), types.Rank(r))]
		}
		for r := rank - 1; r >= 0 && c == types.Black; r-- {
			path |= bitboard.SquareBB[types.MakeSquare(types.File(file), types.Rank(r))]
		}
		front = path | bitboard.ShiftEast(path) | bitboard.ShiftWest(path)
		if front&theirPawns != 0 {
			continue // not passed
		}
		if path&bitboard.SquareBB[theirKing] != 0 {
			continue // king already blocks it
		}

		moves := 7 - rank
		promoRank := 7
		if c == types.Black {
			moves = rank
			promoRank = 0
		}
		if (c == types.White && rank == 1) || (c == types.Black && rank == 6) {
			moves-- // double push
		}
		if path&(bitboard.SquareBB[ourKing]|ourPawns) != 0 {
			moves++ // our own king or pawn must step aside first
		}

		promo := types.MakeSquare(types.File(file), types.Rank(promoRank))
		kingMoves := chebyshev(theirKing, promo)
		if b.SideToMove == them {
			kingMoves-- // defender moves first
		}
		if moves < kingMoves && moves < best {
			best = moves
		}
	}
	return best
}

func chebyshev(a, b types.Square) int {
	df := int(a.File()) - int(b.File())
	dr := int(a.Rank()) - int(b.Rank())
	if df < 0 {
		df = -df
	}
	if dr < 0 {
		dr = -dr
	}
	if df > dr {
		return df
	}
	return dr
}
