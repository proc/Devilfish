package search

import (
	"devilfish/internal/types"
)

const MaxPly = 128

type SearchHistory struct {
	Killers        [MaxPly][2]types.Move
	HistoryTable   [2][64][64]int32
	CounterMoves   [64][64]types.Move
	ContHistory    [64][64]int32
	ContHistory2   [64][64]int32
	CaptureHistory [7][64][7]int32
}

func NewSearchHistory() *SearchHistory {
	return &SearchHistory{}
}

func (h *SearchHistory) Clear() {
	h.Killers = [MaxPly][2]types.Move{}
	h.HistoryTable = [2][64][64]int32{}
	h.CounterMoves = [64][64]types.Move{}
	h.ContHistory = [64][64]int32{}
	h.ContHistory2 = [64][64]int32{}
	h.CaptureHistory = [7][64][7]int32{}
}

func (h *SearchHistory) UpdateKillers(ply int, m types.Move) {
	if ply >= MaxPly || m.IsCapture() {
		return
	}
	if h.Killers[ply][0] != m {
		h.Killers[ply][1] = h.Killers[ply][0]
		h.Killers[ply][0] = m
	}
}

func (h *SearchHistory) UpdateHistory(side types.Color, m types.Move, depth int8) {
	if m.IsCapture() {
		return
	}
	bonus := int32(depth) * int32(depth)
	if bonus > 16384 {
		bonus = 16384
	}
	from := m.From()
	to := m.To()

	cur := h.HistoryTable[side][from][to]
	h.HistoryTable[side][from][to] = cur + bonus - (cur * bonus / 16384)
}

func (h *SearchHistory) PenalizeHistory(side types.Color, m types.Move, depth int8) {
	if m.IsCapture() {
		return
	}
	penalty := int32(depth) * int32(depth)
	if penalty > 16384 {
		penalty = 16384
	}
	from := m.From()
	to := m.To()

	cur := h.HistoryTable[side][from][to]
	h.HistoryTable[side][from][to] = cur - penalty - (cur * penalty / 16384)
}

func (h *SearchHistory) UpdateContHistory(prevMove, m types.Move, depth int8) {
	if prevMove == types.MoveNone || m.IsCapture() || m.IsPromotion() {
		return
	}
	bonus := int32(depth) * int32(depth)
	if bonus > 16384 {
		bonus = 16384
	}
	pTo := prevMove.To()
	to := m.To()
	if pTo < 64 && to < 64 {
		cur := h.ContHistory[pTo][to]
		h.ContHistory[pTo][to] = cur + bonus - (cur * bonus / 16384)
	}
}

func (h *SearchHistory) PenalizeContHistory(prevMove, m types.Move, depth int8) {
	if prevMove == types.MoveNone || m.IsCapture() || m.IsPromotion() {
		return
	}
	penalty := int32(depth) * int32(depth)
	if penalty > 16384 {
		penalty = 16384
	}
	pTo := prevMove.To()
	to := m.To()
	if pTo < 64 && to < 64 {
		cur := h.ContHistory[pTo][to]
		h.ContHistory[pTo][to] = cur - penalty - (cur * penalty / 16384)
	}
}

func (h *SearchHistory) UpdateContHistory2(prevMove2, m types.Move, depth int8) {
	if prevMove2 == types.MoveNone || m.IsCapture() || m.IsPromotion() {
		return
	}
	bonus := int32(depth) * int32(depth)
	if bonus > 16384 {
		bonus = 16384
	}
	pTo := prevMove2.To()
	to := m.To()
	if pTo < 64 && to < 64 {
		cur := h.ContHistory2[pTo][to]
		h.ContHistory2[pTo][to] = cur + bonus - (cur * bonus / 16384)
	}
}

func (h *SearchHistory) PenalizeContHistory2(prevMove2, m types.Move, depth int8) {
	if prevMove2 == types.MoveNone || m.IsCapture() || m.IsPromotion() {
		return
	}
	penalty := int32(depth) * int32(depth)
	if penalty > 16384 {
		penalty = 16384
	}
	pTo := prevMove2.To()
	to := m.To()
	if pTo < 64 && to < 64 {
		cur := h.ContHistory2[pTo][to]
		h.ContHistory2[pTo][to] = cur - penalty - (cur * penalty / 16384)
	}
}

func (h *SearchHistory) UpdateCaptureHistory(attacker, victim types.PieceType, to types.Square, depth int8) {
	if attacker >= 7 || victim >= 7 || to >= 64 {
		return
	}
	bonus := int32(depth) * int32(depth)
	if bonus > 16384 {
		bonus = 16384
	}
	cur := h.CaptureHistory[attacker][to][victim]
	h.CaptureHistory[attacker][to][victim] = cur + bonus - (cur * bonus / 16384)
}

func (h *SearchHistory) UpdateCounterMove(prevMove, m types.Move) {
	if prevMove != types.MoveNone && !m.IsCapture() {
		h.CounterMoves[prevMove.From()][prevMove.To()] = m
	}
}
