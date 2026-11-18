//go:build linux

package threadprio

import (
	"syscall"
	"unsafe"
)

const supported = true

const (
	schedOther = 0
	schedBatch = 3
)

type schedParam struct {
	priority int32
}

func setLow(low bool) bool {
	policy := uintptr(schedOther)
	if low {
		policy = schedBatch
	}
	var p schedParam
	_, _, errno := syscall.Syscall(syscall.SYS_SCHED_SETSCHEDULER, 0, policy, uintptr(unsafe.Pointer(&p)))
	return errno == 0
}
