package bitboard

import (
	"math/bits"

	"devilfish/internal/types"
)

var (
	PawnAttacksTable   [2][64]Bitboard
	KnightAttacksTable [64]Bitboard
	KingAttacksTable   [64]Bitboard

	BishopMasks  [64]Bitboard
	BishopShifts [64]uint8
	BishopOffset [64]uint32
	BishopTable  []Bitboard

	RookMasks  [64]Bitboard
	RookShifts [64]uint8
	RookOffset [64]uint32
	RookTable  []Bitboard

	BetweenTable [64][64]Bitboard
	LineTable    [64][64]Bitboard
)

func init() {
	initPawns()
	initKnights()
	initKings()
	initMagics()
	initBetweenAndLine()
}

func initPawns() {
	for sq := 0; sq < 64; sq++ {
		s := types.Square(sq)
		bb := SquareBB[s]

		// White pawn attacks
		PawnAttacksTable[types.White][sq] = ShiftNorthEast(bb) | ShiftNorthWest(bb)

		// Black pawn attacks
		PawnAttacksTable[types.Black][sq] = ShiftSouthEast(bb) | ShiftSouthWest(bb)
	}
}

func initKnights() {
	knightOffsets := []int{-17, -15, -10, -6, 6, 10, 15, 17}
	for sq := 0; sq < 64; sq++ {
		r, f := int(sq/8), int(sq%8)
		var bb Bitboard
		for _, off := range knightOffsets {
			target := sq + off
			if target >= 0 && target < 64 {
				tr, tf := target/8, target%8
				dr := tr - r
				df := tf - f
				if dr < 0 {
					dr = -dr
				}
				if df < 0 {
					df = -df
				}
				if (dr == 1 && df == 2) || (dr == 2 && df == 1) {
					bb |= SquareBB[target]
				}
			}
		}
		KnightAttacksTable[sq] = bb
	}
}

func initKings() {
	kingOffsets := []int{-9, -8, -7, -1, 1, 7, 8, 9}
	for sq := 0; sq < 64; sq++ {
		r, f := int(sq/8), int(sq%8)
		var bb Bitboard
		for _, off := range kingOffsets {
			target := sq + off
			if target >= 0 && target < 64 {
				tr, tf := target/8, target%8
				dr := tr - r
				df := tf - f
				if dr < 0 {
					dr = -dr
				}
				if df < 0 {
					df = -df
				}
				if dr <= 1 && df <= 1 {
					bb |= SquareBB[target]
				}
			}
		}
		KingAttacksTable[sq] = bb
	}
}

func bishopMask(sq int) Bitboard {
	var mask Bitboard
	r, f := sq/8, sq%8
	for dr, df := 1, 1; r+dr < 7 && f+df < 7; dr, df = dr+1, df+1 {
		mask |= SquareBB[(r+dr)*8+f+df]
	}
	for dr, df := 1, -1; r+dr < 7 && f+df > 0; dr, df = dr+1, df-1 {
		mask |= SquareBB[(r+dr)*8+f+df]
	}
	for dr, df := -1, 1; r+dr > 0 && f+df < 7; dr, df = dr-1, df+1 {
		mask |= SquareBB[(r+dr)*8+f+df]
	}
	for dr, df := -1, -1; r+dr > 0 && f+df > 0; dr, df = dr-1, df-1 {
		mask |= SquareBB[(r+dr)*8+f+df]
	}
	return mask
}

func rookMask(sq int) Bitboard {
	var mask Bitboard
	r, f := sq/8, sq%8
	for r2 := r + 1; r2 < 7; r2++ {
		mask |= SquareBB[r2*8+f]
	}
	for r2 := r - 1; r2 > 0; r2-- {
		mask |= SquareBB[r2*8+f]
	}
	for f2 := f + 1; f2 < 7; f2++ {
		mask |= SquareBB[r*8+f2]
	}
	for f2 := f - 1; f2 > 0; f2-- {
		mask |= SquareBB[r*8+f2]
	}
	return mask
}

func bishopAttacksOnTheFly(sq int, block Bitboard) Bitboard {
	var attacks Bitboard
	r, f := sq/8, sq%8
	for dr, df := 1, 1; r+dr <= 7 && f+df <= 7; dr, df = dr+1, df+1 {
		s := (r+dr)*8 + f + df
		attacks |= SquareBB[s]
		if block&SquareBB[s] != 0 {
			break
		}
	}
	for dr, df := 1, -1; r+dr <= 7 && f+df >= 0; dr, df = dr+1, df-1 {
		s := (r+dr)*8 + f + df
		attacks |= SquareBB[s]
		if block&SquareBB[s] != 0 {
			break
		}
	}
	for dr, df := -1, 1; r+dr >= 0 && f+df <= 7; dr, df = dr-1, df+1 {
		s := (r+dr)*8 + f + df
		attacks |= SquareBB[s]
		if block&SquareBB[s] != 0 {
			break
		}
	}
	for dr, df := -1, -1; r+dr >= 0 && f+df >= 0; dr, df = dr-1, df-1 {
		s := (r+dr)*8 + f + df
		attacks |= SquareBB[s]
		if block&SquareBB[s] != 0 {
			break
		}
	}
	return attacks
}

func rookAttacksOnTheFly(sq int, block Bitboard) Bitboard {
	var attacks Bitboard
	r, f := sq/8, sq%8
	for r2 := r + 1; r2 <= 7; r2++ {
		s := r2*8 + f
		attacks |= SquareBB[s]
		if block&SquareBB[s] != 0 {
			break
		}
	}
	for r2 := r - 1; r2 >= 0; r2-- {
		s := r2*8 + f
		attacks |= SquareBB[s]
		if block&SquareBB[s] != 0 {
			break
		}
	}
	for f2 := f + 1; f2 <= 7; f2++ {
		s := r*8 + f2
		attacks |= SquareBB[s]
		if block&SquareBB[s] != 0 {
			break
		}
	}
	for f2 := f - 1; f2 >= 0; f2-- {
		s := r*8 + f2
		attacks |= SquareBB[s]
		if block&SquareBB[s] != 0 {
			break
		}
	}
	return attacks
}

func initMagics() {
	var bTotal uint32
	for sq := 0; sq < 64; sq++ {
		mask := bishopMask(sq)
		BishopMasks[sq] = mask
		numBits := bits.OnesCount64(mask)
		BishopShifts[sq] = uint8(64 - numBits)
		BishopOffset[sq] = bTotal
		bTotal += 1 << numBits
	}
	BishopTable = make([]Bitboard, bTotal)

	for sq := 0; sq < 64; sq++ {
		mask := BishopMasks[sq]
		shift := BishopShifts[sq]
		offset := BishopOffset[sq]
		magic := BishopMagics[sq]

		b := Bitboard(0)
		for {
			idx := (b * magic) >> shift
			BishopTable[offset+uint32(idx)] = bishopAttacksOnTheFly(sq, b)
			b = (b - mask) & mask
			if b == 0 {
				break
			}
		}
	}

	var rTotal uint32
	for sq := 0; sq < 64; sq++ {
		mask := rookMask(sq)
		RookMasks[sq] = mask
		numBits := bits.OnesCount64(mask)
		RookShifts[sq] = uint8(64 - numBits)
		RookOffset[sq] = rTotal
		rTotal += 1 << numBits
	}
	RookTable = make([]Bitboard, rTotal)

	for sq := 0; sq < 64; sq++ {
		mask := RookMasks[sq]
		shift := RookShifts[sq]
		offset := RookOffset[sq]
		magic := RookMagics[sq]

		b := Bitboard(0)
		for {
			idx := (b * magic) >> shift
			RookTable[offset+uint32(idx)] = rookAttacksOnTheFly(sq, b)
			b = (b - mask) & mask
			if b == 0 {
				break
			}
		}
	}
}

func initBetweenAndLine() {
	for sq1 := 0; sq1 < 64; sq1++ {
		for sq2 := 0; sq2 < 64; sq2++ {
			if sq1 == sq2 {
				continue
			}
			r1, f1 := sq1/8, sq1%8
			r2, f2 := sq2/8, sq2%8
			dr := r2 - r1
			df := f2 - f1

			stepR := 0
			if dr > 0 {
				stepR = 1
			} else if dr < 0 {
				stepR = -1
			}

			stepF := 0
			if df > 0 {
				stepF = 1
			} else if df < 0 {
				stepF = -1
			}

			isLine := false
			if r1 == r2 || f1 == f2 {
				isLine = true
			} else {
				absDr := dr
				if absDr < 0 {
					absDr = -absDr
				}
				absDf := df
				if absDf < 0 {
					absDf = -absDf
				}
				if absDr == absDf {
					isLine = true
				}
			}

			if isLine {
				var between Bitboard
				currR := r1 + stepR
				currF := f1 + stepF
				for currR != r2 || currF != f2 {
					between |= SquareBB[currR*8+currF]
					currR += stepR
					currF += stepF
				}
				BetweenTable[sq1][sq2] = between

				// Ray line
				var line Bitboard
				currR = r1
				currF = f1
				for currR >= 0 && currR < 8 && currF >= 0 && currF < 8 {
					line |= SquareBB[currR*8+currF]
					currR += stepR
					currF += stepF
				}
				currR = r1 - stepR
				currF = f1 - stepF
				for currR >= 0 && currR < 8 && currF >= 0 && currF < 8 {
					line |= SquareBB[currR*8+currF]
					currR -= stepR
					currF -= stepF
				}
				LineTable[sq1][sq2] = line
			}
		}
	}
}

func BishopAttacks(sq types.Square, occ Bitboard) Bitboard {
	idx := ((occ & BishopMasks[sq]) * BishopMagics[sq]) >> BishopShifts[sq]
	return BishopTable[BishopOffset[sq]+uint32(idx)]
}

func RookAttacks(sq types.Square, occ Bitboard) Bitboard {
	idx := ((occ & RookMasks[sq]) * RookMagics[sq]) >> RookShifts[sq]
	return RookTable[RookOffset[sq]+uint32(idx)]
}

func QueenAttacks(sq types.Square, occ Bitboard) Bitboard {
	return BishopAttacks(sq, occ) | RookAttacks(sq, occ)
}

func KnightAttacks(sq types.Square) Bitboard {
	return KnightAttacksTable[sq]
}

func KingAttacks(sq types.Square) Bitboard {
	return KingAttacksTable[sq]
}

func PawnAttacks(c types.Color, sq types.Square) Bitboard {
	return PawnAttacksTable[c][sq]
}

func BetweenSquares(sq1, sq2 types.Square) Bitboard {
	return BetweenTable[sq1][sq2]
}

func LineBetween(sq1, sq2 types.Square) Bitboard {
	return LineTable[sq1][sq2]
}
