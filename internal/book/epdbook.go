package book

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// An EPD suite such as Stefan Pohl's lists only the positions at the end of its
// opening lines. Turning every one into a move tree up front costs hours, so an
// EPD book works at probe time instead: it keeps the end positions, filters them
// down to those still reachable from the game position, finds a legal move path
// to one of them with a small search budget, and plays that path's first move.
// The rest of the path is remembered, so later probes along it are instant.
//
// Suites can hold millions of positions, so each one is stored packed in four
// bitboards plus a small header (34 bytes) rather than as text.

//go:embed pohl.epdb.gz
var pohlPacked []byte

// Names that select an embedded EPD suite in config.json ("book_file"), on the
// command line (--book) and via the UCI BookFile option.
const PohlBookName = "<pohl>"

type embeddedSuite struct {
	data    *[]byte
	aliases []string
	once    sync.Once
	book    *Book
	err     error
}

var embeddedSuites = []*embeddedSuite{
	{data: &pohlPacked, aliases: []string{"pohl"}},
}

// embeddedSuite returns the embedded suite a book_file value names, if any.
// Names match case-insensitively, with or without angle brackets.
func findEmbeddedSuite(name string) *embeddedSuite {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, "<") && strings.HasSuffix(n, ">") {
		n = n[1 : len(n)-1]
	}
	for _, s := range embeddedSuites {
		if slices.Contains(s.aliases, n) {
			return s
		}
	}
	return nil
}

// load decodes the suite on first use only.
func (s *embeddedSuite) load() (*Book, error) {
	s.once.Do(func() {
		gz, err := gzip.NewReader(bytes.NewReader(*s.data))
		if err != nil {
			s.err = err
			return
		}
		s.book, s.err = ReadPackedEPD(gz)
	})
	return s.book, s.err
}

// PohlBook returns the embedded Pohl.epd opening suite.
func PohlBook() (*Book, error) { return findEmbeddedSuite(PohlBookName).load() }

// isEPDPath reports whether path should be read as an EPD suite rather than a
// Polyglot binary.
func isEPDPath(path string) bool {
	p := strings.ToLower(path)
	return strings.HasSuffix(p, ".epd") || strings.HasSuffix(p, ".epd.gz")
}

func loadEPDFile(path string) (*Book, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(strings.ToLower(path), ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	return ReadEPD(r)
}

// Tuning for probe-time path finding.
// Most targets solve within a few hundred nodes and the rest need 100k+, so
// many cheap attempts beat a few expensive ones.
const (
	epdTryNodes    = 2_000                  // node budget for one target
	epdProbeBudget = 150 * time.Millisecond // total time one probe may spend searching
)

// Packed position: pawn bitboards per color, an occupancy bitboard of the other
// pieces, and their piece codes, 4 bits each in square order. meta holds the
// castling rights (low 4 bits) and the ply count from the start position.
type epdBook struct {
	wp, bp, occ, codes []uint64
	meta               []uint16

	mu       sync.Mutex
	searcher *pathSearcher
	line     map[uint64]types.Move // position hash -> next move on the line being followed
	cands    []int32               // scratch for probe
}

func newEPDBook() *epdBook {
	eb := &epdBook{searcher: newPathSearcher(epdTryNodes, nil), line: make(map[uint64]types.Move)}
	eb.searcher.prefer = DefaultBook()
	return eb
}

func (eb *epdBook) size() int { return len(eb.meta) }

// add packs one EPD line; it returns false for lines it cannot use.
func (eb *epdBook) add(line string) bool {
	f := strings.Fields(line)
	if len(f) < 6 || !isDigits(f[4]) || !isDigits(f[5]) {
		// The exact ply count is needed to know when the line ends.
		return false
	}
	var wp, bp, occ uint64
	var pieces [64]types.Piece
	rank, file := 7, 0
	for i := 0; i < len(f[0]); i++ {
		ch := f[0][i]
		switch {
		case ch == '/':
			rank--
			file = 0
		case ch >= '1' && ch <= '8':
			file += int(ch - '0')
		default:
			c, lc := types.White, ch
			if ch >= 'a' && ch <= 'z' {
				c = types.Black
			} else {
				lc = ch + ('a' - 'A')
			}
			pt := strings.IndexByte(" pnbrqk", lc)
			if pt <= 0 || rank < 0 || file > 7 {
				return false
			}
			sq := rank*8 + file
			switch {
			case types.PieceType(pt) != types.Pawn:
				occ |= 1 << uint(sq)
				pieces[sq] = types.MakePiece(c, types.PieceType(pt))
			case c == types.White:
				wp |= 1 << uint(sq)
			default:
				bp |= 1 << uint(sq)
			}
			file++
		}
	}
	if bits.OnesCount64(occ) > 16 {
		return false // promoted pieces; cannot be an opening position anyway
	}
	var codes uint64
	n := 0
	for o := occ; o != 0; o &= o - 1 {
		codes |= uint64(pieces[bits.TrailingZeros64(o)]) << (4 * n)
		n++
	}
	var castling types.CastlingRights
	for _, ch := range f[2] {
		switch ch {
		case 'K':
			castling |= types.CastlingWhiteKing
		case 'Q':
			castling |= types.CastlingWhiteQueen
		case 'k':
			castling |= types.CastlingBlackKing
		case 'q':
			castling |= types.CastlingBlackQueen
		}
	}
	stm := 0
	if f[1] == "b" {
		stm = 1
	} else if f[1] != "w" {
		return false
	}
	fm, _ := strconv.Atoi(f[5])
	plies := 2*(max(fm, 1)-1) + stm
	if plies >= 1<<12 {
		return false
	}
	eb.wp = append(eb.wp, wp)
	eb.bp = append(eb.bp, bp)
	eb.occ = append(eb.occ, occ)
	eb.codes = append(eb.codes, codes)
	eb.meta = append(eb.meta, uint16(castling)|uint16(plies)<<4)
	return true
}

func (eb *epdBook) plies(i int) int                     { return int(eb.meta[i] >> 4) }
func (eb *epdBook) castling(i int) types.CastlingRights { return types.CastlingRights(eb.meta[i] & 15) }

// board unpacks position i.
func (eb *epdBook) board(i int) (*board.Board, error) {
	var pieces [64]types.Piece
	for p := eb.wp[i]; p != 0; p &= p - 1 {
		pieces[bits.TrailingZeros64(p)] = types.WhitePawn
	}
	for p := eb.bp[i]; p != 0; p &= p - 1 {
		pieces[bits.TrailingZeros64(p)] = types.BlackPawn
	}
	n := 0
	for o := eb.occ[i]; o != 0; o &= o - 1 {
		pieces[bits.TrailingZeros64(o)] = types.Piece((eb.codes[i] >> (4 * n)) & 15)
		n++
	}
	var sb strings.Builder
	for r := 7; r >= 0; r-- {
		empty := 0
		for f := 0; f < 8; f++ {
			p := pieces[r*8+f]
			if p == types.PieceNone {
				empty++
				continue
			}
			if empty > 0 {
				sb.WriteByte(byte('0' + empty))
				empty = 0
			}
			ch := " pnbrqk"[p.Type()]
			if p.Color() == types.White {
				ch -= 'a' - 'A'
			}
			sb.WriteByte(ch)
		}
		if empty > 0 {
			sb.WriteByte(byte('0' + empty))
		}
		if r > 0 {
			sb.WriteByte('/')
		}
	}
	stm := " w "
	if eb.plies(i)%2 == 1 {
		stm = " b "
	}
	sb.WriteString(stm)
	c := eb.castling(i)
	cs := ""
	for j, ch := range "KQkq" {
		if c&(1<<j) != 0 {
			cs += string(ch)
		}
	}
	if cs == "" {
		cs = "-"
	}
	sb.WriteString(cs + " - 0 1")
	return board.ParseFEN(sb.String())
}

// ReadEPD reads a text EPD/FEN opening suite into a book. Lines need FEN move
// counters (as in Pohl's suites).
func ReadEPD(r io.Reader) (*Book, error) {
	eb := newEPDBook()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eb.add(line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if eb.size() == 0 {
		return nil, fmt.Errorf("no usable EPD positions found (lines need move counters)")
	}
	return &Book{epd: eb}, nil
}

const packedMagic = "DFEPDB1\n"

// WritePacked writes an EPD book in the compact binary form the engine embeds:
// magic, a uint32 count, then column arrays (little endian) of wp, bp, occ,
// codes and meta. Columns compress far better than interleaved records.
func (bk *Book) WritePacked(w io.Writer) error {
	if bk == nil || bk.epd == nil {
		return errors.New("not an EPD book")
	}
	eb := bk.epd.sorted()
	bw := bufio.NewWriter(w)
	bw.WriteString(packedMagic)
	binary.Write(bw, binary.LittleEndian, uint32(eb.size()))
	for _, col := range [][]uint64{eb.wp, eb.bp, eb.occ, eb.codes} {
		if err := binary.Write(bw, binary.LittleEndian, col); err != nil {
			return err
		}
	}
	if err := binary.Write(bw, binary.LittleEndian, eb.meta); err != nil {
		return err
	}
	return bw.Flush()
}

// sorted returns a copy with positions ordered by pawn structure, which keeps
// similar positions together and so roughly halves the compressed size.
func (eb *epdBook) sorted() *epdBook {
	idx := make([]int, eb.size())
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int {
		for _, col := range [][]uint64{eb.wp, eb.bp, eb.occ, eb.codes} {
			if col[a] != col[b] {
				if col[a] < col[b] {
					return -1
				}
				return 1
			}
		}
		return int(eb.meta[a]) - int(eb.meta[b])
	})
	out := &epdBook{}
	for _, i := range idx {
		out.wp = append(out.wp, eb.wp[i])
		out.bp = append(out.bp, eb.bp[i])
		out.occ = append(out.occ, eb.occ[i])
		out.codes = append(out.codes, eb.codes[i])
		out.meta = append(out.meta, eb.meta[i])
	}
	return out
}

// ReadPackedEPD reads a book written by WritePacked.
func ReadPackedEPD(r io.Reader) (*Book, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	magic := make([]byte, len(packedMagic))
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != packedMagic {
		return nil, errors.New("not a packed EPD book")
	}
	var n uint32
	if err := binary.Read(br, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	// Decode in small chunks: binary.Read on a whole column would allocate a
	// temporary buffer as large as the column itself.
	eb := newEPDBook()
	var buf [8 << 10]byte
	for _, col := range []*[]uint64{&eb.wp, &eb.bp, &eb.occ, &eb.codes} {
		c := make([]uint64, n)
		for i := 0; i < len(c); {
			k := min(len(c)-i, len(buf)/8)
			if _, err := io.ReadFull(br, buf[:k*8]); err != nil {
				return nil, err
			}
			for j := 0; j < k; j++ {
				c[i+j] = binary.LittleEndian.Uint64(buf[j*8:])
			}
			i += k
		}
		*col = c
	}
	eb.meta = make([]uint16, n)
	for i := 0; i < len(eb.meta); {
		k := min(len(eb.meta)-i, len(buf)/2)
		if _, err := io.ReadFull(br, buf[:k*2]); err != nil {
			return nil, err
		}
		for j := 0; j < k; j++ {
			eb.meta[i+j] = binary.LittleEndian.Uint16(buf[j*2:])
		}
		i += k
	}
	return &Book{epd: eb}, nil
}

// pawnReach[c][s] holds the squares a pawn of color c on s could ever reach.
var pawnReach [2][64]uint64

func init() {
	for s := 8; s < 56; s++ {
		sf, sr := s%8, s/8
		for t := 0; t < 64; t++ {
			tf, tr := t%8, t/8
			if d := tr - sr; d >= 0 && absInt(sf-tf) <= d {
				pawnReach[types.White][s] |= 1 << uint(t)
			}
			if d := sr - tr; d >= 0 && absInt(sf-tf) <= d {
				pawnReach[types.Black][s] |= 1 << uint(t)
			}
		}
	}
}

func gamePly(b *board.Board) int {
	fm := int(b.FullmoveNumber)
	if fm < 1 {
		fm = 1
	}
	return 2*(fm-1) + int(b.SideToMove)
}

func (eb *epdBook) probe(b *board.Board) types.Move {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	legal := movegen.GenerateLegalMoves(b)
	if len(legal) == 0 {
		return types.MoveNone
	}
	isLegal := func(m types.Move) bool {
		for _, lm := range legal {
			if lm == m {
				return true
			}
		}
		return false
	}

	// Still on the line found by an earlier probe?
	if m, ok := eb.line[b.Hash]; ok && isLegal(m) {
		return m
	}

	// Cheap necessary conditions: the line is longer than the game so far,
	// castling rights were not lost, and every target pawn can be reached by a
	// pawn still on the board (pawns never move backwards or appear).
	ply := gamePly(b)
	curW, curB := uint64(b.PieceBB[types.WhitePawn]), uint64(b.PieceBB[types.BlackPawn])
	var reachW, reachB uint64
	for p := curW; p != 0; p &= p - 1 {
		reachW |= pawnReach[types.White][bits.TrailingZeros64(p)]
	}
	for p := curB; p != 0; p &= p - 1 {
		reachB |= pawnReach[types.Black][bits.TrailingZeros64(p)]
	}
	nW, nB := bits.OnesCount64(curW), bits.OnesCount64(curB)
	castle := uint16(b.Castling)
	cands := eb.cands[:0]
	for i, meta := range eb.meta {
		if int(meta>>4) <= ply || meta&15&^castle != 0 {
			continue
		}
		wp, bp := eb.wp[i], eb.bp[i]
		if wp&^reachW != 0 || bp&^reachB != 0 || bits.OnesCount64(wp) > nW || bits.OnesCount64(bp) > nB {
			continue
		}
		cands = append(cands, int32(i))
	}
	eb.cands = cands
	if len(cands) == 0 {
		return types.MoveNone
	}

	// Try targets in random order, so play follows the suite's distribution,
	// until one yields a path or the time budget runs out.
	work := b.Clone()
	deadline := time.Now().Add(epdProbeBudget)
	for n := len(cands); n > 0 && time.Now().Before(deadline); n-- {
		j := rand.IntN(n)
		i := int(cands[j])
		cands[j] = cands[n-1]

		tb, err := eb.board(i)
		if err != nil {
			continue
		}
		path, ok := eb.searcher.findFrom(work, tb, eb.plies(i)-ply)
		if !ok || len(path) == 0 || !isLegal(path[0]) {
			continue
		}
		// Remember the rest of the line (not the current position, so a new
		// game from here picks a fresh target).
		clear(eb.line)
		for k, m := range path {
			if k > 0 {
				eb.line[work.Hash] = m
			}
			if !work.MakeMove(m) {
				break
			}
		}
		return path[0]
	}
	return types.MoveNone
}
