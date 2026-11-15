package nnue

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// BenchmarkSFPrepare times building a child's accumulators from its parent's
// (Prepare alone, no forward pass) over every legal move of the test
// positions: the one-move update the search makes at almost every node.
func BenchmarkSFPrepare(b *testing.B) {
	net := loadSFTestNet(b)
	type step struct {
		pos *board.Board
		m   types.Move
	}
	var steps []step
	for _, fen := range eightTestFENs {
		pos, err := board.ParseFEN(fen)
		if err != nil {
			b.Fatal(err)
		}
		for _, m := range movegen.GenerateLegalMoves(pos) {
			steps = append(steps, step{pos, m})
		}
	}
	states := make([]*EightState, len(eightTestFENs))
	stateOf := map[*board.Board]*EightState{}
	for i, fen := range eightTestFENs {
		_ = fen
		states[i] = NewEightState(8)
	}
	i := 0
	for _, s := range steps {
		if _, ok := stateOf[s.pos]; !ok {
			stateOf[s.pos] = states[i]
			states[i].Prepare(net, s.pos, 0, 0, false)
			i++
		}
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		s := steps[n%len(steps)]
		st := stateOf[s.pos]
		parent := s.pos.Hash
		s.pos.MakeMove(s.m)
		st.Prepare(net, s.pos, 1, parent, true)
		s.pos.UnmakeMove(s.m)
	}
}
