// Package testsuite loads EPD test suites (positions with "bm" best moves and
// "am" avoid moves) and scores engine answers against them. The Win At Chess
// suite is embedded for measuring tactical strength.
package testsuite

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
	"devilfish/internal/types"
)

// wacEPD is "Win At Chess" (Fred Reinfeld's 300 tactical positions) in the
// 2018 revision distributed with the Arasan engine (tests/wacnew.epd), which
// fixes broken positions and adds alternative solutions.
//
//go:embed wac.epd
var wacEPD string

// Position is one test position.
type Position struct {
	ID    string
	FEN   string
	Board *board.Board
	Best  []types.Move // "bm": playing any of these solves the position
	Avoid []types.Move // "am": playing any of these fails it
	// The operands as written in the suite, for reports.
	BestSAN, AvoidSAN []string
}

// Solved reports whether m answers the position.
func (p *Position) Solved(m types.Move) bool {
	for _, a := range p.Avoid {
		if a == m {
			return false
		}
	}
	if len(p.Best) == 0 {
		return len(p.Avoid) > 0
	}
	for _, b := range p.Best {
		if b == m {
			return true
		}
	}
	return false
}

// Expected describes the answer, e.g. "bm Qg6" or "am Bxh7".
func (p *Position) Expected() string {
	var parts []string
	if len(p.BestSAN) > 0 {
		parts = append(parts, "bm "+strings.Join(p.BestSAN, " "))
	}
	if len(p.AvoidSAN) > 0 {
		parts = append(parts, "am "+strings.Join(p.AvoidSAN, " "))
	}
	return strings.Join(parts, "; ")
}

// Load returns a suite by name: "wac" for the embedded Win At Chess suite, or a
// path to an EPD file.
func Load(name string) ([]Position, error) {
	if strings.EqualFold(name, "wac") || strings.EqualFold(name, "<wac>") {
		return Parse(strings.NewReader(wacEPD))
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads EPD lines. Every line must have a legal position and at least
// one resolvable bm or am move.
func Parse(r io.Reader) ([]Position, error) {
	var out []Position
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if p.ID == "" {
			p.ID = fmt.Sprintf("#%d", len(out)+1)
		}
		out = append(out, p)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no positions found")
	}
	return out, nil
}

func parseLine(line string) (Position, error) {
	var p Position
	f := strings.Fields(line)
	if len(f) < 4 {
		return p, fmt.Errorf("short EPD line")
	}
	// EPD has 4 position fields; FEN move counters may follow before the ops.
	rest := strings.Join(f[4:], " ")
	fen := strings.Join(f[:4], " ")
	if len(f) >= 6 && isNumber(f[4]) && isNumber(f[5]) {
		fen += " " + f[4] + " " + f[5]
		rest = strings.Join(f[6:], " ")
	} else {
		fen += " 0 1"
	}
	b, err := board.ParseFEN(fen)
	if err != nil {
		return p, err
	}
	p.FEN, p.Board = fen, b

	for _, op := range splitOps(rest) {
		op = strings.TrimSpace(op)
		if op == "" {
			continue
		}
		name, arg, _ := strings.Cut(op, " ")
		arg = strings.TrimSpace(arg)
		switch name {
		case "bm", "am":
			for _, san := range strings.Fields(arg) {
				m, err := ParseSAN(b, san)
				if err != nil {
					return p, fmt.Errorf("%s %s: %w", name, san, err)
				}
				if name == "bm" {
					p.Best, p.BestSAN = append(p.Best, m), append(p.BestSAN, san)
				} else {
					p.Avoid, p.AvoidSAN = append(p.Avoid, m), append(p.AvoidSAN, san)
				}
			}
		case "id":
			p.ID = strings.Trim(arg, `"`)
		}
	}
	if len(p.Best) == 0 && len(p.Avoid) == 0 {
		return p, fmt.Errorf("no bm or am operation")
	}
	return p, nil
}

// splitOps splits EPD operations on semicolons outside quoted strings.
func splitOps(s string) []string {
	var ops []string
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case ';':
			if !inQuote {
				ops = append(ops, s[start:i])
				start = i + 1
			}
		}
	}
	return append(ops, s[start:])
}

func isNumber(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// ParseSAN resolves a move in standard algebraic notation (also accepting
// UCI coordinates, 0-0 castling, and promotions written without '=').
func ParseSAN(b *board.Board, s string) (types.Move, error) {
	want := normalizeSAN(s)
	legal := movegen.GenerateLegalMoves(b)
	for _, m := range legal {
		if normalizeSAN(SAN(b, m)) == want || m.String() == strings.ToLower(s) {
			return m, nil
		}
	}
	return types.MoveNone, fmt.Errorf("no legal move matches")
}

func normalizeSAN(s string) string {
	s = strings.TrimRight(s, "+#!?")
	s = strings.ReplaceAll(s, "=", "")
	return strings.ReplaceAll(s, "0", "O")
}

// SAN formats a legal move in standard algebraic notation, without check marks.
func SAN(b *board.Board, m types.Move) string {
	if m.IsCastle() {
		if m.To().File() > m.From().File() {
			return "O-O"
		}
		return "O-O-O"
	}
	from, to := m.From(), m.To()
	pt := b.PieceAt(from).Type()
	capture := m.IsCapture() || m.IsEnPassant() || b.PieceAt(to) != types.PieceNone

	var sb strings.Builder
	if pt == types.Pawn {
		if capture {
			sb.WriteByte(byte('a' + from.File()))
		}
	} else {
		sb.WriteByte(" PNBRQK"[pt])
		// Disambiguate between same-type pieces that can reach the square.
		sameFile, sameRank, others := false, false, false
		for _, o := range movegen.GenerateLegalMoves(b) {
			if o == m || o.To() != to || o.From() == from || b.PieceAt(o.From()).Type() != pt {
				continue
			}
			others = true
			sameFile = sameFile || o.From().File() == from.File()
			sameRank = sameRank || o.From().Rank() == from.Rank()
		}
		if others {
			switch {
			case !sameFile:
				sb.WriteByte(byte('a' + from.File()))
			case !sameRank:
				sb.WriteByte(byte('1' + from.Rank()))
			default:
				sb.WriteString(from.String())
			}
		}
	}
	if capture {
		sb.WriteByte('x')
	}
	sb.WriteString(to.String())
	if m.IsPromotion() {
		sb.WriteByte('=')
		sb.WriteByte(" PNBRQK"[m.PromotionType()])
	}
	return sb.String()
}
