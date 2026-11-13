package hugemem

import "testing"

func TestMake(t *testing.T) {
	for _, n := range []int{0, 100, 3 << 20, 9<<20 + 5} {
		s, blk := Make[uint64](n)
		if len(s) != n {
			t.Fatalf("len %d, want %d", len(s), n)
		}
		for i := range s {
			if s[i] != 0 {
				t.Fatalf("memory not zeroed at %d", i)
			}
			s[i] = uint64(i)
		}
		if n > 0 && s[n-1] != uint64(n-1) {
			t.Fatal("write lost")
		}
		blk.Free()
	}
}
