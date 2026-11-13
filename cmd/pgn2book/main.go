// Command pgn2book builds a Polyglot opening book from the opening moves of a
// PGN game collection, e.g. the CCRL game database:
//
//	go run ./cmd/pgn2book -in games.pgn -out internal/book/book.bin.gz
//
// Every game's first -plies half-moves are counted per position. A move goes
// into the book if it was played often enough (-min-games, and -min-share of
// the games that reached the position) and scored within -max-loss of the
// best-scoring such move there; its weight is its game count scaled by how
// well it scored. Only positions the book itself can reach from the start
// position are kept.
//
// -merge adds the lines of an existing Polyglot book for the positions the new
// one does not cover (the previous hand-made book, say).
//
// Counting a large collection takes a while, so the counts can be saved
// (-cache) and the selection rerun from them with different thresholds.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"devilfish/internal/board"
	"devilfish/internal/book"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// stat counts one move in one position. points is the mover's score in half
// points (2 per win, 1 per draw).
type stat struct {
	key    uint64
	move   types.Move
	games  uint32
	points uint32
}

const shards = 256

type table struct {
	mu [shards]sync.Mutex
	m  [shards]map[uint64]*stat
}

func newTable() *table {
	t := &table{}
	for i := range t.m {
		t.m[i] = make(map[uint64]*stat)
	}
	return t
}

func (t *table) add(key uint64, move types.Move, points uint32) {
	id := key ^ uint64(move)*0x9E3779B97F4A7C15
	s := id >> 56
	t.mu[s].Lock()
	e := t.m[s][id]
	if e == nil {
		e = &stat{key: key, move: move}
		t.m[s][id] = e
	}
	e.games++
	e.points += points
	t.mu[s].Unlock()
}

type game struct {
	result   string
	minElo   int
	movetext string
}

// parseSAN resolves a SAN token against the legal moves of b.
func parseSAN(b *board.Board, tok string) types.Move {
	tok = strings.TrimRight(tok, "+#!?")
	legal := movegen.GenerateLegalMoves(b)
	if tok == "O-O" || tok == "O-O-O" || tok == "0-0" || tok == "0-0-0" {
		short := len(tok) == 3
		for _, m := range legal {
			if m.IsCastle() && (m.To().File() > m.From().File()) == short {
				return m
			}
		}
		return types.MoveNone
	}
	promo := types.PieceType(0)
	if i := strings.IndexByte(tok, '='); i >= 0 && i+1 < len(tok) {
		promo = types.PieceType(strings.IndexByte(" PNBRQK", tok[i+1]))
		tok = tok[:i]
	} else if n := len(tok); n > 2 && strings.IndexByte("NBRQ", tok[n-1]) >= 0 {
		promo = types.PieceType(strings.IndexByte(" PNBRQK", tok[n-1]))
		tok = tok[:n-1]
	}
	tok = strings.ReplaceAll(tok, "x", "")
	if len(tok) < 2 {
		return types.MoveNone
	}
	pt := types.Pawn
	if i := strings.IndexByte("NBRQK", tok[0]); i >= 0 {
		pt = types.PieceType(i + 2)
		tok = tok[1:]
	}
	if len(tok) < 2 {
		return types.MoveNone
	}
	dest := tok[len(tok)-2:]
	if dest[0] < 'a' || dest[0] > 'h' || dest[1] < '1' || dest[1] > '8' {
		return types.MoveNone
	}
	to := types.Square(int(dest[1]-'1')*8 + int(dest[0]-'a'))
	fromFile, fromRank := -1, -1
	for _, ch := range tok[:len(tok)-2] {
		switch {
		case ch >= 'a' && ch <= 'h':
			fromFile = int(ch - 'a')
		case ch >= '1' && ch <= '8':
			fromRank = int(ch - '1')
		}
	}
	for _, m := range legal {
		if m.To() != to || m.IsCastle() || b.PieceAt(m.From()).Type() != pt {
			continue
		}
		if fromFile >= 0 && int(m.From().File()) != fromFile {
			continue
		}
		if fromRank >= 0 && int(m.From().Rank()) != fromRank {
			continue
		}
		if m.IsPromotion() != (promo != 0) || (promo != 0 && m.PromotionType() != promo) {
			continue
		}
		return m
	}
	return types.MoveNone
}

// count plays the opening of g and adds its moves to t. It returns false if a
// move could not be read.
func count(g game, maxPlies int, t *table) bool {
	var white uint32
	switch g.result {
	case "1-0":
		white = 2
	case "1/2-1/2":
		white = 1
	case "0-1":
		white = 0
	default:
		return true // unfinished game: no result to learn from
	}
	b, _ := board.ParseFEN(board.StartFEN)
	ply, depth := 0, 0
	text := g.movetext
	for i := 0; i < len(text) && ply < maxPlies; {
		ch := text[i]
		switch {
		case ch == '{':
			depth++
			i++
		case ch == '}':
			depth--
			i++
		case depth > 0 || ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t':
			i++
		case ch == '(' || ch == ')' || ch == ';':
			return true // variations: not expected, stop here
		default:
			j := i
			for j < len(text) && text[j] != ' ' && text[j] != '\n' && text[j] != '{' && text[j] != '\r' {
				j++
			}
			tok := text[i:j]
			i = j
			if tok[0] == '$' || tok[0] == '*' {
				continue
			}
			if tok[0] >= '0' && tok[0] <= '9' && !strings.HasPrefix(tok, "0-0") {
				// A move number, possibly glued to the move ("1.e4"), or the result.
				k := strings.LastIndexByte(tok, '.')
				if k < 0 || k+1 >= len(tok) {
					continue
				}
				tok = tok[k+1:]
			}
			m := parseSAN(b, tok)
			if m == types.MoveNone {
				return false
			}
			points := white
			if ply%2 == 1 {
				points = 2 - white
			}
			t.add(book.Hash(b), m, points)
			if !b.MakeMove(m) {
				return false
			}
			ply++
		}
	}
	return true
}

func headerValue(line string) string {
	i := strings.IndexByte(line, '"')
	j := strings.LastIndexByte(line, '"')
	if i < 0 || j <= i {
		return ""
	}
	return line[i+1 : j]
}

// readGames splits a PGN stream into games, keeping only the start of each
// game's movetext (the opening is all that is needed).
func readGames(r io.Reader, out chan<- []game) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	const keep = 6000
	var g game
	var sb strings.Builder
	batch := make([]game, 0, 512)
	inMoves, setup := false, false
	flush := func() {
		if inMoves && !setup {
			g.movetext = sb.String()
			batch = append(batch, g)
			if len(batch) == cap(batch) {
				out <- batch
				batch = make([]game, 0, 512)
			}
		}
		g = game{minElo: 1 << 30}
		sb.Reset()
		inMoves, setup = false, false
	}
	g.minElo = 1 << 30
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "[") && (!inMoves || strings.HasPrefix(line, "[Event ")) {
			if inMoves {
				flush()
			}
			switch {
			case strings.HasPrefix(line, "[Result "):
				g.result = headerValue(line)
			case strings.HasPrefix(line, "[WhiteElo "), strings.HasPrefix(line, "[BlackElo "):
				if e, err := strconv.Atoi(headerValue(line)); err == nil && e < g.minElo {
					g.minElo = e
				}
			case strings.HasPrefix(line, "[FEN "), strings.HasPrefix(line, "[SetUp "):
				setup = true
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		inMoves = true
		if sb.Len() < keep {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	flush()
	if len(batch) > 0 {
		out <- batch
	}
	return sc.Err()
}

func countFile(path string, maxPlies, minElo, workers int) ([]stat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		r = gz
	}
	t := newTable()
	ch := make(chan []game, 4*workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var games, used, bad int
	start := time.Now()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range ch {
				u, bd := 0, 0
				for _, g := range batch {
					if g.minElo < minElo || g.minElo == 1<<30 && minElo > 0 {
						continue
					}
					u++
					if !count(g, maxPlies, t) {
						bd++
					}
				}
				mu.Lock()
				before := games
				games += len(batch)
				used += u
				bad += bd
				if games/100_000 != before/100_000 {
					fmt.Fprintf(os.Stderr, "\r%d games read (%s)", games, time.Since(start).Round(time.Second))
				}
				mu.Unlock()
			}
		}()
	}
	err = readGames(r, ch)
	close(ch)
	wg.Wait()
	fmt.Fprintf(os.Stderr, "\r%d games read, %d used, %d with an unreadable move (%s)\n", games, used, bad, time.Since(start).Round(time.Second))
	if err != nil {
		return nil, err
	}
	var all []stat
	for i := range t.m {
		for _, e := range t.m[i] {
			if e.games >= 3 { // rarer moves can never pass a sensible threshold
				all = append(all, *e)
			}
		}
	}
	return all, nil
}

const cacheMagic = "DFPGNCNT1\n"

func writeCache(path string, stats []stat) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	bw := bufio.NewWriter(gz)
	bw.WriteString(cacheMagic)
	binary.Write(bw, binary.LittleEndian, uint32(len(stats)))
	for _, s := range stats {
		binary.Write(bw, binary.LittleEndian, s.key)
		binary.Write(bw, binary.LittleEndian, uint16(s.move))
		binary.Write(bw, binary.LittleEndian, s.games)
		binary.Write(bw, binary.LittleEndian, s.points)
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Close()
}

func readCache(path string) ([]stat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(gz, 1<<20)
	magic := make([]byte, len(cacheMagic))
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != cacheMagic {
		return nil, fmt.Errorf("%s is not a pgn2book count cache", path)
	}
	var n uint32
	if err := binary.Read(br, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	stats := make([]stat, n)
	var buf [18]byte
	for i := range stats {
		if _, err := io.ReadFull(br, buf[:]); err != nil {
			return nil, err
		}
		stats[i] = stat{
			key:    binary.LittleEndian.Uint64(buf[0:]),
			move:   types.Move(binary.LittleEndian.Uint16(buf[8:])),
			games:  binary.LittleEndian.Uint32(buf[10:]),
			points: binary.LittleEndian.Uint32(buf[14:]),
		}
	}
	return stats, nil
}

type selection struct {
	minGames int
	minShare float64
	maxLoss  float64
	minScore float64
}

// choose returns the book moves of one position (all of stats share a key).
func (sel selection) choose(stats []stat) []book.Entry {
	total := 0
	for _, s := range stats {
		total += int(s.games)
	}
	score := func(s stat) float64 { return float64(s.points) / float64(2*s.games) }
	best := 0.0
	var cand []stat
	for _, s := range stats {
		if int(s.games) < sel.minGames || float64(s.games) < sel.minShare*float64(total) {
			continue
		}
		cand = append(cand, s)
		if sc := score(s); sc > best {
			best = sc
		}
	}
	type pick struct {
		s stat
		w float64
	}
	var picks []pick
	maxW := 0.0
	for _, s := range cand {
		sc := score(s)
		if sc < best-sel.maxLoss || sc < sel.minScore {
			continue
		}
		// Popularity, tilted toward the moves that scored better.
		w := float64(s.games) * sc * sc
		picks = append(picks, pick{s, w})
		if w > maxW {
			maxW = w
		}
	}
	var out []book.Entry
	for _, p := range picks {
		w := uint16(p.w / maxW * 10000)
		if w == 0 {
			w = 1
		}
		out = append(out, book.Entry{Key: p.s.key, Move: uint16(p.s.move), Weight: w, Learn: p.s.games})
	}
	return out
}

// build selects the book: a walk from the start position through the chosen
// moves, so that nothing unreachable is stored. Entry.Move holds the engine
// move until the walk encodes it.
func build(stats []stat, sel selection) *book.Book {
	byKey := make(map[uint64][]stat)
	for _, s := range stats {
		if int(s.games) >= sel.minGames {
			byKey[s.key] = append(byKey[s.key], s)
		}
	}
	// The share threshold needs every move's count, including rare ones.
	totals := make(map[uint64]uint32)
	for _, s := range stats {
		totals[s.key] += s.games
	}
	var entries []book.Entry
	seen := make(map[uint64]bool)
	start, _ := board.ParseFEN(board.StartFEN)
	type node struct{ b *board.Board }
	queue := []node{{start}}
	seen[book.Hash(start)] = true
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		key := book.Hash(n.b)
		cand := byKey[key]
		if len(cand) == 0 {
			continue
		}
		// Stand-in for the moves below the game threshold, so shares are of
		// all games that reached the position.
		rest := totals[key]
		for _, s := range cand {
			rest -= s.games
		}
		withRest := cand
		if rest > 0 {
			withRest = append(append([]stat{}, cand...), stat{key: key, games: rest})
		}
		for _, e := range sel.choose(withRest) {
			if e.Move == 0 {
				continue // the stand-in
			}
			m := types.Move(e.Move)
			child := n.b.Clone()
			if !child.MakeMove(m) {
				continue
			}
			e.Move = book.EncodeMove(n.b, m)
			entries = append(entries, e)
			if ck := book.Hash(child); !seen[ck] {
				seen[ck] = true
				queue = append(queue, node{child})
			}
		}
	}
	return book.NewPolyglot(entries)
}

func main() {
	in := flag.String("in", "", "input PGN (.pgn or .pgn.gz); not needed with an existing -cache")
	out := flag.String("out", "", "output Polyglot book (.bin or .bin.gz)")
	cache := flag.String("cache", "", "file to save the move counts to, or to reuse them from if it exists")
	plies := flag.Int("plies", 30, "opening half-moves counted per game")
	minElo := flag.Int("min-elo", 0, "skip games where either player is rated below this")
	minGames := flag.Int("min-games", 50, "a move needs this many games")
	minShare := flag.Float64("min-share", 0.02, "and this share of the games that reached its position")
	maxLoss := flag.Float64("max-loss", 0.06, "and to score within this of the best-scoring move there (0.06 = 6 points per 100 games)")
	minScore := flag.Float64("min-score", 0.35, "and at least this score for the side playing it")
	workers := flag.Int("workers", runtime.NumCPU(), "parallel workers")
	merge := flag.String("merge", "", "Polyglot book whose moves are added for positions the new book lacks")
	flag.Parse()

	var stats []stat
	var err error
	if *cache != "" {
		if _, e := os.Stat(*cache); e == nil {
			stats, err = readCache(*cache)
		}
	}
	if stats == nil && err == nil {
		if *in == "" {
			flag.Usage()
			os.Exit(2)
		}
		stats, err = countFile(*in, *plies, *minElo, *workers)
		if err == nil && *cache != "" {
			err = writeCache(*cache, stats)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "%d position/move pairs with 3+ games\n", len(stats))

	sel := selection{*minGames, *minShare, *maxLoss, *minScore}
	if *out == "" {
		// No output: show how the book size depends on the game threshold.
		for _, g := range []int{10, 20, 30, 50, 100, 200, 500} {
			s := sel
			s.minGames = g
			fmt.Printf("min-games %4d: %d entries\n", g, build(stats, s).EntryCount())
		}
		return
	}
	bk := build(stats, sel)
	if *merge != "" {
		old, err := book.LoadFile(*merge)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		entries := bk.Entries()
		have := make(map[uint64]bool, len(entries))
		for _, e := range entries {
			have[e.Key] = true
		}
		added := 0
		for _, e := range old.Entries() {
			if !have[e.Key] {
				entries = append(entries, e)
				added++
			}
		}
		bk = book.NewPolyglot(entries)
		fmt.Printf("merged %d moves from %s\n", added, *merge)
	}
	o, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var w io.WriteCloser = o
	if strings.HasSuffix(*out, ".gz") {
		gz, _ := gzip.NewWriterLevel(o, gzip.BestCompression)
		w = gz
	}
	if _, err := bk.WriteTo(w); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if w != io.WriteCloser(o) {
		w.Close()
	}
	o.Close()
	fi, _ := os.Stat(*out)
	fmt.Printf("%s: %d entries, %d bytes\n", *out, bk.EntryCount(), fi.Size())
}
