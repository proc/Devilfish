package main

import (
	"fmt"
	"os"

	"devilfish/internal/uci"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "bench":
			u := uci.New(os.Stdin, os.Stdout)
			depth := int8(7)
			if len(os.Args) > 2 {
				var d int
				fmt.Sscanf(os.Args[2], "%d", &d)
				if d > 0 {
					depth = int8(d)
				}
			}
			u.Bench(depth)
			return
		case "version", "-v", "--version":
			fmt.Printf("%s %s by %s\n", uci.EngineName, uci.EngineVersion, uci.EngineAuthor)
			return
		case "help", "-h", "--help":
			fmt.Printf("%s - A Modern, High-Performance Chess Engine in Go\n\n", uci.EngineName)
			fmt.Println("Usage:")
			fmt.Println("  devilfish          Start UCI loop (standard mode for GUIs like Arena, Cutechess, Banksia)")
			fmt.Println("  devilfish bench    Run benchmark suite")
			fmt.Println("  devilfish --version Show version information")
			return
		}
	}

	uci.Run()
}
