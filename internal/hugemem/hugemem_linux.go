//go:build linux

package hugemem

import "syscall"

const hugePage = 2 << 20

// minHuge: below this size huge pages are not worth a separate mapping.
const minHuge = 4 << 20

func allocHuge(n int) (Block, bool) {
	if n < minHuge {
		return Block{}, false
	}
	// Over-allocate by one huge page so the start can be aligned to 2 MB.
	size := (n + hugePage - 1) &^ (hugePage - 1)
	m, err := syscall.Mmap(-1, 0, size+hugePage, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		return Block{}, false
	}
	off := 0
	if r := int(uintptrOf(m) % hugePage); r != 0 {
		off = hugePage - r
	}
	buf := m[off : off+size]
	// Best effort: with THP disabled the memory simply stays on small pages.
	huge := syscall.Madvise(buf, 14 /* MADV_HUGEPAGE */) == nil
	return Block{buf: buf[:n:n], mapped: m, Huge: huge}, true
}

func unmap(m []byte) { _ = syscall.Munmap(m) }
