package tt

import (
	"testing"

	"devilfish/internal/types"
)

// colliding returns n distinct hashes that map to the same bucket.
func colliding(tt *TT, n int) []uint64 {
	base := uint64(0x9E3779B97F4A7C15)
	out := []uint64{}
	for i := uint64(1); len(out) < n; i++ {
		h := base + i*(tt.mask+1) // same low bits, different high bits
		out = append(out, h)
	}
	return out
}

func TestBucketKeepsCollidingEntries(t *testing.T) {
	tt := New(1)
	hs := colliding(tt, 5)
	m := types.Move(0x1234)

	for i, h := range hs[:4] {
		tt.Store(h, m, int32(100+i), int8(10+i), Exact)
	}
	for i, h := range hs[:4] {
		_, score, depth, _, found := tt.Probe(h)
		if !found || score != int32(100+i) || depth != int8(10+i) {
			t.Fatalf("entry %d lost from a full bucket: found=%v score=%d depth=%d", i, found, score, depth)
		}
	}

	// A quiescence result never displaces this search's entries.
	tt.Store(hs[4], m, 7, 0, Exact)
	if _, _, _, _, found := tt.Probe(hs[4]); found {
		t.Fatal("depth-0 entry displaced a current-generation entry")
	}

	// A deeper one replaces the shallowest (depth 10).
	tt.Store(hs[4], m, 7, 20, Exact)
	if _, _, _, _, found := tt.Probe(hs[4]); !found {
		t.Fatal("deep entry not stored")
	}
	if _, _, _, _, found := tt.Probe(hs[0]); found {
		t.Fatal("shallowest entry should have been replaced")
	}
	for _, h := range hs[1:4] {
		if _, _, _, _, found := tt.Probe(h); !found {
			t.Fatal("a deeper entry was replaced instead of the shallowest")
		}
	}

	// After a few searches, old entries make way even for shallow ones.
	for i := 0; i < 3; i++ {
		tt.IncrementAge()
	}
	tt.Store(hs[0], m, 5, 1, Exact)
	if _, _, _, _, found := tt.Probe(hs[0]); !found {
		t.Fatal("stale entries were kept over a new one")
	}
}

func TestBucketUpdatesInPlace(t *testing.T) {
	tt := New(1)
	h := colliding(tt, 1)[0]
	tt.Store(h, types.Move(0x1111), 50, 8, LowerBound)
	tt.Store(h, types.MoveNone, 60, 9, UpperBound) // keeps the move
	m, score, depth, typ, found := tt.Probe(h)
	if !found || m != types.Move(0x1111) || score != 60 || depth != 9 || typ != UpperBound {
		t.Fatalf("got move %v score %d depth %d type %d found %v", m, score, depth, typ, found)
	}
	used := 0
	for _, e := range tt.bucket(h) {
		if e.Data != 0 {
			used++
		}
	}
	if used != 1 {
		t.Fatalf("one position occupies %d entries", used)
	}
}
