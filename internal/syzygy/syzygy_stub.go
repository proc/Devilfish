//go:build !cgo || nocgo

package syzygy

import (
	"devilfish/internal/board"
	"devilfish/internal/types"
)

const (
	Loss        = 0
	BlessedLoss = 1
	Draw        = 2
	CursedWin   = 3
	Win         = 4
)

var (
	Path          string = ""
	ProbeDepth    int    = 1
	Use50MoveRule bool   = true
	ProbeLimit    int    = 6
)

func Init(path string) bool {
	Path = path
	return false
}

func Free() {
	Path = ""
}

func MaxPieces() int {
	return 0
}

func IsAvailable() bool {
	return false
}

func ProbeWDL(b *board.Board) (wdl int, ok bool) {
	return 0, false
}

func ProbeRoot(b *board.Board) (bestMove types.Move, wdl int, dtz int, ok bool) {
	return types.MoveNone, 0, 0, false
}
