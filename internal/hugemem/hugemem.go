// Package hugemem allocates large, pointer-free tables (network weights, the
// transposition table) so the operating system can back them with huge pages.
//
// Those tables are read at random: every hash probe and every network weight
// row usually lands on a different 4 KB page, so with normal pages most reads
// also miss the CPU's address-translation cache (TLB). Go never asks Linux for
// transparent huge pages itself, and on systems where THP is set to "madvise"
// (the common default) such memory stays on 4 KB pages.
package hugemem

import "unsafe"

// Block is an allocation from Alloc. The zero value is an empty block.
type Block struct {
	buf    []byte // memory handed out (aligned)
	mapped []byte // whole mapping to release, nil if allocated by Go
	Huge   bool   // huge pages were requested from the OS
}

// Bytes returns the block's memory.
func (b *Block) Bytes() []byte { return b.buf }

// Free releases the block. Memory from the Go heap is left to the collector.
func (b *Block) Free() {
	if b.mapped != nil {
		unmap(b.mapped)
	}
	*b = Block{}
}

// Alloc returns n zeroed bytes, backed by huge pages when the OS supports it.
func Alloc(n int) Block {
	if n <= 0 {
		return Block{}
	}
	if blk, ok := allocHuge(n); ok {
		return blk
	}
	return Block{buf: make([]byte, n)}
}

// Slice views a block as a slice of n elements of a pointer-free type T.
func Slice[T any](b *Block, n int) []T {
	if n == 0 {
		return nil
	}
	var zero T
	if uintptr(n)*unsafe.Sizeof(zero) > uintptr(len(b.buf)) {
		panic("hugemem: block too small")
	}
	return unsafe.Slice((*T)(unsafe.Pointer(&b.buf[0])), n)
}

// Make allocates n elements of a pointer-free type T, preferring huge pages.
// The memory lives until Free is called on the returned block.
func Make[T any](n int) ([]T, Block) {
	var zero T
	blk := Alloc(n * int(unsafe.Sizeof(zero)))
	return Slice[T](&blk, n), blk
}
