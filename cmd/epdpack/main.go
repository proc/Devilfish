// Command epdpack converts a text EPD opening suite into the compact packed form
// (.epdb.gz) that the engine embeds, e.g.
//
//	go run ./cmd/epdpack -in Pohl.epd -out internal/book/pohl.epdb.gz
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"os"

	"devilfish/internal/book"
)

func main() {
	in := flag.String("in", "", "input EPD file (.epd or .epd.gz)")
	out := flag.String("out", "", "output packed book (.epdb.gz)")
	flag.Parse()
	if *in == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	bk, err := book.LoadFile(in)
	if err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	gz, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err := bk.WritePacked(gz); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fi, _ := os.Stat(out)
	fmt.Printf("%s -> %s: %d positions, %d bytes\n", in, out, bk.EntryCount(), fi.Size())
	return nil
}
