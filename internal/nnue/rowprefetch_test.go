package nnue

import (
	"testing"
	"unsafe"
)

// prefetchRows must tolerate an empty list and rows at both ends of the
// weight tables, and change nothing.
func TestPrefetchRows(t *testing.T) {
	net := ActiveEightNetwork
	if net == nil || net.IsSF() {
		t.Skip("8Chess network not active")
	}
	lastPiece := int32(len(net.FTPieceWeights)/EightL1Size - 1)
	lastThreat := int32(len(net.FTThreatWeights)/EightL1Size - 1)
	pieceRows := []int32{0, lastPiece, 7}
	threatRows := []int32{0, lastThreat, 1234}
	before0, beforeN := net.FTPieceWeights[0], net.FTThreatWeights[len(net.FTThreatWeights)-1]

	prefetchRows(unsafe.Pointer(&net.FTPieceWeights[0]), &pieceRows[0], 0, 11, 2)
	prefetchRows(unsafe.Pointer(&net.FTPieceWeights[0]), &pieceRows[0], len(pieceRows), 11, 32) // whole 2 KB rows
	prefetchRows(unsafe.Pointer(&net.FTThreatWeights[0]), &threatRows[0], len(threatRows), 10, 16) // whole 1 KB rows

	if net.FTPieceWeights[0] != before0 || net.FTThreatWeights[len(net.FTThreatWeights)-1] != beforeN {
		t.Fatal("prefetch changed memory")
	}
}
