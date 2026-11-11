//go:build !amd64 && !arm64

package tt

import "unsafe"

func prefetch(addr unsafe.Pointer) {}
