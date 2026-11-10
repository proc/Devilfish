package book

import (
	_ "embed"
)

//go:embed book.bin
var defaultBookData []byte

var defaultBook *Book

// DefaultBook returns the builtin compiled-in opening book.
func DefaultBook() *Book {
	if defaultBook == nil {
		b, err := Read(defaultBookData)
		if err == nil {
			defaultBook = b
		}
	}
	return defaultBook
}
