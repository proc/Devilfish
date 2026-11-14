package nnue

import "fmt"

const (
	NumFeatures = 768 // 12 pieces * 64 squares
	HiddenSize  = 256 // Perspective accumulator size
	Layer1Size  = 32  // First hidden layer size
	OutputScale = 16  // Centipawn scaling divisor
)

// Accumulator holds the 16-bit integer accumulator vectors for White and Black perspectives.
type Accumulator struct {
	White [HiddenSize]int16
	Black [HiddenSize]int16
}

// Network holds the weights and biases for the NNUE neural network.
type Network struct {
	// Feature Transformer (768 -> 256)
	Biases  [HiddenSize]int16
	Weights [NumFeatures][HiddenSize]int16

	// Layer 1 (512 -> 32)
	L1Biases  [Layer1Size]int32
	L1Weights [Layer1Size][2 * HiddenSize]int8

	// Output Layer (32 -> 1)
	OutBias    int32
	OutWeights [Layer1Size]int16
}

var ActiveNetwork *Network
var Enabled = true
var CurrentFilePath = "<embedded>"

// NetworkInfo contains metadata and statistics about the active neural network.
type NetworkInfo struct {
	Enabled       bool
	Architecture  string
	FilePath      string
	SIMD          string
	FeatureCount  int
	HiddenSize    int
	Layer1Size    int
	NumParameters int
	SizeBytes     int
}

// GetNetworkInfo returns the current network architecture details, parameter counts, and SIMD status.
func GetNetworkInfo() NetworkInfo {
	path := CurrentFilePath
	if path == "" {
		path = "<embedded>"
	}
	if ActiveEightNetwork.IsSF() {
		features := sfPieceFeatures + sfThreatRows
		return NetworkInfo{
			Enabled:       Enabled,
			Architecture:  fmt.Sprintf("Stockfish SFNNv20 (Piece 67584 + Threat 59808 + Pawn pair 4560 -> 1024 -> 32 -> 32 -> 1, %d layer stacks)", len(ActiveEightNetwork.sf.stacks)),
			FilePath:      path,
			SIMD:          SIMDInstructionSet,
			FeatureCount:  features,
			HiddenSize:    EightL1Size,
			Layer1Size:    sfL2,
			NumParameters: features*EightL1Size + EightL1Size + len(ActiveEightNetwork.sf.stacks)*(sfL2*(EightL1Size+1)+sfL3*(2*sfL2+1)+2*sfL2+2*sfL3+1),
			SizeBytes:     sfRows * EightL1Size,
		}
	}
	if ActiveEightNetwork != nil {
		numParams := EightThreatFeatures*EightL1Size + EightPieceFeatures*EightL1Size + EightL1Size +
			EightOutputBuckets*(EightL2Size*EightL1Size+EightL2Size+EightL2Size*EightL3Size+EightL3Size+EightL3Size+1)
		return NetworkInfo{
			Enabled:       Enabled,
			Architecture:  "8Chess 1024-wide (Piece 7680 + Threat 66864 -> 1024 -> 16x8 -> 32x8 -> 1x8)",
			FilePath:      path,
			SIMD:          SIMDInstructionSet,
			FeatureCount:  EightThreatFeatures + EightPieceFeatures,
			HiddenSize:    EightL1Size,
			Layer1Size:    EightL2Size,
			NumParameters: numParams,
			SizeBytes:     EightNetworkSize,
		}
	}

	numParams := HiddenSize + (NumFeatures * HiddenSize) + Layer1Size + (Layer1Size * 2 * HiddenSize) + 1 + Layer1Size
	sizeBytes := (HiddenSize * 2) + (NumFeatures * HiddenSize * 2) + (Layer1Size * 4) + (Layer1Size * 2 * HiddenSize * 1) + 4 + (Layer1Size * 2)
	return NetworkInfo{
		Enabled:       Enabled && ActiveNetwork != nil,
		Architecture:  "(768 -> 256x2) -> 32 -> 1",
		FilePath:      path,
		SIMD:          SIMDInstructionSet,
		FeatureCount:  NumFeatures,
		HiddenSize:    HiddenSize,
		Layer1Size:    Layer1Size,
		NumParameters: numParams,
		SizeBytes:     sizeBytes,
	}
}
