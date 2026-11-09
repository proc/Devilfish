package search

import (
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

	captures     movegen.MoveList
	capScores    [256]int32
	capIndex     int

	quiets       movegen.MoveList
	quietScores  [256]int32
	quietIndex   int

	badCaps      [64]types.Move
	badCapScores [64]int32
	badCapCount  int
	badCapIndex  int
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
				if m.IsPromotion() && m.PromotionType() == types.Queen {
					mp.capScores[i] = ScoreQueenPromotion + see
				} else if see >= 0 {
					mp.capScores[i] = ScoreGoodCapture + see*100 + mvvLva*10 + capHist
				} else {
					mp.capScores[i] = ScoreBadCapture + see*100 + mvvLva*10 + capHist
				}
			}

		case StageGoodCaptures:
			for mp.capIndex < mp.captures.Count {
				m := PickNextMove(&mp.captures, mp.capScores[:mp.captures.Count], mp.capIndex)
				score := mp.capScores[mp.capIndex]
				mp.capIndex++

				if m == mp.ttMove || m == mp.excludedMove {
					continue
				}

				// Only return good/equal captures & queen promotions in this stage
				if score >= ScoreGoodCapture {
					return m
				}

				// Save losing captures for StageBadCaptures
				if mp.badCapCount < 64 {
					mp.badCaps[mp.badCapCount] = m
					mp.badCapScores[mp.badCapCount] = score
					mp.badCapCount++
				}
			}
			mp.stage = StageKiller1

		case StageKiller1:
			mp.stage = StageKiller2
			if mp.killer1 != types.MoveNone && mp.killer1 != mp.ttMove && mp.killer1 != mp.excludedMove &&
				!mp.killer1.IsCapture() && !mp.killer1.IsPromotion() &&
				movegen.IsPseudoLegal(mp.board, mp.killer1) {
				return mp.killer1
			}

		case StageKiller2:
			mp.stage = StageCounterMove
			if mp.killer2 != types.MoveNone && mp.killer2 != mp.ttMove && mp.killer2 != mp.excludedMove &&
				mp.killer2 != mp.killer1 &&
				!mp.killer2.IsCapture() && !mp.killer2.IsPromotion() &&
				movegen.IsPseudoLegal(mp.board, mp.killer2) {
				return mp.killer2
			}

		case StageCounterMove:
			mp.stage = StageGenQuiets
			if mp.counterMove != types.MoveNone && mp.counterMove != mp.ttMove && mp.counterMove != mp.excludedMove &&
				mp.counterMove != mp.killer1 && mp.counterMove != mp.killer2 &&
				!mp.counterMove.IsCapture() && !mp.counterMove.IsPromotion() &&
				movegen.IsPseudoLegal(mp.board, mp.counterMove) {
				return mp.counterMove
			}

		case StageGenQuiets:
			mp.stage = StageQuiets
			movegen.GenerateMovesByType(mp.board, movegen.GenQuiets, &mp.quiets)
			side := mp.board.SideToMove

			for i := 0; i < mp.quiets.Count; i++ {
				m := mp.quiets.Moves[i]
				histScore := mp.history.HistoryTable[side][m.From()][m.To()]
				if mp.prevMove != types.MoveNone && mp.prevMove.To() < 64 && m.To() < 64 {
					histScore += mp.history.ContHistory[mp.prevMove.To()][m.To()]
				}
				if mp.prevMove2 != types.MoveNone && mp.prevMove2.To() < 64 && m.To() < 64 {
					histScore += mp.history.ContHistory2[mp.prevMove2.To()][m.To()]
				}
				if mp.prevMove4 != types.MoveNone && mp.prevMove4.To() < 64 && m.To() < 64 {
					histScore += mp.history.ContHistory4[mp.prevMove4.To()][m.To()]
				}
				if mp.board.GivesCheck(m) {
					mp.quietScores[i] = ScoreQuietCheck + histScore/10
				} else {
					mp.quietScores[i] = histScore
				}
			}

		case StageQuiets:
			for mp.quietIndex < mp.quiets.Count {
				m := PickNextMove(&mp.quiets, mp.quietScores[:mp.quiets.Count], mp.quietIndex)
				mp.quietIndex++

				if m == mp.ttMove || m == mp.excludedMove || m == mp.killer1 || m == mp.killer2 || m == mp.counterMove {
					continue
				}
				return m
			}
			mp.stage = StageBadCaptures

		case StageBadCaptures:
			if mp.badCapIndex < mp.badCapCount {
				m := mp.badCaps[mp.badCapIndex]
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
