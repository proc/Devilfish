package nnue

import (
	_ "embed"
	"os"
	"path/filepath"

	"devilfish/internal/types"
)

//go:embed default.nnue
var embeddedDefaultNNUE []byte

//go:embed eightchess_1024_tuned768.nnue
var embeddedEightNNUE []byte

// The Stockfish network selected by EvalFile "<stockfish>" (see LoadFile).
//
//go:embed nn-134a887f4c8f.nnue
var embeddedSFNNUE []byte

var defaultPST = [7][64]int32{
	0: {}, // None
	// Pawn
	1: {
		0, 0, 0, 0, 0, 0, 0, 0,
		98, 134, 61, 95, 68, 126, 34, -11,
		-6, 7, 26, 31, 65, 56, 25, -20,
		-14, 13, 6, 21, 23, 12, 17, -23,
		-27, -2, -5, 12, 17, 6, 10, -25,
		-26, -4, -4, -10, 3, 3, 33, -12,
		-35, -1, -20, -23, -15, 24, 38, -22,
		0, 0, 0, 0, 0, 0, 0, 0,
	},
	// Knight
	2: {
		-167, -89, -34, -49, 61, -97, -15, -107,
		-73, -41, 72, 36, 23, 62, 7, -17,
		-47, 60, 37, 65, 84, 129, 73, 44,
		-9, 17, 19, 53, 37, 69, 18, 22,
		-13, 4, 16, 13, 28, 19, 21, -8,
		-23, -9, 12, 10, 19, 17, 25, -16,
		-29, -53, -12, -3, -1, 18, -14, -19,
		-105, -21, -58, -33, -17, -28, -19, -23,
	},
	// Bishop
	3: {
		-29, 4, -82, -37, -25, -42, 7, -8,
		-26, 16, -18, -13, 30, 59, 18, -47,
		-16, 37, 43, 40, 35, 50, 37, -2,
		-4, 5, 19, 50, 37, 37, 7, -2,
		-6, 13, 13, 26, 34, 12, 10, 4,
		0, 15, -9, 22, 5, 28, 20, 14,
		4, 9, 13, 1, 17, 14, 17, 16,
		-33, -3, -14, -21, -13, -12, -39, -21,
	},
	// Rook
	4: {
		32, 42, 32, 51, 63, 9, 31, 43,
		27, 32, 58, 62, 80, 67, 26, 44,
		-5, 19, 26, 36, 17, 45, 61, 16,
		-24, -11, 7, 26, 24, 35, -8, -20,
		-36, -26, -12, -1, 9, -7, 6, -23,
		-45, -25, -16, -17, 3, 0, -5, -33,
		-44, -16, -20, -9, -1, 11, -6, -71,
		-19, -13, 1, 17, 16, 7, -37, -26,
	},
	// Queen
	5: {
		-28, 0, 29, 12, 59, 44, 43, 45,
		-24, -39, -5, 1, -16, 57, 28, 54,
		-13, -17, 7, 8, 29, 56, 47, 57,
		-27, -27, -16, -16, -1, 17, -2, 1,
		-9, -26, -9, -10, -2, -4, 3, -3,
		-14, 2, -11, -2, -5, 2, 14, 5,
		-35, -8, 11, 2, 8, 15, -3, 1,
		-1, -18, -9, 10, -15, -25, -24, -50,
	},
	// King
	6: {
		-65, 23, 16, -15, -56, -34, 2, 13,
		29, -1, -20, -7, -8, -4, -38, -29,
		-9, 24, 2, -16, -20, 6, 22, -22,
		-17, -20, -12, -27, -30, -25, -14, -36,
		-49, -1, -27, -39, -46, -44, -33, -51,
		-14, -14, -22, -46, -44, -30, -15, -27,
		1, 7, -8, -64, -43, -16, 9, 8,
		-15, 36, 12, -54, 8, -28, 24, 14,
	},
}

func init() {
	ResetToDefault()
}

func ResetToDefault() {
	// Always initialize ActiveNetwork from embedded default weights as baseline
	if len(embeddedDefaultNNUE) > 0 {
		net, err := LoadBytes(embeddedDefaultNNUE)
		if err == nil {
			ActiveNetwork = net
			CurrentFilePath = "<embedded>"
		}
	}
	if ActiveNetwork == nil {
		ActiveNetwork = generateDefaultNetwork()
		CurrentFilePath = "<embedded (synthetic)>"
	}

	// 1. Prefer embedded 8Chess NNUE if compiled into binary
	if len(embeddedEightNNUE) == EightNetworkSize {
		eightNet, err := LoadEightBytes(embeddedEightNNUE)
		if err == nil {
			ActiveEightNetwork = eightNet
			CurrentFilePath = "<embedded (8chess)>"
			return
		}
	}

	// 2. Check for EightChess network if present in local directory or Downloads
	eightPaths := []string{
		"eightchess_1024_tuned768.nnue",
		"../eightchess_1024_tuned768.nnue",
		"../../eightchess_1024_tuned768.nnue",
	}
	if home, err := os.UserHomeDir(); err == nil {
		eightPaths = append(eightPaths, filepath.Join(home, "Downloads", "eightchess_1024_tuned768.nnue"))
	}

	for _, p := range eightPaths {
		if fi, err := os.Stat(p); err == nil && fi.Size() == EightNetworkSize {
			if eightNet, err := LoadEightFile(p); err == nil {
				ActiveEightNetwork = eightNet
				CurrentFilePath = p
				return
			}
		}
	}
}

func generateDefaultNetwork() *Network {
	net := &Network{}

	pieceValsMG := [7]int32{0, 100, 320, 330, 500, 950, 0}
	pieceValsEG := [7]int32{0, 130, 335, 345, 530, 1020, 0}

	for f := 0; f < NumFeatures; f++ {
		ptIdx := f / 64
		sq := types.Square(f % 64)

		isEnemy := ptIdx >= 6
		if isEnemy {
			continue // L1 layer already performs us - them subtraction; enemy features remain 0 in synthetic baseline
		}
		pt := types.PieceType(ptIdx + 1)

		scoreMG := pieceValsMG[pt]
		scoreEG := pieceValsEG[pt]

		if pt <= types.King {
			tableSq := sq ^ 56
			scoreMG += defaultPST[pt][tableSq]
			scoreEG += defaultPST[pt][tableSq]
		}

		sign := int32(1)
		if isEnemy {
			sign = -1
		}

		// Channels 0..63: Midgame evaluation terms
		for i := 0; i < 64; i++ {
			net.Weights[f][i] = int16(sign * scoreMG)
		}

		// Channels 64..127: Endgame evaluation terms
		for i := 0; i < 64; i++ {
			advBonus := int32(0)
			if pt == types.Pawn {
				advBonus = int32(sq.Rank()) * 15
			}
			net.Weights[f][64+i] = int16(sign * (scoreEG + advBonus))
		}

		// Channels 128..191: Center control & spatial mobility
		centerDist := int32(absInt(3-int(sq.File())) + absInt(3-int(sq.Rank())))
		spatialScore := (8 - centerDist) * 10
		for i := 0; i < 64; i++ {
			net.Weights[f][128+i] = int16(sign * (scoreMG + spatialScore))
		}

		// Channels 192..255: King safety & coordination
		for i := 0; i < 64; i++ {
			net.Weights[f][192+i] = int16(sign * scoreMG)
		}
	}

	// Layer 1 and Output weights with anti-symmetric advantage detection
	for i := 0; i < 16; i++ {
		// Neurons 0..15: positive advantage (us > them)
		net.L1Weights[i][i] = 127
		net.L1Weights[i][256+i] = -127
		net.L1Biases[i] = 0
		net.OutWeights[i] = 1

		// Neurons 16..31: negative advantage (them > us)
		net.L1Weights[16+i][i] = -127
		net.L1Weights[16+i][256+i] = 127
		net.L1Biases[16+i] = 0
		net.OutWeights[16+i] = -1
	}

	return net
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
