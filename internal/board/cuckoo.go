package board

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/types"
)

// Upcoming-repetition detection (Marcel van Kervinck's cuckoo tables, as in
// Stockfish). cuckoo holds, for every reversible move of a piece on an empty
// board, the hash difference that move makes; cuckooMove its two squares.
// If the current position and one an odd number of plies back differ by
// exactly such a key, one move takes the side to move back to that position.
var (
	cuckoo     [8192]uint64
	cuckooMove [8192]uint16 // from | to<<6
)

func cuckooH1(k uint64) int { return int(k & 0x1fff) }
func cuckooH2(k uint64) int { return int((k >> 16) & 0x1fff) }

// initCuckoo fills the tables. It needs the Zobrist keys, so the Zobrist
// init calls it.
func initCuckoo() {
	for c := types.White; c <= types.Black; c++ {
		for pt := types.Knight; pt <= types.King; pt++ {
			pc := types.MakePiece(c, pt)
			for s1 := types.Square(0); s1 < 64; s1++ {
				var att bitboard.Bitboard
				switch pt {
				case types.Knight:
					att = bitboard.KnightAttacks(s1)
				case types.Bishop:
					att = bitboard.BishopAttacks(s1, 0)
				case types.Rook:
					att = bitboard.RookAttacks(s1, 0)
				case types.Queen:
					att = bitboard.QueenAttacks(s1, 0)
				default:
					att = bitboard.KingAttacks(s1)
				}
				for s2 := s1 + 1; s2 < 64; s2++ {
					if att&bitboard.SquareBB[s2] == 0 {
						continue
					}
					key := ZobristPiece[pc][s1] ^ ZobristPiece[pc][s2] ^ ZobristSide
					mv := uint16(s1) | uint16(s2)<<6
					for i := cuckooH1(key); ; {
						cuckoo[i], key = key, cuckoo[i]
						cuckooMove[i], mv = mv, cuckooMove[i]
						if key == 0 {
							break
						}
						if i == cuckooH1(key) {
							i = cuckooH2(key)
						} else {
							i = cuckooH1(key)
						}
					}
				}
			}
		}
	}
}

// UpcomingRepetition reports whether the side to move has a move that
// repeats a position from earlier on the current line or in the game.
func (b *Board) UpcomingRepetition() bool {
	n := len(b.UndoStack)
	end := min(int(b.HalfmoveClock), n)
	if end < 3 || b.UndoStack[n-1].Move == types.MoveNone || b.UndoStack[n-2].Move == types.MoveNone {
		return false
	}
	orig := b.Hash
	other := orig ^ b.UndoStack[n-1].Hash ^ ZobristSide
	occ := b.Occupied()
	for i := 3; i <= end; i += 2 {
		// Nothing is searched back across a null move.
		if b.UndoStack[n-i].Move == types.MoveNone || b.UndoStack[n-i+1].Move == types.MoveNone {
			return false
		}
		then := b.UndoStack[n-i].Hash
		other ^= b.UndoStack[n-i+1].Hash ^ then ^ ZobristSide
		if other != 0 {
			continue
		}
		key := orig ^ then
		j := cuckooH1(key)
		if cuckoo[j] != key {
			if j = cuckooH2(key); cuckoo[j] != key {
				continue
			}
		}
		s1, s2 := types.Square(cuckooMove[j]&63), types.Square(cuckooMove[j]>>6)
		between := bitboard.BetweenSquares(s1, s2) &^ (bitboard.SquareBB[s1] | bitboard.SquareBB[s2])
		if between&occ == 0 {
			return true
		}
	}
	return false
}
