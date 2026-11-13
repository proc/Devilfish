//go:build linux

package hugemem

import "testing"

// Blocks big enough for a huge-page mapping start on a 2 MB boundary.
func TestMakeAligned(t *testing.T) {
	for _, n := range []int{minHuge / 8, 9<<20 + 5} {
		_, blk := Make[uint64](n)
		if uintptrOf(blk.Bytes())%hugePage != 0 {
			t.Errorf("block of %d uint64 not aligned to 2 MB", n)
		}
		blk.Free()
	}
}
