package search

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

const MaxPly = 128

// contRow holds one history value per (piece, destination square).
type contRow = [15][64]int16

const pawnHistSize = 1024

// SearchHistory holds a worker's move-ordering and correction statistics.
//
// Quiet moves are scored from three kinds of table, all on the same scale
// (saturating at +-histLimit):
//
//   - Main: by side, origin and destination, split by whether the moving piece
//     stands under a tactical threat and whether the destination is attacked by
//     an enemy pawn. Escaping a threat and walking into one are different
//     moves even between the same two squares.
//   - Cont (continuation history): by the piece and destination of an earlier
//     move and the piece and destination of this one. One table serves the
//     moves 1, 2 and 4 plies back.
//   - PawnHist: by pawn structure, piece and destination. Where a piece
//     belongs depends on the pawns far more than on anything else.
type SearchHistory struct {
	Killers        [MaxPly][2]types.Move
	Main           [2][2][2][64][64]int16
	Cont           [15][64]contRow
	PawnHist       [pawnHistSize]contRow
	CounterMoves   [64][64]types.Move
	CaptureHistory [7][64][7]int32
	CorrHist       [2][corrSize]int16 // pawn structure
	NonPawnCorr    [2][2][corrSize]int16
	MinorCorr      [2][corrSize]int16
	ContCorr       [2][64][64]int16

	// contStack[ply+contPad] is the Cont row of the move played at ply on the
	// current search path (nil after a null move), which the nodes below read
	// their continuation history from.
	contStack [MaxPly + contPad]*contRow
}

const (
	histLimit = 16384
	contPad   = 4 // plies of look-back, so that the root can index below itself
)

func NewSearchHistory() *SearchHistory {
	return &SearchHistory{}
}

func (h *SearchHistory) Clear() {
	*h = SearchHistory{}
}

// NewSearch readies the tables for the next search of the same game. Killers
// belong to plies of the previous search tree and are dropped; the history,
// counter-move and correction tables describe the game's pawn structures and
// piece play, which carry over to the next move, so they are kept.
func (h *SearchHistory) NewSearch() {
	h.Killers = [MaxPly][2]types.Move{}
	h.contStack = [MaxPly + contPad]*contRow{}
}

// PushMove records that piece is being moved to to at ply, for the nodes
// below. PushNull records a null move.
func (h *SearchHistory) PushMove(ply int, piece types.Piece, to types.Square) {
	if ply < MaxPly {
		h.contStack[ply+contPad] = &h.Cont[piece][to]
	}
}

func (h *SearchHistory) PushNull(ply int) {
	if ply < MaxPly {
		h.contStack[ply+contPad] = nil
	}
}

// histCtx gathers, for one node, the table rows the history of its quiet
// moves is read from and written to.
type histCtx struct {
	main         *[2][2][64][64]int16
	threatened   bitboard.Bitboard // own pieces under tactical threat
	pawnAttacked bitboard.Bitboard // squares attacked by enemy pawns
	cont         [3]*contRow       // the moves 1, 2 and 4 plies back; nil where there is none
	pawn         *contRow
}

// Context returns the history rows for the node at ply, where b is the
// position and threatened the side to move's pieces under tactical threat.
func (h *SearchHistory) Context(b *board.Board, ply int, threatened bitboard.Bitboard) histCtx {
	us := b.SideToMove
	c := histCtx{
		main:       &h.Main[us],
		threatened: threatened,
		pawn:       &h.PawnHist[b.PawnHash%pawnHistSize],
	}
	if us == types.White {
		p := b.PieceBB[types.BlackPawn]
		c.pawnAttacked = (p&^bitboard.FileABB)>>9 | (p&^bitboard.FileHBB)>>7
	} else {
		p := b.PieceBB[types.WhitePawn]
		c.pawnAttacked = (p&^bitboard.FileABB)<<7 | (p&^bitboard.FileHBB)<<9
	}
	if ply >= 0 && ply < MaxPly {
		i := ply + contPad
		c.cont = [3]*contRow{h.contStack[i-1], h.contStack[i-2], h.contStack[i-4]}
	}
	return c
}

func (c *histCtx) mainEntry(m types.Move) *int16 {
	from, to := m.From(), m.To()
	ft, tt := 0, 0
	if c.threatened&bitboard.SquareBB[from] != 0 {
		ft = 1
	}
	if c.pawnAttacked&bitboard.SquareBB[to] != 0 {
		tt = 1
	}
	return &c.main[ft][tt][from][to]
}

// Order is the score quiet moves are sorted by: every table.
func (c *histCtx) Order(piece types.Piece, m types.Move) int32 {
	to := m.To()
	s := int32(*c.mainEntry(m)) + int32(c.pawn[piece][to])
	for _, row := range c.cont {
		if row != nil {
			s += int32(row[piece][to])
		}
	}
	return s
}

// Stat is the score late-move reductions are adjusted by (main and
// continuation history); Prune the one history pruning compares (the same
// without the move four plies back).
func (c *histCtx) Stat(piece types.Piece, m types.Move) int32 {
	s := c.Prune(piece, m)
	if c.cont[2] != nil {
		s += int32(c.cont[2][piece][m.To()])
	}
	return s
}

func (c *histCtx) Prune(piece types.Piece, m types.Move) int32 {
	to := m.To()
	s := int32(*c.mainEntry(m))
	if c.cont[0] != nil {
		s += int32(c.cont[0][piece][to])
	}
	if c.cont[1] != nil {
		s += int32(c.cont[1][piece][to])
	}
	return s
}

// Update moves every table's entry for the quiet move m by bonus (negative to
// penalize).
func (c *histCtx) Update(piece types.Piece, m types.Move, bonus int32) {
	if m.IsCapture() || m.IsPromotion() {
		return
	}
	to := m.To()
	histGravity(c.mainEntry(m), bonus)
	histGravity(&c.pawn[piece][to], bonus)
	for _, row := range c.cont {
		if row != nil {
			histGravity(&row[piece][to], bonus)
		}
	}
}

// histGravity adds bonus to *e, pulling large entries back toward zero so
// they stay within +-histLimit.
func histGravity(e *int16, bonus int32) {
	abs := bonus
	if abs < 0 {
		abs = -abs
	}
	cur := int32(*e)
	*e = int16(cur + bonus - cur*abs/histLimit)
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
