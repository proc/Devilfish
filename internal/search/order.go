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

			// Run SEE to determine if winning/equal or losing capture
			seeVal := eval.SEE(b, m)
			if seeVal >= 0 {
				scores[i] = 10000000 + mvvLva*1000 + seeVal
			} else {
				scores[i] = -1000000 + seeVal
			}
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

		// 5. History Heuristic
		scores[i] = history.HistoryTable[side][m.From()][m.To()]
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
