package book

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

type Entry struct {
	Key    uint64
	Move   uint16
	Weight uint16
	Learn  uint32
}

// Book is either a Polyglot book (entries) or an EPD opening suite (epd).
type Book struct {
	entries []Entry
	epd     *epdBook
}

// Read parses raw Polyglot binary book data.
func Read(data []byte) (*Book, error) {
	if len(data)%16 != 0 {
		return nil, fmt.Errorf("invalid polyglot book size: %d bytes (not divisible by 16)", len(data))
	}
	numEntries := len(data) / 16
	entries := make([]Entry, numEntries)
	for i := 0; i < numEntries; i++ {
		offset := i * 16
		entries[i] = Entry{
			Key:    binary.BigEndian.Uint64(data[offset : offset+8]),
			Move:   binary.BigEndian.Uint16(data[offset+8 : offset+10]),
			Weight: binary.BigEndian.Uint16(data[offset+10 : offset+12]),
			Learn:  binary.BigEndian.Uint32(data[offset+12 : offset+16]),
		}
	}
	return &Book{entries: entries}, nil
}

// LoadFile loads an opening book. Besides a filesystem path it accepts:
//   - "" or "<embedded>": the builtin Polyglot book (DefaultBook)
//   - "<pohl>": the embedded Pohl.epd opening suite (PohlBook)
//
// Paths ending in .epd or .epd.gz are read as EPD opening suites; anything else
// as a Polyglot .bin book.
func LoadFile(path string) (*Book, error) {
	if path == "" || path == "<embedded>" {
		return DefaultBook(), nil
	}
	if s := findEmbeddedSuite(path); s != nil {
		return s.load()
	}
	if isEPDPath(path) {
		return loadEPDFile(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Read(data)
}

// EntryCount returns the number of entries in the book (positions, for an EPD book).
func (b *Book) EntryCount() int {
	if b == nil {
		return 0
	}
	if b.epd != nil {
		return b.epd.size()
	}
	return len(b.entries)
}

type candidateMove struct {
	move   types.Move
	weight uint16
}

// Probe searches the book for the given position and returns a legal move, selected probabilistically by weight.
func (bk *Book) Probe(b *board.Board) types.Move {
	if bk == nil {
		return types.MoveNone
	}
	if bk.epd != nil {
		return bk.epd.probe(b)
	}
	if len(bk.entries) == 0 {
		return types.MoveNone
	}

	candidates := bk.polyglotMoves(b)
	if len(candidates) == 0 {
		return types.MoveNone
	}

	if len(candidates) == 1 {
		return candidates[0].move
	}

	// Weighted random selection
	var totalWeight uint32
	for _, c := range candidates {
		w := uint32(c.weight)
		if w == 0 {
			w = 1
		}
		totalWeight += w
	}

	pick := rand.Uint32N(totalWeight)
	var cumulative uint32
	for _, c := range candidates {
		w := uint32(c.weight)
		if w == 0 {
			w = 1
		}
		cumulative += w
		if pick < cumulative {
			return c.move
		}
	}

	return candidates[0].move
}

// polyglotMoves returns the legal book moves stored for b, with their weights.
func (bk *Book) polyglotMoves(b *board.Board) []candidateMove {
	key := Hash(b)

	// Binary search for key in sorted entries
	idx := sort.Search(len(bk.entries), func(i int) bool {
		return bk.entries[i].Key >= key
	})

	if idx >= len(bk.entries) || bk.entries[idx].Key != key {
		return nil
	}

	// Generate legal moves on board to validate book moves
	legalMoves := movegen.GenerateLegalMoves(b)
	if len(legalMoves) == 0 {
		return nil
	}

	var candidates []candidateMove

	// Iterate all entries matching this key
	for i := idx; i < len(bk.entries) && bk.entries[i].Key == key; i++ {
		entry := bk.entries[i]
		toSq := types.Square(entry.Move & 0x3F)
		fromSq := types.Square((entry.Move >> 6) & 0x3F)
		promo := (entry.Move >> 12) & 0x7

		// Convert Polyglot castling move convention (King captures Rook) to standard UCI king moves
		p := b.Pieces[fromSq]
		if p == types.WhiteKing && fromSq == types.E1 {
			if toSq == types.H1 {
				toSq = types.G1
			} else if toSq == types.A1 {
				toSq = types.C1
			}
		} else if p == types.BlackKing && fromSq == types.E8 {
			if toSq == types.H8 {
				toSq = types.G8
			} else if toSq == types.A8 {
				toSq = types.C8
			}
		}

		var targetPromo types.PieceType
		switch promo {
		case 1:
			targetPromo = types.Knight
		case 2:
			targetPromo = types.Bishop
		case 3:
			targetPromo = types.Rook
		case 4:
			targetPromo = types.Queen
		}

		for _, lm := range legalMoves {
			if lm.From() == fromSq && lm.To() == toSq {
				if promo == 0 && !lm.IsPromotion() {
					candidates = append(candidates, candidateMove{move: lm, weight: entry.Weight})
					break
				} else if promo != 0 && lm.IsPromotion() && lm.PromotionType() == targetPromo {
					candidates = append(candidates, candidateMove{move: lm, weight: entry.Weight})
					break
				}
			}
		}
	}

	return candidates
}
