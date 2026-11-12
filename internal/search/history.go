package search

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
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
	CorrHist       [2][corrSize]int16 // pawn structure
	NonPawnCorr    [2][2][corrSize]int16
	MinorCorr      [2][corrSize]int16
	ContCorr       [2][64][64]int16
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
	h.CorrHist = [2][corrSize]int16{}
	h.NonPawnCorr = [2][2][corrSize]int16{}
	h.MinorCorr = [2][corrSize]int16{}
	h.ContCorr = [2][64][64]int16{}
}

func (h *SearchHistory) UpdateKillers(ply int, m types.Move) {
	if ply >= MaxPly || m.IsCapture() || m.IsCastle() {
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
	bonus := quietStatBonus(depth)
	from := m.From()
	to := m.To()

	cur := h.HistoryTable[side][from][to]
	h.HistoryTable[side][from][to] = cur + bonus - (cur * bonus / 16384)
}

func (h *SearchHistory) PenalizeHistory(side types.Color, m types.Move, depth int8) {
	if m.IsCapture() {
		return
	}
	penalty := quietStatBonus(depth)
	from := m.From()
	to := m.To()

	cur := h.HistoryTable[side][from][to]
	h.HistoryTable[side][from][to] = cur - penalty - (cur * penalty / 16384)
}

func (h *SearchHistory) UpdateContHistory(prevMove, m types.Move, depth int8) {
	if prevMove == types.MoveNone || m.IsCapture() || m.IsPromotion() {
		return
	}
	bonus := quietStatBonus(depth)
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
	penalty := quietStatBonus(depth)
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
	bonus := quietStatBonus(depth)
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
	penalty := quietStatBonus(depth)
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
	bonus := quietStatBonus(depth)
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
	penalty := quietStatBonus(depth)
	pTo := prevMove4.To()
	to := m.To()
	if pTo < 64 && to < 64 {
		cur := h.ContHistory4[pTo][to]
		h.ContHistory4[pTo][to] = cur - penalty - (cur * penalty / 16384)
	}
}

// Correction history learns how far the static eval tends to be off for
// positions sharing some feature, and adds that back onto the eval. Each
// table is keyed on a different feature; they are trained on the error left
// after all of them are applied, so their contributions add up.
const (
	corrSize      = 16384
	corrUnit      = 128   // entries are stored in cp*corrUnit
	corrLimit     = 16384 // |entry| bound, i.e. 128 cp
	corrMaxBonus  = 4096
	corrMaxOutput = 128 // cp
)

// corrKeys holds the table indices for one position.
type corrKeys struct {
	stm      types.Color
	pawn     uint32
	nonPawn  [2]uint32 // each colour's pieces other than pawns, kings included
	minor    uint32    // knights, bishops and kings of both colours
	contLast types.Square
	contPrev types.Square
	hasCont  bool
}

func newCorrKeys(b *board.Board) corrKeys {
	k := corrKeys{stm: b.SideToMove, pawn: uint32(b.PawnHash % corrSize)}
	var np [2]uint64
	var minor uint64
	for pt := types.Knight; pt <= types.King; pt++ {
		for c := types.White; c <= types.Black; c++ {
			p := types.MakePiece(c, pt)
			for bb := b.PieceBB[p]; bb != 0; {
				z := board.ZobristPiece[p][bitboard.PopLSB(&bb)]
				np[c] ^= z
				if pt == types.Knight || pt == types.Bishop || pt == types.King {
					minor ^= z
				}
			}
		}
	}
	k.nonPawn[0] = uint32(np[0] % corrSize)
	k.nonPawn[1] = uint32(np[1] % corrSize)
	k.minor = uint32(minor % corrSize)
	// The last two moves (theirs, then ours), when neither was a null move
	if n := len(b.UndoStack); n >= 2 {
		last, prev := b.UndoStack[n-1].Move, b.UndoStack[n-2].Move
		if last != types.MoveNone && prev != types.MoveNone {
			k.contLast, k.contPrev, k.hasCont = last.To(), prev.To(), true
		}
	}
	return k
}

// GetCorrection returns the eval correction in centipawns for b, from the
// side to move's point of view.
func (h *SearchHistory) GetCorrection(b *board.Board) int32 {
	k := newCorrKeys(b)
	s := k.stm
	sum := 2*int32(h.CorrHist[s][k.pawn]) +
		int32(h.NonPawnCorr[s][0][k.nonPawn[0]]) +
		int32(h.NonPawnCorr[s][1][k.nonPawn[1]]) +
		int32(h.MinorCorr[s][k.minor])
	if k.hasCont {
		sum += int32(h.ContCorr[s][k.contLast][k.contPrev])
	}
	// Weights: pawn 1, each of the others 1/2
	corr := sum / (2 * corrUnit)
	if corr > corrMaxOutput {
		return corrMaxOutput
	}
	if corr < -corrMaxOutput {
		return -corrMaxOutput
	}
	return corr
}

// UpdateCorrHist trains every table on diff, the search result minus the
// already-corrected static eval, weighted by depth.
func (h *SearchHistory) UpdateCorrHist(b *board.Board, diff int32, depth int8) {
	k := newCorrKeys(b)
	s := k.stm
	weight := int32(depth)
	if weight > 16 {
		weight = 16
	}
	bonus := diff * corrUnit * weight / 32
	if bonus > corrMaxBonus {
		bonus = corrMaxBonus
	} else if bonus < -corrMaxBonus {
		bonus = -corrMaxBonus
	}
	corrGravity(&h.CorrHist[s][k.pawn], bonus)
	corrGravity(&h.NonPawnCorr[s][0][k.nonPawn[0]], bonus)
	corrGravity(&h.NonPawnCorr[s][1][k.nonPawn[1]], bonus)
	corrGravity(&h.MinorCorr[s][k.minor], bonus)
	if k.hasCont {
		corrGravity(&h.ContCorr[s][k.contLast][k.contPrev], bonus)
	}
}

// corrGravity adds bonus to *e, pulling large entries back toward zero so
// they stay within ±corrLimit.
func corrGravity(e *int16, bonus int32) {
	abs := bonus
	if abs < 0 {
		abs = -abs
	}
	cur := int32(*e)
	v := cur + bonus - cur*abs/corrLimit
	if v > corrLimit {
		v = corrLimit
	} else if v < -corrLimit {
		v = -corrLimit
	}
	*e = int16(v)
}

func (h *SearchHistory) UpdateCounterMove(prevMove, m types.Move) {
	if prevMove != types.MoveNone && !m.IsCapture() && !m.IsCastle() {
		h.CounterMoves[prevMove.From()][prevMove.To()] = m
	}
}

// quietStatBonus is the update size for the quiet-move history tables. The
// gravity update (h += b - h*b/16384) saturates at +-16384, and history
// pruning, LMR and move ordering all read these tables on that scale, so the
// bonus has to be large enough to move values across it within a few
// updates. (It used to be depth^2: at most ~100, which left real history
// scores in the hundreds, so history pruning almost never fired and the
// history term in LMR almost never changed a reduction.)
func quietStatBonus(depth int8) int32 {
	return min(max(160*int32(depth)-80, 16), 1600)
}
