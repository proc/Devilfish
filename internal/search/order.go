package search

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/eval"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

var PieceMVV = [7]int32{
	0, // None
	1, // Pawn
	2, // Knight
	3, // Bishop
	4, // Rook
	5, // Queen
	6, // King
}

const (
	ScoreTTMove            int32 = 2_000_000_000
	ScoreQueenPromotion    int32 = 150_000_000
	ScoreGoodCapture       int32 = 100_000_000
	ScorePawnThreatEvasion int32 = 80_000_000
	ScoreKingDefense       int32 = 75_000_000
	ScoreKiller1           int32 = 50_000_000
	ScoreKiller2           int32 = 45_000_000
	ScoreCounterMove       int32 = 40_000_000
	ScoreQuietCheck        int32 = 30_000_000
	ScoreThreatMove        int32 = 28_000_000
	ScoreKingThreat        int32 = 27_000_000
	ScorePassedPawnPush    int32 = 25_000_000
	ScoreBadCapture        int32 = -50_000_000
)

// IsTacticalThreat returns true if a quiet move attacks a higher-value enemy piece
// (e.g., Pawn attacks Minor/Rook/Queen, Knight/Bishop attacks Rook/Queen, Rook attacks Queen).
func IsTacticalThreat(b *board.Board, m types.Move) bool {
	us := b.SideToMove
	them := us.Other()
	fromSq := m.From()
	toSq := m.To()
	attacker := b.Pieces[fromSq].Type()
	if attacker == types.King {
		return false
	}

	themQueen := b.PieceBB[types.MakePiece(them, types.Queen)]
	themRook := b.PieceBB[types.MakePiece(them, types.Rook)]
	themMinor := b.PieceBB[types.MakePiece(them, types.Knight)] | b.PieceBB[types.MakePiece(them, types.Bishop)]

	occ := b.Occupied()
	occ = (occ & ^(bitboard.Bitboard(1) << fromSq)) | (bitboard.Bitboard(1) << toSq)

	switch attacker {
	case types.Pawn:
		pawnAttacks := bitboard.PawnAttacks(us, toSq)
		return (pawnAttacks & (themMinor | themRook | themQueen)) != 0
	case types.Knight:
		knightAttacks := bitboard.KnightAttacks(toSq)
		return (knightAttacks & (themRook | themQueen)) != 0
	case types.Bishop:
		if (themRook | themQueen) != 0 {
			return (bitboard.BishopAttacks(toSq, occ) & (themRook | themQueen)) != 0
		}
	case types.Rook:
		if (themQueen | themRook | themMinor) != 0 {
			return (bitboard.RookAttacks(toSq, occ) & (themQueen | themRook | themMinor)) != 0
		}
	case types.Queen:
		if (themQueen | themRook | themMinor) != 0 {
			return (bitboard.QueenAttacks(toSq, occ) & (themQueen | themRook | themMinor)) != 0
		}
	}
	return false
}

// IsKingAttack returns true if a quiet piece move aims attacks into the enemy king ring.
func IsKingAttack(b *board.Board, m types.Move) bool {
	them := b.SideToMove.Other()
	kSq := b.KingSquare(them)
	if kSq >= types.SquareNone {
		return false
	}
	kingRing := bitboard.KingAttacks(kSq)
	fromSq := m.From()
	toSq := m.To()
	attacker := b.Pieces[fromSq].Type()
	if attacker == types.King || attacker == types.Pawn {
		return false
	}

	occ := b.Occupied()
	occ = (occ & ^(bitboard.Bitboard(1) << fromSq)) | (bitboard.Bitboard(1) << toSq)

	switch attacker {
	case types.Knight:
		return (bitboard.KnightAttacks(toSq) & (kingRing | bitboard.KnightAttacks(kSq))) != 0
	case types.Bishop:
		return (bitboard.BishopAttacks(toSq, occ) & (kingRing | bitboard.BishopAttacks(kSq, occ))) != 0
	case types.Rook:
		return (bitboard.RookAttacks(toSq, occ) & (kingRing | bitboard.RookAttacks(kSq, occ))) != 0
	case types.Queen:
		return (bitboard.QueenAttacks(toSq, occ) & (kingRing | bitboard.QueenAttacks(kSq, occ))) != 0
	}
	return false
}

// IsKingThreatAfterMove checks if the move just played (m) attacks or aims at our King.
// Called when m has ALREADY been made on b, so b.SideToMove is the defending side.
func IsKingThreatAfterMove(b *board.Board, m types.Move) bool {
	if m == types.MoveNone {
		return false
	}
	us := b.SideToMove
	kSq := b.KingSquare(us)
	if kSq >= types.SquareNone {
		return false
	}
	toSq := m.To()
	if toSq >= types.SquareNone {
		return false
	}
	attacker := b.Pieces[toSq].Type()
	if attacker == types.King || attacker == types.Pawn || attacker == types.PieceTypeNone {
		return false
	}
	kingRing := bitboard.KingAttacks(kSq)
	if (bitboard.SquareBB[toSq] & kingRing) != 0 {
		return true
	}
	occ := b.Occupied()
	switch attacker {
	case types.Knight:
		return (bitboard.KnightAttacks(toSq) & (kingRing | bitboard.KnightAttacks(kSq))) != 0
	case types.Bishop:
		return (bitboard.BishopAttacks(toSq, occ) & (kingRing | bitboard.BishopAttacks(kSq, occ))) != 0
	case types.Rook:
		return (bitboard.RookAttacks(toSq, occ) & (kingRing | bitboard.RookAttacks(kSq, occ))) != 0
	case types.Queen:
		return (bitboard.QueenAttacks(toSq, occ) & (kingRing | bitboard.QueenAttacks(kSq, occ))) != 0
	}
	return false
}

// IsKingDefenseMove returns true if a quiet move moves the King or reinforces the King zone
func IsKingDefenseMove(b *board.Board, m types.Move, us types.Color) bool {
	kSq := b.KingSquare(us)
	if kSq >= types.SquareNone {
		return false
	}
	if m.From() == kSq {
		return true
	}
	kRing := bitboard.KingAttacks(kSq) | bitboard.SquareBB[kSq]
	return (bitboard.SquareBB[m.To()] & kRing) != 0
}

func ScoreMoves(
	b *board.Board,
	ml *movegen.MoveList,
	scores []int32,
	ttMove types.Move,
	ply int,
	prevMove types.Move,
	prevMove2 types.Move,
	prevMove4 types.Move,
	history *SearchHistory,
) {
	side := b.SideToMove
	pawnThreats := b.PawnThreatsOnPieces(side)
	kAttackers, _ := b.KingAttackersCount(side)
	kingInDanger := kAttackers >= 2

	var killer1, killer2, counterMove types.Move
	if ply < MaxPly {
		killer1 = history.Killers[ply][0]
		killer2 = history.Killers[ply][1]
	}
	if prevMove != types.MoveNone {
		counterMove = history.CounterMoves[prevMove.From()][prevMove.To()]
	}

	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]

		// 1. TT Move
		if m == ttMove {
			scores[i] = ScoreTTMove
			continue
		}

		// 2. Queen Promotion (capture or quiet)
		if m.IsPromotion() && m.PromotionType() == types.Queen {
			scores[i] = ScoreQueenPromotion + eval.SEE(b, m)
			continue
		}

		// 3. Captures
		if m.IsCapture() {
			attacker := b.Pieces[m.From()].Type()
			victim := b.Pieces[m.To()].Type()
			if m.IsEnPassant() {
				victim = types.Pawn
			}
			mvvLva := PieceMVV[victim]*10 - PieceMVV[attacker]
			capHist := int32(0)
			if attacker < 7 && victim < 7 && m.To() < 64 {
				capHist = history.CaptureHistory[attacker][m.To()][victim]
			}

			seeScore := eval.SEE(b, m)
			givesCheck := b.GivesCheck(m)
			checkers := b.Checkers(side)
			isCheckerCapture := checkers != 0 && (bitboard.SquareBB[m.To()]&checkers) != 0

			if givesCheck && seeScore >= 0 {
				scores[i] = ScoreGoodCapture + 20_000_000 + seeScore*100 + mvvLva*10 + capHist
			} else if isCheckerCapture {
				bonus := int32(15_000_000)
				if seeScore < 0 {
					bonus = 12_000_000
				}
				scores[i] = ScoreGoodCapture + bonus + seeScore*100 + mvvLva*10 + capHist
			} else if seeScore > 0 {
				// Winning capture: free/hanging piece or gaining material
				scores[i] = ScoreGoodCapture + 10_000_000 + seeScore*100 + mvvLva*10 + capHist
			} else if seeScore == 0 {
				// Equal trade
				scores[i] = ScoreGoodCapture + mvvLva*10 + capHist
			} else {
				bonus := int32(0)
				if givesCheck {
					bonus = 10_000_000
				}
				scores[i] = ScoreBadCapture + bonus + seeScore*100 + mvvLva*10 + capHist
			}
			continue
		}

		// 4. Minor promotions (Knight/Rook/Bishop) without Queen
		if m.IsPromotion() {
			scores[i] = ScoreBadCapture + eval.SEE(b, m)
			continue
		}

		// 5. Killer Moves
		if m == killer1 {
			scores[i] = ScoreKiller1
			continue
		}
		if m == killer2 {
			scores[i] = ScoreKiller2
			continue
		}

		// 6. Countermove Heuristic
		if m == counterMove {
			scores[i] = ScoreCounterMove
			continue
		}

		// 7. Quiet Moves: History + 1-ply, 2-ply & 4-ply Continuation History
		histScore := history.HistoryTable[side][m.From()][m.To()]
		if prevMove != types.MoveNone && prevMove.To() < 64 && m.To() < 64 {
			histScore += history.ContHistory[prevMove.To()][m.To()]
		}
		if prevMove2 != types.MoveNone && prevMove2.To() < 64 && m.To() < 64 {
			histScore += history.ContHistory2[prevMove2.To()][m.To()]
		}
		if prevMove4 != types.MoveNone && prevMove4.To() < 64 && m.To() < 64 {
			histScore += history.ContHistory4[prevMove4.To()][m.To()]
		}

		isPawnPush := b.Pieces[m.From()].Type() == types.Pawn
		toSq := m.To()
		isAdvancedPawn := isPawnPush && ((side == types.White && toSq >= types.A6) || (side == types.Black && toSq <= types.H3))

		// Quiet checks, tactical threats, king defense and king attacks priority
		if pawnThreats != 0 && (bitboard.SquareBB[m.From()]&pawnThreats) != 0 && eval.SEE(b, m) >= 0 {
			scores[i] = ScorePawnThreatEvasion + histScore/10
		} else if kingInDanger && IsKingDefenseMove(b, m, side) {
			scores[i] = ScoreKingDefense + histScore/10
		} else if b.GivesCheck(m) {
			scores[i] = ScoreQuietCheck + histScore/10
		} else if pawnThreats == 0 && IsTacticalThreat(b, m) {
			scores[i] = ScoreThreatMove + histScore/10
		} else if pawnThreats == 0 && IsKingAttack(b, m) {
			scores[i] = ScoreKingThreat + histScore/10
		} else if isAdvancedPawn {
			scores[i] = ScorePassedPawnPush + histScore/10
		} else {
			scores[i] = histScore
		}
	}
}

func PickNextMove(ml *movegen.MoveList, scores []int32, currentIndex int) types.Move {
	bestIndex := currentIndex
	bestScore := scores[currentIndex]

	for i := currentIndex + 1; i < ml.Count; i++ {
		if scores[i] > bestScore {
			bestScore = scores[i]
			bestIndex = i
		}
	}

	if bestIndex != currentIndex {
		ml.Moves[currentIndex], ml.Moves[bestIndex] = ml.Moves[bestIndex], ml.Moves[currentIndex]
		scores[currentIndex], scores[bestIndex] = scores[bestIndex], scores[currentIndex]
	}

	return ml.Moves[currentIndex]
}
