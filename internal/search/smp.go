package search

import (
	"sync/atomic"

	"devilfish/internal/types"
)

// Coordination between the threads of one search, beyond the shared hash
// table: which moves are being searched right now (P.SMPDefer), and the
// deepest iteration a helper has finished (P.SMPAdopt).

// busySlots is the size of the table of moves being searched. Only nodes of
// at least P.SMPDeferDepth plies mark their moves, a few dozen per thread at
// a time, so a small table is enough; a collision only puts off a move.
// Each slot is padded to 64 bytes to prevent false sharing across cores.
const busySlots = 1 << 12

type busySlot struct {
	key atomic.Uint64
	_   [56]byte
}

// busyTable records the (position, move) pairs some thread is searching.
// Each slot holds the key of the pair that last claimed it, or 0.
type busyTable struct {
	slots []busySlot
}

func newBusyTable() *busyTable {
	return &busyTable{slots: make([]busySlot, busySlots)}
}

// busyKey identifies move m in the position with hash. Never 0.
func busyKey(hash uint64, m types.Move) uint64 {
	k := hash ^ (uint64(m)+1)*0x9E3779B97F4A7C15
	if k == 0 {
		k = 1
	}
	return k
}

func (t *busyTable) slot(k uint64) *atomic.Uint64 {
	return &t.slots[k>>(64-12)].key
}

// busy reports whether a thread is searching the pair with key k.
func (t *busyTable) busy(k uint64) bool { return t.slot(k).Load() == k }

// mark records that the calling thread is searching the pair with key k.
func (t *busyTable) mark(k uint64) { t.slot(k).Store(k) }

// unmark ends mark, unless another pair has claimed the slot since.
func (t *busyTable) unmark(k uint64) {
	s := t.slot(k)
	if s.Load() == k {
		s.CompareAndSwap(k, 0)
	}
}

func (t *busyTable) clear() { clear(t.slots) }

// deferredMove is a move put off because another thread was searching it,
// with the SEE value the move picker gave it.
type deferredMove struct {
	m        types.Move
	see      int32
	seeKnown bool
}

// maxDeferred bounds the moves one node puts off; beyond it they are
// searched in their turn.
const maxDeferred = 32

// helperResult packs a helper's finished iteration for the main thread:
// depth in bits 32-39, score in the low 32 bits. 0 means none.
func packHelperResult(depth int8, score int32) uint64 {
	return uint64(uint8(depth))<<32 | uint64(uint32(score))
}

func unpackHelperResult(v uint64) (depth int8, score int32) {
	return int8(uint8(v >> 32)), int32(uint32(v))
}

// publishHelperResult records a helper's finished iteration if it is deeper
// than any recorded so far in this search.
func (e *Engine) publishHelperResult(depth int8, score int32) {
	v := packHelperResult(depth, score)
	for {
		old := e.helperBest.Load()
		if old != 0 {
			if d, _ := unpackHelperResult(old); d >= depth {
				return
			}
		}
		if e.helperBest.CompareAndSwap(old, v) {
			return
		}
	}
}
