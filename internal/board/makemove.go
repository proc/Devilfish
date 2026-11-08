package board

import (
	"devilfish/internal/types"
)

func (b *Board) MakeMove(m types.Move) bool {
	if m == types.MoveNone {
		return false
	}
	from := m.From()
	to := m.To()
	if from >= types.SquareNone || to >= types.SquareNone || from == to {
		return false
	}

	us := b.SideToMove
	movingPiece := b.Pieces[from]
	if movingPiece == types.PieceNone || movingPiece.Color() != us {
		return false
	}

	flags := m.Flags()
	them := us.Other()

	capturedPiece := b.Pieces[to]
	if m.IsEnPassant() {
		if us == types.White {
			if to < 8 {
				return false
			}
			capturedPiece = b.Pieces[to-8]
		} else {
			if to >= 56 {
				return false
			}
			capturedPiece = b.Pieces[to+8]
		}
	}

	// Record undo state
	undo := UndoState{
		Move:          m,
		Captured:      capturedPiece,
		Castling:      b.Castling,
		EPSquare:      b.EPSquare,
		HalfmoveClock: b.HalfmoveClock,
		Hash:          b.Hash,
	}
	b.UndoStack = append(b.UndoStack, undo)

	// Update halfmove clock
	if movingPiece.Type() == types.Pawn || m.IsCapture() {
		b.HalfmoveClock = 0
	} else {
		b.HalfmoveClock++
	}

	// Clear existing en passant square from hash
	if b.EPSquare < types.SquareNone {
		b.Hash ^= ZobristEP[b.EPSquare.File()]
		b.EPSquare = types.SquareNone
	}

	// Remove captured piece
	if m.IsEnPassant() {
		if us == types.White {
			b.RemovePiece(to - 8)
		} else {
			b.RemovePiece(to + 8)
		}
	} else if capturedPiece != types.PieceNone {
		b.RemovePiece(to)
	}

	// Move the piece
	if m.IsPromotion() {
		b.RemovePiece(from)
		promotedPiece := types.MakePiece(us, m.PromotionType())
		b.PutPiece(promotedPiece, to)
	} else {
		b.MovePiece(movingPiece, from, to)
	}

	// Handle Castling
	if m.IsCastle() {
		switch flags {
		case types.FlagKingCastle:
			if us == types.White {
				b.MovePiece(types.WhiteRook, types.H1, types.F1)
			} else {
				b.MovePiece(types.BlackRook, types.H8, types.F8)
			}
		case types.FlagQueenCastle:
			if us == types.White {
				b.MovePiece(types.WhiteRook, types.A1, types.D1)
			} else {
				b.MovePiece(types.BlackRook, types.A8, types.D8)
			}
		}
	}

	// Handle Double Pawn Push (set en passant square)
	if flags == types.FlagDoublePawnPush {
		epSq := types.Square((int(from) + int(to)) / 2)
		b.EPSquare = epSq
		b.Hash ^= ZobristEP[epSq.File()]
	}

	// Update castling rights
	newCastling := b.Castling & CastlingMask[from] & CastlingMask[to]
	if newCastling != b.Castling {
		b.Hash ^= ZobristCastling[b.Castling] ^ ZobristCastling[newCastling]
		b.Castling = newCastling
	}

	// Toggle side to move
	b.SideToMove = them
	b.Hash ^= ZobristSide

	// Increment fullmove number on Black's move
	if us == types.Black {
		b.FullmoveNumber++
	}

	// Legality check: ensure king is not left in check
	if b.IsSquareAttacked(b.KingSquare(us), them) {
		b.UnmakeMove(m)
		return false
	}

	return true
}

func (b *Board) UnmakeMove(m types.Move) {
	if len(b.UndoStack) == 0 {
		return
	}
	undo := b.UndoStack[len(b.UndoStack)-1]
	b.UndoStack = b.UndoStack[:len(b.UndoStack)-1]

	us := b.SideToMove.Other()
	from := m.From()
	to := m.To()
	flags := m.Flags()

	if us == types.Black {
		b.FullmoveNumber--
	}

	// Unmake castling rook move
	if m.IsCastle() {
		switch flags {
		case types.FlagKingCastle:
			if us == types.White {
				b.MovePiece(types.WhiteRook, types.F1, types.H1)
			} else {
				b.MovePiece(types.BlackRook, types.F8, types.H8)
			}
		case types.FlagQueenCastle:
			if us == types.White {
				b.MovePiece(types.WhiteRook, types.D1, types.A1)
			} else {
				b.MovePiece(types.BlackRook, types.D8, types.A8)
			}
		}
	}

	// Unmake moving piece
	if m.IsPromotion() {
		b.RemovePiece(to)
		b.PutPiece(types.MakePiece(us, types.Pawn), from)
	} else {
		p := b.Pieces[to]
		b.MovePiece(p, to, from)
	}

	// Restore captured piece
	if m.IsEnPassant() {
		if us == types.White {
			b.PutPiece(undo.Captured, to-8)
		} else {
			b.PutPiece(undo.Captured, to+8)
		}
	} else if undo.Captured != types.PieceNone {
		b.PutPiece(undo.Captured, to)
	}

	// Restore board state
	b.SideToMove = us
	b.Castling = undo.Castling
	b.EPSquare = undo.EPSquare
	b.HalfmoveClock = undo.HalfmoveClock
	b.Hash = undo.Hash
}

func (b *Board) MakeNullMove() {
	undo := UndoState{
		Move:          types.MoveNone,
		Captured:      types.PieceNone,
		Castling:      b.Castling,
		EPSquare:      b.EPSquare,
		HalfmoveClock: b.HalfmoveClock,
		Hash:          b.Hash,
	}
	b.UndoStack = append(b.UndoStack, undo)

	if b.EPSquare < types.SquareNone {
		b.Hash ^= ZobristEP[b.EPSquare.File()]
		b.EPSquare = types.SquareNone
	}

	b.SideToMove = b.SideToMove.Other()
	b.Hash ^= ZobristSide
	b.HalfmoveClock++
}

func (b *Board) UnmakeNullMove() {
	undo := b.UndoStack[len(b.UndoStack)-1]
	b.UndoStack = b.UndoStack[:len(b.UndoStack)-1]

	b.SideToMove = b.SideToMove.Other()
	b.Castling = undo.Castling
	b.EPSquare = undo.EPSquare
	b.HalfmoveClock = undo.HalfmoveClock
	b.Hash = undo.Hash
}
