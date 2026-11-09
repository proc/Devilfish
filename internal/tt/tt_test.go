package tt

import (
	"sync"
	"testing"

	"devilfish/internal/types"
)

func TestTTProbeStore(t *testing.T) {
	table := New(16) // 16 MB
	defer table.Clear()

	hash := uint64(0x123456789ABCDEF0)
	mv := types.MakeMove(types.E2, types.E4, types.FlagDoublePawnPush)
	score := int32(150)
	depth := int8(6)
	nodeType := Exact

	table.Store(hash, mv, score, depth, nodeType)

	retMv, retScore, retDepth, retType, found := table.Probe(hash)
	if !found {
		t.Fatalf("expected entry to be found in TT")
	}
	if retMv != mv || retScore != score || retDepth != depth || retType != nodeType {
		t.Fatalf("TT entry mismatch: got mv=%s score=%d depth=%d type=%d", retMv, retScore, retDepth, retType)
	}
}

func TestTTConcurrentAccess(t *testing.T) {
	table := New(32)
	var wg sync.WaitGroup

	numGoroutines := 8
	iterations := 10000

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				hash := uint64((id*100000 + i) * 2654435761)
				mv := types.MakeMove(types.Square(i%64), types.Square((i+1)%64), types.FlagQuiet)
				score := int32(i % 1000)
				depth := int8(i % 16)
				table.Store(hash, mv, score, depth, Exact)

				table.Probe(hash)
			}
		}(g)
	}

	wg.Wait()
}

func TestTTHashFull(t *testing.T) {
	table := New(1) // 1 MB: 65,536 entries
	defer table.Clear()

	if hf := table.HashFull(); hf != 0 {
		t.Errorf("expected empty table hashfull 0, got %d", hf)
	}

	// Fill roughly 50% of the entries using a linear congruential step to avoid stride harmonics
	total := len(table.entries)
	for i := 0; i < total/2; i++ {
		hash := uint64(i) * 2654435761
		table.Store(hash, types.MakeMove(types.E2, types.E4, types.FlagQuiet), 100, 4, Exact)
	}

	hf := table.HashFull()
	// Half-filled random placement yields ~393 permille (1 - e^-0.5 ~ 0.393 with replacement)
	if hf < 300 || hf > 500 {
		t.Errorf("expected hashfull ~390 for half-filled table, got %d", hf)
	}

	table.Clear()
	if hf := table.HashFull(); hf != 0 {
		t.Errorf("expected hashfull 0 after clear, got %d", hf)
	}
}
