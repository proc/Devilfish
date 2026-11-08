package movegen

import (
	"fmt"
	"time"

	"devilfish/internal/board"
)

func Perft(b *board.Board, depth int) uint64 {
	if depth <= 0 {
		return 1
	}

	var ml MoveList
	GenerateMoves(b, false, &ml)

	if depth == 1 {
		var count uint64
		for i := 0; i < ml.Count; i++ {
			if b.MakeMove(ml.Moves[i]) {
				count++
				b.UnmakeMove(ml.Moves[i])
			}
		}
		return count
	}

	var nodes uint64
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if !b.MakeMove(m) {
			continue
		}
		nodes += Perft(b, depth-1)
		b.UnmakeMove(m)
	}
	return nodes
}

func PerftDivide(b *board.Board, depth int) uint64 {
	start := time.Now()
	var ml MoveList
	GenerateMoves(b, false, &ml)

	var totalNodes uint64
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if !b.MakeMove(m) {
			continue
		}
		var nodes uint64
		if depth > 1 {
			nodes = Perft(b, depth-1)
		} else {
			nodes = 1
		}
		b.UnmakeMove(m)
		totalNodes += nodes
		fmt.Printf("%s: %d\n", m.String(), nodes)
	}
	elapsed := time.Since(start)
	nps := float64(totalNodes) / elapsed.Seconds()
	fmt.Printf("\nTotal nodes: %d, Time: %v (%.0f nps)\n", totalNodes, elapsed, nps)
	return totalNodes
}
