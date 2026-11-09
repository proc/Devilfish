//go:build !nocgo && cgo

package syzygy

/*
#cgo CFLAGS: -O3 -std=gnu11 -w
#include "tbprobe.h"
#include <stdlib.h>
*/
import "C"
import (
	"strings"
	"sync"
	"unsafe"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/movegen"
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
	mu             sync.RWMutex
	Path           string = ""
	ProbeDepth     int    = 1
	Use50MoveRule  bool   = true
	ProbeLimit     int    = 6
	maxPiecesCount int    = 0
)

func Init(path string) bool {
	mu.Lock()
	defer mu.Unlock()

	trimmed := strings.TrimSpace(path)
	Path = trimmed
	if trimmed == "" {
		C.tb_free()
		maxPiecesCount = 0
		return true
	}

	cPath := C.CString(trimmed)
	defer C.free(unsafe.Pointer(cPath))

	success := bool(C.tb_init(cPath))
	maxPiecesCount = int(C.TB_LARGEST)
	return success
}

func Free() {
	mu.Lock()
	defer mu.Unlock()
	C.tb_free()
	maxPiecesCount = 0
	Path = ""
}

func maxPiecesLocked() int {
	limit := ProbeLimit
	if limit > 0 && limit < maxPiecesCount {
		return limit
	}
	return maxPiecesCount
}

func MaxPieces() int {
	mu.RLock()
	defer mu.RUnlock()
	return maxPiecesLocked()
}

func IsAvailable() bool {
	mu.RLock()
	defer mu.RUnlock()
	return maxPiecesCount > 0
}

func extractBitboards(b *board.Board) (white, black, kings, queens, rooks, bishops, knights, pawns uint64, ep uint32) {
	white = uint64(b.ColorBB[types.White])
	black = uint64(b.ColorBB[types.Black])

	kings = uint64(b.PieceBB[types.MakePiece(types.White, types.King)] |
		b.PieceBB[types.MakePiece(types.Black, types.King)])
	queens = uint64(b.PieceBB[types.MakePiece(types.White, types.Queen)] |
		b.PieceBB[types.MakePiece(types.Black, types.Queen)])
	rooks = uint64(b.PieceBB[types.MakePiece(types.White, types.Rook)] |
		b.PieceBB[types.MakePiece(types.Black, types.Rook)])
	bishops = uint64(b.PieceBB[types.MakePiece(types.White, types.Bishop)] |
		b.PieceBB[types.MakePiece(types.Black, types.Bishop)])
	knights = uint64(b.PieceBB[types.MakePiece(types.White, types.Knight)] |
		b.PieceBB[types.MakePiece(types.Black, types.Knight)])
	pawns = uint64(b.PieceBB[types.MakePiece(types.White, types.Pawn)] |
		b.PieceBB[types.MakePiece(types.Black, types.Pawn)])

	if b.EPSquare != types.SquareNone && b.EPSquare < 64 {
		ep = uint32(b.EPSquare)
	}
	return
}

func ProbeWDL(b *board.Board) (wdl int, ok bool) {
	maxP := MaxPieces()
	if maxP == 0 || b.Castling != 0 {
		return 0, false
	}

	occupiedCount := bitboard.Count(b.Occupied())
	if occupiedCount > maxP {
		return 0, false
	}

	white, black, kings, queens, rooks, bishops, knights, pawns, ep := extractBitboards(b)

	res := C.tb_probe_wdl(
		C.uint64_t(white),
		C.uint64_t(black),
		C.uint64_t(kings),
		C.uint64_t(queens),
		C.uint64_t(rooks),
		C.uint64_t(bishops),
		C.uint64_t(knights),
		C.uint64_t(pawns),
		C.uint(b.HalfmoveClock),
		C.uint(0),
		C.uint(ep),
		C.bool(b.SideToMove == types.White),
	)

	if res == C.TB_RESULT_FAILED {
		return 0, false
	}

	return int(res), true
}

func ProbeRoot(b *board.Board) (bestMove types.Move, wdl int, dtz int, ok bool) {
	mu.Lock()
	defer mu.Unlock()

	maxP := maxPiecesLocked()
	if maxP == 0 || b.Castling != 0 {
		return types.MoveNone, 0, 0, false
	}

	occupiedCount := bitboard.Count(b.Occupied())
	if occupiedCount > maxP {
		return types.MoveNone, 0, 0, false
	}

	white, black, kings, queens, rooks, bishops, knights, pawns, ep := extractBitboards(b)

	res := C.tb_probe_root(
		C.uint64_t(white),
		C.uint64_t(black),
		C.uint64_t(kings),
		C.uint64_t(queens),
		C.uint64_t(rooks),
		C.uint64_t(bishops),
		C.uint64_t(knights),
		C.uint64_t(pawns),
		C.uint(b.HalfmoveClock),
		C.uint(0),
		C.uint(ep),
		C.bool(b.SideToMove == types.White),
		nil,
	)

	if res == C.TB_RESULT_FAILED || res == C.TB_RESULT_CHECKMATE || res == C.TB_RESULT_STALEMATE {
		return types.MoveNone, 0, 0, false
	}

	wdlVal := int(C.tb_get_wdl_go(res))
	dtzVal := int(C.tb_get_dtz_go(res))
	fromSq := types.Square(C.tb_get_from_go(res))
	toSq := types.Square(C.tb_get_to_go(res))
	promVal := uint(C.tb_get_promotes_go(res))
	isEp := C.tb_get_ep_go(res) != 0

	var promoType types.PieceType = types.PieceTypeNone
	switch promVal {
	case 1: // TB_PROMOTES_QUEEN
		promoType = types.Queen
	case 2: // TB_PROMOTES_ROOK
		promoType = types.Rook
	case 3: // TB_PROMOTES_BISHOP
		promoType = types.Bishop
	case 4: // TB_PROMOTES_KNIGHT
		promoType = types.Knight
	}

	legalMoves := movegen.GenerateLegalMoves(b)
	for _, m := range legalMoves {
		if m.From() == fromSq && m.To() == toSq {
			if promoType != types.PieceTypeNone {
				if m.IsPromotion() && m.PromotionType() == promoType {
					return m, wdlVal, dtzVal, true
				}
			} else if isEp {
				if m.IsEnPassant() {
					return m, wdlVal, dtzVal, true
				}
			} else {
				return m, wdlVal, dtzVal, true
			}
		}
	}

	return types.MoveNone, 0, 0, false
}
