package board

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/types"
)

// CheckInfo holds what a node needs to answer, for any of its moves, "does
// it give check?" and "can it leave my own king in check?" without looking
// at the board again for each move (Stockfish's check info). It describes
// the position it was made from and is stale after any move.
type CheckInfo struct {
	KingSq types.Square // the opponent's king
	// CheckSq[pt]: squares from which a piece of type pt checks that king.
	CheckSq [7]bitboard.Bitboard
	// Blockers: the side to move's pieces standing alone between one of its
	// own sliders and the opponent's king; moving one off the line is a
	// discovered check.
	Blockers bitboard.Bitboard
	// Pinned: the side to move's pieces standing alone between its own king
	// and an enemy slider.
	Pinned bitboard.Bitboard
}

// soleBlockers returns the pieces that stand alone between ksq and a slider
// of colour by that would otherwise attack it.
func (b *Board) soleBlockers(ksq types.Square, by types.Color) bitboard.Bitboard {
	queens := b.PieceBB[types.MakePiece(by, types.Queen)]
	snipers := bitboard.RookAttacks(ksq, 0)&(b.PieceBB[types.MakePiece(by, types.Rook)]|queens) |
		bitboard.BishopAttacks(ksq, 0)&(b.PieceBB[types.MakePiece(by, types.Bishop)]|queens)
	occ := b.Occupied()
	var out bitboard.Bitboard
	for snipers != 0 {
		between := bitboard.BetweenTable[ksq][bitboard.PopLSB(&snipers)] & occ
		if between != 0 && between&(between-1) == 0 {
			out |= between
		}
	}
	return out
}

// NewCheckInfo computes the check information of the current position for
// the side to move.
func (b *Board) NewCheckInfo() CheckInfo {
	us := b.SideToMove
	them := us.Other()
	ci := CheckInfo{KingSq: b.KingSquare(them)}
	ours := b.ColorBB[us]
	if ksq := ci.KingSq; ksq < types.SquareNone {
		occ := b.Occupied()
		ci.CheckSq[types.Pawn] = bitboard.PawnAttacks(them, ksq)
		ci.CheckSq[types.Knight] = bitboard.KnightAttacks(ksq)
		ci.CheckSq[types.Bishop] = bitboard.BishopAttacks(ksq, occ)
		ci.CheckSq[types.Rook] = bitboard.RookAttacks(ksq, occ)
		ci.CheckSq[types.Queen] = ci.CheckSq[types.Bishop] | ci.CheckSq[types.Rook]
		ci.Blockers = b.soleBlockers(ksq, us) & ours
	}
	if ksq := b.KingSquare(us); ksq < types.SquareNone {
		ci.Pinned = b.soleBlockers(ksq, them) & ours
	}
	return ci
}

// GivesCheckWith is GivesCheck using check information computed once for
// the position. Promotions, en passant and castling, which change more than
// one square's worth of the board, go through GivesCheck.
func (b *Board) GivesCheckWith(m types.Move, ci *CheckInfo) bool {
	if m.IsPromotion() || m.IsEnPassant() || m.IsCastle() {
		return b.GivesCheck(m)
	}
	from, to := m.From(), m.To()
	toBB := bitboard.SquareBB[to]
	if ci.CheckSq[b.Pieces[from].Type()]&toBB != 0 {
		return true
	}
	return ci.Blockers&bitboard.SquareBB[from] != 0 && bitboard.LineTable[from][ci.KingSq]&toBB == 0
}

// SkipsKingTest reports that the pseudo-legal move m cannot leave the
// mover's king in check, so MakeMoveUnchecked may play it: the mover is not
// in check now, and m moves a piece that is neither the king nor pinned,
// and is not an en-passant capture (which takes two pawns off one rank).
func (ci *CheckInfo) SkipsKingTest(b *Board, m types.Move, inCheck bool) bool {
	if inCheck || m.IsEnPassant() {
		return false
	}
	from := m.From()
	return ci.Pinned&bitboard.SquareBB[from] == 0 && b.Pieces[from].Type() != types.King
}
