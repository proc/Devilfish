//go:build unix

package coreshare

import "testing"

// Each Pool stands for one engine process: locks taken through different
// open files exclude each other inside one process too.

func TestAloneGetsEverything(t *testing.T) {
	a := Open(t.TempDir(), 8)
	if n, _ := a.Acquire(8, Search); n != 8 {
		t.Fatalf("alone: got %d threads, want 8", n)
	}
	if n, _ := a.Acquire(4, Search); n != 4 {
		t.Fatalf("capped at 4: got %d threads", n)
	}
	if n, _ := a.Acquire(16, Search); n != 8 {
		t.Fatalf("cap above the core count: got %d threads, want 8", n)
	}
}

func TestSearchersSplitTheCores(t *testing.T) {
	dir := t.TempDir()
	a, b, c := Open(dir, 8), Open(dir, 8), Open(dir, 8)
	_, ca := a.Acquire(8, Search)
	if n, _ := b.Acquire(8, Search); n != 4 {
		t.Fatalf("second searcher: got %d threads, want 4", n)
	}
	if n, _ := c.Acquire(8, Search); n != 2 {
		t.Fatalf("third searcher: got %d threads, want 2", n)
	}
	// a's next search sees the other two.
	if n, _ := a.Acquire(8, Search); n != 2 {
		t.Fatalf("first searcher's next search: got %d threads, want 2", n)
	}
	a.Release(ca) // a stale claim: must not drop the new one
	if n, _ := b.Acquire(8, Search); n != 2 {
		t.Fatalf("after a stale release: got %d threads, want 2", n)
	}
}

func TestReleaseFreesTheCores(t *testing.T) {
	dir := t.TempDir()
	a, b := Open(dir, 8), Open(dir, 8)
	_, ca := a.Acquire(8, Search)
	a.Release(ca)
	if n, _ := b.Acquire(8, Search); n != 8 {
		t.Fatalf("after release: got %d threads, want 8", n)
	}
}

func TestIdleTakesWhatIsLeft(t *testing.T) {
	dir := t.TempDir()
	a, b := Open(dir, 8), Open(dir, 8)
	a.Acquire(5, Search)
	n, cb := b.Acquire(8, Idle)
	if n != 3 {
		t.Fatalf("idle with 3 free: got %d threads, want 3", n)
	}
	// a holds 5 slots and b the other 3: a still gets its full count.
	if n, _ := a.Acquire(8, Search); n != 8 {
		t.Fatalf("searcher with slots missing: got %d threads, want 8", n)
	}
	b.Release(cb)
	a.Acquire(8, Search)
	if n, _ := b.Acquire(8, Idle); n != 1 {
		t.Fatalf("idle with none free: got %d threads, want 1", n)
	}
	// An idle search does not count as searching.
	if n, _ := a.Acquire(8, Search); n != 8 {
		t.Fatalf("searcher next to an idle search: got %d threads, want 8", n)
	}
}

func TestPonderCountsOnlyAfterPromote(t *testing.T) {
	dir := t.TempDir()
	a, b := Open(dir, 8), Open(dir, 8)
	a.Acquire(8, Search)
	if n, _ := b.Acquire(8, Ponder); n != 4 {
		t.Fatalf("ponder next to a searcher: got %d threads, want 4", n)
	}
	if n, _ := a.Acquire(8, Search); n != 8 {
		t.Fatalf("searcher next to a ponder search: got %d threads, want 8", n)
	}
	b.Promote()
	if n, _ := a.Acquire(8, Search); n != 4 {
		t.Fatalf("searcher next to a promoted ponder search: got %d threads, want 4", n)
	}
}

func TestUnusableDirectoryFallsBack(t *testing.T) {
	a := Open("/dev/null/cores", 8)
	if n, _ := a.Acquire(6, Search); n != 6 {
		t.Fatalf("unusable directory: got %d threads, want 6", n)
	}
}
