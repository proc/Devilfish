package nnue

import (
	"devilfish/internal/board"
	"devilfish/internal/types"
)

// FeatureIndex calculates the 768-feature index for a piece on a square from a given perspective.
func FeatureIndex(perspective types.Color, p types.Piece, sq types.Square) int {
	if p == types.PieceNone || p.Color() >= types.ColorNone || sq >= types.SquareNone {
		return -1
	}

	pColor := p.Color()
	pType := p.Type()
	if pType < types.Pawn || pType > types.King {
		return -1
	}

	perspSq := int(sq)
	if perspective == types.Black {
		perspSq ^= 56 // Flip ranks 1-8 vertically
	}

	var ptIdx int
	if pColor == perspective {
		ptIdx = int(pType) - 1 // 0..5 (Friendly P, N, B, R, Q, K)
	} else {
		ptIdx = 6 + int(pType) - 1 // 6..11 (Enemy P, N, B, R, Q, K)
	}

	return ptIdx*64 + perspSq
}

// UsesAccumulator reports whether evaluation reads the 768->256 Accumulator.
// It does not when NNUE is switched off or when the 8Chess network is active
// (that network keeps its own state, see EightState), so the search can skip
// maintaining it.
func UsesAccumulator() bool {
	return Enabled && ActiveNetwork != nil && ActiveEightNetwork == nil
}

// Refresh recomputes the accumulators for White and Black completely from scratch.
func Refresh(b *board.Board, acc *Accumulator) {
	if ActiveNetwork == nil || ActiveEightNetwork != nil {
		return
	}

	acc.White = ActiveNetwork.Biases
	acc.Black = ActiveNetwork.Biases

	for sq := types.Square(0); sq < 64; sq++ {
		p := b.Pieces[sq]
		if p == types.PieceNone {
			continue
		}

		wFeat := FeatureIndex(types.White, p, sq)
		bFeat := FeatureIndex(types.Black, p, sq)

		if wFeat >= 0 && wFeat < NumFeatures {
			addVector(&acc.White, &ActiveNetwork.Weights[wFeat])
		}
		if bFeat >= 0 && bFeat < NumFeatures {
			addVector(&acc.Black, &ActiveNetwork.Weights[bFeat])
		}
	}
}

// Update incrementally updates next Accumulator from curr Accumulator based on move m.
func Update(curr *Accumulator, next *Accumulator, m types.Move, b *board.Board) {
	if ActiveNetwork == nil || ActiveEightNetwork != nil {
		return
	}

	*next = *curr // Copy 1KB from previous ply

	from := m.From()
	to := m.To()
	movingPiece := b.Pieces[from]
	if movingPiece == types.PieceNone {
		return
	}

	// 1. Remove moving piece from 'from'
	wFrom := FeatureIndex(types.White, movingPiece, from)
	bFrom := FeatureIndex(types.Black, movingPiece, from)
	if wFrom >= 0 {
		subVector(&next.White, &ActiveNetwork.Weights[wFrom])
	}
	if bFrom >= 0 {
		subVector(&next.Black, &ActiveNetwork.Weights[bFrom])
	}

	// 2. Remove captured piece (if capture)
	isEP := m.IsEnPassant() || (movingPiece.Type() == types.Pawn && to == b.EPSquare && b.EPSquare < types.SquareNone)
	if isEP {
		var capSq types.Square
		if b.SideToMove == types.White {
			capSq = to - 8
		} else {
			capSq = to + 8
		}
		if capSq < types.SquareNone {
			capPiece := b.Pieces[capSq]
			if capPiece != types.PieceNone {
				wCap := FeatureIndex(types.White, capPiece, capSq)
				bCap := FeatureIndex(types.Black, capPiece, capSq)
				if wCap >= 0 {
					subVector(&next.White, &ActiveNetwork.Weights[wCap])
				}
				if bCap >= 0 {
					subVector(&next.Black, &ActiveNetwork.Weights[bCap])
				}
			}
		}
	} else {
		capPiece := b.Pieces[to]
		if capPiece != types.PieceNone {
			wCap := FeatureIndex(types.White, capPiece, to)
			bCap := FeatureIndex(types.Black, capPiece, to)
			if wCap >= 0 {
				subVector(&next.White, &ActiveNetwork.Weights[wCap])
			}
			if bCap >= 0 {
				subVector(&next.Black, &ActiveNetwork.Weights[bCap])
			}
		}
	}

	// 3. Add placed piece on 'to' (accounting for promotion)
	placedPiece := movingPiece
	if m.IsPromotion() {
		promoType := m.PromotionType()
		placedPiece = types.MakePiece(b.SideToMove, promoType)
	} else if movingPiece.Type() == types.Pawn && (to.Rank() == types.Rank8 || to.Rank() == types.Rank1) {
		placedPiece = types.MakePiece(b.SideToMove, types.Queen)
	}
	wTo := FeatureIndex(types.White, placedPiece, to)
	bTo := FeatureIndex(types.Black, placedPiece, to)
	if wTo >= 0 {
		addVector(&next.White, &ActiveNetwork.Weights[wTo])
	}
	if bTo >= 0 {
		addVector(&next.Black, &ActiveNetwork.Weights[bTo])
	}

	// 4. Castling rook move
	isCastle := m.IsCastle() || (movingPiece.Type() == types.King && (from == types.E1 && (to == types.G1 || to == types.C1) || from == types.E8 && (to == types.G8 || to == types.C8)))
	if isCastle {
		var rFrom, rTo types.Square
		rookPiece := types.MakePiece(b.SideToMove, types.Rook)
		if to == types.G1 || to == types.G8 || m.Flags() == types.FlagKingCastle {
			if b.SideToMove == types.White {
				rFrom, rTo = types.H1, types.F1
			} else {
				rFrom, rTo = types.H8, types.F8
			}
		} else {
			if b.SideToMove == types.White {
				rFrom, rTo = types.A1, types.D1
			} else {
				rFrom, rTo = types.A8, types.D8
			}
		}
		wRFrom := FeatureIndex(types.White, rookPiece, rFrom)
		bRFrom := FeatureIndex(types.Black, rookPiece, rFrom)
		wRTo := FeatureIndex(types.White, rookPiece, rTo)
		bRTo := FeatureIndex(types.Black, rookPiece, rTo)

		if wRFrom >= 0 {
			subVector(&next.White, &ActiveNetwork.Weights[wRFrom])
		}
		if bRFrom >= 0 {
			subVector(&next.Black, &ActiveNetwork.Weights[bRFrom])
		}
		if wRTo >= 0 {
			addVector(&next.White, &ActiveNetwork.Weights[wRTo])
		}
		if bRTo >= 0 {
			addVector(&next.Black, &ActiveNetwork.Weights[bRTo])
		}
	}
}
