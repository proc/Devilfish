package nnue

import (
	"math/bits"
	"os"
	"unsafe"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

// Incremental evaluation for the 8Chess network.
//
// EvaluateEight rebuilds both accumulators from nothing: one 1024-wide vector
// add per piece feature and per threat feature, for each perspective. Almost
// all of those rows are the same as in the position searched one move earlier.
//
// EightState keeps one finished accumulator set per search ply, together with
// a snapshot of the position it belongs to. To build the accumulators for a
// new position it takes a stored one for a nearby position (normally the
// parent) and works out which features differ from the squares whose
// contents differ (D):
//
//   - Piece features differ only on D.
//   - A threat feature is an (attacker, attacked piece) pair. Between the two
//     positions, the pairs that differ are those whose attacker or target is
//     on D, plus those of sliders off D whose rays pass through D, limited to
//     the targets such a slider gained or lost. Every other pair is the same
//     in both positions, and the pairs listed for the old position and for
//     the new one never coincide, so the lists need no comparing.
//
// A perspective whose king has changed input bucket (or side of the board,
// or, for a Stockfish network, whose piece-square block has changed) has
// every piece feature renumbered. That accumulator is then built from a
// cache holding, per perspective and king bucket, the last accumulator made
// with that bucket and the pieces it was made from (the "Finny table" idea),
// which is again a difference of a few squares. A king changing side also
// renumbers that perspective's threat features; those are rebuilt in full.
//
// A Stockfish network has every row in one int8 table, so it keeps a single
// accumulator per perspective (bias, piece rows and threat rows together)
// and applies all the rows of an update in one pass: see sfApply.
//
// The result is bit-identical to EvaluateEight: accumulators are int16 sums
// with wrapping arithmetic, so the order of adds and subtracts cannot matter.

const (
	// eightMaxRows bounds the rows applied to one accumulator in one update.
	// A from-scratch threat accumulator stays well below this in real
	// positions; anything larger falls back to EvaluateEight.
	eightMaxRows = 512
)

var eightZeroAcc [EightL1Size]int16

// rowPrefetch enables prefetching the weight rows of each accumulator
// update. DEVILFISH_PREFETCH=0 turns it off, to measure its effect.
var rowPrefetch = os.Getenv("DEVILFISH_PREFETCH") != "0"

// eightSnap is the part of a position the feature differences are read from.
type eightSnap struct {
	pieces [64]types.Piece
	bb     [15]bitboard.Bitboard // indexed by types.Piece, as board.PieceBB
	occ    bitboard.Bitboard
}

func (s *eightSnap) load(b *board.Board) {
	s.pieces = b.Pieces
	s.bb = b.PieceBB
	s.occ = b.Occupied()
}

// eightView is how one perspective numbers its features in a position.
type eightView struct {
	bucket int  // king input bucket (piece features)
	flip   int  // square transform for piece features
	mirror bool // threat features see the board mirrored
	sf     bool // Stockfish feature numbering (see stockfish.go)
}

func eightViews(net *EightNetwork, b *board.Board) (v [2]eightView, ok bool) {
	k0, k1 := b.KingSquare(types.White), b.KingSquare(types.Black)
	if k0 >= 64 || k1 >= 64 {
		return v, false
	}
	if net.sf != nil {
		return [2]eightView{sfView(0, int(k0), sfBlock(b, 0)), sfView(1, int(k1), sfBlock(b, 1))}, true
	}
	v[0].mirror = int(k0)&7 >= 4
	v[1].mirror = int(k1)&7 >= 4
	if v[0].mirror {
		v[0].flip = 7
	}
	v[1].flip = 56
	if v[1].mirror {
		v[1].flip ^= 7
	}
	v[0].bucket = EightInputBucketsLayout[int(k0)^v[0].flip]
	v[1].bucket = EightInputBucketsLayout[int(k1)^v[1].flip]
	return v, true
}

// pieceFeature numbers piece p on sq from perspective pov.
func pieceFeature(v *eightView, pov int, p types.Piece, sq int) int32 {
	if v.sf {
		return sfPieceFeature(v, pov, p, sq)
	}
	color := int(p.Color()) ^ pov
	ptype := int(p.Type()) - 1
	return int32(v.bucket*768 + 384*color + 64*ptype + (sq ^ v.flip))
}

// pieceCode is the attacker/target numbering used by attacksWithOcc and
// getThreatIndex.
func pieceCode(p types.Piece) int {
	return (int(p.Type())-1)*2 + int(p.Color())
}

type eightPly struct {
	valid bool
	hash  uint64
	pst   [2][EightL1Size]int16 // piece accumulators; for a Stockfish network, the whole accumulators
	thr   [2][EightL1Size]int16 // threat accumulators (unused for a Stockfish network)
	snap  eightSnap
	view  [2]eightView
}

type finnyEntry struct {
	ready bool
	acc   [EightL1Size]int16
	snap  eightSnap // pieces acc was built from
}

// rowList collects weight-row indices for one fused update.
type rowList struct {
	n    int
	over bool
	rows [eightMaxRows]int32
}

// append adds every row of o.
func (l *rowList) append(o *rowList) {
	if l.n+o.n > eightMaxRows {
		l.over = true
		return
	}
	copy(l.rows[l.n:], o.rows[:o.n])
	l.n += o.n
}

func (l *rowList) push(r int32) {
	if l.n < eightMaxRows {
		l.rows[l.n] = r
		l.n++
	} else {
		l.over = true
	}
}

// EightState is the per-search-thread accumulator stack. It is not safe for
// concurrent use; give each worker its own.
type EightState struct {
	plies []eightPly
	net   *EightNetwork // network the finny entries were built for

	finny [2][sfKingBuckets][2]finnyEntry // [pov][bucket][mirror]; 8Chess networks use the first EightInputBuckets

	pieceAdd, pieceRem [2]rowList
	thrAdd, thrRem     [2]rowList
	// Stockfish networks, a perspective whose piece numbering changed: the
	// piece rows that take the cache entry for the old numbering to the base
	// position (see sfApply).
	oldAdd, oldRem [2]rowList
	hybrid         [2]bool
	pair           [2][EightL1Size]int16
	sfScratch      sfScratch

	// Statistics.
	Evals          uint64 // accumulator sets produced
	RowOps         uint64 // weight rows applied
	FinnyRefreshes uint64 // piece accumulators taken from the king-bucket cache
	ThreatRebuilds uint64 // threat accumulators rebuilt from nothing
	Hybrids        uint64 // Stockfish networks: piece rows renumbered, threat rows carried over
}

// NewEightState allocates an accumulator stack for maxPly search plies.
func NewEightState(maxPly int) *EightState {
	return &EightState{plies: make([]eightPly, maxPly)}
}

// Reset forgets every stored accumulator.
func (s *EightState) Reset() {
	for i := range s.plies {
		s.plies[i].valid = false
	}
}

// Has reports whether the accumulator stored at ply belongs to the position
// with the given hash.
func (s *EightState) Has(ply int, hash uint64) bool {
	return ply >= 0 && ply < len(s.plies) && s.plies[ply].valid && s.plies[ply].hash == hash
}

// Evaluate returns the same value as EvaluateEight(net, b), reusing stored
// accumulators. ply is the search ply of b. If hasParent is set, parentHash is
// the hash of the position one move before b on the current search path; when
// the accumulator stored at ply-1 belongs to that position it is used as the
// starting point. Otherwise the accumulator last stored at this ply (a sibling
// or near relative) is used.
//
// Stored accumulators are recognised by position hash, as the transposition
// table is.
func (s *EightState) Evaluate(net *EightNetwork, b *board.Board, ply int, parentHash uint64, hasParent bool) int32 {
	if net == nil {
		return 0
	}
	if !s.Prepare(net, b, ply, parentHash, hasParent) {
		return EvaluateEight(net, b)
	}
	cur := &s.plies[ply]
	occCount := bits.OnesCount64(uint64(b.Occupied()))
	if net.sf != nil {
		return sfToCP(sfForward(net, &s.sfScratch, &cur.pst, b, occCount), b)
	}
	return eightForward(net, &cur.pst, &cur.thr, b, occCount)
}

// Prepare makes sure the accumulator stored at ply is the one for b, without
// running the rest of the network. It returns false if b cannot be handled
// incrementally (the caller then evaluates from scratch).
func (s *EightState) Prepare(net *EightNetwork, b *board.Board, ply int, parentHash uint64, hasParent bool) bool {
	if net == nil || ply < 0 || ply >= len(s.plies) {
		return false
	}
	cur := &s.plies[ply]
	if cur.valid && cur.hash == b.Hash {
		return true
	}
	initEightTables()
	if s.net != net {
		s.resetFinny(net)
	}

	views, ok := eightViews(net, b)
	if !ok {
		cur.valid = false
		return false
	}

	var base *eightPly
	if hasParent && ply > 0 && s.plies[ply-1].valid && s.plies[ply-1].hash == parentHash {
		base = &s.plies[ply-1]
	} else if cur.valid {
		base = cur
	}

	var cs eightSnap
	cs.load(b)
	tables := net.threatTables()

	// Work out every row change before writing anything: base may be cur.
	var pieceInc, thrInc [2]bool
	for pov := 0; pov < 2; pov++ {
		s.pieceAdd[pov].n, s.pieceRem[pov].n = 0, 0
		s.thrAdd[pov].n, s.thrRem[pov].n = 0, 0
		s.pieceAdd[pov].over, s.pieceRem[pov].over = false, false
		s.thrAdd[pov].over, s.thrRem[pov].over = false, false
		if base != nil {
			pieceInc[pov] = base.view[pov] == views[pov]
			thrInc[pov] = base.view[pov].mirror == views[pov].mirror
		}
		s.hybrid[pov] = false
		if net.sf != nil && !pieceInc[pov] && thrInc[pov] {
			// The accumulator holds piece and threat rows together, so the
			// threat rows can only be carried over if the old piece rows can
			// be taken out again (sfHybrid); otherwise they are all rebuilt.
			s.hybrid[pov] = s.sfHybrid(base, pov)
			thrInc[pov] = s.hybrid[pov]
		}
	}
	if base != nil {
		d := changedSquares(&base.snap, &cs)
		for pov := 0; pov < 2; pov++ {
			if pieceInc[pov] {
				pieceDelta(&base.snap, &cs, d, &views[pov], pov, &s.pieceAdd[pov], &s.pieceRem[pov])
			}
		}
		if thrInc[0] || thrInc[1] {
			threatDelta(tables, &base.snap, &cs, d, &views, thrInc, &s.thrAdd, &s.thrRem)
			if net.sf != nil {
				pawnPairs(&base.snap, d, &views, thrInc, &s.thrRem)
				pawnPairs(&cs, d, &views, thrInc, &s.thrAdd)
			}
		}
	}
	if full := [2]bool{!thrInc[0], !thrInc[1]}; net.sf != nil && (full[0] || full[1]) {
		pawnPairs(&cs, ^bitboard.Bitboard(0), &views, full, &s.thrAdd)
	}
	for pov := 0; pov < 2; pov++ {
		if !thrInc[pov] {
			fullThreats(tables, &cs, &views[pov], pov, &s.thrAdd[pov])
		}
		if s.thrAdd[pov].over || s.thrRem[pov].over || s.pieceAdd[pov].over || s.pieceRem[pov].over {
			cur.valid = false
			return false
		}
	}

	// Start fetching every weight row this update will read, before reading
	// any: the rows are scattered over 80 to 200 MB, so most come from L3 or
	// memory, and issuing the requests together lets them overlap. The first
	// two cache lines of each row are enough; the hardware prefetcher
	// follows the rest of a row once it is being read. (Measured: fetching
	// whole rows up front is slower than not prefetching at all.)
	for pov := 0; pov < 2 && rowPrefetch; pov++ {
		if pieceInc[pov] {
			a, r := &s.pieceAdd[pov], &s.pieceRem[pov]
			if net.sf != nil {
				prefetchRows(unsafe.Pointer(&net.FTThreatWeights[0]), &a.rows[0], a.n, 10, 2)
				prefetchRows(unsafe.Pointer(&net.FTThreatWeights[0]), &r.rows[0], r.n, 10, 2)
			} else {
				prefetchRows(unsafe.Pointer(&net.FTPieceWeights[0]), &a.rows[0], a.n, 11, 2)
				prefetchRows(unsafe.Pointer(&net.FTPieceWeights[0]), &r.rows[0], r.n, 11, 2)
			}
		}
		a, r := &s.thrAdd[pov], &s.thrRem[pov]
		prefetchRows(unsafe.Pointer(&net.FTThreatWeights[0]), &a.rows[0], a.n, 10, 2)
		prefetchRows(unsafe.Pointer(&net.FTThreatWeights[0]), &r.rows[0], r.n, 10, 2)
	}

	s.Evals++
	if net.sf != nil {
		if !s.sfApply(net, base, cur, &cs, &views, pieceInc) {
			cur.valid = false
			return false
		}
		cur.snap = cs
		cur.view = views
		cur.hash = b.Hash
		cur.valid = true
		return true
	}
	for pov := 0; pov < 2; pov++ {
		if pieceInc[pov] {
			a, r := &s.pieceAdd[pov], &s.pieceRem[pov]
			net.applyPieceRows(&cur.pst[pov], &base.pst[pov], a, r)
			s.RowOps += uint64(a.n + r.n)
		} else {
			cur.pst[pov] = s.finnyRefresh(net, &cs, &views[pov], pov).acc
		}

		a, r := &s.thrAdd[pov], &s.thrRem[pov]
		src := &eightZeroAcc
		if thrInc[pov] {
			src = &base.thr[pov]
		} else {
			s.ThreatRebuilds++
		}
		fusedThreat(&cur.thr[pov], src, &net.FTThreatWeights[0], &a.rows[0], a.n, &r.rows[0], r.n)
		s.RowOps += uint64(a.n + r.n)
	}

	cur.snap = cs
	cur.view = views
	cur.hash = b.Hash
	cur.valid = true
	return true
}

// applyPieceRows sets dst to src plus the piece-square rows in add minus
// those in rem. dst may be src.
func (net *EightNetwork) applyPieceRows(dst, src *[EightL1Size]int16, add, rem *rowList) {
	if net.sf != nil {
		// int8 rows, in the same table as the threat rows: the threat kernel.
		fusedThreat(dst, src, &net.FTThreatWeights[0], &add.rows[0], add.n, &rem.rows[0], rem.n)
		return
	}
	fusedPiece(dst, src, &net.FTPieceWeights[0], &add.rows[0], add.n, &rem.rows[0], rem.n)
}

// changedSquares returns the squares whose contents differ between a and b.
func changedSquares(a, b *eightSnap) bitboard.Bitboard {
	var d bitboard.Bitboard
	for p := range a.bb {
		d |= a.bb[p] ^ b.bb[p]
	}
	return d
}

// pieceDelta lists the piece-feature rows to remove (old pieces on d) and add
// (new pieces on d) for one perspective whose numbering has not changed.
func pieceDelta(old, cur *eightSnap, d bitboard.Bitboard, v *eightView, pov int, add, rem *rowList) {
	for d != 0 {
		sq := int(bitboard.PopLSB(&d))
		if p := old.pieces[sq]; p != types.PieceNone {
			rem.push(pieceFeature(v, pov, p, sq))
		}
		if p := cur.pieces[sq]; p != types.PieceNone {
			add.push(pieceFeature(v, pov, p, sq))
		}
	}
}

// finnyEntry returns the cache entry for perspective pov numbered as v.
func (s *EightState) finnyEntry(v *eightView, pov int) *finnyEntry {
	m := 0
	if v.mirror {
		m = 1
	}
	return &s.finny[pov][v.bucket][m]
}

// finnyRefresh builds the piece accumulator (bias plus piece rows) for a
// perspective whose king bucket or side differs from the stored
// accumulator's, starting from the cached accumulator for that bucket and
// side. It returns the cache entry, which then holds that accumulator.
func (s *EightState) finnyRefresh(net *EightNetwork, cs *eightSnap, v *eightView, pov int) *finnyEntry {
	e := s.finnyEntry(v, pov)
	if !e.ready {
		e.acc = net.FTBiases
		e.snap = eightSnap{}
		e.ready = true
	}
	d := changedSquares(&e.snap, cs)
	a, r := &s.pieceAdd[pov], &s.pieceRem[pov]
	a.n, r.n = 0, 0
	pieceDelta(&e.snap, cs, d, v, pov, a, r)
	net.applyPieceRows(&e.acc, &e.acc, a, r)
	e.snap = *cs
	s.RowOps += uint64(a.n + r.n)
	s.FinnyRefreshes++
	return e
}

// sfHybridMaxRows is the most piece rows sfHybrid will spend on taking the
// old numbering's cache entry to the base position. Beyond it, rebuilding
// the threat rows costs about the same.
const sfHybridMaxRows = 12

// sfHybrid reports whether a perspective whose piece numbering differs from
// base's (its king moved, or its piece-square block changed) can still start
// from base's accumulator. That needs base's piece rows taken out again,
// which the cache entry for base's numbering gives if it is close to base's
// position; the rows between the two are left in s.oldAdd and s.oldRem.
func (s *EightState) sfHybrid(base *eightPly, pov int) bool {
	old := s.finnyEntry(&base.view[pov], pov)
	if !old.ready {
		return false
	}
	a, r := &s.oldAdd[pov], &s.oldRem[pov]
	a.n, r.n, a.over, r.over = 0, 0, false, false
	d := changedSquares(&old.snap, &base.snap)
	if bitboard.Count(d) > sfHybridMaxRows {
		return false
	}
	pieceDelta(&old.snap, &base.snap, d, &base.view[pov], pov, a, r)
	return a.n+r.n <= sfHybridMaxRows
}

// sfApply writes the accumulators of a Stockfish network, which hold bias,
// piece rows and threat rows together, from the row lists Prepare made. For
// each perspective one of:
//
//   - numbering unchanged: base's accumulator plus every changed row, in one
//     pass;
//   - piece numbering changed, threat numbering not, and sfHybrid allowed
//     it: base's accumulator, minus base's piece rows (the cache entry for
//     the old numbering, corrected to base's position), plus the new piece
//     accumulator from the cache, plus the changed threat rows;
//   - otherwise: the new piece accumulator from the cache plus every threat
//     row.
//
// It returns false if a row list overflowed.
func (s *EightState) sfApply(net *EightNetwork, base, cur *eightPly, cs *eightSnap, views *[2]eightView, pieceInc [2]bool) bool {
	w := &net.FTThreatWeights[0]
	for pov := 0; pov < 2; pov++ {
		a, r := &s.thrAdd[pov], &s.thrRem[pov]
		dst := &cur.pst[pov]
		switch {
		case pieceInc[pov]:
			// The threat numbering is part of the view, so it is unchanged too.
			a.append(&s.pieceAdd[pov])
			r.append(&s.pieceRem[pov])
			if a.over || r.over {
				return false
			}
			fusedThreat(dst, &base.pst[pov], w, &a.rows[0], a.n, &r.rows[0], r.n)
		case s.hybrid[pov]:
			// base = old entry + oldAdd - oldRem + threats, so
			// dst = base - old entry - oldAdd + oldRem + new entry + threat changes.
			old := s.finnyEntry(&base.view[pov], pov)
			s.Hybrids++
			a.append(&s.oldRem[pov])
			r.append(&s.oldAdd[pov])
			if a.over || r.over {
				return false
			}
			fusedThreat(dst, &base.pst[pov], w, &a.rows[0], a.n, &r.rows[0], r.n)
			e := s.finnyRefresh(net, cs, &views[pov], pov)
			s.pair[0], s.pair[1] = e.acc, old.acc
			plus, minus := [1]int32{0}, [1]int32{1}
			fusedPiece(dst, dst, &s.pair[0][0], &plus[0], 1, &minus[0], 1)
		default:
			e := s.finnyRefresh(net, cs, &views[pov], pov)
			s.ThreatRebuilds++
			fusedThreat(dst, &e.acc, w, &a.rows[0], a.n, &r.rows[0], r.n)
		}
		s.RowOps += uint64(a.n + r.n)
	}
	return true
}

func (s *EightState) resetFinny(net *EightNetwork) {
	for pov := range s.finny {
		for b := range s.finny[pov] {
			for m := range s.finny[pov][b] {
				s.finny[pov][b][m].ready = false
			}
		}
	}
	s.net = net
}

// emitThreat adds the features for attacker a (code ac) attacking target t
// (code tc) to the lists of the perspectives in want.
func emitThreat(tt *threatTables, ac, a, tc, t int, views *[2]eightView, want [2]bool, out *[2]rowList) {
	for pov := 0; pov < 2; pov++ {
		if want[pov] {
			if idx := getThreatIndex(tt, ac, a, tc, t, views[pov].mirror, pov); idx >= 0 {
				out[pov].push(int32(idx))
			}
		}
	}
}

// threatsOf lists the threat pairs of position x that can differ from the
// other position: attackers on d, targets on d, and the targets gained or
// lost by the sliders in sl. otherOcc is the other position's occupancy.
func threatsOf(tt *threatTables, x *eightSnap, otherOcc, d, sl bitboard.Bitboard, views *[2]eightView, want [2]bool, out *[2]rowList) {
	occ := uint64(x.occ)

	// Attackers on d: every target.
	for a := d & x.occ; a != 0; {
		sq := int(bitboard.PopLSB(&a))
		ac := pieceCode(x.pieces[sq])
		for t := attacksWithOcc(ac, sq, occ) & occ; t != 0; t &= t - 1 {
			to := bits.TrailingZeros64(t)
			emitThreat(tt, ac, sq, pieceCode(x.pieces[to]), to, views, want, out)
		}
	}

	// Sliders whose attacks changed: the targets they gained or lost, and any
	// target on d.
	for a := sl; a != 0; {
		sq := int(bitboard.PopLSB(&a))
		ac := pieceCode(x.pieces[sq])
		atk := attacksWithOcc(ac, sq, occ)
		changed := atk ^ attacksWithOcc(ac, sq, uint64(otherOcc))
		for t := atk & (changed | uint64(d)) & occ; t != 0; t &= t - 1 {
			to := bits.TrailingZeros64(t)
			emitThreat(tt, ac, sq, pieceCode(x.pieces[to]), to, views, want, out)
		}
	}

	// Other attackers of pieces on d.
	skip := d | sl
	for t := d & x.occ; t != 0; {
		to := types.Square(bitboard.PopLSB(&t))
		tc := pieceCode(x.pieces[to])
		for a := attackersTo(x, to) &^ skip; a != 0; {
			sq := int(bitboard.PopLSB(&a))
			emitThreat(tt, pieceCode(x.pieces[sq]), sq, tc, int(to), views, want, out)
		}
	}
}

// attackersTo returns every piece of either colour in x that attacks sq, by
// the same rules as attacksWithOcc.
func attackersTo(x *eightSnap, sq types.Square) bitboard.Bitboard {
	bb := &x.bb
	diag := bb[types.WhiteBishop] | bb[types.BlackBishop] | bb[types.WhiteQueen] | bb[types.BlackQueen]
	orth := bb[types.WhiteRook] | bb[types.BlackRook] | bb[types.WhiteQueen] | bb[types.BlackQueen]
	return bitboard.PawnAttacksTable[types.Black][sq]&bb[types.WhitePawn] |
		bitboard.PawnAttacksTable[types.White][sq]&bb[types.BlackPawn] |
		bitboard.KnightAttacksTable[sq]&(bb[types.WhiteKnight]|bb[types.BlackKnight]) |
		bitboard.KingAttacksTable[sq]&(bb[types.WhiteKing]|bb[types.BlackKing]) |
		bitboard.BishopAttacks(sq, x.occ)&diag |
		bitboard.RookAttacks(sq, x.occ)&orth
}

// threatDelta lists, for the perspectives in want, the threat rows to remove
// (pairs that hold in old but not in cur) and to add (the reverse).
func threatDelta(tt *threatTables, old, cur *eightSnap, d bitboard.Bitboard, views *[2]eightView, want [2]bool, add, rem *[2]rowList) {
	if d == 0 {
		return
	}
	// Sliders off d (so the same piece in both positions) with a ray through
	// d: their attack sets may differ. Found from d by the reverse lookup.
	bb := &old.bb
	diag := bb[types.WhiteBishop] | bb[types.BlackBishop] | bb[types.WhiteQueen] | bb[types.BlackQueen]
	orth := bb[types.WhiteRook] | bb[types.BlackRook] | bb[types.WhiteQueen] | bb[types.BlackQueen]
	var sl bitboard.Bitboard
	for x := d; x != 0; {
		sq := types.Square(bitboard.PopLSB(&x))
		sl |= bitboard.BishopAttacks(sq, old.occ)&diag | bitboard.RookAttacks(sq, old.occ)&orth
	}
	sl &^= d

	threatsOf(tt, old, cur.occ, d, sl, views, want, rem)
	threatsOf(tt, cur, old.occ, d, sl, views, want, add)
}

// fullThreats lists every threat feature of x for one perspective.
func fullThreats(tt *threatTables, x *eightSnap, v *eightView, pov int, out *rowList) {
	occ := uint64(x.occ)
	for a := x.occ; a != 0; {
		sq := int(bitboard.PopLSB(&a))
		ac := pieceCode(x.pieces[sq])
		for t := attacksWithOcc(ac, sq, occ) & occ; t != 0; t &= t - 1 {
			to := bits.TrailingZeros64(t)
			if idx := getThreatIndex(tt, ac, sq, pieceCode(x.pieces[to]), to, v.mirror, pov); idx >= 0 {
				out.push(int32(idx))
			}
		}
	}
}
