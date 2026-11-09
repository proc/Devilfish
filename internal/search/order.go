package search

import (
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
	ScoreTTMove         int32 = 2_000_000_000
	ScoreQueenPromotion int32 = 150_000_000
	ScoreGoodCapture    int32 = 100_000_000
	ScoreKiller1        int32 = 50_000_000
	ScoreKiller2        int32 = 45_000_000
	ScoreCounterMove    int32 = 40_000_000
	ScoreQuietCheck     int32 = 30_000_000
	ScorePassedPawnPush int32 = 25_000_000
	ScoreBadCapture     int32 = -50_000_000
)

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
			if givesCheck {
				if seeScore >= 0 {
					scores[i] = ScoreGoodCapture + 20_000_000 + seeScore*100 + mvvLva*10 + capHist
				} else {
					scores[i] = ScoreGoodCapture + 10_000_000 + seeScore*100 + mvvLva*10 + capHist
				}
			} else if seeScore >= 0 {
				scores[i] = ScoreGoodCapture + seeScore*100 + mvvLva*10 + capHist
			} else {
				scores[i] = ScoreBadCapture + seeScore*100 + mvvLva*10 + capHist
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

		// Quiet checks priority: boost check-giving moves above ordinary quiet moves
		if b.GivesCheck(m) {
			scores[i] = ScoreQuietCheck + histScore/10
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
