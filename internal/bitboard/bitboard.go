package bitboard

import (
	"math/bits"

	"devilfish/internal/types"
)

type Bitboard = uint64

const (
	Empty Bitboard = 0
	All   Bitboard = 0xFFFFFFFFFFFFFFFF

	FileABB Bitboard = 0x0101010101010101
	FileBBB Bitboard = 0x0202020202020202
	FileCBB Bitboard = 0x0404040404040404
	FileDBB Bitboard = 0x0808080808080808
	FileEBB Bitboard = 0x1010101010101010
	FileFBB Bitboard = 0x2020202020202020
	FileGBB Bitboard = 0x4040404040404040
	FileHBB Bitboard = 0x8080808080808080

	Rank1BB Bitboard = 0x00000000000000FF
	Rank2BB Bitboard = 0x000000000000FF00
	Rank3BB Bitboard = 0x0000000000FF0000
	Rank4BB Bitboard = 0x00000000FF000000
	Rank5BB Bitboard = 0x000000FF00000000
	Rank6BB Bitboard = 0x0000FF0000000000
	Rank7BB Bitboard = 0x00FF000000000000
	Rank8BB Bitboard = 0xFF00000000000000
)

var (
	SquareBB = func() [64]Bitboard {
		var arr [64]Bitboard
		for i := 0; i < 64; i++ {
			arr[i] = 1 << i
		}
		return arr
	}()
	FilesBB = [8]Bitboard{FileABB, FileBBB, FileCBB, FileDBB, FileEBB, FileFBB, FileGBB, FileHBB}
	RanksBB = [8]Bitboard{Rank1BB, Rank2BB, Rank3BB, Rank4BB, Rank5BB, Rank6BB, Rank7BB, Rank8BB}
)

func PopLSB(b *Bitboard) types.Square {
	s := bits.TrailingZeros64(*b)
	*b &= *b - 1
	return types.Square(s)
}

func LSB(b Bitboard) types.Square {
	return types.Square(bits.TrailingZeros64(b))
}

func Count(b Bitboard) int {
	return bits.OnesCount64(b)
}

func ShiftNorth(b Bitboard) Bitboard {
	return b << 8
}

func ShiftSouth(b Bitboard) Bitboard {
	return b >> 8
}

func ShiftEast(b Bitboard) Bitboard {
	return (b & ^FileHBB) << 1
}

func ShiftWest(b Bitboard) Bitboard {
	return (b & ^FileABB) >> 1
}

func ShiftNorthEast(b Bitboard) Bitboard {
	return (b & ^FileHBB) << 9
}

func ShiftNorthWest(b Bitboard) Bitboard {
	return (b & ^FileABB) << 7
}

func ShiftSouthEast(b Bitboard) Bitboard {
	return (b & ^FileHBB) >> 7
}

func ShiftSouthWest(b Bitboard) Bitboard {
	return (b & ^FileABB) >> 9
}
