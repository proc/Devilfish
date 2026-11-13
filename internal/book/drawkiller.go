package book

import (
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Stefan Pohl's Drawkiller V5 openings (www.sp-cc.de), as the Polyglot books
// shipped with the suite: the "classic 4 moves, big" set of each of its seven
// folders. A Drawkiller line shuffles the kings to opposite corners and the
// queens off a shared file, then adds four pawn moves; the folders differ in
// how large an advantage the lines leave White, from [+0.90;+0.99] to
// [+1.50;+1.59]. The books were built from each folder's wider "big" interval
// (0.29 pawns rather than 0.09).
//
// Both sides have to follow the book for a game to reach a Drawkiller position:
// the lines begin 1. e3 d6 or 1. d3 e6 and no ordinary opening leads into them.

//go:embed drawkiller/*.bin.gz
var drawkillerFS embed.FS

// DrawkillerBookName selects the recommended Drawkiller set, [+0.90;+0.99].
// The others are named by the lower bound of their folder, e.g.
// "<drawkiller_1.20>".
const DrawkillerBookName = "<drawkiller>"

// DrawkillerLevels lists the embedded sets by the lower bound of their folder's
// evaluation interval.
var DrawkillerLevels = []string{"0.90", "1.00", "1.10", "1.20", "1.30", "1.40", "1.50"}

type drawkillerSet struct {
	once sync.Once
	book *Book
	err  error
}

var drawkillerSets = func() map[string]*drawkillerSet {
	m := make(map[string]*drawkillerSet, len(DrawkillerLevels))
	for _, l := range DrawkillerLevels {
		m[l] = &drawkillerSet{}
	}
	return m
}()

// drawkillerLevel returns the set a book_file value names ("" if none). Names
// match case-insensitively, with or without angle brackets: "drawkiller" and
// "dk" for the first set, and "drawkiller_1.20", "dk-1.2", "drawkiller+1.20"
// and the like for a specific one.
func drawkillerLevel(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, "<") && strings.HasSuffix(n, ">") {
		n = n[1 : len(n)-1]
	}
	rest, ok := strings.CutPrefix(n, "drawkiller")
	if !ok {
		if rest, ok = strings.CutPrefix(n, "dk"); !ok {
			return ""
		}
	}
	rest = strings.TrimLeft(rest, "_-+ ")
	rest = strings.TrimPrefix(rest, "v5")
	rest = strings.TrimLeft(rest, "_-+ ")
	if rest == "" {
		return DrawkillerLevels[0]
	}
	if len(rest) == 3 { // "1.2" -> "1.20"
		rest += "0"
	}
	if _, ok := drawkillerSets[rest]; ok {
		return rest
	}
	return ""
}

// DrawkillerBook returns the embedded Drawkiller V5 book of the given level
// (one of DrawkillerLevels), decoding it on first use.
func DrawkillerBook(level string) (*Book, error) {
	s, ok := drawkillerSets[level]
	if !ok {
		return nil, fmt.Errorf("no Drawkiller book %q (have %s)", level, strings.Join(DrawkillerLevels, ", "))
	}
	s.once.Do(func() {
		raw, err := drawkillerFS.ReadFile("drawkiller/dk_" + level + ".bin.gz")
		if err != nil {
			s.err = err
			return
		}
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			s.err = err
			return
		}
		data, err := io.ReadAll(gz)
		if err != nil {
			s.err = err
			return
		}
		s.book, s.err = Read(data)
	})
	return s.book, s.err
}
