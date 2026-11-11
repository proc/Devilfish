package movegen

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

type MoveList struct {
	Moves [256]types.Move
	Count int
}

func (ml *MoveList) Add(m types.Move) {
	ml.Moves[ml.Count] = m
	ml.Count++
}

func (ml *MoveList) Slice() []types.Move {
	return ml.Moves[:ml.Count]
}

type MoveGenType uint8

const (
	GenAll MoveGenType = iota
	GenCaptures
	GenQuiets
)

func GenerateMoves(b *board.Board, capturesOnly bool, ml *MoveList) {
	if capturesOnly {
		GenerateMovesByType(b, GenCaptures, ml)
	} else {
		GenerateMovesByType(b, GenAll, ml)
	}
}

func GenerateMovesByType(b *board.Board, genType MoveGenType, ml *MoveList) {
	ml.Count = 0
	us := b.SideToMove
	them := us.Other()

	ourPieces := b.OccupiedBy(us)
	theirPieces := b.OccupiedBy(them)
	allPieces := ourPieces | theirPieces
	empty := ^allPieces

	pawn := types.MakePiece(us, types.Pawn)
	knight := types.MakePiece(us, types.Knight)
	bishop := types.MakePiece(us, types.Bishop)
	rook := types.MakePiece(us, types.Rook)
	queen := types.MakePiece(us, types.Queen)

	pawns := b.PieceBB[pawn]

	// 1. Pawn Moves
	if us == types.White {
		// Single pawn push
		if genType != GenCaptures {
			singlePush := bitboard.ShiftNorth(pawns) & empty
			spCopy := singlePush
			for spCopy != 0 {
				to := bitboard.PopLSB(&spCopy)
				from := to - 8
				if to.Rank() == types.Rank8 {
					if genType != GenQuiets {
						ml.Add(types.MakeMove(from, to, types.FlagQueenPromo))
						ml.Add(types.MakeMove(from, to, types.FlagRookPromo))
						ml.Add(types.MakeMove(from, to, types.FlagBishopPromo))
						ml.Add(types.MakeMove(from, to, types.FlagKnightPromo))
					}
				} else {
					ml.Add(types.MakeMove(from, to, types.FlagQuiet))
				}
			}

			// Double pawn push
			rank3Empty := bitboard.ShiftNorth(pawns&bitboard.Rank2BB) & empty
			doublePush := bitboard.ShiftNorth(rank3Empty) & empty & bitboard.Rank4BB
			for doublePush != 0 {
				to := bitboard.PopLSB(&doublePush)
				from := to - 16
				ml.Add(types.MakeMove(from, to, types.FlagDoublePawnPush))
			}
		} else {
			// In GenCaptures, promotions from single push
			singlePush := (bitboard.ShiftNorth(pawns) & empty) & bitboard.Rank8BB
			for singlePush != 0 {
				to := bitboard.PopLSB(&singlePush)
				from := to - 8
				ml.Add(types.MakeMove(from, to, types.FlagQueenPromo))
				ml.Add(types.MakeMove(from, to, types.FlagRookPromo))
				ml.Add(types.MakeMove(from, to, types.FlagBishopPromo))
				ml.Add(types.MakeMove(from, to, types.FlagKnightPromo))
			}
		}

		// Pawn captures
		if genType != GenQuiets {
			capNorthWest := bitboard.ShiftNorthWest(pawns) & theirPieces
			for capNorthWest != 0 {
				to := bitboard.PopLSB(&capNorthWest)
				from := to - 7
				if to.Rank() == types.Rank8 {
					ml.Add(types.MakeMove(from, to, types.FlagQueenPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagRookPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagBishopPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagKnightPromoCap))
				} else {
					ml.Add(types.MakeMove(from, to, types.FlagCapture))
				}
			}

			capNorthEast := bitboard.ShiftNorthEast(pawns) & theirPieces
			for capNorthEast != 0 {
				to := bitboard.PopLSB(&capNorthEast)
				from := to - 9
				if to.Rank() == types.Rank8 {
					ml.Add(types.MakeMove(from, to, types.FlagQueenPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagRookPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagBishopPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagKnightPromoCap))
				} else {
					ml.Add(types.MakeMove(from, to, types.FlagCapture))
				}
			}

			// En Passant
			if b.EPSquare < types.SquareNone {
				epAttacks := bitboard.PawnAttacks(types.Black, b.EPSquare) & pawns
				for epAttacks != 0 {
					from := bitboard.PopLSB(&epAttacks)
					ml.Add(types.MakeMove(from, b.EPSquare, types.FlagEnPassant))
				}
			}
		}
	} else {
		// Black pawns
		// Single push
		if genType != GenCaptures {
			singlePush := bitboard.ShiftSouth(pawns) & empty
			spCopy := singlePush
			for spCopy != 0 {
				to := bitboard.PopLSB(&spCopy)
				from := to + 8
				if to.Rank() == types.Rank1 {
					if genType != GenQuiets {
						ml.Add(types.MakeMove(from, to, types.FlagQueenPromo))
						ml.Add(types.MakeMove(from, to, types.FlagRookPromo))
						ml.Add(types.MakeMove(from, to, types.FlagBishopPromo))
						ml.Add(types.MakeMove(from, to, types.FlagKnightPromo))
					}
				} else {
					ml.Add(types.MakeMove(from, to, types.FlagQuiet))
				}
			}

			// Double push
			rank6Empty := bitboard.ShiftSouth(pawns&bitboard.Rank7BB) & empty
			doublePush := bitboard.ShiftSouth(rank6Empty) & empty & bitboard.Rank5BB
			for doublePush != 0 {
				to := bitboard.PopLSB(&doublePush)
				from := to + 16
				ml.Add(types.MakeMove(from, to, types.FlagDoublePawnPush))
			}
		} else {
			// In GenCaptures, promotions from single push
			singlePush := (bitboard.ShiftSouth(pawns) & empty) & bitboard.Rank1BB
			for singlePush != 0 {
				to := bitboard.PopLSB(&singlePush)
				from := to + 8
				ml.Add(types.MakeMove(from, to, types.FlagQueenPromo))
				ml.Add(types.MakeMove(from, to, types.FlagRookPromo))
				ml.Add(types.MakeMove(from, to, types.FlagBishopPromo))
				ml.Add(types.MakeMove(from, to, types.FlagKnightPromo))
			}
		}

		// Pawn captures
		if genType != GenQuiets {
			capSouthWest := bitboard.ShiftSouthWest(pawns) & theirPieces
			for capSouthWest != 0 {
				to := bitboard.PopLSB(&capSouthWest)
				from := to + 9
				if to.Rank() == types.Rank1 {
					ml.Add(types.MakeMove(from, to, types.FlagQueenPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagRookPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagBishopPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagKnightPromoCap))
				} else {
					ml.Add(types.MakeMove(from, to, types.FlagCapture))
				}
			}

			capSouthEast := bitboard.ShiftSouthEast(pawns) & theirPieces
			for capSouthEast != 0 {
				to := bitboard.PopLSB(&capSouthEast)
				from := to + 7
				if to.Rank() == types.Rank1 {
					ml.Add(types.MakeMove(from, to, types.FlagQueenPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagRookPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagBishopPromoCap))
					ml.Add(types.MakeMove(from, to, types.FlagKnightPromoCap))
				} else {
					ml.Add(types.MakeMove(from, to, types.FlagCapture))
				}
			}

			// En Passant
			if b.EPSquare < types.SquareNone {
				epAttacks := bitboard.PawnAttacks(types.White, b.EPSquare) & pawns
				for epAttacks != 0 {
					from := bitboard.PopLSB(&epAttacks)
					ml.Add(types.MakeMove(from, b.EPSquare, types.FlagEnPassant))
				}
			}
		}
	}

	// 2. Knight Moves
	knights := b.PieceBB[knight]
	for knights != 0 {
		from := bitboard.PopLSB(&knights)
		attacks := bitboard.KnightAttacks(from)
		if genType == GenCaptures {
			attacks &= theirPieces
		} else if genType == GenQuiets {
			attacks &= empty
		} else {
			attacks &= ^ourPieces
		}
		for attacks != 0 {
			to := bitboard.PopLSB(&attacks)
			if theirPieces&bitboard.SquareBB[to] != 0 {
				ml.Add(types.MakeMove(from, to, types.FlagCapture))
			} else {
				ml.Add(types.MakeMove(from, to, types.FlagQuiet))
			}
		}
	}

	// 3. Bishop Moves
	bishops := b.PieceBB[bishop]
	for bishops != 0 {
		from := bitboard.PopLSB(&bishops)
		attacks := bitboard.BishopAttacks(from, allPieces)
		if genType == GenCaptures {
			attacks &= theirPieces
		} else if genType == GenQuiets {
			attacks &= empty
		} else {
			attacks &= ^ourPieces
		}
		for attacks != 0 {
			to := bitboard.PopLSB(&attacks)
			if theirPieces&bitboard.SquareBB[to] != 0 {
				ml.Add(types.MakeMove(from, to, types.FlagCapture))
			} else {
				ml.Add(types.MakeMove(from, to, types.FlagQuiet))
			}
		}
	}

	// 4. Rook Moves
	rooks := b.PieceBB[rook]
	for rooks != 0 {
		from := bitboard.PopLSB(&rooks)
		attacks := bitboard.RookAttacks(from, allPieces)
		if genType == GenCaptures {
			attacks &= theirPieces
		} else if genType == GenQuiets {
			attacks &= empty
		} else {
			attacks &= ^ourPieces
		}
		for attacks != 0 {
			to := bitboard.PopLSB(&attacks)
			if theirPieces&bitboard.SquareBB[to] != 0 {
				ml.Add(types.MakeMove(from, to, types.FlagCapture))
			} else {
				ml.Add(types.MakeMove(from, to, types.FlagQuiet))
			}
		}
	}

	// 5. Queen Moves
	queens := b.PieceBB[queen]
	for queens != 0 {
		from := bitboard.PopLSB(&queens)
		attacks := bitboard.QueenAttacks(from, allPieces)
		if genType == GenCaptures {
			attacks &= theirPieces
		} else if genType == GenQuiets {
			attacks &= empty
		} else {
			attacks &= ^ourPieces
		}
		for attacks != 0 {
			to := bitboard.PopLSB(&attacks)
			if theirPieces&bitboard.SquareBB[to] != 0 {
				ml.Add(types.MakeMove(from, to, types.FlagCapture))
			} else {
				ml.Add(types.MakeMove(from, to, types.FlagQuiet))
			}
		}
	}

	// 6. King Moves
	kingSq := b.KingSquare(us)
	if kingSq < types.SquareNone {
		attacks := bitboard.KingAttacks(kingSq)
		if genType == GenCaptures {
			attacks &= theirPieces
		} else if genType == GenQuiets {
			attacks &= empty
		} else {
			attacks &= ^ourPieces
		}
		for attacks != 0 {
			to := bitboard.PopLSB(&attacks)
			if theirPieces&bitboard.SquareBB[to] != 0 {
				ml.Add(types.MakeMove(kingSq, to, types.FlagCapture))
			} else {
				ml.Add(types.MakeMove(kingSq, to, types.FlagQuiet))
			}
		}

		// Castling (never captures, only if genType != GenCaptures and King is NOT in check)
		if genType != GenCaptures && !b.IsSquareAttacked(kingSq, them) {
			if us == types.White {
				// White Kingside
				if b.Castling&types.CastlingWhiteKing != 0 {
					occBetween := (bitboard.SquareBB[types.F1] | bitboard.SquareBB[types.G1]) & allPieces
					if occBetween == 0 {
						if !b.IsSquareAttacked(types.F1, types.Black) && !b.IsSquareAttacked(types.G1, types.Black) {
							ml.Add(types.MakeMove(types.E1, types.G1, types.FlagKingCastle))
						}
					}
				}
				// White Queenside
				if b.Castling&types.CastlingWhiteQueen != 0 {
					occBetween := (bitboard.SquareBB[types.B1] | bitboard.SquareBB[types.C1] | bitboard.SquareBB[types.D1]) & allPieces
					if occBetween == 0 {
						if !b.IsSquareAttacked(types.D1, types.Black) && !b.IsSquareAttacked(types.C1, types.Black) {
							ml.Add(types.MakeMove(types.E1, types.C1, types.FlagQueenCastle))
						}
					}
				}
			} else {
				// Black Kingside
				if b.Castling&types.CastlingBlackKing != 0 {
					occBetween := (bitboard.SquareBB[types.F8] | bitboard.SquareBB[types.G8]) & allPieces
					if occBetween == 0 {
						if !b.IsSquareAttacked(types.F8, types.White) && !b.IsSquareAttacked(types.G8, types.White) {
							ml.Add(types.MakeMove(types.E8, types.G8, types.FlagKingCastle))
						}
					}
				}
				// Black Queenside
				if b.Castling&types.CastlingBlackQueen != 0 {
					occBetween := (bitboard.SquareBB[types.B8] | bitboard.SquareBB[types.C8] | bitboard.SquareBB[types.D8]) & allPieces
					if occBetween == 0 {
						if !b.IsSquareAttacked(types.D8, types.White) && !b.IsSquareAttacked(types.C8, types.White) {
							ml.Add(types.MakeMove(types.E8, types.C8, types.FlagQueenCastle))
						}
					}
				}
			}
		}
	}
}

func GenerateLegalMoves(b *board.Board) []types.Move {
	var ml MoveList
	GenerateMoves(b, false, &ml)
	legalMoves := make([]types.Move, 0, ml.Count)
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		if b.MakeMove(m) {
			b.UnmakeMove(m)
			legalMoves = append(legalMoves, m)
		}
	}
	return legalMoves
}

func IsPseudoLegal(b *board.Board, m types.Move) bool {
	if m == types.MoveNone {
		return false
	}
	if m.IsCastle() {
		return isPseudoLegalCastle(b, m)
	}
	from := m.From()
	to := m.To()
	if from >= types.SquareNone || to >= types.SquareNone || from == to {
		return false
	}

	p := b.Pieces[from]
	us := b.SideToMove
	if p == types.PieceNone || p.Color() != us {
		return false
	}

	target := b.Pieces[to]
	if target != types.PieceNone && target.Color() == us {
		return false
	}

	pt := p.Type()
	switch pt {
	case types.Knight:
		return bitboard.KnightAttacks(from)&bitboard.SquareBB[to] != 0
	case types.Bishop:
		return bitboard.BishopAttacks(from, b.Occupied())&bitboard.SquareBB[to] != 0
	case types.Rook:
		return bitboard.RookAttacks(from, b.Occupied())&bitboard.SquareBB[to] != 0
	case types.Queen:
		return bitboard.QueenAttacks(from, b.Occupied())&bitboard.SquareBB[to] != 0
	case types.King:
		return bitboard.KingAttacks(from)&bitboard.SquareBB[to] != 0
	case types.Pawn:
		if us == types.White {
			if to == from+8 && target == types.PieceNone {
				return true
			}
			if from.Rank() == types.Rank2 && to == from+16 && b.Pieces[from+8] == types.PieceNone && target == types.PieceNone {
				return true
			}
			if (to == from+7 || to == from+9) && (target != types.PieceNone || to == b.EPSquare) {
				return true
			}
		} else {
			if to == from-8 && target == types.PieceNone {
				return true
			}
			if from.Rank() == types.Rank7 && to == from-16 && b.Pieces[from-8] == types.PieceNone && target == types.PieceNone {
				return true
			}
			if (to == from-7 || to == from-9) && (target != types.PieceNone || to == b.EPSquare) {
				return true
			}
		}
	}
	return false
}

// isPseudoLegalCastle applies the same conditions the generator uses before
// emitting a castling move: king and rook at home, the right still held, the
// squares between empty, and the king neither in check nor passing through an
// attacked square.
func isPseudoLegalCastle(b *board.Board, m types.Move) bool {
	us := b.SideToMove
	them := us.Other()
	occ := b.Occupied()

	var kFrom, kTo, rFrom, pass types.Square
	var right types.CastlingRights
	var between bitboard.Bitboard
	king := types.MakePiece(us, types.King)
	rook := types.MakePiece(us, types.Rook)

	switch {
	case us == types.White && m.Flags() == types.FlagKingCastle:
		kFrom, kTo, rFrom, pass, right = types.E1, types.G1, types.H1, types.F1, types.CastlingWhiteKing
		between = bitboard.SquareBB[types.F1] | bitboard.SquareBB[types.G1]
	case us == types.White && m.Flags() == types.FlagQueenCastle:
		kFrom, kTo, rFrom, pass, right = types.E1, types.C1, types.A1, types.D1, types.CastlingWhiteQueen
		between = bitboard.SquareBB[types.B1] | bitboard.SquareBB[types.C1] | bitboard.SquareBB[types.D1]
	case us == types.Black && m.Flags() == types.FlagKingCastle:
		kFrom, kTo, rFrom, pass, right = types.E8, types.G8, types.H8, types.F8, types.CastlingBlackKing
		between = bitboard.SquareBB[types.F8] | bitboard.SquareBB[types.G8]
	case us == types.Black && m.Flags() == types.FlagQueenCastle:
		kFrom, kTo, rFrom, pass, right = types.E8, types.C8, types.A8, types.D8, types.CastlingBlackQueen
		between = bitboard.SquareBB[types.B8] | bitboard.SquareBB[types.C8] | bitboard.SquareBB[types.D8]
	default:
		return false
	}

	if m.From() != kFrom || m.To() != kTo {
		return false
	}
	if b.Castling&right == 0 || b.Pieces[kFrom] != king || b.Pieces[rFrom] != rook {
		return false
	}
	if occ&between != 0 {
		return false
	}
	return !b.IsSquareAttacked(kFrom, them) && !b.IsSquareAttacked(pass, them) && !b.IsSquareAttacked(kTo, them)
}
