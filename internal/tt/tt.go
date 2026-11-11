package tt

import (
	"math/bits"
	"unsafe"

	"devilfish/internal/types"
)

const (
	Exact      uint8 = 0
	LowerBound uint8 = 1
	UpperBound uint8 = 2

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

type Entry struct {
	Key  uint64
	Data uint64
}

// Data packing:
// Bits 0..15:  Move (16 bits)
// Bits 16..47: Score (32 bits, int32 cast to uint32)
// Bits 48..55: Depth (8 bits, int8 cast to uint8)
// Bits 56..59: NodeType (4 bits)
// Bits 60..63: Age (4 bits)

func packData(m types.Move, score int32, depth int8, nodeType uint8, age uint8) uint64 {
	return uint64(m) |
		(uint64(uint32(score)) << 16) |
		(uint64(uint8(depth)) << 48) |
		(uint64(nodeType&0x0F) << 56) |
		(uint64(age&0x0F) << 60)
}

func unpackData(data uint64) (m types.Move, score int32, depth int8, nodeType uint8, age uint8) {
	m = types.Move(data & 0xFFFF)
	score = int32(uint32((data >> 16) & 0xFFFFFFFF))
	depth = int8(uint8((data >> 48) & 0xFF))
	nodeType = uint8((data >> 56) & 0x0F)
	age = uint8((data >> 60) & 0x0F)
	return
}

// The table is an array of buckets of bucketSize entries, one 64-byte cache
// line each. A position maps to one bucket and may sit in any of its entries,
// so a deep result is not lost the moment another position lands on the same
// index.
const bucketSize = 4

type TT struct {
	entries []Entry
	mask    uint64 // bucket index mask
	age     uint8
}

func New(sizeMB int) *TT {
	if sizeMB < 1 {
		sizeMB = 1
	}
	bytes := uint64(sizeMB) * 1024 * 1024
	entrySize := uint64(16) // 8 bytes Key + 8 bytes Data
	numEntries := bytes / entrySize

	// Round to nearest power of 2 (round up if within 25% of next power of 2)
	pow2 := uint64(1) << (63 - bits.LeadingZeros64(numEntries))
	if pow2*2 <= numEntries || float64(numEntries)/float64(pow2*2) >= 0.75 {
		pow2 *= 2
	}
	return &TT{
		entries: make([]Entry, pow2),
		mask:    pow2/bucketSize - 1,
		age:     0,
	}
}

func (tt *TT) SizeMB() int {
	return int((uint64(len(tt.entries)) * 16) / (1024 * 1024))
}

func (tt *TT) Resize(sizeMB int) {
	if sizeMB < 1 {
		sizeMB = 1
	}
	bytes := uint64(sizeMB) * 1024 * 1024
	entrySize := uint64(16)
	numEntries := bytes / entrySize

	pow2 := uint64(1) << (63 - bits.LeadingZeros64(numEntries))
	if pow2*2 <= numEntries || float64(numEntries)/float64(pow2*2) >= 0.75 {
		pow2 *= 2
	}
	tt.entries = make([]Entry, pow2)
	tt.mask = pow2/bucketSize - 1
	tt.age = 0
}

func (tt *TT) Clear() {
	for i := range tt.entries {
		tt.entries[i].Key = 0
		tt.entries[i].Data = 0
	}
	tt.age = 0
}

func (tt *TT) IncrementAge() {
	tt.age = (tt.age + 1) & 0x0F
}

// bucket returns the entries hash may occupy.
func (tt *TT) bucket(hash uint64) *[bucketSize]Entry {
	return (*[bucketSize]Entry)(tt.entries[(hash&tt.mask)*bucketSize:])
}

// Prefetch starts loading the bucket for hash into the cache, so that a
// Probe or Store for it shortly afterwards does not wait on main memory.
func (tt *TT) Prefetch(hash uint64) {
	prefetch(unsafe.Pointer(&tt.entries[(hash&tt.mask)*bucketSize]))
}

func (tt *TT) Probe(hash uint64) (m types.Move, score int32, depth int8, nodeType uint8, found bool) {
	b := tt.bucket(hash)
	for i := range b {
		key, data := b[i].Key, b[i].Data
		// The key is stored XORed with the data, so a torn write by another
		// thread fails this check instead of returning a mixed entry.
		if key == hash^data && data != 0 {
			m, score, depth, nodeType, _ = unpackData(data)
			return m, score, depth, nodeType, true
		}
	}
	return types.MoveNone, 0, 0, 0, false
}

// worth ranks entries for replacement: deeper is worth more, and each
// generation of age costs as much as eight plies of depth.
func (tt *TT) worth(data uint64) int {
	if data == 0 {
		return -1 << 20 // empty
	}
	_, _, depth, _, age := unpackData(data)
	return int(depth) - 8*int((tt.age-age)&0x0F)
}

func (tt *TT) Store(hash uint64, m types.Move, score int32, depth int8, nodeType uint8) {
	b := tt.bucket(hash)

	// Same position already stored: update it in place.
	for i := range b {
		entry := &b[i]
		currKey, currData := entry.Key, entry.Data
		if currData == 0 || currKey != hash^currData {
			continue
		}
		existingMove, existingScore, currDepth, existingType, currAge := unpackData(currData)

		// Never overwrite a valid move with MoveNone (e.g. on UpperBound fail-low)
		if m == types.MoveNone && existingMove != types.MoveNone {
			m = existingMove
		}

		// Don't overwrite deeper entries from the current generation unless this is an exact score
		if currAge == tt.age && currDepth > depth && nodeType != Exact {
			if m != types.MoveNone && existingMove == types.MoveNone {
				newData := packData(m, existingScore, currDepth, existingType, currAge)
				entry.Data = newData
				entry.Key = hash ^ newData
			}
			return
		}
		newData := packData(m, score, depth, nodeType, tt.age)
		entry.Data = newData
		entry.Key = hash ^ newData
		return
	}

	// New position: replace the least valuable entry of the bucket.
	victim := &b[0]
	least := tt.worth(b[0].Data)
	for i := 1; i < bucketSize; i++ {
		if w := tt.worth(b[i].Data); w < least {
			victim, least = &b[i], w
		}
	}
	if victim.Data != 0 {
		// Keep deep entries from this search: shallow subtrees and
		// quiescence results do not displace them.
		_, _, vDepth, _, vAge := unpackData(victim.Data)
		if vAge == tt.age && (depth <= 0 || vDepth > depth+2) {
			return
		}
	}
	newData := packData(m, score, depth, nodeType, tt.age)
	victim.Data = newData
	victim.Key = hash ^ newData
}

func (tt *TT) HashFull() int {
	sampleCount := 2000
	if len(tt.entries) < sampleCount {
		sampleCount = len(tt.entries)
	}

	used := 0
	for i := 0; i < sampleCount; i++ {
		data := tt.entries[i].Data
		if data != 0 {
			age := uint8((data >> 60) & 0x0F)
			if age == tt.age {
				used++
			}
		}
	}
	hf := (used * 1000) / sampleCount
	if used > 0 && hf == 0 {
		hf = 1
	}
	return hf
}
