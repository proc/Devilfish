//go:build windows

package threadprio

import "syscall"

const supported = true

const (
	threadPriorityNormal      = 0
	threadPriorityBelowNormal = -1
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentThread   = kernel32.NewProc("GetCurrentThread")
	procSetThreadPriority  = kernel32.NewProc("SetThreadPriority")
)

func setLow(low bool) bool {
	prio := uintptr(threadPriorityNormal)
	if low {
		prio = ^uintptr(0) // int32(-1) = THREAD_PRIORITY_BELOW_NORMAL
	}
	h, _, _ := procGetCurrentThread.Call()
	r, _, _ := procSetThreadPriority.Call(h, prio)
	return r != 0
}
