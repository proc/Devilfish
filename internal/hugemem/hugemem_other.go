//go:build !linux

package hugemem

func allocHuge(n int) (Block, bool) { return Block{}, false }

func unmap(m []byte) {}
