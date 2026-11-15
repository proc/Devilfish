package threadprio

import (
	"runtime"
	"testing"
)

func TestSetLowRoundTrip(t *testing.T) {
	if !Supported() {
		t.Skip("thread priority is not supported on this platform")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !SetLow(true) {
		t.Fatal("SetLow(true) failed")
	}
	if !SetLow(false) {
		t.Fatal("SetLow(false) failed")
	}
}
