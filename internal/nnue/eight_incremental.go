package nnue

import (
	"math/bits"

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
// EightState keeps one finished accumulator set per search ply together with
// the list of feature indices it was built from. To evaluate a position it
// lists the position's features (cheap: table lookups only), compares that
// list with the list of a nearby position, and applies only the rows that
// differ. The comparison is position against position, so it needs no
// knowledge of the move played and is correct for any pair of positions; a
// king crossing a bucket or the mirror line simply shows up as a large
// difference and that accumulator is rebuilt.
//
// The result is bit-identical to EvaluateEight: accumulators are int16 sums
// with wrapping arithmetic, so the order of adds and subtracts cannot matter.

const (
	eightMaxPieces  = 32
	eightMaxThreats = 256
)

// eightFeatures is the feature index list of one position, per perspective.
type eightFeatures struct {
	pc   [2][eightMaxPieces]int32
	thr  [2][eightMaxThreats]int32
	npc  int
	nthr [2]int
}

type eightPly struct {
	valid bool
	hash  uint64
	pst   [2][EightL1Size]int16
	thr   [2][EightL1Size]int16
	feats eightFeatures
}

// EightState is the per-search-thread accumulator stack. It is not safe for
// concurrent use; give each worker its own.
type EightState struct {
	plies []eightPly
	tmp   eightFeatures

	// Scratch multiset counters used to diff two feature lists. Always left
	// zeroed between calls.
	pcCnt  [EightPieceFeatures]uint8
	thrCnt [EightThreatFeatures]uint8

	addBuf [eightMaxThreats]int32
	remBuf [eightMaxThreats]int32

	// Statistics.
	Evals      uint64 // accumulator sets produced
	Rebuilds   uint64 // accumulators rebuilt from nothing (out of 4 per eval)
	RowOps     uint64 // vector adds/subs actually performed
	ScratchOps uint64 // vector adds a from-scratch evaluation would have needed
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

// eightListFeatures fills f with the feature indices of b. It returns false if
// the position cannot be handled here (missing king, impossible piece counts).
func eightListFeatures(b *board.Board, f *eightFeatures) bool {
	kingSq0 := b.KingSquare(types.White)
	kingSq1 := b.KingSquare(types.Black)
	if kingSq0 >= 64 || kingSq1 >= 64 {
		return false
	}

	kingside0 := (int(kingSq0) & 7) >= 4
	kingside1 := (int(kingSq1) & 7) >= 4

	flip0 := 0
	if kingside0 {
		flip0 = 7
	}
	flip1 := 56
	if kingside1 {
		flip1 ^= 7
	}

	bucket0 := EightInputBucketsLayout[int(kingSq0)^flip0]
	bucket1 := EightInputBucketsLayout[int(kingSq1)^flip1]

	occBB := b.Occupied()
	occ := uint64(occBB)

	npc := 0
	n0, n1 := 0, 0

	occScan := occBB
	for occScan != 0 {
		sq := int(bitboard.PopLSB(&occScan))
		piece := b.Pieces[sq]
		if piece == types.PieceNone {
			continue
		}
		if npc >= eightMaxPieces {
			return false
		}

		pColor := int(piece.Color())
		ptype := int(piece.Type()) - 1
		pieceCode := ptype*2 + pColor

		f.pc[0][npc] = int32(bucket0*768 + 384*pColor + 64*ptype + (sq ^ flip0))
		f.pc[1][npc] = int32(bucket1*768 + 384*(pColor^1) + 64*ptype + (sq ^ flip1))
		npc++

		targets := attacksWithOcc(pieceCode, sq, occ) & occ
		for targets != 0 {
			to := bits.TrailingZeros64(targets)
			targets &= targets - 1

			targetObj := b.Pieces[to]
			if targetObj == types.PieceNone {
				continue
			}
			attackedCode := (int(targetObj.Type())-1)*2 + int(targetObj.Color())

			if idx := getThreatIndex(pieceCode, sq, attackedCode, to, kingside0, 0); idx >= 0 {
				if n0 >= eightMaxThreats {
					return false
				}
				f.thr[0][n0] = int32(idx)
				n0++
			}
			if idx := getThreatIndex(pieceCode, sq, attackedCode, to, kingside1, 1); idx >= 0 {
				if n1 >= eightMaxThreats {
					return false
				}
				f.thr[1][n1] = int32(idx)
				n1++
			}
		}
	}

	f.npc = npc
	f.nthr[0] = n0
	f.nthr[1] = n1
	return true
}

// diffLists computes the multiset difference between old and cur using cnt as
// scratch (cnt must be all zero and is left all zero). It returns the number of
// entries written to s.addBuf (in cur but not old) and s.remBuf (in old but not
// cur).
func (s *EightState) diffLists(cnt []uint8, old, cur []int32) (nAdd, nRem int) {
	for _, i := range old {
		cnt[i]++
	}
	for _, i := range cur {
		if cnt[i] > 0 {
			cnt[i]--
		} else {
			s.addBuf[nAdd] = i
			nAdd++
		}
	}
	for _, i := range old {
		if cnt[i] > 0 {
			cnt[i]--
			s.remBuf[nRem] = i
			nRem++
		}
	}
	return
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
	return eightForward(net, &cur.pst, &cur.thr, b, bits.OnesCount64(uint64(b.Occupied())))
}

// Prepare makes sure the accumulator stored at ply is the one for b, without
// running the rest of the network. The search calls it before expanding a node
// whose own evaluation was skipped (side to move in check) or answered from
// the eval cache, so that the node's children can each be built from a
// position one move away rather than from an unrelated one. It returns false
// if b cannot be handled incrementally.
func (s *EightState) Prepare(net *EightNetwork, b *board.Board, ply int, parentHash uint64, hasParent bool) bool {
	if net == nil || ply < 0 || ply >= len(s.plies) {
		return false
	}
	cur := &s.plies[ply]
	if cur.valid && cur.hash == b.Hash {
		return true
	}
	initEightTables()

	nf := &s.tmp
	if !eightListFeatures(b, nf) {
		cur.valid = false
		return false
	}

	var base *eightPly
	if hasParent && ply > 0 && s.plies[ply-1].valid && s.plies[ply-1].hash == parentHash {
		base = &s.plies[ply-1]
	} else if cur.valid {
		base = cur
	}

	s.Evals++
	s.ScratchOps += uint64(2*nf.npc + nf.nthr[0] + nf.nthr[1])

	for pov := 0; pov < 2; pov++ {
		// Piece accumulator.
		newPc := nf.pc[pov][:nf.npc]
		done := false
		if base != nil {
			nAdd, nRem := s.diffLists(s.pcCnt[:], base.feats.pc[pov][:base.feats.npc], newPc)
			if nAdd+nRem < len(newPc) {
				if base != cur {
					cur.pst[pov] = base.pst[pov]
				}
				for _, i := range s.remBuf[:nRem] {
					subVector1024NEON(&cur.pst[pov], &net.FTPieceWeights[int(i)*EightL1Size])
				}
				for _, i := range s.addBuf[:nAdd] {
					addVector1024NEON(&cur.pst[pov], &net.FTPieceWeights[int(i)*EightL1Size])
				}
				s.RowOps += uint64(nAdd + nRem)
				done = true
			}
		}
		if !done {
			cur.pst[pov] = net.FTBiases
			for _, i := range newPc {
				addVector1024NEON(&cur.pst[pov], &net.FTPieceWeights[int(i)*EightL1Size])
			}
			s.RowOps += uint64(len(newPc))
			s.Rebuilds++
		}

		// Threat accumulator.
		newThr := nf.thr[pov][:nf.nthr[pov]]
		done = false
		if base != nil {
			nAdd, nRem := s.diffLists(s.thrCnt[:], base.feats.thr[pov][:base.feats.nthr[pov]], newThr)
			if nAdd+nRem <= len(newThr) {
				if base != cur {
					cur.thr[pov] = base.thr[pov]
				}
				for _, i := range s.remBuf[:nRem] {
					subThreatVector1024NEON(&cur.thr[pov], &net.FTThreatWeights[int(i)*EightL1Size])
				}
				for _, i := range s.addBuf[:nAdd] {
					addThreatVector1024NEON(&cur.thr[pov], &net.FTThreatWeights[int(i)*EightL1Size])
				}
				s.RowOps += uint64(nAdd + nRem)
				done = true
			}
		}
		if !done {
			cur.thr[pov] = [EightL1Size]int16{}
			for _, i := range newThr {
				addThreatVector1024NEON(&cur.thr[pov], &net.FTThreatWeights[int(i)*EightL1Size])
			}
			s.RowOps += uint64(len(newThr))
			s.Rebuilds++
		}
	}

	// Remember what this accumulator was built from.
	cur.feats.npc = nf.npc
	cur.feats.nthr = nf.nthr
	for pov := 0; pov < 2; pov++ {
		copy(cur.feats.pc[pov][:nf.npc], nf.pc[pov][:nf.npc])
		copy(cur.feats.thr[pov][:nf.nthr[pov]], nf.thr[pov][:nf.nthr[pov]])
	}
	cur.hash = b.Hash
	cur.valid = true
	return true
}
