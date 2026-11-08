package search

import (
	"devilfish/internal/board"
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

func ScoreMoves(
	b *board.Board,
	ml *movegen.MoveList,
	scores []int32,
	ttMove types.Move,
	ply int,
	prevMove types.Move,
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
			scores[i] = 2000000000
			continue
		}

		// 2. Captures and Promotions
		if m.IsCapture() || m.IsPromotion() {
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
			scores[i] = 10000000 + mvvLva*1000 + capHist
			continue
		}

		// 3. Killer Moves
		if m == killer1 {
			scores[i] = 9000000
			continue
		}
		if m == killer2 {
			scores[i] = 8000000
			continue
		}

		// 4. Countermove Heuristic
		if m == counterMove {
			scores[i] = 7000000
			continue
		}

		// 5. History Heuristic + Continuation History
		histScore := history.HistoryTable[side][m.From()][m.To()]
		if prevMove != types.MoveNone && prevMove.To() < 64 && m.To() < 64 {
			histScore += history.ContHistory[prevMove.To()][m.To()]
		}
		scores[i] = histScore
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
