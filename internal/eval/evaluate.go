package eval

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/nnue"
	"devilfish/internal/types"
)

var PassedPawnBonus = [8]Score{
	{mg: 0, eg: 0},
	{mg: 0, eg: 0},
	{mg: 10, eg: 20},
	{mg: 20, eg: 40},
	{mg: 35, eg: 80},
	{mg: 60, eg: 140},
	{mg: 100, eg: 240},
	{mg: 0, eg: 0},
}

func absVal(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}

func Evaluate(b *board.Board) int32 {
	if nnue.Enabled && nnue.ActiveNetwork != nil {
		var acc nnue.Accumulator
		nnue.Refresh(b, &acc)
		return nnue.Evaluate(&acc, b.SideToMove)
	}
	return EvaluateClassical(b)
}

func EvaluateClassical(b *board.Board) int32 {
	var whiteScore, blackScore Score
	var phase int32

	whitePawns := b.PieceBB[types.WhitePawn]
	blackPawns := b.PieceBB[types.BlackPawn]
	occ := b.Occupied()

	// 1. Material and PST (Bitboard iteration)
	for pt := types.Pawn; pt <= types.King; pt++ {
		wp := types.MakePiece(types.White, pt)
		wBB := b.PieceBB[wp]
		if wBB != 0 {
			wCount := int32(bitboard.Count(wBB))
			phase += wCount * PiecePhase[pt]
			whiteScore.mg += wCount * PieceValues[pt].mg
			whiteScore.eg += wCount * PieceValues[pt].eg
			for wBB != 0 {
				sq := bitboard.PopLSB(&wBB)
				whiteScore = whiteScore.Add(PST[pt][sq])
			}
		}

		bp := types.MakePiece(types.Black, pt)
		bBB := b.PieceBB[bp]
		if bBB != 0 {
			bCount := int32(bitboard.Count(bBB))
			phase += bCount * PiecePhase[pt]
			blackScore.mg += bCount * PieceValues[pt].mg
			blackScore.eg += bCount * PieceValues[pt].eg
			for bBB != 0 {
				sq := bitboard.PopLSB(&bBB)
				blackScore = blackScore.Add(PST[pt][sq^56])
			}
		}
	}

	// 2. Bishop Pair Bonus
	if bitboard.Count(b.PieceBB[types.WhiteBishop]) >= 2 {
		whiteScore = whiteScore.Add(Score{mg: 30, eg: 45})
	}
	if bitboard.Count(b.PieceBB[types.BlackBishop]) >= 2 {
		blackScore = blackScore.Add(Score{mg: 30, eg: 45})
	}

	// 3. Rooks: Open and Semi-Open Files, 7th Rank
	wRooks := b.PieceBB[types.WhiteRook]
	for wRooks != 0 {
		sq := bitboard.PopLSB(&wRooks)
		file := sq.File()
		fileBB := bitboard.FilesBB[file]

		if fileBB&(whitePawns|blackPawns) == 0 {
			// Fully open file
			whiteScore = whiteScore.Add(Score{mg: 25, eg: 20})
		} else if fileBB&whitePawns == 0 {
			// Semi-open file
			whiteScore = whiteScore.Add(Score{mg: 15, eg: 10})
		}

		if sq.Rank() == types.Rank7 {
			whiteScore = whiteScore.Add(Score{mg: 20, eg: 35})
		}
	}

	bRooks := b.PieceBB[types.BlackRook]
	for bRooks != 0 {
		sq := bitboard.PopLSB(&bRooks)
		file := sq.File()
		fileBB := bitboard.FilesBB[file]

		if fileBB&(whitePawns|blackPawns) == 0 {
			blackScore = blackScore.Add(Score{mg: 25, eg: 20})
		} else if fileBB&blackPawns == 0 {
			blackScore = blackScore.Add(Score{mg: 15, eg: 10})
		}

		if sq.Rank() == types.Rank2 {
			blackScore = blackScore.Add(Score{mg: 20, eg: 35})
		}
	}

	// 4. Pawn Structure: Doubled, Isolated, Passed Pawns
	wKingSq := b.KingSquare(types.White)
	bKingSq := b.KingSquare(types.Black)

	// White pawns
	wpCopy := whitePawns
	for wpCopy != 0 {
		sq := bitboard.PopLSB(&wpCopy)
		f := sq.File()
		r := sq.Rank()

		// Doubled pawns
		pawnsAheadOnFile := bitboard.FilesBB[f] & whitePawns & ^bitboard.SquareBB[sq] & (bitboard.All << (sq + 1))
		if pawnsAheadOnFile != 0 {
			whiteScore = whiteScore.Sub(Score{mg: 15, eg: 25})
		}

		// Isolated pawns
		var adjacentFiles bitboard.Bitboard
		if f > 0 {
			adjacentFiles |= bitboard.FilesBB[f-1]
		}
		if f < 7 {
			adjacentFiles |= bitboard.FilesBB[f+1]
		}
		if adjacentFiles&whitePawns == 0 {
			whiteScore = whiteScore.Sub(Score{mg: 12, eg: 18})
		}

		// Passed pawns
		span := bitboard.FilesBB[f] | adjacentFiles
		// Mask of squares with rank > r
		var ranksAhead bitboard.Bitboard
		for rNext := int(r) + 1; rNext <= 7; rNext++ {
			ranksAhead |= bitboard.RanksBB[rNext]
		}
		if span&ranksAhead&blackPawns == 0 {
			bonus := PassedPawnBonus[r]
			// Check if protected by friendly pawn
			if bitboard.PawnAttacks(types.Black, sq)&whitePawns != 0 {
				bonus.mg = bonus.mg * 5 / 4
				bonus.eg = bonus.eg * 5 / 4
			}
			// In endgame, King proximity to passed pawns
			if wKingSq < types.SquareNone && bKingSq < types.SquareNone && phase < TotalPhase {
				wDist := max(absVal(int32(wKingSq.File())-int32(f)), absVal(int32(wKingSq.Rank())-int32(r)))
				bDist := max(absVal(int32(bKingSq.File())-int32(f)), absVal(int32(bKingSq.Rank())-int32(r)))
				bonus.eg += (5 - wDist) * 5
				bonus.eg += (bDist - 3) * 5
			}
			whiteScore = whiteScore.Add(bonus)
		}
	}

	// Black pawns
	bpCopy := blackPawns
	for bpCopy != 0 {
		sq := bitboard.PopLSB(&bpCopy)
		f := sq.File()
		r := sq.Rank()

		// Doubled pawns
		pawnsAheadOnFile := bitboard.FilesBB[f] & blackPawns & ^bitboard.SquareBB[sq] & ((1 << sq) - 1)
		if pawnsAheadOnFile != 0 {
			blackScore = blackScore.Sub(Score{mg: 15, eg: 25})
		}

		// Isolated pawns
		var adjacentFiles bitboard.Bitboard
		if f > 0 {
			adjacentFiles |= bitboard.FilesBB[f-1]
		}
		if f < 7 {
			adjacentFiles |= bitboard.FilesBB[f+1]
		}
		if adjacentFiles&blackPawns == 0 {
			blackScore = blackScore.Sub(Score{mg: 12, eg: 18})
		}

		// Passed pawns
		span := bitboard.FilesBB[f] | adjacentFiles
		var ranksAhead bitboard.Bitboard
		for rNext := 0; rNext < int(r); rNext++ {
			ranksAhead |= bitboard.RanksBB[rNext]
		}
		if span&ranksAhead&whitePawns == 0 {
			mirroredRank := 7 - r
			bonus := PassedPawnBonus[mirroredRank]
			if bitboard.PawnAttacks(types.White, sq)&blackPawns != 0 {
				bonus.mg = bonus.mg * 5 / 4
				bonus.eg = bonus.eg * 5 / 4
			}
			// In endgame, King proximity to passed pawns
			if wKingSq < types.SquareNone && bKingSq < types.SquareNone && phase < TotalPhase {
				bDist := max(absVal(int32(bKingSq.File())-int32(f)), absVal(int32(bKingSq.Rank())-int32(r)))
				wDist := max(absVal(int32(wKingSq.File())-int32(f)), absVal(int32(wKingSq.Rank())-int32(r)))
				bonus.eg += (5 - bDist) * 5
				bonus.eg += (wDist - 3) * 5
			}
			blackScore = blackScore.Add(bonus)
		}
	}

	// 5. Piece Mobility
	whitePawnAttacks := bitboard.ShiftNorthEast(whitePawns) | bitboard.ShiftNorthWest(whitePawns)
	blackPawnAttacks := bitboard.ShiftSouthEast(blackPawns) | bitboard.ShiftSouthWest(blackPawns)

	// White Knights & Bishops mobility
	wKnights := b.PieceBB[types.WhiteKnight]
	for wKnights != 0 {
		sq := bitboard.PopLSB(&wKnights)
		mob := bitboard.KnightAttacks(sq) & ^b.OccupiedBy(types.White) & ^blackPawnAttacks
		count := int32(bitboard.Count(mob))
		whiteScore = whiteScore.Add(Score{mg: count * 4, eg: count * 4})
	}
	wBishops := b.PieceBB[types.WhiteBishop]
	for wBishops != 0 {
		sq := bitboard.PopLSB(&wBishops)
		mob := bitboard.BishopAttacks(sq, occ) & ^b.OccupiedBy(types.White) & ^blackPawnAttacks
		count := int32(bitboard.Count(mob))
		whiteScore = whiteScore.Add(Score{mg: count * 3, eg: count * 3})
	}

	// Black Knights & Bishops mobility
	bKnights := b.PieceBB[types.BlackKnight]
	for bKnights != 0 {
		sq := bitboard.PopLSB(&bKnights)
		mob := bitboard.KnightAttacks(sq) & ^b.OccupiedBy(types.Black) & ^whitePawnAttacks
		count := int32(bitboard.Count(mob))
		blackScore = blackScore.Add(Score{mg: count * 4, eg: count * 4})
	}
	bBishops := b.PieceBB[types.BlackBishop]
	for bBishops != 0 {
		sq := bitboard.PopLSB(&bBishops)
		mob := bitboard.BishopAttacks(sq, occ) & ^b.OccupiedBy(types.Black) & ^whitePawnAttacks
		count := int32(bitboard.Count(mob))
		blackScore = blackScore.Add(Score{mg: count * 3, eg: count * 3})
	}

	// 6. King Safety (Midgame)
	if wKingSq < types.SquareNone {
		// Pawn shield
		wKingFile := wKingSq.File()
		if wKingFile >= types.FileF || wKingFile <= types.FileC {
			shieldMask := (bitboard.SquareBB[wKingSq] | bitboard.KnightAttacks(wKingSq) | bitboard.KingAttacks(wKingSq)) & (bitboard.Rank2BB | bitboard.Rank3BB)
			shieldCount := int32(bitboard.Count(shieldMask & whitePawns))
			whiteScore.mg += (shieldCount - 3) * 25
		}
		// Penalty for enemy attackers aiming at White King
		if wAttackers, wUnits := b.KingAttackersCount(types.White); wAttackers >= 2 {
			whiteScore.mg -= (wUnits * (wUnits + 2)) / 2
		}
	}

	if bKingSq < types.SquareNone {
		bKingFile := bKingSq.File()
		if bKingFile >= types.FileF || bKingFile <= types.FileC {
			shieldMask := (bitboard.SquareBB[bKingSq] | bitboard.KnightAttacks(bKingSq) | bitboard.KingAttacks(bKingSq)) & (bitboard.Rank7BB | bitboard.Rank6BB)
			shieldCount := int32(bitboard.Count(shieldMask & blackPawns))
			blackScore.mg += (shieldCount - 3) * 25
		}
		// Penalty for enemy attackers aiming at Black King
		if bAttackers, bUnits := b.KingAttackersCount(types.Black); bAttackers >= 2 {
			blackScore.mg -= (bUnits * (bUnits + 2)) / 2
		}
	}

	// 7. Tapered Evaluation Interpolation
	if phase > TotalPhase {
		phase = TotalPhase
	}
	totalScore := whiteScore.Sub(blackScore)
	evalScore := (totalScore.mg*phase + totalScore.eg*(TotalPhase-phase)) / TotalPhase

	// Return score relative to SideToMove
	if b.SideToMove == types.Black {
		evalScore = -evalScore
	}

	// Tempo bonus
	evalScore += 15

	return evalScore
}
