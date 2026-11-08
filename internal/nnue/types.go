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
