package search

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/eval"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// Stockfish-style move ordering (P.MoveOrder 1).
//
// The staged picker in picker.go sorts moves into fixed tiers: killers, the
// counter-move, then quiet moves ranked by hand-written classes (escapes a
// pawn threat, defends the king, gives check, ...) with history breaking
// ties inside a class. This one follows Stockfish's MovePicker instead:
//
//   - Captures are ranked by the value of the piece taken plus capture
//     history. A capture is tried early only if its exchange does not lose
//     more than a margin that shrinks as that rank grows; the exchange is
//     evaluated when the move comes up, not for every capture up front.
//   - Quiet moves get one score: the history tables added together, a bonus
//     for a safe check, a bonus for moving a piece out of an attack by a
//     lesser piece and a penalty for moving it into one, and near the root
//     the low-ply history. There are no killer or counter-move stages; the
//     continuation history carries that information.
//   - Only quiets above a limit that falls with depth are sorted. Quiets
//     with a clearly bad score are held back until after the losing
//     captures.
//   - In check, captures come first by victim value, then the other moves by
//     main and continuation history.

type sfStage int

const (
	sfInit sfStage = iota
	sfGoodCaptures
	sfGenQuiets
	sfGoodQuiets
	sfBadCaptures
	sfBadQuiets
	sfEvasionCaptures
	sfEvasionQuiets
	sfDone
)

// sfPieceValue is Stockfish's piece value, by piece type.
var sfPieceValue = [7]int32{types.Pawn: 208, types.Knight: 781, types.Bishop: 825, types.Rook: 1276, types.Queen: 2538}

const (
	sfGoodQuietThreshold = -14000
	sfSortLimitPerDepth  = -3560
	sfCheckBonus         = 16384
)

// scoreCapturesSF generates the captures and ranks them.
func (mp *MovePicker) scoreCapturesSF(evasion bool) {
	b := mp.board
	movegen.GenerateMovesByType(b, movegen.GenCaptures, &mp.captures)
	for i := 0; i < mp.captures.Count; i++ {
		m := mp.captures.Moves[i]
		attacker := b.Pieces[m.From()].Type()
		victim := b.Pieces[m.To()].Type()
		if m.IsEnPassant() {
			victim = types.Pawn
		}
		if evasion {
			mp.capScores[i] = sfPieceValue[victim]
			continue
		}
		mp.capScores[i] = mp.history.CaptureHistory[attacker][m.To()][victim] + 7*sfPieceValue[victim]
	}
}

// scoreQuietsSF generates the quiet moves and scores them.
func (mp *MovePicker) scoreQuietsSF(evasion bool) {
	b := mp.board
	movegen.GenerateMovesByType(b, movegen.GenQuiets, &mp.quiets)
	us := b.SideToMove
	them := us.Other()
	hc := mp.hc
	if hc == nil {
		c := mp.history.Context(b, -1, 0)
		hc = &c
	}
	if evasion {
		for i := 0; i < mp.quiets.Count; i++ {
			m := mp.quiets.Moves[i]
			mp.quietScores[i] = hc.Evasion(b.Pieces[m.From()], m)
		}
		return
	}

	occ := b.Occupied()

	// Squares from which each piece type checks the enemy king directly.
	var checkSq [7]bitboard.Bitboard
	if ksq := b.KingSquare(them); ksq < types.SquareNone {
		checkSq[types.Pawn] = bitboard.PawnAttacks(them, ksq)
		checkSq[types.Knight] = bitboard.KnightAttacks(ksq)
		checkSq[types.Bishop] = bitboard.BishopAttacks(ksq, occ)
		checkSq[types.Rook] = bitboard.RookAttacks(ksq, occ)
		checkSq[types.Queen] = checkSq[types.Bishop] | checkSq[types.Rook]
	}

	// Squares attacked by an enemy piece worth less than each piece type.
	var byLesser [7]bitboard.Bitboard
	pawnAtt := hc.pawnAttacked
	minorAtt := pawnAtt
	for n := b.PieceBB[types.MakePiece(them, types.Knight)]; n != 0; {
		minorAtt |= bitboard.KnightAttacks(bitboard.PopLSB(&n))
	}
	for n := b.PieceBB[types.MakePiece(them, types.Bishop)]; n != 0; {
		minorAtt |= bitboard.BishopAttacks(bitboard.PopLSB(&n), occ)
	}
	rookAtt := minorAtt
	for n := b.PieceBB[types.MakePiece(them, types.Rook)]; n != 0; {
		rookAtt |= bitboard.RookAttacks(bitboard.PopLSB(&n), occ)
	}
	byLesser[types.Knight], byLesser[types.Bishop] = pawnAtt, pawnAtt
	byLesser[types.Rook] = minorAtt
	byLesser[types.Queen] = rookAtt

	for i := 0; i < mp.quiets.Count; i++ {
		m := mp.quiets.Moves[i]
		from, to := m.From(), m.To()
		piece := b.Pieces[from]
		pt := piece.Type()
		v := hc.OrderSF(piece, m, mp.ply)

		if checkSq[pt]&bitboard.SquareBB[to] != 0 && eval.SEE(b, m) >= -75 {
			v += sfCheckBonus
		}
		if t := byLesser[pt]; t != 0 {
			if t&bitboard.SquareBB[from] != 0 {
				v += 20 * sfPieceValue[pt]
			}
			if t&bitboard.SquareBB[to] != 0 {
				v -= 20 * sfPieceValue[pt]
			}
		}
		mp.quietScores[i] = v
	}
}

// partialSortQuiets sorts, best first, the quiet moves scoring at least
// limit to the front of the list. The order of the rest is unspecified.
func (mp *MovePicker) partialSortQuiets(limit int32) {
	moves, scores := &mp.quiets.Moves, &mp.quietScores
	sortedEnd := 0
	for p := 1; p < mp.quiets.Count; p++ {
		if scores[p] < limit {
			continue
		}
		m, sc := moves[p], scores[p]
		sortedEnd++
		moves[p], scores[p] = moves[sortedEnd], scores[sortedEnd]
		q := sortedEnd
		for ; q > 0 && scores[q-1] < sc; q-- {
			moves[q], scores[q] = moves[q-1], scores[q-1]
		}
		moves[q], scores[q] = m, sc
	}
}

func (mp *MovePicker) nextSF() types.Move {
	b := mp.board
	for {
		switch mp.sfStage {
		case sfInit:
			if b.Checkers(b.SideToMove) != 0 {
				mp.scoreCapturesSF(true)
				mp.sfStage = sfEvasionCaptures
				continue
			}
			mp.scoreCapturesSF(false)
			mp.sfStage = sfGoodCaptures

		case sfGoodCaptures:
			for mp.capIndex < mp.captures.Count {
				m := mp.pickCapture(mp.capIndex)
				score := mp.capScores[mp.capIndex]
				mp.capIndex++
				if m == mp.ttMove || m == mp.excludedMove {
					continue
				}
				see := eval.SEE(b, m)
				if see >= -score/18 {
					mp.lastSEE, mp.lastSEEKnown = see, true
					return m
				}
				if mp.badCapCount < len(mp.badCaps) {
					mp.badCaps[mp.badCapCount] = m
					mp.badCapSEE[mp.badCapCount] = see
					mp.badCapCount++
				}
			}
			mp.sfStage = sfGenQuiets

		case sfGenQuiets:
			mp.scoreQuietsSF(false)
			mp.partialSortQuiets(sfSortLimitPerDepth * int32(mp.depth))
			mp.sfStage = sfGoodQuiets

		case sfGoodQuiets:
			for mp.quietIndex < mp.quiets.Count {
				m := mp.quiets.Moves[mp.quietIndex]
				score := mp.quietScores[mp.quietIndex]
				mp.quietIndex++
				if m == mp.ttMove || m == mp.excludedMove {
					continue
				}
				if score > sfGoodQuietThreshold {
					return m
				}
				// Held back: the slots before quietIndex are free to reuse.
				mp.quiets.Moves[mp.badQuietCount] = m
				mp.badQuietCount++
			}
			mp.sfStage = sfBadCaptures

		case sfBadCaptures:
			if mp.badCapIndex < mp.badCapCount {
				m := mp.badCaps[mp.badCapIndex]
				mp.lastSEE, mp.lastSEEKnown = mp.badCapSEE[mp.badCapIndex], true
				mp.badCapIndex++
				return m
			}
			mp.sfStage = sfBadQuiets

		case sfBadQuiets:
			if mp.badQuietIndex < mp.badQuietCount {
				m := mp.quiets.Moves[mp.badQuietIndex]
				mp.badQuietIndex++
				return m
			}
			mp.sfStage = sfDone
			return types.MoveNone

		case sfEvasionCaptures:
			for mp.capIndex < mp.captures.Count {
				m := mp.pickCapture(mp.capIndex)
				mp.capIndex++
				if m == mp.ttMove || m == mp.excludedMove {
					continue
				}
				return m
			}
			mp.scoreQuietsSF(true)
			sortQuiets(&mp.quiets, mp.quietScores[:mp.quiets.Count])
			mp.sfStage = sfEvasionQuiets

		case sfEvasionQuiets:
			for mp.quietIndex < mp.quiets.Count {
				m := mp.quiets.Moves[mp.quietIndex]
				mp.quietIndex++
				if m == mp.ttMove || m == mp.excludedMove {
					continue
				}
				return m
			}
			mp.sfStage = sfDone
			return types.MoveNone

		default:
			return types.MoveNone
		}
	}
}
