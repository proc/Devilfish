//go:build amd64 || arm64

package tt

import "unsafe"

// prefetch asks the CPU to start loading the cache line at addr. It has no
// other effect.
//
//go:noescape
func prefetch(addr unsafe.Pointer)
