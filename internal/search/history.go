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
	ContHistory4   [64][64]int32
	CaptureHistory [7][64][7]int32
	CorrHist       [2][16384]int16
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
	h.ContHistory4 = [64][64]int32{}
	h.CaptureHistory = [7][64][7]int32{}
	h.CorrHist = [2][16384]int16{}
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

func (h *SearchHistory) PenalizeCaptureHistory(attacker, victim types.PieceType, to types.Square, depth int8) {
	if attacker >= 7 || victim >= 7 || to >= 64 {
		return
	}
	penalty := int32(depth) * int32(depth)
	if penalty > 16384 {
		penalty = 16384
	}
	cur := h.CaptureHistory[attacker][to][victim]
	h.CaptureHistory[attacker][to][victim] = cur - penalty - (cur * penalty / 16384)
}

func (h *SearchHistory) UpdateContHistory4(prevMove4, m types.Move, depth int8) {
	if prevMove4 == types.MoveNone || m.IsCapture() || m.IsPromotion() {
		return
	}
	bonus := int32(depth) * int32(depth)
	if bonus > 16384 {
		bonus = 16384
	}
	pTo := prevMove4.To()
	to := m.To()
	if pTo < 64 && to < 64 {
		cur := h.ContHistory4[pTo][to]
		h.ContHistory4[pTo][to] = cur + bonus - (cur * bonus / 16384)
	}
}

func (h *SearchHistory) PenalizeContHistory4(prevMove4, m types.Move, depth int8) {
	if prevMove4 == types.MoveNone || m.IsCapture() || m.IsPromotion() {
		return
	}
	penalty := int32(depth) * int32(depth)
	if penalty > 16384 {
		penalty = 16384
	}
	pTo := prevMove4.To()
	to := m.To()
	if pTo < 64 && to < 64 {
		cur := h.ContHistory4[pTo][to]
		h.ContHistory4[pTo][to] = cur - penalty - (cur * penalty / 16384)
	}
}

func (h *SearchHistory) GetCorrection(side types.Color, pawnHash uint64) int32 {
	if side >= 2 {
		return 0
	}
	corr := int32(h.CorrHist[side][pawnHash%16384]) / 128
	if corr > 64 {
		return 64
	}
	if corr < -64 {
		return -64
	}
	return corr
}

func (h *SearchHistory) UpdateCorrHist(side types.Color, pawnHash uint64, diff int32, depth int8) {
	if side >= 2 {
		return
	}
	if diff > 400 {
		diff = 400
	} else if diff < -400 {
		diff = -400
	}
	weight := int32(depth)
	if weight > 16 {
		weight = 16
	}
	idx := pawnHash % 16384
	cur := int32(h.CorrHist[side][idx])
	newVal := cur + (diff*128-cur)*weight/256
	if newVal > 16384 {
		newVal = 16384
	} else if newVal < -16384 {
		newVal = -16384
	}
	h.CorrHist[side][idx] = int16(newVal)
}

func (h *SearchHistory) UpdateCounterMove(prevMove, m types.Move) {
	if prevMove != types.MoveNone && !m.IsCapture() {
		h.CounterMoves[prevMove.From()][prevMove.To()] = m
	}
}
