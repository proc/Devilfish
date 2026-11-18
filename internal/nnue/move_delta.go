package nnue

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

// Threat changes worked out from the move itself.
//
// The general update (threatDelta) compares two snapshots of the board: it
// finds the squares that differ, every slider with a ray through one of
// them, and then lists the threats touching those squares in both positions.
// When the stored accumulator is the position one move earlier, the same
// answer follows from the move alone, as Stockfish computes it: a move is a
// few pieces taken off and put on squares, and each of those changes exactly
//
//   - the threats the piece makes from that square,
//   - the threats other pieces make against it there, and
//   - for each slider aimed at the square, the one threat on the first piece
//     behind it, which the piece on the square hides.
//
// That is about half the attack lookups, and it needs no search for which
// squares changed.
//
// The move is played backwards on a copy of the current position (the last
// move and what it captured are on the board's undo stack), so the copy ends
// as the earlier position. Threats that exist before a backward step and not
// after it are present now and were absent then: rows to add. The reverse
// are rows to remove. A threat that comes and goes between two steps is
// listed once on each side, which cancels in the sum.

// moveDelta switches the method on. Tests turn it off to compare.
var moveDelta = true

// moveDeltaVerify makes Prepare check that the position reached by playing
// the move backwards is the stored one. Set by tests.
var moveDeltaVerify = false

var bothViews = [2]bool{true, true}

// aimedAt returns the pieces attacking sq in x, and the rook-line and
// bishop-line attack sets from sq that found the sliders among them. The
// piece on sq, if any, does not affect either.
func aimedAt(x *eightSnap, sq types.Square) (att, diagAtk, orthAtk bitboard.Bitboard) {
	bb := &x.bb
	diagAtk = bitboard.BishopAttacks(sq, x.occ)
	orthAtk = bitboard.RookAttacks(sq, x.occ)
	att = bitboard.PawnAttacksTable[types.Black][sq]&bb[types.WhitePawn] |
		bitboard.PawnAttacksTable[types.White][sq]&bb[types.BlackPawn] |
		bitboard.KnightAttacksTable[sq]&(bb[types.WhiteKnight]|bb[types.BlackKnight]) |
		bitboard.KingAttacksTable[sq]&(bb[types.WhiteKing]|bb[types.BlackKing]) |
		diagAtk&(bb[types.WhiteBishop]|bb[types.BlackBishop]|bb[types.WhiteQueen]|bb[types.BlackQueen]) |
		orthAtk&(bb[types.WhiteRook]|bb[types.BlackRook]|bb[types.WhiteQueen]|bb[types.BlackQueen])
	return
}

// emitBehind lists, for each slider in att aimed at sq, its threat on the
// first piece on the far side of sq: the one a piece on sq hides. Along the
// slider's line the attacks from sq end at the slider itself on one side
// and at that piece on the other.
func emitBehind(tt *threatTables, x *eightSnap, sq int, att, diagAtk, orthAtk bitboard.Bitboard, views *[2]eightView, out *[2]rowList) {
	bb := &x.bb
	sliders := att & (bb[types.WhiteBishop] | bb[types.BlackBishop] | bb[types.WhiteRook] | bb[types.BlackRook] |
		bb[types.WhiteQueen] | bb[types.BlackQueen])
	ends := (diagAtk | orthAtk) & x.occ
	for sliders != 0 {
		a := bitboard.PopLSB(&sliders)
		behind := ends & bitboard.LineTable[a][sq] &^ bitboard.SquareBB[a]
		emitFrom(tt, x, pieceCode(x.pieces[a]), int(a), uint64(behind), views, bothViews, out)
	}
}

// emitAround lists the threats the piece on sq makes and those made
// against it by att.
func emitAround(tt *threatTables, x *eightSnap, sq int, att bitboard.Bitboard, views *[2]eightView, out *[2]rowList) {
	pc := pieceCode(x.pieces[sq])
	occ := uint64(x.occ)
	emitFrom(tt, x, pc, sq, attacksWithOcc(pc, sq, occ)&occ, views, bothViews, out)
	emitTo(tt, x, pc, sq, uint64(att), views, bothViews, out)
}

func (x *eightSnap) set(sq int, p types.Piece) {
	bit := bitboard.SquareBB[sq]
	if old := x.pieces[sq]; old != types.PieceNone {
		x.bb[old] &^= bit
	}
	x.pieces[sq] = p
	if p != types.PieceNone {
		x.bb[p] |= bit
		x.occ |= bit
	} else {
		x.occ &^= bit
	}
}

// takeOff removes the piece on sq from x. now receives the rows of the
// threats that go with it; then those of the threats uncovered behind it.
func takeOff(tt *threatTables, x *eightSnap, sq int, views *[2]eightView, now, then *[2]rowList) {
	att, diagAtk, orthAtk := aimedAt(x, types.Square(sq))
	emitAround(tt, x, sq, att, views, now)
	x.set(sq, types.PieceNone)
	emitBehind(tt, x, sq, att, diagAtk, orthAtk, views, then)
}

// putOn places p on the empty square sq of x. now receives the rows of the
// threats it hides from the sliders aimed at the square; then those of the
// threats that come with it.
func putOn(tt *threatTables, x *eightSnap, p types.Piece, sq int, views *[2]eightView, now, then *[2]rowList) {
	att, diagAtk, orthAtk := aimedAt(x, types.Square(sq))
	emitBehind(tt, x, sq, att, diagAtk, orthAtk, views, now)
	x.set(sq, p)
	emitAround(tt, x, sq, att, views, then)
}

// swapOn replaces the piece on sq by p. The square stays occupied, so no
// slider sees further or less far: only the threats by and against the
// piece on it change.
func swapOn(tt *threatTables, x *eightSnap, p types.Piece, sq int, views *[2]eightView, now, then *[2]rowList) {
	att, _, _ := aimedAt(x, types.Square(sq))
	emitAround(tt, x, sq, att, views, now)
	x.set(sq, p)
	emitAround(tt, x, sq, att, views, then)
}

// stepBack takes back the move recorded in u on x, which holds the position
// after it, lists the threat rows that differ (add for those of the later
// position, rem for those of the earlier one), and returns the bitboard of
// squares whose occupancy or piece changed.
func stepBack(tt *threatTables, x *eightSnap, u *board.UndoState, views *[2]eightView, add, rem *[2]rowList) bitboard.Bitboard {
	m := u.Move
	if m == types.MoveNone {
		return 0 // a null move changes no piece
	}
	from, to := int(m.From()), int(m.To())
	d := bitboard.SquareBB[from] | bitboard.SquareBB[to]
	moved := x.pieces[to]
	us := moved.Color()
	if m.IsCastle() {
		rFrom, rTo := int(types.H1), int(types.F1)
		if m.Flags() == types.FlagQueenCastle {
			rFrom, rTo = int(types.A1), int(types.D1)
		}
		if us == types.Black {
			rFrom, rTo = rFrom^56, rTo^56
		}
		rook := x.pieces[rTo]
		takeOff(tt, x, to, views, add, rem)
		takeOff(tt, x, rTo, views, add, rem)
		putOn(tt, x, moved, from, views, add, rem)
		putOn(tt, x, rook, rFrom, views, add, rem)
		return d | bitboard.SquareBB[rFrom] | bitboard.SquareBB[rTo]
	}
	switch {
	case u.Captured == types.PieceNone:
		takeOff(tt, x, to, views, add, rem)
	case m.IsEnPassant():
		takeOff(tt, x, to, views, add, rem)
		capSq := to - 8
		if us == types.Black {
			capSq = to + 8
		}
		putOn(tt, x, u.Captured, capSq, views, add, rem)
		d |= bitboard.SquareBB[capSq]
	default:
		// A capture: the captured piece goes back where the capturer stands.
		swapOn(tt, x, u.Captured, to, views, add, rem)
	}
	if m.IsPromotion() {
		moved = types.MakePiece(us, types.Pawn)
	}
	putOn(tt, x, moved, from, views, add, rem)
	return d
}
