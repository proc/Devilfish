package nnue

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// LoadFile loads an NNUE network file from disk and sets it as the ActiveNetwork.
func LoadFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open nnue file: %w", err)
	}
	defer file.Close()

	net := &Network{}

	// Read Feature Transformer Biases (256 * 2 bytes)
	if err := binary.Read(file, binary.LittleEndian, &net.Biases); err != nil {
		return fmt.Errorf("failed to read feature transformer biases: %w", err)
	}

	// Read Feature Transformer Weights (768 * 256 * 2 bytes)
	if err := binary.Read(file, binary.LittleEndian, &net.Weights); err != nil {
		return fmt.Errorf("failed to read feature transformer weights: %w", err)
	}

	// Read Layer 1 Biases (32 * 4 bytes)
	if err := binary.Read(file, binary.LittleEndian, &net.L1Biases); err != nil {
		return fmt.Errorf("failed to read layer 1 biases: %w", err)
	}

	// Read Layer 1 Weights (32 * 512 bytes)
	if err := binary.Read(file, binary.LittleEndian, &net.L1Weights); err != nil {
		return fmt.Errorf("failed to read layer 1 weights: %w", err)
	}

	// Read Output Bias (4 bytes)
	if err := binary.Read(file, binary.LittleEndian, &net.OutBias); err != nil {
		return fmt.Errorf("failed to read output bias: %w", err)
	}

	// Read Output Weights (32 * 2 bytes)
	if err := binary.Read(file, binary.LittleEndian, &net.OutWeights); err != nil && err != io.EOF {
		return fmt.Errorf("failed to read output weights: %w", err)
	}

	ActiveNetwork = net
	Enabled = true
	CurrentFilePath = path
	return nil
}

// SaveFile exports the current ActiveNetwork to an NNUE file.
func SaveFile(path string) error {
	if ActiveNetwork == nil {
		return fmt.Errorf("no active network to save")
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create nnue file: %w", err)
	}
	defer file.Close()

	if err := binary.Write(file, binary.LittleEndian, &ActiveNetwork.Biases); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, &ActiveNetwork.Weights); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, &ActiveNetwork.L1Biases); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, &ActiveNetwork.L1Weights); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, &ActiveNetwork.OutBias); err != nil {
		return err
	}
	if err := binary.Write(file, binary.LittleEndian, &ActiveNetwork.OutWeights); err != nil {
		return err
	}

	return nil
}
