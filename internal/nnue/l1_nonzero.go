package nnue

import (
	"math/bits"
	"unsafe"
)

// l1NzIdx[m] lists the positions of the set bits of m, lowest first, as
// uint16 (unused entries are zero).
var l1NzIdx = func() (t [256][8]uint16) {
	for m := range t {
		k := 0
		for bit := 0; bit < 8; bit++ {
			if m>>bit&1 != 0 {
				t[m][k] = uint16(bit)
				k++
			}
		}
	}
	return
}()

// l1NonzeroIdx writes the indices of the non-zero bytes of ft to out, in
// order, and returns how many there are. It has no data-dependent branches.
//
// Eight inputs at a time: a mask with one bit per non-zero byte selects a
// row of l1NzIdx, which is offset by the block's position and stored whole
// (eight entries, of which only the first popcount(mask) are kept by
// advancing the output that far). out has eight entries of slack for the
// last store.
func l1NonzeroIdx(ft *[EightL1Size]uint8, out *[EightL1Size + 8]uint16) int {
	const (
		lo7 = 0x7f7f7f7f7f7f7f7f
		hi  = 0x8080808080808080
		// Moves bit 0 of byte i to bit 56+i; the partial products all land
		// on different bits, so nothing carries.
		gather = 0x0102040810204080
		lanes  = 0x0001000100010001
	)
	src := unsafe.Pointer(ft)
	dst := unsafe.Pointer(out)
	n := 0
	for c := uintptr(0); c < EightL1Size; c += 8 {
		x := *(*uint64)(unsafe.Add(src, c)) // little-endian on every supported target
		nz := (((x & lo7) + lo7) | x) & hi  // bit 7 of each non-zero byte
		m := ((nz >> 7) * gather) >> 56
		row := (*[2]uint64)(unsafe.Pointer(&l1NzIdx[m]))
		d := (*[2]uint64)(unsafe.Add(dst, n*2))
		base := uint64(c) * lanes
		d[0] = row[0] + base
		d[1] = row[1] + base
		n += bits.OnesCount64(m)
	}
	return n
}
