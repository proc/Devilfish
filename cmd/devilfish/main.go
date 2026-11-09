package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"devilfish/internal/config"
	"devilfish/internal/nnue"
	"devilfish/internal/uci"

	"net/http"
	_ "net/http/pprof" // Import for profiling, but not used directly
)

func printHelp() {
	fmt.Printf("%s - A Modern, High-Performance Chess Engine in Go\n\n", uci.EngineName)
	fmt.Println("Usage:")
	fmt.Println("  devilfish [flags]           Start UCI loop (standard mode for GUIs like Arena, Cutechess, Banksia)")
	fmt.Println("  devilfish bench [depth]     Run benchmark suite")
	fmt.Println("  devilfish --version         Show version information")
	fmt.Println("  devilfish --help            Show this help message")
	fmt.Println("\nConfiguration Flags:")
	fmt.Println("  -t, --threads <N>           Number of Lazy SMP search threads (overrides config file)")
	fmt.Println("  -hash <MB>, --hash <MB>     Transposition Table size in Megabytes")
	fmt.Println("  -c, --config <path>         Path to JSON config file (default: ~/.devilfish/config.json)")
	fmt.Println("  --eval-file <path>          Path to custom NNUE network file (default: embedded)")
	fmt.Println("  --no-nnue                   Disable NNUE evaluation (use classical HCE)")
	fmt.Println("  --syzygy-path <path>        Path to directory containing Syzygy tablebase files (.rtbw / .rtbz)")
	fmt.Println("\nConfiguration File:")
	fmt.Printf("  If present, configuration is automatically loaded from:\n    %s\n", config.GetDefaultConfigPath())
	fmt.Println("  Example format: {\"threads\": 10, \"hash\": 2000, \"ponder\": true, \"use_nnue\": true, \"eval_file\": \"<embedded>\", \"syzygy_path\": \"/path/to/syzygy\"}")
}

func main() {
	var customConfigPath string
	var evalFilePath string
	var syzygyPath string
	var cliThreads int
	var cliHash int
	var cliNoNNUE bool
	var isBench bool
	var benchDepth int8 = 7

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "-h" || arg == "--help" || arg == "help" {
			printHelp()
			return
		}

		if arg == "-v" || arg == "--version" || arg == "version" {
			fmt.Printf("%s %s by %s\n", uci.EngineName, uci.EngineVersion, uci.EngineAuthor)
			return
		}

		if arg == "bench" {
			isBench = true
			if i+1 < len(args) {
				d, err := strconv.Atoi(args[i+1])
				if err == nil && d > 0 {
					benchDepth = int8(d)
					i++
				}
			}
			continue
		}

		// Handle --threads / -threads / -t
		if arg == "-t" || arg == "-threads" || arg == "--threads" {
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err == nil && n > 0 {
					cliThreads = n
					i++
				}
			}
			continue
		}
		if strings.HasPrefix(arg, "--threads=") || strings.HasPrefix(arg, "-threads=") || strings.HasPrefix(arg, "-t=") {
			parts := strings.SplitN(arg, "=", 2)
			n, err := strconv.Atoi(parts[1])
			if err == nil && n > 0 {
				cliThreads = n
			}
			continue
		}

		// Handle --hash / -hash
		if arg == "-hash" || arg == "--hash" {
			if i+1 < len(args) {
				h, err := strconv.Atoi(args[i+1])
				if err == nil && h > 0 {
					cliHash = h
					i++
				}
			}
			continue
		}
		if strings.HasPrefix(arg, "--hash=") || strings.HasPrefix(arg, "-hash=") {
			parts := strings.SplitN(arg, "=", 2)
			h, err := strconv.Atoi(parts[1])
			if err == nil && h > 0 {
				cliHash = h
			}
			continue
		}

		// Handle --config / -config / -c
		if arg == "-c" || arg == "-config" || arg == "--config" {
			if i+1 < len(args) {
				customConfigPath = args[i+1]
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "-c=") {
			parts := strings.SplitN(arg, "=", 2)
			customConfigPath = parts[1]
			continue
		}

		// Handle --eval-file
		if arg == "-eval-file" || arg == "--eval-file" {
			if i+1 < len(args) {
				evalFilePath = args[i+1]
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--eval-file=") || strings.HasPrefix(arg, "-eval-file=") {
			parts := strings.SplitN(arg, "=", 2)
			evalFilePath = parts[1]
			continue
		}

		// Handle --no-nnue
		if arg == "--no-nnue" || arg == "-no-nnue" {
			cliNoNNUE = true
			continue
		}

		// Handle --syzygy-path
		if arg == "-syzygy-path" || arg == "--syzygy-path" {
			if i+1 < len(args) {
				syzygyPath = args[i+1]
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--syzygy-path=") || strings.HasPrefix(arg, "-syzygy-path=") {
			parts := strings.SplitN(arg, "=", 2)
			syzygyPath = parts[1]
			continue
		}
	}

	// 1. Load configuration from file (custom path or ~/.devilfish/config.json)
	cfg, err := config.Load(customConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse config file: %v\n", err)
	}

	// 2. Command line arguments override configuration file
	if cliThreads > 0 {
		cfg.Threads = cliThreads
	}
	if cliHash > 0 {
		cfg.Hash = cliHash
	}
	if evalFilePath != "" {
		cfg.EvalFile = evalFilePath
	}
	if syzygyPath != "" {
		cfg.SyzygyPath = syzygyPath
	}
	if cliNoNNUE {
		cfg.UseNNUE = false
	}

	// Apply NNUE settings from config (and CLI overrides)
	nnue.Enabled = cfg.UseNNUE
	if cfg.EvalFile != "" && cfg.EvalFile != "<embedded>" {
		if err := nnue.LoadFile(cfg.EvalFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	// 3. Run benchmark or UCI loop
	if isBench {
		u := uci.New(os.Stdin, os.Stdout, cfg)
		u.Bench(benchDepth)
		return
	}
	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	uci.Run(cfg)
}
