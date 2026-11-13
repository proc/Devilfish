package search

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/eval"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

type PickerStage int

const (
	StageTTMove PickerStage = iota
	StageGenCaptures
	StageGoodCaptures
	StageKiller1
	StageKiller2
	StageCounterMove
	StageGenQuiets
	StageQuiets
	StageBadCaptures
	StageDone
)

type MovePicker struct {
	board        *board.Board
	history      *SearchHistory
	ttMove       types.Move
	excludedMove types.Move
	killer1      types.Move
	killer2      types.Move
	counterMove  types.Move
	prevMove     types.Move
	prevMove2    types.Move
	prevMove4    types.Move
	ply          int
	stage        PickerStage

	captures  movegen.MoveList
	capScores [256]int32
	capSEE    [256]int32
	capIndex  int

	quiets      movegen.MoveList
	quietScores [256]int32
	quietIndex  int

	badCaps      [64]types.Move
	badCapScores [64]int32
	badCapSEE    [64]int32
	badCapCount  int
	badCapIndex  int

	// SEE of the move most recently returned by Next, when it came from the
	// capture list (where SEE is computed for scoring anyway).
	lastSEE      int32
	lastSEEKnown bool

	// Per-node facts the search has already worked out. When not supplied the
	// picker computes them itself.
	haveNodeInfo    bool
	threats         bitboard.Bitboard
	kingInDanger    bool
	hasActiveAttack bool
	enemyKingRing   bitboard.Bitboard

	// History rows for this node. Set by the search; without it the picker
	// builds a context that has no continuation history.
	hc *histCtx
}

// SetNodeInfo hands the picker values the caller has already computed for this
// position, so they are not computed twice: the caller's pieces under tactical
// threat, whether its king zone is attacked by two or more pieces, whether it
// has an attack going against the enemy king, and the enemy king ring.
func (mp *MovePicker) SetNodeInfo(threats bitboard.Bitboard, kingInDanger, hasActiveAttack bool, enemyKingRing bitboard.Bitboard) {
	mp.haveNodeInfo = true
	mp.threats = threats
	mp.kingInDanger = kingInDanger
	mp.hasActiveAttack = hasActiveAttack
	mp.enemyKingRing = enemyKingRing
}

// LastSEE returns the static exchange value of the move Next just returned, if
// the picker computed one for it.
func (mp *MovePicker) LastSEE() (int32, bool) {
	return mp.lastSEE, mp.lastSEEKnown
}

// enemyRing returns the squares around the enemy king, extended two ranks
// toward the side to move.
func (mp *MovePicker) enemyRing() bitboard.Bitboard {
	if mp.haveNodeInfo {
		return mp.enemyKingRing
	}
	them := mp.board.SideToMove.Other()
	kSq := mp.board.KingSquare(them)
	ring := bitboard.Bitboard(0)
	if kSq < types.SquareNone {
		ring = bitboard.KingAttacks(kSq) | bitboard.SquareBB[kSq]
		if them == types.White {
			ring |= (ring << 8) | (ring << 16)
		} else {
			ring |= (ring >> 8) | (ring >> 16)
		}
	}
	return ring
}

// pickCapture is PickNextMove for the capture list, keeping capSEE in step.
func (mp *MovePicker) pickCapture(currentIndex int) types.Move {
	bestIndex := currentIndex
	bestScore := mp.capScores[currentIndex]
	for i := currentIndex + 1; i < mp.captures.Count; i++ {
		if mp.capScores[i] > bestScore {
			bestScore = mp.capScores[i]
			bestIndex = i
		}
	}
	if bestIndex != currentIndex {
		ml := &mp.captures
		ml.Moves[currentIndex], ml.Moves[bestIndex] = ml.Moves[bestIndex], ml.Moves[currentIndex]
		mp.capScores[currentIndex], mp.capScores[bestIndex] = mp.capScores[bestIndex], mp.capScores[currentIndex]
		mp.capSEE[currentIndex], mp.capSEE[bestIndex] = mp.capSEE[bestIndex], mp.capSEE[currentIndex]
	}
	return mp.captures.Moves[currentIndex]
}

func NewMovePicker(
	b *board.Board,
	ttMove types.Move,
	excludedMove types.Move,
	ply int,
	prevMove types.Move,
	prevMove2 types.Move,
	prevMove4 types.Move,
	history *SearchHistory,
) MovePicker {
	mp := MovePicker{
		board:        b,
		history:      history,
		ttMove:       ttMove,
		excludedMove: excludedMove,
		ply:          ply,
		prevMove:     prevMove,
		prevMove2:    prevMove2,
		prevMove4:    prevMove4,
		stage:        StageTTMove,
	}

	if ply < MaxPly {
		mp.killer1 = history.Killers[ply][0]
		mp.killer2 = history.Killers[ply][1]
	}
	if prevMove != types.MoveNone {
		mp.counterMove = history.CounterMoves[prevMove.From()][prevMove.To()]
	}

	return mp
}

func (mp *MovePicker) Next() types.Move {
	mp.lastSEEKnown = false
	for {
		switch mp.stage {
		case StageTTMove:
			mp.stage = StageGenCaptures
			if mp.ttMove != types.MoveNone && mp.ttMove != mp.excludedMove &&
				movegen.IsPseudoLegal(mp.board, mp.ttMove) {
				return mp.ttMove
			}

		case StageGenCaptures:
			mp.stage = StageGoodCaptures
			movegen.GenerateMovesByType(mp.board, movegen.GenCaptures, &mp.captures)
			checkers := mp.board.Checkers(mp.board.SideToMove)
			kingRing := mp.enemyRing()

			for i := 0; i < mp.captures.Count; i++ {
				m := mp.captures.Moves[i]
				attacker := mp.board.Pieces[m.From()].Type()
				victim := mp.board.Pieces[m.To()].Type()
				if m.IsEnPassant() {
					victim = types.Pawn
				}
				mvvLva := PieceMVV[victim]*10 - PieceMVV[attacker]
				capHist := int32(0)
				if attacker < 7 && victim < 7 && m.To() < 64 {
					capHist = mp.history.CaptureHistory[attacker][m.To()][victim]
				}
				see := eval.SEE(mp.board, m)
				mp.capSEE[i] = see
				givesCheck := mp.board.GivesCheck(m)
				isCheckerCapture := checkers != 0 && (bitboard.SquareBB[m.To()]&checkers) != 0
				isKingZoneCapture := (bitboard.SquareBB[m.To()] & kingRing) != 0

				if m.IsPromotion() && m.PromotionType() == types.Queen {
					mp.capScores[i] = ScoreQueenPromotion + see
				} else if givesCheck && see >= 0 {
					mp.capScores[i] = ScoreGoodCapture + 20_000_000 + see*100 + mvvLva*10 + capHist
				} else if isCheckerCapture {
					// Capturing the checking piece eliminates the check threat!
					// Must be prioritized in StageGoodCaptures ahead of quiet king evasions.
					bonus := int32(15_000_000)
					if see < 0 {
						bonus = 12_000_000
					}
					mp.capScores[i] = ScoreGoodCapture + bonus + see*100 + mvvLva*10 + capHist
				} else if see > 0 {
					// Winning capture: free/hanging piece or gaining material
					bonus := int32(0)
					if isKingZoneCapture {
						bonus = 2_000_000 // High urgency on captures near the enemy king
					}
					mp.capScores[i] = ScoreGoodCapture + 10_000_000 + bonus + see*100 + mvvLva*10 + capHist
				} else if see == 0 {
					// Equal trade
					bonus := int32(0)
					if isKingZoneCapture {
						bonus = 1_000_000
					}
					mp.capScores[i] = ScoreGoodCapture + bonus + mvvLva*10 + capHist
				} else {
					// Losing capture (SEE < 0): strictly saved for StageBadCaptures after quiet moves
					bonus := int32(0)
					if givesCheck {
						bonus = 10_000_000
					}
					mp.capScores[i] = ScoreBadCapture + bonus + see*100 + mvvLva*10 + capHist
				}
			}

		case StageGoodCaptures:
			for mp.capIndex < mp.captures.Count {
				m := mp.pickCapture(mp.capIndex)
				score := mp.capScores[mp.capIndex]
				see := mp.capSEE[mp.capIndex]
				mp.capIndex++

				if m == mp.ttMove || m == mp.excludedMove {
					continue
				}

				// Return good/equal captures, queen promotions, and forcing checking captures in this stage
				if score >= ScoreQuietCheck {
					mp.lastSEE, mp.lastSEEKnown = see, true
					return m
				}

				// Save remaining losing captures for StageBadCaptures
				if mp.badCapCount < 64 {
					mp.badCaps[mp.badCapCount] = m
					mp.badCapScores[mp.badCapCount] = score
					mp.badCapSEE[mp.badCapCount] = see
					mp.badCapCount++
				}
			}
			mp.stage = StageKiller1

		case StageKiller1:
			mp.stage = StageKiller2
			if mp.killer1 != types.MoveNone && mp.killer1 != mp.ttMove && mp.killer1 != mp.excludedMove &&
				!mp.killer1.IsCapture() && !mp.killer1.IsPromotion() && !mp.killer1.IsCastle() &&
				movegen.IsPseudoLegal(mp.board, mp.killer1) {
				return mp.killer1
			}

		case StageKiller2:
			mp.stage = StageCounterMove
			if mp.killer2 != types.MoveNone && mp.killer2 != mp.ttMove && mp.killer2 != mp.excludedMove &&
				mp.killer2 != mp.killer1 &&
				!mp.killer2.IsCapture() && !mp.killer2.IsPromotion() && !mp.killer2.IsCastle() &&
				movegen.IsPseudoLegal(mp.board, mp.killer2) {
				return mp.killer2
			}

		case StageCounterMove:
			mp.stage = StageGenQuiets
			if mp.counterMove != types.MoveNone && mp.counterMove != mp.ttMove && mp.counterMove != mp.excludedMove &&
				mp.counterMove != mp.killer1 && mp.counterMove != mp.killer2 &&
				!mp.counterMove.IsCapture() && !mp.counterMove.IsPromotion() && !mp.counterMove.IsCastle() &&
				movegen.IsPseudoLegal(mp.board, mp.counterMove) {
				return mp.counterMove
			}

		case StageGenQuiets:
			mp.stage = StageQuiets
			movegen.GenerateMovesByType(mp.board, movegen.GenQuiets, &mp.quiets)
			side := mp.board.SideToMove
			var pawnThreats bitboard.Bitboard
			var kingInDanger, hasActiveAttack bool
			if mp.haveNodeInfo {
				pawnThreats = mp.threats
				kingInDanger = mp.kingInDanger
				hasActiveAttack = mp.hasActiveAttack
			} else {
				pawnThreats = mp.board.PawnThreatsOnPieces(side)
				kAttackers, _ := mp.board.KingAttackersCount(side)
				kingInDanger = kAttackers >= 2
				enemyAttackers, enemyUnits := mp.board.KingAttackersCount(side.Other())
				hasActiveAttack = enemyAttackers >= 2 || enemyUnits >= 4
			}
			enemyKingRing := mp.enemyRing()
			hc := mp.hc
			if hc == nil {
				c := mp.history.Context(mp.board, -1, pawnThreats)
				hc = &c
			}

			for i := 0; i < mp.quiets.Count; i++ {
				m := mp.quiets.Moves[i]
				histScore := hc.Order(mp.board.Pieces[m.From()], m)
				isPawnPush := mp.board.Pieces[m.From()].Type() == types.Pawn
				toSq := m.To()
				isAdvancedPawn := isPawnPush && ((side == types.White && toSq >= types.A6) || (side == types.Black && toSq <= types.H3))
				isKingZoneQuiet := (bitboard.SquareBB[toSq] & enemyKingRing) != 0

				if pawnThreats != 0 && (bitboard.SquareBB[m.From()]&pawnThreats) != 0 && eval.SEE(mp.board, m) >= 0 {
					mp.quietScores[i] = ScorePawnThreatEvasion + histScore/10
				} else if kingInDanger && IsKingDefenseMove(mp.board, m, side) {
					mp.quietScores[i] = ScoreKingDefense + histScore/10
				} else if mp.board.GivesCheck(m) {
					mp.quietScores[i] = ScoreQuietCheck + histScore/10
				} else if hasActiveAttack && (IsKingAttack(mp.board, m) || isKingZoneQuiet) {
					// Aggressive quiet move towards enemy King during an active attack
					mp.quietScores[i] = ScoreQuietCheck + 1_000_000 + histScore/10
				} else if pawnThreats == 0 && IsTacticalThreat(mp.board, m) {
					mp.quietScores[i] = ScoreThreatMove + histScore/10
				} else if pawnThreats == 0 && IsKingAttack(mp.board, m) {
					mp.quietScores[i] = ScoreKingThreat + histScore/10
				} else if isAdvancedPawn {
					mp.quietScores[i] = ScorePassedPawnPush + histScore/10
				} else {
					mp.quietScores[i] = histScore
				}
			}
			// Sort once (stable, best first) rather than scanning the rest of
			// the list for every move handed out.
			sortQuiets(&mp.quiets, mp.quietScores[:mp.quiets.Count])
			fallthrough

		case StageQuiets:
			mp.stage = StageQuiets
			for mp.quietIndex < mp.quiets.Count {
				m := mp.quiets.Moves[mp.quietIndex]
				mp.quietIndex++

				if m == mp.ttMove || m == mp.excludedMove || m == mp.killer1 || m == mp.killer2 || m == mp.counterMove {
					continue
				}
				return m
			}
			mp.stage = StageBadCaptures

		case StageBadCaptures:
			if mp.badCapIndex < mp.badCapCount {
				bestIdx := mp.badCapIndex
				bestScore := mp.badCapScores[bestIdx]
				for j := mp.badCapIndex + 1; j < mp.badCapCount; j++ {
					if mp.badCapScores[j] > bestScore {
						bestScore = mp.badCapScores[j]
						bestIdx = j
					}
				}
				if bestIdx != mp.badCapIndex {
					mp.badCaps[mp.badCapIndex], mp.badCaps[bestIdx] = mp.badCaps[bestIdx], mp.badCaps[mp.badCapIndex]
					mp.badCapScores[mp.badCapIndex], mp.badCapScores[bestIdx] = mp.badCapScores[bestIdx], mp.badCapScores[mp.badCapIndex]
					mp.badCapSEE[mp.badCapIndex], mp.badCapSEE[bestIdx] = mp.badCapSEE[bestIdx], mp.badCapSEE[mp.badCapIndex]
				}
				m := mp.badCaps[mp.badCapIndex]
				mp.lastSEE, mp.lastSEEKnown = mp.badCapSEE[mp.badCapIndex], true
				mp.badCapIndex++
				return m
			}
			mp.stage = StageDone
			return types.MoveNone

		case StageDone:
			return types.MoveNone
		}
	}
}

// sortQuiets sorts moves by descending score with an insertion sort, which is
// fast for the short lists move generation produces and keeps equal-scored
// moves in generation order.
func sortQuiets(ml *movegen.MoveList, scores []int32) {
	for i := 1; i < len(scores); i++ {
		m, sc := ml.Moves[i], scores[i]
		j := i - 1
		for ; j >= 0 && scores[j] < sc; j-- {
			ml.Moves[j+1], scores[j+1] = ml.Moves[j], scores[j]
		}
		ml.Moves[j+1], scores[j+1] = m, sc
	}
}
