package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"devilfish/internal/config"
	"devilfish/internal/logger"
	"devilfish/internal/nnue"
	"devilfish/internal/syzygy"
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
	fmt.Println("  -c, --config <path>         Path to JSON config file (default: ./config.json or ~/.devilfish/config.json)")
	fmt.Println("  -l, --log <path>            Path to log file for engine internal details, profiling, and Syzygy status")
	fmt.Println("  --multipv <N>               Number of principal variations to calculate (default: 1)")
	fmt.Println("  --eval-file <path>          Path to custom NNUE network file (default: embedded)")
	fmt.Println("  --no-nnue                   Disable NNUE evaluation (use classical HCE)")
	fmt.Println("  --book <path>               Path to Polyglot opening book (.bin) file (default: embedded)")
	fmt.Println("  --no-book                   Disable opening book")
	fmt.Println("  --syzygy-path <path>        Path to directory containing Syzygy tablebase files (.rtbw / .rtbz)")
	fmt.Println("\nConfiguration File:")
	fmt.Println("  If present, configuration is automatically loaded from:")
	fmt.Println("    1. ./config.json (current working directory)")
	fmt.Printf("    2. %s (user home)\n", config.GetDefaultConfigPath())
	fmt.Println("  Example format: {\"threads\": 10, \"hash\": 128, \"multipv\": 1, \"own_book\": true, \"book_file\": \"<embedded>\", \"ponder\": true, \"use_nnue\": true, \"eval_file\": \"<embedded>\", \"syzygy_path\": \"syzygy\"}")
}

func main() {
	var customConfigPath string
	var evalFilePath string
	var syzygyPath string
	var syzygyPathSet bool
	var cliLogPath string
	var cliThreads int
	var cliHash int
	var cliMultiPV int
	var cliNoNNUE bool
	var cliBookFile string
	var cliNoBook bool
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

		// Handle --book / -book
		if arg == "-book" || arg == "--book" {
			if i+1 < len(args) {
				cliBookFile = args[i+1]
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--book=") || strings.HasPrefix(arg, "-book=") {
			parts := strings.SplitN(arg, "=", 2)
			cliBookFile = parts[1]
			continue
		}

		// Handle --no-book
		if arg == "--no-book" || arg == "-no-book" {
			cliNoBook = true
			continue
		}

		// Handle --log / -log / -l
		if arg == "-l" || arg == "-log" || arg == "--log" {
			if i+1 < len(args) {
				cliLogPath = args[i+1]
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--log=") || strings.HasPrefix(arg, "-log=") || strings.HasPrefix(arg, "-l=") {
			parts := strings.SplitN(arg, "=", 2)
			cliLogPath = parts[1]
			continue
		}

		// Handle --syzygy-path
		if arg == "-syzygy-path" || arg == "--syzygy-path" {
			if i+1 < len(args) {
				syzygyPath = args[i+1]
				syzygyPathSet = true
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--syzygy-path=") || strings.HasPrefix(arg, "-syzygy-path=") {
			parts := strings.SplitN(arg, "=", 2)
			syzygyPath = parts[1]
			syzygyPathSet = true
			continue
		}

		// Handle --multipv / -multipv
		if arg == "-multipv" || arg == "--multipv" {
			if i+1 < len(args) {
				m, err := strconv.Atoi(args[i+1])
				if err == nil && m > 0 {
					cliMultiPV = m
					i++
				}
			}
			continue
		}
		if strings.HasPrefix(arg, "--multipv=") || strings.HasPrefix(arg, "-multipv=") {
			parts := strings.SplitN(arg, "=", 2)
			m, err := strconv.Atoi(parts[1])
			if err == nil && m > 0 {
				cliMultiPV = m
			}
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
	if cliMultiPV > 0 {
		cfg.MultiPV = cliMultiPV
	}
	if evalFilePath != "" {
		cfg.EvalFile = evalFilePath
	}
	if syzygyPathSet {
		cfg.SyzygyPath = syzygyPath
	}
	if cliNoNNUE {
		cfg.UseNNUE = false
	}
	if cliBookFile != "" {
		cfg.BookFile = cliBookFile
	}
	if cliNoBook {
		cfg.OwnBook = false
	}
	if cliLogPath != "" {
		cfg.LogFile = cliLogPath
	}

	// 3. Initialize Logger if requested via CLI (--log) or config
	if cfg.LogFile != "" {
		if err := logger.Init(cfg.LogFile); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize log file %s: %v\n", cfg.LogFile, err)
		} else {
			defer logger.Close()
			logger.Infof("Engine: %s %s by %s", uci.EngineName, uci.EngineVersion, uci.EngineAuthor)
			logger.Infof("System: OS=%s Arch=%s CPUs=%d Go=%s PID=%d", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.Version(), os.Getpid())
			logger.Infof("Configuration: Threads=%d, Hash=%dMB, MultiPV=%d, Ponder=%t, UseNNUE=%t, EvalFile=%s, OwnBook=%t, BookFile=%s", cfg.Threads, cfg.Hash, cfg.MultiPV, cfg.Ponder, cfg.UseNNUE, cfg.EvalFile, cfg.OwnBook, cfg.BookFile)
			logger.Infof("Syzygy Settings: ConfiguredPath=%q, ProbeDepth=%d, 50MoveRule=%t, ProbeLimit=%d", cfg.SyzygyPath, cfg.SyzygyProbeDepth, cfg.Syzygy50MoveRule, cfg.SyzygyProbeLimit)
		}
	}

	// 4. Initialize Syzygy tablebases and log startup status
	if cfg.SyzygyPath != "" {
		syzygy.Init(cfg.SyzygyPath)
		syzygy.ProbeDepth = cfg.SyzygyProbeDepth
		syzygy.Use50MoveRule = cfg.Syzygy50MoveRule
		syzygy.ProbeLimit = cfg.SyzygyProbeLimit

		if syzygy.IsAvailable() {
			logger.Infof("Syzygy Startup Status: LOADED successfully (%d-piece tablebases available)", syzygy.MaxPieces())
		} else {
			logger.Warnf("Syzygy Startup Status: FAILED to load tablebases from path %q", cfg.SyzygyPath)
		}
	} else {
		logger.Infof("Syzygy Startup Status: NOT LOADED on startup (no syzygy path configured)")
	}

	// Apply NNUE settings from config (and CLI overrides)
	nnue.Enabled = cfg.UseNNUE
	if cfg.EvalFile != "" && cfg.EvalFile != "<embedded>" {
		if err := nnue.LoadFile(cfg.EvalFile); err != nil {
			logger.Warnf("NNUE Load Failed: file %q: %v", cfg.EvalFile, err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	netInfo := nnue.GetNetworkInfo()
	statusStr := "ENABLED"
	if !netInfo.Enabled {
		statusStr = "DISABLED"
	}
	logger.Infof("NNUE Startup Status: %s | Network=%s | Arch=%s | SIMD=%s | Params=%d (%.1f KB)",
		statusStr, netInfo.FilePath, netInfo.Architecture, netInfo.SIMD, netInfo.NumParameters, float64(netInfo.SizeBytes)/1024.0)

	// 5. Run benchmark or UCI loop
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
