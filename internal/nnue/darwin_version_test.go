package nnue

import "testing"

func TestDarwinVersionAtLeast(t *testing.T) {
	for _, c := range []struct {
		release string
		want    bool
	}{
		{"19.6.0", false}, {"20.6.0", false}, {"21.2.0", false}, // affected kernels
		{"21.3.0", true}, {"21.6.0", true}, {"22.1.0", true}, {"24.0.0", true},
		{"21.3", true}, {"22", true}, {"21.3.0\n", true},
		{"", false}, {"garbage", false},
	} {
		if got := darwinVersionAtLeast(c.release, 21, 3, 0); got != c.want {
			t.Errorf("%q: got %v, want %v", c.release, got, c.want)
		}
	}
}
