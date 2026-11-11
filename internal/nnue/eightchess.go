package nnue

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"os"
	"unsafe"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

const (
	EightL1Size = 1024
	EightL2Size = 16
	EightL3Size = 32

	EightInputBuckets  = 10
	EightOutputBuckets = 8

	EightThreatFeatures = 66864
	EightPieceFeatures  = EightInputBuckets * 768 // 7680

	EightFTQuant           = 255
	EightL1Quant           = 64
	EightFTShift           = 9
	EightNetworkScale      = 380.0
	EightNetworkSize       = 84349504
	EightDequantMultiplier = float32(1<<EightFTShift) / float32(EightFTQuant*EightFTQuant*EightL1Quant)
)

var EightInputBucketsLayout = [64]int{
	0, 1, 2, 3, 3, 2, 1, 0,
	4, 5, 6, 7, 7, 6, 5, 4,
	8, 8, 8, 8, 8, 8, 8, 8,
	9, 9, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 9, 9,
	9, 9, 9, 9, 9, 9, 9, 9,
}

var EightOutputBucketsLayout = [33]int{
	0, 0, 0, 0, 0, 0, 0, 0, 0,
	1, 1, 1, 1,
	2, 2, 2, 2,
	3, 3, 3,
	4, 4, 4,
	5, 5, 5,
	6, 6, 6,
	7, 7, 7, 7,
}

var (
	eightPiecePairLookup   [12][12]uint32
	eightPieceOffsetLookup [12][64]int32
	eightAttackIndexLookup [12][64][64]uint8
	eightInitialized       bool
)

func initEightTables() {
	if eightInitialized {
		return
	}

	pieceInteractionMap := [6][6]int32{
		{0, 1, -1, 2, -1, -1},
		{0, 1, 2, 3, 4, -1},
		{0, 1, 2, 3, -1, -1},
		{0, 1, 2, 3, -1, -1},
		{0, 1, 2, 3, 4, -1},
		{0, 1, 2, 3, -1, -1},
	}

	pieceTargetCount := [6]int32{6, 10, 8, 8, 10, 8}

	var offset int32 = 0
	var pieceOffset [12]int32
	var offsetTable [12]int32

	for color := 0; color < 2; color++ {
		for ptype := 0; ptype < 6; ptype++ {
			piece := ptype*2 + color
			var count int32 = 0

			for sq := 0; sq < 64; sq++ {
				eightPieceOffsetLookup[piece][sq] = count
				if ptype != 0 || (sq >= 8 && sq < 56) {
					atk := attacksEmpty(piece, sq)
					count += int32(bits.OnesCount64(atk))
				}
			}

			pieceOffset[piece] = count
			offsetTable[piece] = offset
			offset += pieceTargetCount[ptype] * count
		}
	}

	for attacking := 0; attacking < 12; attacking++ {
		for attacked := 0; attacked < 12; attacked++ {
			atkType := attacking >> 1
			atkColor := attacking & 1

			defType := attacked >> 1
			defColor := attacked & 1

			m := pieceInteractionMap[atkType][defType]
			base := offsetTable[attacking] +
				(int32(defColor)*(pieceTargetCount[atkType]/2)+m)*pieceOffset[attacking]

			enemy := atkColor != defColor
			semi := atkType == defType && (enemy || atkType != 0)
			excluded := m < 0

			var inner uint32
			if semi && !excluded {
				inner |= 1 << 30
			}
			if excluded {
				inner |= 1 << 31
			}
			inner |= uint32(base & 0x3FFFFFFF)
			eightPiecePairLookup[attacking][attacked] = inner
		}
	}

	for piece := 0; piece < 12; piece++ {
		for from := 0; from < 64; from++ {
			atk := attacksEmpty(piece, from)
			for to := 0; to < 64; to++ {
				eightAttackIndexLookup[piece][from][to] = uint8(bits.OnesCount64(atk & ((1 << to) - 1)))
			}
		}
	}

	eightInitialized = true
}

func attacksEmpty(piece int, square int) uint64 {
	ptype := piece >> 1
	color := piece & 1
	sq := types.Square(square)

	switch ptype {
	case 0: // Pawn
		return uint64(bitboard.PawnAttacksTable[color][sq])
	case 1: // Knight
		return uint64(bitboard.KnightAttacksTable[sq])
	case 2: // Bishop
		return uint64(bitboard.BishopAttacks(sq, 0))
	case 3: // Rook
		return uint64(bitboard.RookAttacks(sq, 0))
	case 4: // Queen
		return uint64(bitboard.BishopAttacks(sq, 0) | bitboard.RookAttacks(sq, 0))
	case 5: // King
		return uint64(bitboard.KingAttacksTable[sq])
	}
	return 0
}

func attacksWithOcc(piece int, square int, occ uint64) uint64 {
	ptype := piece >> 1
	sq := types.Square(square)
	occBB := bitboard.Bitboard(occ)

	switch ptype {
	case 0: // Pawn
		color := piece & 1
		return uint64(bitboard.PawnAttacksTable[color][sq])
	case 1: // Knight
		return uint64(bitboard.KnightAttacksTable[sq])
	case 2: // Bishop
		return uint64(bitboard.BishopAttacks(sq, occBB))
	case 3: // Rook
		return uint64(bitboard.RookAttacks(sq, occBB))
	case 4: // Queen
		return uint64(bitboard.BishopAttacks(sq, occBB) | bitboard.RookAttacks(sq, occBB))
	case 5: // King
		return uint64(bitboard.KingAttacksTable[sq])
	}
	return 0
}

func getThreatIndex(piece int, from int, attacked int, to int, mirrored bool, pov int) int {
	if pov == 1 {
		from ^= 56
		to ^= 56
	}
	if mirrored {
		from ^= 7
		to ^= 7
	}

	attacking := piece ^ pov
	attackedP := attacked ^ pov

	inner := eightPiecePairLookup[attacking][attackedP]
	var below uint32 = 0
	if from < to {
		below = 1
	}

	pairBase := int32((inner + (below << 30)) & 0x80FFFFFF)
	if pairBase < 0 {
		return -1
	}

	idx := int(pairBase) + int(eightPieceOffsetLookup[attacking][from]) + int(eightAttackIndexLookup[attacking][from][to])
	if idx >= EightThreatFeatures {
		return -1
	}
	return idx
}

// EightNetwork holds the deserialized weights of an 8Chess 1024-wide NNUE model.
type EightNetwork struct {
	FTThreatWeights []int8                  // [EightThreatFeatures * EightL1Size]
	FTPieceWeights  []int16                 // [EightPieceFeatures * EightL1Size]
	FTBiases        [EightL1Size]int16      // [EightL1Size]
	L1Weights       [EightOutputBuckets][EightL2Size * EightL1Size]int8
	L1Biases        [EightOutputBuckets][EightL2Size]float32
	L2Weights       [EightOutputBuckets][EightL2Size][EightL3Size]float32
	L2Biases        [EightOutputBuckets][EightL3Size]float32
	L3Weights       [EightOutputBuckets][EightL3Size]float32
	L3Biases        [EightOutputBuckets]float32
}

var ActiveEightNetwork *EightNetwork

// LoadEightFile loads an 84,349,504-byte 8Chess NNUE model.
func LoadEightFile(path string) (*EightNetwork, error) {
	initEightTables()

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() != EightNetworkSize {
		return nil, fmt.Errorf("invalid 8Chess network size: expected %d bytes, got %d", EightNetworkSize, fi.Size())
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return LoadEightBytes(data)
}

// LoadEightBytes loads an EightNetwork from raw byte data.
func LoadEightBytes(data []byte) (*EightNetwork, error) {
	initEightTables()

	if len(data) != EightNetworkSize {
		return nil, fmt.Errorf("invalid byte size for 8Chess network: expected %d, got %d", EightNetworkSize, len(data))
	}

	net := &EightNetwork{}

	// 1. FT Threat Weights: offset 0, size 68,468,736 (66864 * 1024 bytes)
	net.FTThreatWeights = make([]int8, EightThreatFeatures*EightL1Size)
	threatRaw := data[0 : EightThreatFeatures*EightL1Size]
	for i, b := range threatRaw {
		net.FTThreatWeights[i] = int8(b)
	}

	// 2. FT Piece Weights: offset 68,468,736, size 15,728,640 (7680 * 1024 int16s = 15,728,640 bytes)
	net.FTPieceWeights = make([]int16, EightPieceFeatures*EightL1Size)
	pieceOffset := EightThreatFeatures * EightL1Size
	pieceRaw := data[pieceOffset : pieceOffset+EightPieceFeatures*EightL1Size*2]
	for i := 0; i < EightPieceFeatures*EightL1Size; i++ {
		net.FTPieceWeights[i] = int16(binary.LittleEndian.Uint16(pieceRaw[i*2:]))
	}

	// 3. FT Biases: offset 84,197,376, size 2,048 (1024 int16s)
	biasOffset := pieceOffset + EightPieceFeatures*EightL1Size*2
	for i := 0; i < EightL1Size; i++ {
		net.FTBiases[i] = int16(binary.LittleEndian.Uint16(data[biasOffset+i*2:]))
	}

	// 4. L1 Weights: offset 84,199,424, size 131,072 (8 * 16 * 1024 int8s)
	l1WOffset := biasOffset + EightL1Size*2
	for b := 0; b < EightOutputBuckets; b++ {
		for i := 0; i < EightL2Size*EightL1Size; i++ {
			idx := l1WOffset + b*(EightL2Size*EightL1Size) + i
			net.L1Weights[b][i] = int8(data[idx])
		}
	}

	// 5. L1 Biases: offset 84,330,496, size 512 (8 * 16 float32s)
	l1BOffset := l1WOffset + EightOutputBuckets*(EightL2Size*EightL1Size)
	for b := 0; b < EightOutputBuckets; b++ {
		for i := 0; i < EightL2Size; i++ {
			idx := l1BOffset + (b*EightL2Size+i)*4
			bitsVal := binary.LittleEndian.Uint32(data[idx:])
			net.L1Biases[b][i] = *(*float32)(unsafe.Pointer(&bitsVal))
		}
	}

	// 6. L2 Weights: offset 84,331,008, size 16,384 (8 * 16 * 32 float32s)
	l2WOffset := l1BOffset + EightOutputBuckets*EightL2Size*4
	for b := 0; b < EightOutputBuckets; b++ {
		for i := 0; i < EightL2Size; i++ {
			for j := 0; j < EightL3Size; j++ {
				idx := l2WOffset + ((b*EightL2Size+i)*EightL3Size+j)*4
				bitsVal := binary.LittleEndian.Uint32(data[idx:])
				net.L2Weights[b][i][j] = *(*float32)(unsafe.Pointer(&bitsVal))
			}
		}
	}

	// 7. L2 Biases: offset 84,347,392, size 1,024 (8 * 32 float32s)
	l2BOffset := l2WOffset + EightOutputBuckets*EightL2Size*EightL3Size*4
	for b := 0; b < EightOutputBuckets; b++ {
		for i := 0; i < EightL3Size; i++ {
			idx := l2BOffset + (b*EightL3Size+i)*4
			bitsVal := binary.LittleEndian.Uint32(data[idx:])
			net.L2Biases[b][i] = *(*float32)(unsafe.Pointer(&bitsVal))
		}
	}

	// 8. L3 Weights: offset 84,348,416, size 1,024 (8 * 32 float32s)
	l3WOffset := l2BOffset + EightOutputBuckets*EightL3Size*4
	for b := 0; b < EightOutputBuckets; b++ {
		for i := 0; i < EightL3Size; i++ {
			idx := l3WOffset + (b*EightL3Size+i)*4
			bitsVal := binary.LittleEndian.Uint32(data[idx:])
			net.L3Weights[b][i] = *(*float32)(unsafe.Pointer(&bitsVal))
		}
	}

	// 9. L3 Biases: offset 84,349,440, size 32 (8 float32s)
	l3BOffset := l3WOffset + EightOutputBuckets*EightL3Size*4
	for b := 0; b < EightOutputBuckets; b++ {
		idx := l3BOffset + b*4
		bitsVal := binary.LittleEndian.Uint32(data[idx:])
		net.L3Biases[b] = *(*float32)(unsafe.Pointer(&bitsVal))
	}

	return net, nil
}

// EvaluateEight performs forward inference using the EightChess architecture.
func EvaluateEight(net *EightNetwork, b *board.Board) int32 {
	if net == nil {
		return 0
	}

	initEightTables()

	var accPst [2][EightL1Size]int16
	var accThr [2][EightL1Size]int16

	// Base biases
	accPst[0] = net.FTBiases
	accPst[1] = net.FTBiases

	occBB := b.Occupied()
	occ := uint64(occBB)
	occCount := bits.OnesCount64(occ)

	kingSq0 := b.KingSquare(types.White)
	kingSq1 := b.KingSquare(types.Black)

	kingside0 := (int(kingSq0) & 7) >= 4
	kingside1 := (int(kingSq1) & 7) >= 4

	flip0 := 0
	if kingside0 {
		flip0 = 7
	}
	flip1 := 56
	if kingside1 {
		flip1 ^= 7
	}

	bucket0 := EightInputBucketsLayout[int(kingSq0)^flip0]
	bucket1 := EightInputBucketsLayout[int(kingSq1)^flip1]

	// 1. Piece features & Threat features in a single board pass
	occScan := occBB
	for occScan != 0 {
		sqBB := bitboard.PopLSB(&occScan)
		sq := int(sqBB)
		piece := b.PieceAt(types.Square(sq))
		if piece == types.PieceNone {
			continue
		}

		pColor := int(piece.Color())
		ptype := int(piece.Type()) - 1 // Pawn=0..King=5
		pieceCode := ptype*2 + pColor

		// Piece features for POV 0 (White)
		diffColor0 := pColor // 0 if White, 1 if Black
		idx0 := bucket0*768 + 384*diffColor0 + 64*ptype + (sq ^ flip0)
		wOff0 := idx0 * EightL1Size
		addVector1024NEON(&accPst[0], &net.FTPieceWeights[wOff0])

		// Piece features for POV 1 (Black)
		diffColor1 := pColor ^ 1 // 0 if Black, 1 if White
		idx1 := bucket1*768 + 384*diffColor1 + 64*ptype + (sq ^ flip1)
		wOff1 := idx1 * EightL1Size
		addVector1024NEON(&accPst[1], &net.FTPieceWeights[wOff1])

		// Threat features for this piece attacking occupied targets
		targets := attacksWithOcc(pieceCode, sq, occ) & occ
		for targets != 0 {
			to := bits.TrailingZeros64(targets)
			targets &= targets - 1

			targetObj := b.PieceAt(types.Square(to))
			if targetObj == types.PieceNone {
				continue
			}
			tColor := int(targetObj.Color())
			tType := int(targetObj.Type()) - 1
			attackedCode := tType*2 + tColor

			// Threat for POV 0 (White)
			thrIdx0 := getThreatIndex(pieceCode, sq, attackedCode, to, kingside0, 0)
			if thrIdx0 >= 0 {
				wOff := thrIdx0 * EightL1Size
				addThreatVector1024NEON(&accThr[0], &net.FTThreatWeights[wOff])
			}

			// Threat for POV 1 (Black)
			thrIdx1 := getThreatIndex(pieceCode, sq, attackedCode, to, kingside1, 1)
			if thrIdx1 >= 0 {
				wOff := thrIdx1 * EightL1Size
				addThreatVector1024NEON(&accThr[1], &net.FTThreatWeights[wOff])
			}
		}
	}

	// 3. Activation (per flip)
	var ftOut [EightL1Size]uint8
	stm := int(b.SideToMove)

	for flip := 0; flip < 2; flip++ {
		pov := stm ^ flip
		activateEightFTNEON(&ftOut[flip*(EightL1Size/2)], &accPst[pov][0], &accThr[pov][0])
	}

	// 4. Layer 1 Propagation
	bucket := EightOutputBucketsLayout[occCount]
	var preActivations [EightL2Size]int32
	l1w := &net.L1Weights[bucket]

	chunks := EightL1Size / 4 // 256 chunks of 4 bytes
	for c := 0; c < chunks; c++ {
		quad := *(*uint32)(unsafe.Pointer(&ftOut[c*4]))
		if quad == 0 {
			continue
		}

		b0 := int32(byte(quad))
		b1 := int32(byte(quad >> 8))
		b2 := int32(byte(quad >> 16))
		b3 := int32(byte(quad >> 24))

		wBase := c * EightL2Size * 4
		weights := (*[64]int8)(unsafe.Pointer(&l1w[wBase]))
		for j := 0; j < EightL2Size; j++ {
			w0 := int32(weights[j*4+0])
			w1 := int32(weights[j*4+1])
			w2 := int32(weights[j*4+2])
			w3 := int32(weights[j*4+3])

			vector := b0*w0 + b1*w1 + b2*w2 + b3*w3
			preActivations[j] += vector
		}
	}

	var l1Out [EightL2Size]float32
	for i := 0; i < EightL2Size; i++ {
		val := float32(preActivations[i])*EightDequantMultiplier + net.L1Biases[bucket][i]
		if val < 0.0 {
			val = 0.0
		} else if val > 1.0 {
			val = 1.0
		}
		l1Out[i] = val
	}

	// 5. Layer 2 Propagation
	var l2Out [EightL3Size]float32
	for j := 0; j < EightL3Size; j++ {
		sum := net.L2Biases[bucket][j]
		for i := 0; i < EightL2Size; i++ {
			sum += net.L2Weights[bucket][i][j] * l1Out[i]
		}
		if sum < 0.0 {
			sum = 0.0
		} else if sum > 1.0 {
			sum = 1.0
		}
		l2Out[j] = sum
	}

	// 6. Layer 3 Propagation (Halving reduction)
	var sums [16]float32
	for j := 0; j < 16; j++ {
		sums[j] = net.L3Weights[bucket][j]*l2Out[j] + net.L3Weights[bucket][j+16]*l2Out[j+16]
	}

	stride := 8
	for stride > 0 {
		for i := 0; i < stride; i++ {
			sums[i] += sums[i+stride]
		}
		stride /= 2
	}

	l3Out := sums[0] + net.L3Biases[bucket]
	return int32(l3Out * EightNetworkScale)
}
