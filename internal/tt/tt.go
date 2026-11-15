package tt

import (
	"math/bits"
	"time"
	"unsafe"

	"devilfish/internal/hugemem"
	"devilfish/internal/types"
)

const (
	Exact      uint8 = 0
	LowerBound uint8 = 1
	UpperBound uint8 = 2

	// PVFlag may be added to the bound a position is stored with: the
	// position has been on a principal variation. ProbeEval returns it with
	// the bound; Probe strips it. Once set it stays while the entry is only
	// refreshed, not replaced.
	PVFlag    uint8 = 4
	BoundMask uint8 = 3

	MateScore     int32 = 30000
	MateThreshold int32 = 29000
)

func ScoreToTT(score int32, ply int) int32 {
	if score > MateThreshold {
		return score + int32(ply)
	}
	if score < -MateThreshold {
		return score - int32(ply)
	}
	return score
}

func ScoreFromTT(score int32, ply int) int32 {
	if score > MateThreshold {
		return score - int32(ply)
	}
	if score < -MateThreshold {
		return score + int32(ply)
	}
	return score
}

// EvalNone marks an entry without a static evaluation (e.g. side in check).
const EvalNone int32 = -32768

// entry is one stored position, 10 bytes, laid out as Stockfish's:
//
//	key    upper 16 bits of the hash (the bucket index comes from the lower bits)
//	move   best move
//	score  search score; every score is within +-Infinity (32000)
//	eval   static evaluation, or EvalNone
//	depth  search depth plus one; 0 marks an empty entry
//	info   bound type and PV flag (low 4 bits), age (high 4 bits)
//
// Sixteen bits of key are not proof of identity: about one probe in eleven
// thousand that should miss matches another position's entry instead. Its
// score is then used as if it were this position's, as in Stockfish; its
// move is checked for legality by the search before it is played.
type entry struct {
	key   uint16
	move  uint16
	score int16
	eval  int16
	depth uint8
	info  uint8
}

// bucketSize entries share one 64-byte cache line. A position maps to one
// bucket and may sit in any of its entries, so a deep result is not lost the
// moment another position lands on the same index. (Entries used to be 16
// bytes, four to a line: the same memory now holds half as many again.)
const bucketSize = 6

type bucket struct {
	e [bucketSize]entry
	_ [64 - bucketSize*10]byte
}

func (e *entry) empty() bool      { return e.depth == 0 }
func (e *entry) age() uint8       { return e.info >> 4 }
func (e *entry) nodeType() uint8  { return e.info & 0x0F }
func (e *entry) searched() int8   { return int8(e.depth - 1) }
func entryKey(hash uint64) uint16 { return uint16(hash >> 48) }

func makeEntry(hash uint64, m types.Move, score, eval int32, depth int8, nodeType, age uint8) entry {
	if depth < 0 {
		depth = 0
	}
	return entry{
		key:   entryKey(hash),
		move:  uint16(m),
		score: int16(score),
		eval:  int16(eval),
		depth: uint8(depth) + 1,
		info:  nodeType&0x0F | age<<4,
	}
}

type TT struct {
	buckets []bucket
	mask    uint64 // bucket index mask
	age     uint8
	mem     hugemem.Block // backing memory (huge pages where available)
}

// bucketCount returns the power-of-two number of buckets for a table of
// sizeMB.
func bucketCount(sizeMB int) uint64 {
	if sizeMB < 1 {
		sizeMB = 1
	}
	n := uint64(sizeMB) * 1024 * 1024 / 64

	// Round to nearest power of 2 (round up if within 25% of next power of 2)
	pow2 := uint64(1) << (63 - bits.LeadingZeros64(n))
	if pow2*2 <= n || float64(n)/float64(pow2*2) >= 0.75 {
		pow2 *= 2
	}
	return pow2
}

func New(sizeMB int) *TT {
	n := bucketCount(sizeMB)
	buckets, mem := hugemem.Make[bucket](int(n))
	return &TT{
		buckets: buckets,
		mask:    n - 1,
		age:     0,
		mem:     mem,
	}
}

func (tt *TT) SizeMB() int {
	return int((uint64(len(tt.buckets)) * 64) / (1024 * 1024))
}

// Entries returns how many positions the table can hold.
func (tt *TT) Entries() int { return len(tt.buckets) * bucketSize }

func (tt *TT) Resize(sizeMB int) {
	n := bucketCount(sizeMB)
	old := tt.mem
	tt.buckets, tt.mem = hugemem.Make[bucket](int(n))
	tt.mask = n - 1
	tt.age = 0
	// A search thread may still be inside a probe of the old table (GUIs can
	// change Hash while pondering), so release it only after a grace period.
	time.AfterFunc(10*time.Second, old.Free)
}

func (tt *TT) Clear() {
	clear(tt.buckets)
	tt.age = 0
}

func (tt *TT) IncrementAge() {
	tt.age = (tt.age + 1) & 0x0F
}

// bucket returns the entries hash may occupy.
func (tt *TT) bucket(hash uint64) *[bucketSize]entry {
	return &tt.buckets[hash&tt.mask].e
}

// Prefetch starts loading the bucket for hash into the cache, so that a
// Probe or Store for it shortly afterwards does not wait on main memory.
func (tt *TT) Prefetch(hash uint64) {
	prefetch(unsafe.Pointer(&tt.buckets[hash&tt.mask]))
}

// ProbeEval is Probe that also returns the entry's static evaluation, or
// EvalNone if it has none. The node type it returns includes PVFlag.
func (tt *TT) ProbeEval(hash uint64) (m types.Move, score, eval int32, depth int8, nodeType uint8, found bool) {
	b := tt.bucket(hash)
	key := entryKey(hash)
	for i := range b {
		// One copy of the entry: another thread may be rewriting it.
		if e := b[i]; e.key == key && !e.empty() {
			return types.Move(e.move), int32(e.score), int32(e.eval), e.searched(), e.nodeType(), true
		}
	}
	return types.MoveNone, 0, EvalNone, 0, 0, false
}

func (tt *TT) Probe(hash uint64) (m types.Move, score int32, depth int8, nodeType uint8, found bool) {
	m, score, _, depth, nodeType, found = tt.ProbeEval(hash)
	return m, score, depth, nodeType & BoundMask, found
}

// worth ranks entries for replacement: deeper is worth more, and each
// generation of age costs as much as eight plies of depth.
func (tt *TT) worth(e *entry) int {
	if e.empty() {
		return -1 << 20
	}
	return int(e.searched()) - 8*int((tt.age-e.age())&0x0F)
}

func (tt *TT) Store(hash uint64, m types.Move, score int32, depth int8, nodeType uint8) {
	tt.StoreEval(hash, m, score, EvalNone, depth, nodeType)
}

// StoreEval is Store with the position's static evaluation (EvalNone if
// unknown; an evaluation already stored for the position is then kept).
func (tt *TT) StoreEval(hash uint64, m types.Move, score, eval int32, depth int8, nodeType uint8) {
	b := tt.bucket(hash)
	key := entryKey(hash)

	// Same position already stored: update it in place.
	for i := range b {
		cur := b[i]
		if cur.empty() || cur.key != key {
			continue
		}
		// Never overwrite a valid move with MoveNone (e.g. on UpperBound fail-low)
		if m == types.MoveNone {
			m = types.Move(cur.move)
		}
		if eval == EvalNone {
			eval = int32(cur.eval)
		}

		// Don't overwrite deeper entries from the current generation unless this is an exact score
		if cur.age() == tt.age && cur.searched() > depth && nodeType&BoundMask != Exact {
			if (m != types.MoveNone && cur.move == 0) || (eval != EvalNone && int32(cur.eval) == EvalNone) || nodeType&PVFlag > cur.info&PVFlag {
				cur.move, cur.eval = uint16(m), int16(eval)
				cur.info |= nodeType & PVFlag
				b[i] = cur
			}
			return
		}
		b[i] = makeEntry(hash, m, score, eval, depth, nodeType, tt.age)
		return
	}

	// New position: replace the least valuable entry of the bucket.
	victim := &b[0]
	least := tt.worth(&b[0])
	for i := 1; i < bucketSize; i++ {
		if w := tt.worth(&b[i]); w < least {
			victim, least = &b[i], w
		}
	}
	// Keep deep entries from this search: shallow subtrees and quiescence
	// results do not displace them.
	if !victim.empty() && victim.age() == tt.age && (depth <= 0 || victim.searched() > depth+2) {
		return
	}
	*victim = makeEntry(hash, m, score, eval, depth, nodeType, tt.age)
}

// HashFull returns how full the table is with entries of the current search,
// in permille, from a sample.
func (tt *TT) HashFull() int {
	sample := min(len(tt.buckets), 2000/bucketSize+1)
	if sample == 0 {
		return 0
	}
	used := 0
	for i := 0; i < sample; i++ {
		for j := range tt.buckets[i].e {
			if e := &tt.buckets[i].e[j]; !e.empty() && e.age() == tt.age {
				used++
			}
		}
	}
	hf := (used * 1000) / (sample * bucketSize)
	if used > 0 && hf == 0 {
		hf = 1
	}
	return hf
}
