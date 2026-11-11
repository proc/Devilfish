package nnue

import (
	"strconv"
	"strings"
)

// darwinVersionAtLeast reports whether a kern.osrelease string such as
// "21.3.0" is at least major.minor.patch. Unparseable strings count as older.
func darwinVersionAtLeast(release string, major, minor, patch int) bool {
	parts := strings.SplitN(strings.TrimSpace(release), ".", 3)
	want := [3]int{major, minor, patch}
	for i := 0; i < 3; i++ {
		v := 0
		if i < len(parts) {
			n, err := strconv.Atoi(strings.TrimRightFunc(parts[i], func(r rune) bool { return r < '0' || r > '9' }))
			if err != nil {
				return false
			}
			v = n
		}
		if v != want[i] {
			return v > want[i]
		}
	}
	return true
}
