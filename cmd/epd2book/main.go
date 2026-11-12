// Command epd2book converts an EPD opening suite (e.g. Pohl.epd) into a
// Polyglot opening book by reconstructing a legal move path to every position.
//
//	go run ./cmd/epd2book -in Pohl.epd -out internal/book/pohl.bin.gz
//
// An output name ending in .gz is gzip-compressed (the form the engine embeds).
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"devilfish/internal/book"
)

func main() {
	in := flag.String("in", "Pohl.epd", "input EPD file")
	out := flag.String("out", "internal/book/pohl.bin.gz", "output Polyglot book (.bin or .bin.gz)")
	nodes := flag.Int("nodes", 2_000_000, "search node limit per position")
	workers := flag.Int("workers", 0, "parallel workers (0 = all CPUs)")
	flag.Parse()

	f, err := os.Open(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	start := time.Now()
	bk, stats, err := book.BuildFromEPD(f, book.EPDBuildOptions{
		NodeLimit: *nodes,
		Workers:   *workers,
		Progress: func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r%d/%d positions (%s)", done, total, time.Since(start).Round(time.Second))
		},
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
	if err := o.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s -> %s: %s in %s\n", *in, *out, stats, time.Since(start).Round(time.Millisecond))
}
