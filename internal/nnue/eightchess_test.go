package nnue

import (
	"os"
	"testing"

	"devilfish/internal/board"
)

func TestEightChessLoadingAndEval(t *testing.T) {
	path := "../../eightchess_1024_tuned768.nnue"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("eightchess_1024_tuned768.nnue not found in project root, skipping")
	}

	net, err := LoadEightFile(path)
	if err != nil {
		t.Fatalf("failed to load 8chess network: %v", err)
	}

	tests := []struct {
		name          string
		fen           string
		expectedScore int32
	}{
		{"startpos", board.StartFEN, 90},
		{"after 1.e4", "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1", 8},
		{"+Knight", "r1bqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", 1286},
		{"+Queen", "rnb1kbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", 1581},
		{"-Rook", "1nbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR b KQkq - 0 1", -1000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := board.ParseFEN(tc.fen)
			if err != nil {
				t.Fatalf("failed to parse FEN %s: %v", tc.fen, err)
			}
			score := EvaluateEight(net, b)
			t.Logf("Position %s: got %d cp, expected %d cp", tc.name, score, tc.expectedScore)
			diff := score - tc.expectedScore
			if diff < -2 || diff > 2 {
				t.Errorf("score mismatch for %s: got %d, expected %d", tc.name, score, tc.expectedScore)
			}
		})
	}
}

func TestEmbeddedEightChess(t *testing.T) {
	if len(embeddedEightNNUE) != EightNetworkSize {
		t.Fatalf("embeddedEightNNUE size mismatch: got %d, expected %d", len(embeddedEightNNUE), EightNetworkSize)
	}

	net, err := LoadEightBytes(embeddedEightNNUE)
	if err != nil {
		t.Fatalf("failed to load embedded 8chess network: %v", err)
	}

	b, err := board.ParseFEN(board.StartFEN)
	if err != nil {
		t.Fatalf("failed to parse FEN: %v", err)
	}

	score := EvaluateEight(net, b)
	if score != 90 {
		t.Errorf("expected embedded network startpos score 90, got %d", score)
	}
}

func TestActivateEightFTNEON(t *testing.T) {
	var pst, thr [EightL1Size]int16
	// Fill with diverse values: negative, zero, positive, overflow
	for i := 0; i < EightL1Size; i++ {
		pst[i] = int16((i*37 - 500) % 600)
		thr[i] = int16((i*19 - 250) % 400)
	}

	var expected [EightL1Size / 2]uint8
	for i := 0; i < EightL1Size/2; i++ {
		sLeft := int32(pst[i] + thr[i])
		sRight := int32(pst[i+EightL1Size/2] + thr[i+EightL1Size/2])

		if sLeft < 0 {
			sLeft = 0
		} else if sLeft > EightFTQuant {
			sLeft = EightFTQuant
		}

		if sRight > EightFTQuant {
			sRight = EightFTQuant
		}

		prod := (sLeft * sRight) >> EightFTShift
		if prod < 0 {
			prod = 0
		} else if prod > 255 {
			prod = 255
		}
		expected[i] = uint8(prod)
	}

	var actual [EightL1Size / 2]uint8
	activateEightFTNEON(&actual[0], &pst[0], &thr[0])

	for i := 0; i < EightL1Size/2; i++ {
		if actual[i] != expected[i] {
			t.Fatalf("mismatch at index %d: actual=%d, expected=%d (pstL=%d thrL=%d pstR=%d thrR=%d)",
				i, actual[i], expected[i], pst[i], thr[i], pst[i+512], thr[i+512])
		}
	}
}

func BenchmarkEvaluateEight(b *testing.B) {
	ResetToDefault()
	if ActiveEightNetwork == nil {
		b.Skip("ActiveEightNetwork not available")
	}

	brd, _ := board.ParseFEN(board.StartFEN)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EvaluateEight(ActiveEightNetwork, brd)
	}
}
