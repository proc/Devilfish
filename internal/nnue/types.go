package nnue

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
