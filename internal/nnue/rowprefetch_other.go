//go:build !amd64 && !arm64

package nnue

import "unsafe"

func prefetchRows(base unsafe.Pointer, rows *int32, n int, shift uint, lines int) {}
