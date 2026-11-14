package nnue

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoadFile loads an NNUE network file from disk and sets it as the ActiveNetwork.
//
// The name "<stockfish>" (StockfishName) selects the Stockfish network built
// into the binary instead of a file.
func LoadFile(path string) error {
	if strings.EqualFold(strings.TrimSpace(path), StockfishName) {
		net, err := EmbeddedSF()
		if err != nil {
			return fmt.Errorf("embedded Stockfish network: %w", err)
		}
		ActiveEightNetwork = net
		ActiveNetwork = nil
		Enabled = true
		CurrentFilePath = StockfishName + " (" + EmbeddedSFName + ")"
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open nnue file: %w", err)
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return err
	}

	if fi.Size() == EightNetworkSize {
		eightNet, err := LoadEightFile(path)
		if err != nil {
			return err
		}
		ActiveEightNetwork = eightNet
		ActiveNetwork = nil
		Enabled = true
		CurrentFilePath = path
		return nil
	}

	expectedSize := int64((HiddenSize * 2) + (NumFeatures * HiddenSize * 2) + (Layer1Size * 4) + (Layer1Size * 2 * HiddenSize * 1) + 4 + (Layer1Size * 2))

	// Stockfish networks start with a version word.
	var magic uint32
	if err := binary.Read(file, binary.LittleEndian, &magic); err == nil {
		switch magic {
		case SFVersion:
			sfNet, err := LoadSFFile(path)
			if err != nil {
				return fmt.Errorf("Stockfish network %s: %w", path, err)
			}
			ActiveEightNetwork = sfNet
			ActiveNetwork = nil
			Enabled = true
			CurrentFilePath = path
			return nil
		case 0x7AF32F20, 0x7f0b0fc6:
			return fmt.Errorf("unsupported NNUE network: %s is an older Stockfish-format network (version %#08x, %.1f MB); the Stockfish format supported is version %#08x, architecture SFNNv20 (threat inputs, 1024-wide)", path, magic, float64(fi.Size())/(1024*1024), uint32(SFVersion))
		}
		_, _ = file.Seek(0, io.SeekStart)
	}

	if fi.Size() != expectedSize {
		return fmt.Errorf("invalid NNUE network size: got %d bytes, expected %d bytes (768->256) or %d bytes (8Chess 1024-wide)", fi.Size(), expectedSize, EightNetworkSize)
	}

	net, err := LoadReader(file)
	if err != nil {
		return err
	}

	ActiveNetwork = net
	ActiveEightNetwork = nil
	Enabled = true
	CurrentFilePath = path
	return nil
}

// LoadBytes parses an NNUE network from raw byte data.
func LoadBytes(data []byte) (*Network, error) {
	expectedSize := (HiddenSize * 2) + (NumFeatures * HiddenSize * 2) + (Layer1Size * 4) + (Layer1Size * 2 * HiddenSize * 1) + 4 + (Layer1Size * 2)
	if len(data) != expectedSize {
		return nil, fmt.Errorf("invalid byte size for NNUE: got %d, expected %d", len(data), expectedSize)
	}
	return LoadReader(bytes.NewReader(data))
}

// LoadReader parses an NNUE network from an io.Reader stream.
func LoadReader(r io.Reader) (*Network, error) {
	net := &Network{}

	// Read Feature Transformer Biases (256 * 2 bytes)
	if err := binary.Read(r, binary.LittleEndian, &net.Biases); err != nil {
		return nil, fmt.Errorf("failed to read feature transformer biases: %w", err)
	}

	// Read Feature Transformer Weights (768 * 256 * 2 bytes)
	if err := binary.Read(r, binary.LittleEndian, &net.Weights); err != nil {
		return nil, fmt.Errorf("failed to read feature transformer weights: %w", err)
	}

	// Read Layer 1 Biases (32 * 4 bytes)
	if err := binary.Read(r, binary.LittleEndian, &net.L1Biases); err != nil {
		return nil, fmt.Errorf("failed to read layer 1 biases: %w", err)
	}

	// Read Layer 1 Weights (32 * 512 bytes)
	if err := binary.Read(r, binary.LittleEndian, &net.L1Weights); err != nil {
		return nil, fmt.Errorf("failed to read layer 1 weights: %w", err)
	}

	// Read Output Bias (4 bytes)
	if err := binary.Read(r, binary.LittleEndian, &net.OutBias); err != nil {
		return nil, fmt.Errorf("failed to read output bias: %w", err)
	}

	// Read Output Weights (32 * 2 bytes)
	if err := binary.Read(r, binary.LittleEndian, &net.OutWeights); err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read output weights: %w", err)
	}

	return net, nil
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
