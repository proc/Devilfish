package threadprio

import (
	"syscall"
	_ "unsafe" // for go:linkname
)

const supported = true

// macOS quality-of-service classes (sys/qos.h). A utility thread still runs
// on the performance cores when they are free, but gives way to threads of
// the default class and above, which is what every other engine's search
// threads are.
const (
	qosClassUtility = 0x11
	qosClassDefault = 0x15
)

// pthread_set_qos_class_self_np is reached the way the syscall package
// reaches libSystem: a dynamic import and a trampoline (threadprio_darwin.s),
// so the engine still builds without cgo.

//go:cgo_import_dynamic libc_pthread_set_qos_class_self_np pthread_set_qos_class_self_np "/usr/lib/libSystem.B.dylib"

var libc_pthread_set_qos_class_self_np_trampoline_addr uintptr

//go:linkname syscall_syscall syscall.syscall
func syscall_syscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)

func setLow(low bool) bool {
	class := uintptr(qosClassDefault)
	if low {
		class = qosClassUtility
	}
	// The function returns its error number; 0 is success.
	r, _, _ := syscall_syscall(libc_pthread_set_qos_class_self_np_trampoline_addr, class, 0, 0)
	return r == 0
}
