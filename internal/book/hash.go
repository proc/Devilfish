package book

import (
	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/types"
)

// Hash computes the 64-bit Polyglot Zobrist hash of a position.
func Hash(b *board.Board) uint64 {
	var key uint64

	// 1. Pieces on squares
	for sq := types.Square(0); sq < 64; sq++ {
		p := b.Pieces[sq]
		if p == types.PieceNone {
			continue
		}
		pt := p.Type()
		c := p.Color()
		if pt < types.Pawn || pt > types.King || (c != types.White && c != types.Black) {
			continue
		}

		colorCode := 0
		if c == types.White {
			colorCode = 1
		}
		pieceIndex := (int(pt)-1)*2 + colorCode
		key ^= PolyglotRandomArray[64*pieceIndex+int(sq)]
	}

	// 2. Castling rights
	if b.Castling&types.CastlingWhiteKing != 0 {
		key ^= PolyglotRandomArray[768]
	}
	if b.Castling&types.CastlingWhiteQueen != 0 {
		key ^= PolyglotRandomArray[769]
	}
	if b.Castling&types.CastlingBlackKing != 0 {
		key ^= PolyglotRandomArray[770]
	}
	if b.Castling&types.CastlingBlackQueen != 0 {
		key ^= PolyglotRandomArray[771]
	}

	// 3. En passant square: hashed in only if a friendly pawn can capture it
	if b.EPSquare < types.SquareNone {
		var epPawnAttackers bitboard.Bitboard
		if b.SideToMove == types.White {
			epPawnAttackers = bitboard.PawnAttacks(types.Black, b.EPSquare) & b.PieceBB[types.WhitePawn]
		} else {
			epPawnAttackers = bitboard.PawnAttacks(types.White, b.EPSquare) & b.PieceBB[types.BlackPawn]
		}

		if epPawnAttackers != 0 {
			file := b.EPSquare.File()
			key ^= PolyglotRandomArray[772+int(file)]
		}
	}

	// 4. Side to move: hashed in if White to move
	if b.SideToMove == types.White {
		key ^= PolyglotRandomArray[780]
	}

	return key
}
