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
	LowPly         [lowPlySize][64][64]int16 // by ply near the root, origin and destination (P.MoveOrder 1)
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
	histLimit  = 16384
	contPad    = 6 // plies of look-back, so that the root can index below itself
	lowPlySize = 5 // plies from the root that have a low-ply history
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
	h.LowPly = [lowPlySize][64][64]int16{}
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

	// P.MoveOrder 1 only: the moves 3 and 6 plies back, and this ply's
	// low-ply history (nil away from the root).
	contFar [2]*contRow
	low     *[64][64]int16
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
		if P.MoveOrder != 0 || P.HistUpdate != 0 {
			c.contFar = [2]*contRow{h.contStack[i-3], h.contStack[i-6]}
			if ply < lowPlySize {
				c.low = &h.LowPly[ply]
			}
		}
	}
	return c
}

// contCtx returns only the continuation-history rows of the node at ply,
// for updating the history of the move that node played from a node below.
func (h *SearchHistory) contCtx(ply int) histCtx {
	var c histCtx
	if ply >= 0 && ply < MaxPly {
		i := ply + contPad
		c.cont = [3]*contRow{h.contStack[i-1], h.contStack[i-2], h.contStack[i-4]}
		c.contFar = [2]*contRow{h.contStack[i-3], h.contStack[i-6]}
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

// OrderSF is the history part of the score quiet moves are sorted by under
// P.MoveOrder 1, weighted as Stockfish weights its tables: main and pawn
// history twice, the continuation histories 1, 2, 3, 4 and 6 plies back
// once, and the low-ply history near the root.
func (c *histCtx) OrderSF(piece types.Piece, m types.Move, ply int) int32 {
	to := m.To()
	s := 2*int32(*c.mainEntry(m)) + 2*int32(c.pawn[piece][to])
	for _, row := range c.cont {
		if row != nil {
			s += int32(row[piece][to])
		}
	}
	for _, row := range c.contFar {
		if row != nil {
			s += int32(row[piece][to])
		}
	}
	if c.low != nil {
		s += 8 * int32(c.low[m.From()][to]) / int32(1+ply)
	}
	return s
}

// Evasion is the score non-captures are sorted by when in check under
// P.MoveOrder 1: main history and the continuation history one ply back.
func (c *histCtx) Evasion(piece types.Piece, m types.Move) int32 {
	s := int32(*c.mainEntry(m))
	if c.cont[0] != nil {
		s += int32(c.cont[0][piece][m.To()])
	}
	return s
}

// Stat is the score late-move reductions are adjusted by (main and
// continuation history); Prune the one history pruning compares (the same
// without the move four plies back).
//
// Under P.HistUpdate 1 both are Stockfish's: Stat weighs main history about
// twice the continuation histories one and two plies back; Prune is those
// two continuation histories plus pawn history.
func (c *histCtx) Stat(piece types.Piece, m types.Move) int32 {
	if P.HistUpdate != 0 {
		to := m.To()
		s := 2252 * int32(*c.mainEntry(m))
		if c.cont[0] != nil {
			s += 1126 * int32(c.cont[0][piece][to])
		}
		if c.cont[1] != nil {
			s += 1093 * int32(c.cont[1][piece][to])
		}
		return s / 1024
	}
	s := c.Prune(piece, m)
	if c.cont[2] != nil {
		s += int32(c.cont[2][piece][m.To()])
	}
	return s
}

func (c *histCtx) Prune(piece types.Piece, m types.Move) int32 {
	to := m.To()
	if P.HistUpdate != 0 {
		s := int32(c.pawn[piece][to])
		if c.cont[0] != nil {
			s += int32(c.cont[0][piece][to])
		}
		if c.cont[1] != nil {
			s += int32(c.cont[1][piece][to])
		}
		return s
	}
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
	// The far continuation histories and the low-ply history learn more
	// slowly, about as Stockfish's do relative to its nearer ones.
	for _, row := range c.contFar {
		if row != nil {
			histGravity(&row[piece][to], bonus/2)
		}
	}
	if c.low != nil {
		histGravity(&c.low[m.From()][to], bonus*712/1024)
	}
}

// Stockfish's history updates (P.HistUpdate 1). Each table has its own
// bound, and the bonus a move earns is split between the tables in fixed
// proportions; the thresholds in picker_sf.go assume these scales.
const (
	sfMainLimit    = 7183 // main and low-ply history
	sfContLimit    = 30000
	sfPawnLimit    = 8192
	sfCaptureLimit = 10692
)

// sfGravity is Stockfish's history update: the entry moves by bonus, less
// the fraction |bonus|/limit of its value, which keeps it within +-limit.
func sfGravity(e *int16, bonus, limit int32) {
	bonus = max(-limit, min(bonus, limit))
	cur := int32(*e)
	if cur > limit {
		cur = limit // an entry written under the other update rule
	} else if cur < -limit {
		cur = -limit
	}
	*e = int16(cur + bonus - cur*abs(bonus)/limit)
}

// sfStatBonus and sfStatMalus are what the move that caused a cutoff earns
// and what the moves tried before it lose. tried counts those earlier moves.
//
// Stockfish also adds a 28th of the history score of the move that led to
// the node. Here that made ordering worse (first-move cutoffs 78% against
// 88%), so it is left out.
func sfStatBonus(depth int8, isTTMove, isPV bool, tried int) int32 {
	b := min(133*int32(depth)-81, 1487)
	if isTTMove {
		b += 364
	}
	if !isPV {
		b += b * int32(tried) / 256
	}
	return b
}

func sfStatMalus(depth int8) int32 {
	return min(968*int32(depth)-235, 2244)
}

// updateContSF spreads bonus over the continuation histories 1, 2, 3, 4 and
// 6 plies back (only the first two when in check), each with its own weight.
// The more of them already agree the move is good, the larger the step.
func (c *histCtx) updateContSF(piece types.Piece, to types.Square, bonus int32, inCheck bool) {
	rows := [5]*contRow{c.cont[0], c.cont[1], c.contFar[0], c.cont[2], c.contFar[1]}
	weights := [5]int32{520, 390, 145, 251, 209}
	mult := [6]int32{94, 103, 110, 106, 119, 126}
	positive := 0
	for i, row := range rows {
		if inCheck && i >= 2 {
			break
		}
		if row == nil {
			continue
		}
		e := &row[piece][to]
		if *e > 0 {
			positive++
		}
		b := bonus * weights[i] * mult[positive] / 65536
		if i == 0 {
			b += 73
		}
		sfGravity(e, b, sfContLimit)
	}
}

// UpdateSF moves every table's entry for the quiet move m by bonus (negative
// to penalize), as Stockfish's update_quiet_histories does.
func (c *histCtx) UpdateSF(piece types.Piece, m types.Move, bonus int32, inCheck bool) {
	to := m.To()
	sfGravity(c.mainEntry(m), bonus, sfMainLimit)
	if c.low != nil {
		sfGravity(&c.low[m.From()][to], bonus*712/1024, sfMainLimit)
	}
	c.updateContSF(piece, to, bonus*750/1024, inCheck)
	pawn := bonus * 459 / 1024
	if bonus > -4 {
		pawn = bonus * 1104 / 1024
	}
	sfGravity(&c.pawn[piece][to], pawn, sfPawnLimit)
}

// UpdateCaptureSF moves a capture's history entry by bonus.
func (h *SearchHistory) UpdateCaptureSF(attacker, victim types.PieceType, to types.Square, bonus int32) {
	if attacker >= 7 || victim >= 7 || to >= 64 {
		return
	}
	e := &h.CaptureHistory[attacker][to][victim]
	bonus = max(-sfCaptureLimit, min(bonus, sfCaptureLimit))
	cur := max(-sfCaptureLimit, min(*e, sfCaptureLimit))
	*e = cur + bonus - cur*abs(bonus)/sfCaptureLimit
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
