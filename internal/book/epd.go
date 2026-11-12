package book

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"devilfish/internal/bitboard"
	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// EPD books (such as Stefan Pohl's opening suites) list only the position at the
// end of each opening line, not the moves that lead there. To use one as an
// opening book we reconstruct a legal move sequence from the start position to
// every EPD position, then turn all those sequences into a Polyglot-style move
// tree. Weights count how many EPD positions are reached through each move, so
// play follows the distribution of the suite.

// EPDBuildOptions tunes the reconstruction search.
type EPDBuildOptions struct {
	// NodeLimit caps search nodes per EPD position (0 = default 2,000,000).
	NodeLimit int
	// Workers is the number of parallel searches (0 = runtime.NumCPU()).
	Workers int
	// Progress, if non-nil, is called periodically with (done, total).
	Progress func(done, total int)
}

// EPDBuildStats summarises a build.
type EPDBuildStats struct {
	Positions int // EPD lines parsed
	Solved    int // positions a move path was found for
	Skipped   int // unreachable from the start position, or node limit hit
	Invalid   int // lines that did not parse
	Entries   int // resulting book entries
}

// BuildFromEPD reads EPD/FEN lines and produces an opening book whose lines end in
// those positions. Positions must be reachable from the standard start position.
func BuildFromEPD(r io.Reader, opts EPDBuildOptions) (*Book, EPDBuildStats, error) {
	var stats EPDBuildStats
	type target struct {
		b     *board.Board
		plies int // exact path length, -1 if the line has no move counters
	}
	var targets []target

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		stats.Positions++
		f := strings.Fields(line)
		if len(f) < 4 {
			stats.Invalid++
			continue
		}
		// EPD has 4 position fields followed by optional opcodes. Many suites
		// (Pohl's included) carry FEN move counters, which fix the path length.
		tb, err := board.ParseFEN(strings.Join(f[:4], " ") + " 0 1")
		if err != nil {
			stats.Invalid++
			continue
		}
		plies := -1
		if len(f) >= 6 && isDigits(f[4]) && isDigits(f[5]) {
			if fm, _ := strconv.Atoi(f[5]); fm >= 1 {
				plies = 2*(fm-1) + int(tb.SideToMove)
			}
		}
		targets = append(targets, target{tb, plies})
	}
	if err := sc.Err(); err != nil {
		return nil, stats, err
	}

	if opts.NodeLimit <= 0 {
		opts.NodeLimit = 2_000_000
	}
	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}

	type edge struct {
		key  uint64
		move uint16
	}
	paths := make([][]types.Move, len(targets))

	// Pass 1 solves each position independently. Pass 2 retries the failures
	// with a larger budget, steering the search along lines already solved in
	// pass 1 (openings share long prefixes, so this resolves most of the rest).
	runPass := func(limit int, hints map[uint64][]types.Move, todo []int, done *atomic.Int64, total int) {
		var next atomic.Int64
		var wg sync.WaitGroup
		for w := 0; w < opts.Workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s := newPathSearcher(limit, hints)
				for {
					n := int(next.Add(1) - 1)
					if n >= len(todo) {
						return
					}
					i := todo[n]
					if moves, ok := s.find(targets[i].b, targets[i].plies); ok {
						paths[i] = moves
					}
					if d := int(done.Add(1)); opts.Progress != nil && (d%1000 == 0 || d == total) {
						opts.Progress(d, total)
					}
				}
			}()
		}
		wg.Wait()
	}

	all := make([]int, len(targets))
	for i := range all {
		all[i] = i
	}
	var done atomic.Int64
	runPass(opts.NodeLimit/5, nil, all, &done, len(targets))

	var retry []int
	hints := make(map[uint64][]types.Move)
	for i, p := range paths {
		if p == nil {
			retry = append(retry, i)
			continue
		}
		b, _ := board.ParseFEN(board.StartFEN)
		for _, m := range p {
			h := hints[b.Hash]
			if !slices.Contains(h, m) {
				hints[b.Hash] = append(h, m)
			}
			b.MakeMove(m)
		}
	}
	if len(retry) > 0 {
		var done2 atomic.Int64
		total := len(retry)
		prog := opts.Progress
		if prog != nil {
			opts.Progress = func(d, _ int) { prog(len(targets)-total+d, len(targets)) }
		}
		runPass(opts.NodeLimit, hints, retry, &done2, total)
		opts.Progress = prog
	}

	edges := make([][]edge, len(paths))
	for i, p := range paths {
		if p == nil {
			continue
		}
		b, _ := board.ParseFEN(board.StartFEN)
		e := make([]edge, len(p))
		for j, m := range p {
			e[j] = edge{Hash(b), encodePolyglotMove(b, m)}
			b.MakeMove(m)
		}
		edges[i] = e
	}

	counts := make(map[edge]uint32)
	for _, p := range edges {
		if p == nil {
			stats.Skipped++
			continue
		}
		stats.Solved++
		for _, e := range p {
			counts[e]++
		}
	}

	byKey := make(map[uint64][]Entry)
	for e, c := range counts {
		byKey[e.key] = append(byKey[e.key], Entry{Key: e.key, Move: e.move, Learn: c})
	}
	entries := make([]Entry, 0, len(counts))
	for _, es := range byKey {
		// Scale counts into the 16-bit Polyglot weight field, keeping proportions.
		var max uint32
		for _, e := range es {
			if e.Learn > max {
				max = e.Learn
			}
		}
		for i := range es {
			w := es[i].Learn
			if max > 65535 {
				w = uint32(uint64(w) * 65535 / uint64(max))
			}
			if w == 0 {
				w = 1
			}
			es[i].Weight = uint16(w)
			es[i].Learn = 0
		}
		entries = append(entries, es...)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Key != entries[j].Key {
			return entries[i].Key < entries[j].Key
		}
		if entries[i].Weight != entries[j].Weight {
			return entries[i].Weight > entries[j].Weight
		}
		return entries[i].Move < entries[j].Move
	})
	stats.Entries = len(entries)
	return &Book{entries: entries}, stats, nil
}

// WriteTo serialises the book in Polyglot binary format.
func (bk *Book) WriteTo(w io.Writer) (int64, error) {
	bw := bufio.NewWriter(w)
	var buf [16]byte
	var n int64
	for _, e := range bk.entries {
		binary.BigEndian.PutUint64(buf[0:8], e.Key)
		binary.BigEndian.PutUint16(buf[8:10], e.Move)
		binary.BigEndian.PutUint16(buf[10:12], e.Weight)
		binary.BigEndian.PutUint32(buf[12:16], e.Learn)
		k, err := bw.Write(buf[:])
		n += int64(k)
		if err != nil {
			return n, err
		}
	}
	return n, bw.Flush()
}

// encodePolyglotMove converts an engine move into Polyglot's 16-bit encoding
// (castling is written as king-takes-rook, as Probe expects).
func encodePolyglotMove(b *board.Board, m types.Move) uint16 {
	from, to := m.From(), m.To()
	if m.IsCastle() {
		switch to {
		case types.G1:
			to = types.H1
		case types.C1:
			to = types.A1
		case types.G8:
			to = types.H8
		case types.C8:
			to = types.A8
		}
	}
	var promo uint16
	switch m.PromotionType() {
	case types.Knight:
		promo = 1
	case types.Bishop:
		promo = 2
	case types.Rook:
		promo = 3
	case types.Queen:
		promo = 4
	}
	return uint16(to) | uint16(from)<<6 | promo<<12
}

// ---------------------------------------------------------------------------
// Path reconstruction: depth-first search from the start position with an
// admissible per-side "moves still needed" bound, which makes almost every
// opening position solve with very little branching.

const infDist = 64

var knightDist [64][64]int8

func init() {
	for s := 0; s < 64; s++ {
		for t := range knightDist[s] {
			knightDist[s][t] = -1
		}
		knightDist[s][s] = 0
		q := []int{s}
		for len(q) > 0 {
			c := q[0]
			q = q[1:]
			cf, cr := c%8, c/8
			for _, d := range [8][2]int{{1, 2}, {2, 1}, {2, -1}, {1, -2}, {-1, -2}, {-2, -1}, {-2, 1}, {-1, 2}} {
				f, r := cf+d[0], cr+d[1]
				if f < 0 || f > 7 || r < 0 || r > 7 {
					continue
				}
				n := r*8 + f
				if knightDist[s][n] < 0 {
					knightDist[s][n] = knightDist[s][c] + 1
					q = append(q, n)
				}
			}
		}
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// pieceDist is a lower bound on the moves a piece needs to travel from s to t on
// an empty board (ignoring castling, handled by the caller).
func pieceDist(c types.Color, pt types.PieceType, s, t types.Square) int {
	if s == t {
		return 0
	}
	sf, sr, tf, tr := int(s.File()), int(s.Rank()), int(t.File()), int(t.Rank())
	df, dr := absInt(sf-tf), absInt(sr-tr)
	switch pt {
	case types.Pawn:
		fwd := tr - sr
		start := 1
		if c == types.Black {
			fwd = -fwd
			start = 6
		}
		if fwd <= 0 || fwd < df {
			return infDist
		}
		if sr == start && fwd >= 2 && fwd-2 >= df {
			return fwd - 1
		}
		return fwd
	case types.Knight:
		return int(knightDist[s][t])
	case types.Bishop:
		if (sf+sr)%2 != (tf+tr)%2 {
			return infDist
		}
		if df == dr {
			return 1
		}
		return 2
	case types.Rook:
		if df == 0 || dr == 0 {
			return 1
		}
		return 2
	case types.Queen:
		if df == 0 || dr == 0 || df == dr {
			return 1
		}
		return 2
	case types.King:
		return max(df, dr)
	}
	return infDist
}

type pathSearcher struct {
	target   *board.Board
	targetSq [15][]types.Square
	limit    int
	nodes    int
	failed   map[uint64]struct{}
	path     []types.Move
	totalPly int
	hints    map[uint64][]types.Move // moves known to lie on solved lines (read-only)
	prefer   *Book                   // optional Polyglot book whose moves are tried early
	lists    [64]movegen.MoveList
	order    [64][256]scoredMove
}

type scoredMove struct {
	m     types.Move
	score int
}

func newPathSearcher(limit int, hints map[uint64][]types.Move) *pathSearcher {
	return &pathSearcher{limit: limit, failed: make(map[uint64]struct{}), hints: hints}
}

// sideNeed summarises what one color still has to do to reach the target.
type sideNeed struct {
	moves     int // lower bound on moves to bring its pieces to their target squares
	pawnShift int // minimum file changes its pawns must make (each one is a capture)
	excess    int // its pieces that are absent from the target and so must be captured
}

// assign solves the minimum-cost assignment of targets to distinct sources
// (len(src) <= 16, len(tgt) <= len(src)) with a bitmask DP.
func assign(cost *[16][16]int, nt, ns int, dp []int) int {
	full := 1 << ns
	for m := range dp[:full] {
		dp[m] = infDist
	}
	dp[0] = 0
	best := infDist
	for m := 0; m < full; m++ {
		v := dp[m]
		if v >= infDist {
			continue
		}
		i := popcount(uint(m))
		if i == nt {
			if v < best {
				best = v
			}
			continue
		}
		for j := 0; j < ns; j++ {
			if m&(1<<j) != 0 {
				continue
			}
			if c := v + cost[i][j]; c < dp[m|1<<j] {
				dp[m|1<<j] = c
			}
		}
	}
	return best
}

func popcount(x uint) int {
	n := 0
	for x != 0 {
		x &= x - 1
		n++
	}
	return n
}

// need computes the per-side lower bounds; ok is false if the target is
// unreachable for color c from b.
func (s *pathSearcher) need(b *board.Board, c types.Color) (sideNeed, bool) {
	var res sideNeed
	var cost, shift [16][16]int
	var dp [1 << 10]int
	for pt := types.Pawn; pt <= types.King; pt++ {
		p := types.MakePiece(c, pt)
		tgts := s.targetSq[p]
		var srcs [16]types.Square
		ns := 0
		for bb := uint64(b.PieceBB[p]); bb != 0; bb &= bb - 1 {
			srcs[ns] = types.Square(trailingZeros(bb))
			ns++
		}
		nt := len(tgts)
		if ns < nt || ns > 10 {
			return res, false // would need promotions; not an opening position
		}
		res.excess += ns - nt
		if nt == 0 {
			continue
		}
		for i, t := range tgts {
			for j := 0; j < ns; j++ {
				d := pieceDist(c, pt, srcs[j], t)
				// Castling moves king and rook together in one move.
				if pt == types.King {
					d = min(d, castleKingDist(b, c, srcs[j], t))
				} else if pt == types.Rook {
					d = min(d, castleRookDist(b, c, srcs[j], t))
				}
				cost[i][j] = min(d, infDist)
				if pt == types.Pawn {
					if d >= infDist {
						shift[i][j] = infDist
					} else {
						shift[i][j] = absInt(int(srcs[j].File()) - int(t.File()))
					}
				}
			}
		}
		m := assign(&cost, nt, ns, dp[:])
		if m >= infDist {
			return res, false
		}
		res.moves += m
		if pt == types.Pawn {
			res.pawnShift = assign(&shift, nt, ns, dp[:])
		}
	}
	return res, true
}

// required returns the minimum number of moves each side still needs, or ok=false.
func (s *pathSearcher) required(b *board.Board) (white, black int, ok bool) {
	w, ok1 := s.need(b, types.White)
	k, ok2 := s.need(b, types.Black)
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	// Every enemy piece that disappears costs one capture move, and every pawn
	// file change is a capture of some enemy piece.
	if w.pawnShift > k.excess || k.pawnShift > w.excess {
		return 0, 0, false
	}
	return max(w.moves, k.excess), max(k.moves, w.excess), true
}

func castleKingDist(b *board.Board, c types.Color, s, t types.Square) int {
	d := infDist
	if c == types.White && s == types.E1 {
		if b.Castling&types.CastlingWhiteKing != 0 {
			d = min(d, 1+pieceDist(c, types.King, types.G1, t))
		}
		if b.Castling&types.CastlingWhiteQueen != 0 {
			d = min(d, 1+pieceDist(c, types.King, types.C1, t))
		}
	} else if c == types.Black && s == types.E8 {
		if b.Castling&types.CastlingBlackKing != 0 {
			d = min(d, 1+pieceDist(c, types.King, types.G8, t))
		}
		if b.Castling&types.CastlingBlackQueen != 0 {
			d = min(d, 1+pieceDist(c, types.King, types.C8, t))
		}
	}
	return d
}

func castleRookDist(b *board.Board, c types.Color, s, t types.Square) int {
	// The rook's castling hop is paid for by the king's move, so it is free here.
	switch {
	case c == types.White && s == types.H1 && b.Castling&types.CastlingWhiteKing != 0:
		return pieceDist(c, types.Rook, types.F1, t)
	case c == types.White && s == types.A1 && b.Castling&types.CastlingWhiteQueen != 0:
		return pieceDist(c, types.Rook, types.D1, t)
	case c == types.Black && s == types.H8 && b.Castling&types.CastlingBlackKing != 0:
		return pieceDist(c, types.Rook, types.F8, t)
	case c == types.Black && s == types.A8 && b.Castling&types.CastlingBlackQueen != 0:
		return pieceDist(c, types.Rook, types.D8, t)
	}
	return infDist
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func trailingZeros(x uint64) int {
	n := 0
	for x&1 == 0 {
		x >>= 1
		n++
	}
	return n
}

func (s *pathSearcher) matches(b *board.Board) bool {
	return b.Pieces == s.target.Pieces && b.SideToMove == s.target.SideToMove && b.Castling == s.target.Castling
}

// find returns a legal move sequence from the start position to target. plies
// is the exact path length, or -1 if unknown.
func (s *pathSearcher) find(target *board.Board, plies int) ([]types.Move, bool) {
	b, _ := board.ParseFEN(board.StartFEN)
	return s.findFrom(b, target, plies)
}

// findFrom returns a legal move sequence from b to target. plies is the exact
// path length, or -1 if unknown. b is used as scratch space but is restored
// before returning.
func (s *pathSearcher) findFrom(b *board.Board, target *board.Board, plies int) ([]types.Move, bool) {
	s.target = target
	for i := range s.targetSq {
		s.targetSq[i] = s.targetSq[i][:0]
	}
	for sq := types.Square(0); sq < 64; sq++ {
		if p := target.Pieces[sq]; p != types.PieceNone {
			s.targetSq[p] = append(s.targetSq[p], sq)
		}
	}
	s.nodes = 0
	s.path = s.path[:0]

	minPly, maxPly := plies, plies
	if plies < 0 {
		// Unknown length: start from the shortest the bounds allow and try longer
		// paths with the right parity for the side to move.
		w, k, ok := s.required(b)
		if !ok {
			return nil, false
		}
		if target.SideToMove == types.White {
			minPly = 2 * max(w, k) // equal moves for both sides
		} else {
			minPly = max(2*w-1, 2*k+1) // White has made one move more
		}
		maxPly = minPly + 8
	}
	for ply := minPly; ply <= maxPly && ply < len(s.lists); ply += 2 {
		s.totalPly = ply
		clear(s.failed)
		if s.dfs(b, 0) {
			out := make([]types.Move, len(s.path))
			copy(out, s.path)
			return out, true
		}
		if s.nodes >= s.limit {
			break
		}
	}
	return nil, false
}

// fits reports whether b can still reach the target with remaining plies.
func (s *pathSearcher) fits(b *board.Board, remaining int) (slack int, ok bool) {
	// Castling rights can only be lost, never regained.
	if b.Castling&s.target.Castling != s.target.Castling {
		return 0, false
	}
	w, k, ok := s.required(b)
	if !ok {
		return 0, false
	}
	usLeft, themLeft := (remaining+1)/2, remaining/2
	us, them := w, k
	if b.SideToMove == types.Black {
		us, them = k, w
	}
	if us > usLeft || them > themLeft {
		return 0, false
	}
	return (usLeft - us) + (themLeft - them), true
}

func (s *pathSearcher) dfs(b *board.Board, ply int) bool {
	remaining := s.totalPly - ply
	if remaining == 0 {
		return s.matches(b)
	}
	if s.nodes >= s.limit {
		return false
	}
	s.nodes++

	key := b.Hash ^ uint64(remaining)*0x9E3779B97F4A7C15
	if _, bad := s.failed[key]; bad {
		return false
	}
	if _, ok := s.fits(b, remaining); !ok {
		s.failed[key] = struct{}{}
		return false
	}

	// Order moves: those on already-solved lines first, then moves that bring a
	// piece closer to a target square or capture a piece that must disappear.
	var hinted []types.Move
	if s.hints != nil {
		hinted = s.hints[b.Hash]
	}
	// Then moves the preferred (Polyglot) book plays here, so reconstructed
	// lines follow familiar move orders where they can.
	var preferred []candidateMove
	if s.prefer != nil {
		preferred = s.prefer.polyglotMoves(b)
	}
	ml := &s.lists[ply]
	movegen.GenerateMoves(b, false, ml)
	order := s.order[ply][:0]
	for i := 0; i < ml.Count; i++ {
		m := ml.Moves[i]
		score := s.progress(b, m)
		for _, h := range hinted {
			if h == m {
				score += 1000
				break
			}
		}
		for _, p := range preferred {
			if p.move == m {
				score += 100 + int(p.weight>>10)
				break
			}
		}
		order = append(order, scoredMove{m, score})
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].score > order[j].score })

	for _, sm := range order {
		if !b.MakeMove(sm.m) {
			continue
		}
		s.path = append(s.path, sm.m)
		if s.dfs(b, ply+1) {
			b.UnmakeMove(sm.m)
			return true
		}
		s.path = s.path[:len(s.path)-1]
		b.UnmakeMove(sm.m)
		if s.nodes >= s.limit {
			return false
		}
	}
	s.failed[key] = struct{}{}
	return false
}

// progress is a cheap move-ordering score: how much closer the moved piece gets
// to its nearest target square, plus a bonus for capturing surplus pieces.
func (s *pathSearcher) progress(b *board.Board, m types.Move) int {
	from, to := m.From(), m.To()
	p := b.Pieces[from]
	c, pt := p.Color(), p.Type()
	score := 0
	if tg := s.targetSq[p]; len(tg) > 0 {
		before, after := infDist, infDist
		for _, t := range tg {
			before = min(before, pieceDist(c, pt, from, t))
			after = min(after, pieceDist(c, pt, to, t))
		}
		score += 2 * max(-2, min(2, before-after))
		if after == 0 {
			score++
		}
	}
	if victim := b.Pieces[to]; victim != types.PieceNone {
		if bitboard.Count(b.PieceBB[victim]) > len(s.targetSq[victim]) {
			score += 2
		} else {
			score -= 4
		}
	}
	return score
}

// String renders build statistics for logs.
func (st EPDBuildStats) String() string {
	return fmt.Sprintf("%d positions, %d solved, %d skipped, %d invalid, %d book entries",
		st.Positions, st.Solved, st.Skipped, st.Invalid, st.Entries)
}
