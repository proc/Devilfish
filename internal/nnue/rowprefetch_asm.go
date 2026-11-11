//go:build amd64 || arm64

package nnue

import "unsafe"

// prefetchRows prefetches the first `lines` cache lines (64
// bytes each) of every weight row listed in rows[:n]; row k starts at
// base + k<<shift. It has no other effect.
//
//go:noescape
func prefetchRows(base unsafe.Pointer, rows *int32, n int, shift uint, lines int)
