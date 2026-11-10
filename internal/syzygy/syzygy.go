//go:build !nocgo && cgo

package syzygy

/*
#cgo CFLAGS: -O3 -std=gnu11 -w
#include "tbprobe.h"
#include <stdlib.h>
*/
import "C"
import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/logger"
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

// NormalizePath resolves path elements, automatically expanding directories that contain
// subdirectories of tablebases (such as Syzygy345WDL and Syzygy345DTZ), and formats them
// with the platform separator expected by Fathom.
func NormalizePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}

	rawDirs := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == ':' || r == ';'
	})

	var validDirs []string
	seen := make(map[string]bool)

	addDir := func(d string) {
		abs, err := filepath.Abs(d)
		if err == nil {
			d = abs
		}
		if !seen[d] {
			seen[d] = true
			validDirs = append(validDirs, d)
		}
	}

	hasTBFiles := func(dir string) bool {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, e := range entries {
			if !e.IsDir() {
				name := strings.ToLower(e.Name())
				if strings.HasSuffix(name, ".rtbw") || strings.HasSuffix(name, ".rtbz") {
					return true
				}
			}
		}
		return false
	}

	for _, d := range rawDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}

		fi, err := os.Stat(d)
		if err != nil || !fi.IsDir() {
			continue
		}

		foundAny := false
		if hasTBFiles(d) {
			addDir(d)
			foundAny = true
		}

		// Also check immediate subdirectories (e.g. Syzygy345WDL, Syzygy345DTZ, etc.)
		entries, err := os.ReadDir(d)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					subDir := filepath.Join(d, e.Name())
					if hasTBFiles(subDir) {
						addDir(subDir)
						foundAny = true
					}
				}
			}
		}

		if !foundAny {
			addDir(d)
		}
	}

	if len(validDirs) == 0 {
		return trimmed
	}

	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	return strings.Join(validDirs, sep)
}

func Init(path string) bool {
	mu.Lock()
	defer mu.Unlock()

	trimmed := strings.TrimSpace(path)
	Path = trimmed
	if trimmed == "" {
		C.tb_free()
		maxPiecesCount = 0
		logger.Infof("Syzygy tablebases disabled: no path configured")
		return true
	}

	normalized := NormalizePath(trimmed)

	cPath := C.CString(normalized)
	defer C.free(unsafe.Pointer(cPath))

	success := bool(C.tb_init(cPath))
	maxPiecesCount = int(C.TB_LARGEST)
	if maxPiecesCount > 0 {
		logger.Infof("Syzygy tablebases loaded successfully: %d-piece tablebases detected (configured: %q, normalized: %q)", maxPiecesCount, Path, normalized)
	} else {
		logger.Warnf("Syzygy tablebases failed to load: 0 tablebases detected (configured: %q, normalized: %q)", Path, normalized)
	}
	return success
}

func Free() {
	mu.Lock()
	defer mu.Unlock()
	C.tb_free()
	maxPiecesCount = 0
	Path = ""
	logger.Infof("Syzygy tablebases unloaded / freed")
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

	var results [C.TB_MAX_MOVES]C.uint

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
		&results[0],
	)

	if res == C.TB_RESULT_FAILED || res == C.TB_RESULT_CHECKMATE || res == C.TB_RESULT_STALEMATE {
		return types.MoveNone, 0, 0, false
	}

	legalMoves := movegen.GenerateLegalMoves(b)
	if len(legalMoves) == 0 {
		return types.MoveNone, 0, 0, false
	}

	type candMove struct {
		m   types.Move
		wdl int
		dtz int
	}

	var candidates []candMove
	maxWdl := -1

	for i := 0; i < int(C.TB_MAX_MOVES); i++ {
		r := results[i]
		if r == C.TB_RESULT_FAILED {
			break
		}

		w := int(C.tb_get_wdl_go(r))
		d := int(C.tb_get_dtz_go(r))
		from := types.Square(C.tb_get_from_go(r))
		to := types.Square(C.tb_get_to_go(r))
		prom := uint(C.tb_get_promotes_go(r))
		isEp := C.tb_get_ep_go(r) != 0

		var promoType types.PieceType = types.PieceTypeNone
		switch prom {
		case 1:
			promoType = types.Queen
		case 2:
			promoType = types.Rook
		case 3:
			promoType = types.Bishop
		case 4:
			promoType = types.Knight
		}

		for _, m := range legalMoves {
			if m.From() == from && m.To() == to {
				match := false
				if promoType != types.PieceTypeNone {
					match = m.IsPromotion() && m.PromotionType() == promoType
				} else if isEp {
					match = m.IsEnPassant()
				} else {
					match = !m.IsPromotion() && !m.IsEnPassant()
				}
				if match {
					candidates = append(candidates, candMove{m: m, wdl: w, dtz: d})
					if w > maxWdl {
						maxWdl = w
					}
					break
				}
			}
		}
	}

	// If we have candidates from results, pick the highest quality move matching maxWdl
	if len(candidates) > 0 && maxWdl >= 0 {
		inCheck := b.IsInCheck(b.SideToMove)
		checkers := b.Checkers(b.SideToMove)

		var best candMove
		bestScore := int32(-1_000_000_000)

		for _, c := range candidates {
			if c.wdl != maxWdl {
				continue
			}

			m := c.m
			score := int32(0)

			// 1. If in check, CAPTURING THE CHECKER IS TOP PRIORITY!
			// Eliminates the checking piece to safely resolve the threat.
			if inCheck && (bitboard.SquareBB[m.To()]&checkers) != 0 {
				score += 50_000_000
			}

			// 2. Material capture bonus: prefer eliminating enemy pieces over quiet moves
			if m.IsCapture() {
				attacker := b.Pieces[m.From()].Type()
				victim := b.Pieces[m.To()].Type()
				if m.IsEnPassant() {
					victim = types.Pawn
				}
				score += 10_000_000 + int32(victim)*1000 - int32(attacker)*100
			}

			// 3. Queen promotion
			if m.IsPromotion() && m.PromotionType() == types.Queen {
				score += 5_000_000
			}

			// 4. DTZ distance:
			// For Win: lower DTZ is faster win
			// For Loss: higher DTZ prolongs the game
			if maxWdl >= Win {
				score += int32(1000-c.dtz) * 1000
			} else if maxWdl <= Loss {
				score += int32(c.dtz) * 1000
			}

			if score > bestScore {
				bestScore = score
				best = c
			}
		}

		if best.m != types.MoveNone {
			return best.m, best.wdl, best.dtz, true
		}
	}

	// Fallback to single move decoded from res
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
