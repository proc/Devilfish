package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Threads          int    `json:"threads"`
	Hash             int    `json:"hash"`
	Ponder           bool   `json:"ponder"`
	UseNNUE          bool   `json:"use_nnue"`
	EvalFile         string `json:"eval_file"`
	SyzygyPath       string `json:"syzygy_path"`
	SyzygyProbeDepth int    `json:"syzygy_probe_depth"`
	Syzygy50MoveRule bool   `json:"syzygy_50_move_rule"`
	SyzygyProbeLimit int    `json:"syzygy_probe_limit"`
	LogFile          string `json:"log_file"`
	MoveOverhead     int    `json:"move_overhead"`
	MultiPV          int    `json:"multipv"`
	OwnBook          bool   `json:"own_book"`
	BookFile         string `json:"book_file"`
}

func DefaultConfig() Config {
	return Config{
		Threads:          10,
		Hash:             128,
		Ponder:           false,
		UseNNUE:          true,
		EvalFile:         "<embedded>",
		SyzygyPath:       "",
		SyzygyProbeDepth: 1,
		Syzygy50MoveRule: true,
		SyzygyProbeLimit: 6,
		LogFile:          "",
		MoveOverhead:     20,
		MultiPV:          1,
		OwnBook:          true,
		BookFile:         "<embedded>",
	}
}

func GetDefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".devilfish", "config.json")
}

// FindConfigFile resolves the config file path:
// 1. If explicitPath is non-empty, use it directly.
// 2. If ./config.json exists in the current working directory, use it.
// 3. If config.json exists in the executable's directory, use it.
// 4. Otherwise, use ~/.devilfish/config.json.
func FindConfigFile(explicitPath string) string {
	if explicitPath != "" {
		return explicitPath
	}

	// Check current working directory
	if fi, err := os.Stat("config.json"); err == nil && !fi.IsDir() {
		return "config.json"
	}

	// Check executable directory
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidate := filepath.Join(exeDir, "config.json")
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}

	return GetDefaultConfigPath()
}

type rawConfig struct {
	Threads          *int        `json:"threads"`
	Hash             *int        `json:"hash"`
	Ponder           *bool       `json:"ponder"`
	UseNNUE          *bool       `json:"use_nnue"`
	AltUseNNUE       *bool       `json:"usennue"`
	EvalFile         *string     `json:"eval_file"`
	AltEvalFile      *string     `json:"evalfile"`
	SyzygyPath       interface{} `json:"syzygy_path"`
	AltSyzygyPath    interface{} `json:"syzygypath"`
	SyzygyProbeDepth *int        `json:"syzygy_probe_depth"`
	Syzygy50MoveRule *bool       `json:"syzygy_50_move_rule"`
	SyzygyProbeLimit *int        `json:"syzygy_probe_limit"`
	LogFile          *string     `json:"log_file"`
	AltLogFile       *string     `json:"logfile"`
	LogPath          *string     `json:"log_path"`
	AltLogPath       *string     `json:"logpath"`
	MoveOverhead     *int        `json:"move_overhead"`
	AltMoveOverhead  *int        `json:"moveoverhead"`
	MultiPV          *int        `json:"multipv"`
	AltMultiPV       *int        `json:"multi_pv"`
	OwnBook          *bool       `json:"own_book"`
	AltOwnBook       *bool       `json:"ownbook"`
	UseBook          *bool       `json:"use_book"`
	AltUseBook       *bool       `json:"usebook"`
	OpeningBook      *bool       `json:"opening_book"`
	AltOpeningBook   *bool       `json:"openingbook"`
	BookVal          interface{} `json:"book"`
	BookFile         *string     `json:"book_file"`
	AltBookFile      *string     `json:"bookfile"`
	BookPath         *string     `json:"book_path"`
}

func parseSyzygyPath(val interface{}) string {
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		var parts []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return strings.Join(parts, ":")
	case []string:
		return strings.Join(v, ":")
	default:
		return ""
	}
}

// Load reads the config file from path (or resolves via FindConfigFile if path is empty).
// If the file does not exist, it creates the file with default settings and returns it.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	resolvedPath := FindConfigFile(path)
	if resolvedPath == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			_ = Save(resolvedPath, cfg)
			return cfg, nil
		}
		return cfg, err
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, err
	}

	if raw.Threads != nil && *raw.Threads > 0 {
		cfg.Threads = *raw.Threads
	}
	if raw.Hash != nil && *raw.Hash > 0 {
		cfg.Hash = *raw.Hash
	}
	if raw.Ponder != nil {
		cfg.Ponder = *raw.Ponder
	}
	if raw.UseNNUE != nil {
		cfg.UseNNUE = *raw.UseNNUE
	} else if raw.AltUseNNUE != nil {
		cfg.UseNNUE = *raw.AltUseNNUE
	}
	if raw.EvalFile != nil {
		cfg.EvalFile = *raw.EvalFile
	} else if raw.AltEvalFile != nil {
		cfg.EvalFile = *raw.AltEvalFile
	}

	// Handle SyzygyPath with various casing/keys and types (string or array)
	if p := parseSyzygyPath(raw.SyzygyPath); p != "" {
		cfg.SyzygyPath = p
	} else if p := parseSyzygyPath(raw.AltSyzygyPath); p != "" {
		cfg.SyzygyPath = p
	}

	// Also check flexible map to accept any casing like syzygyPath, SyzygyPath, tb_path, tablebases, etc.
	var genericMap map[string]interface{}
	if err := json.Unmarshal(data, &genericMap); err == nil {
		for k, v := range genericMap {
			normKey := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(k, "_", ""), "-", ""))
			switch normKey {
			case "syzygypath", "syzygy", "tbpath", "tablebases", "tablebasepath":
				if cfg.SyzygyPath == "" {
					cfg.SyzygyPath = parseSyzygyPath(v)
				}
			case "syzygyprobedepth":
				if n, ok := v.(float64); ok && int(n) >= 1 {
					cfg.SyzygyProbeDepth = int(n)
				}
			case "syzygy50moverule":
				if b, ok := v.(bool); ok {
					cfg.Syzygy50MoveRule = b
				}
			case "syzygyprobelimit":
				if n, ok := v.(float64); ok && int(n) >= 0 {
					cfg.SyzygyProbeLimit = int(n)
				}
			case "logfile", "log", "logpath", "logfilepath", "logfilelocation":
				if s, ok := v.(string); ok && cfg.LogFile == "" {
					cfg.LogFile = strings.TrimSpace(s)
				}
			case "moveoverhead", "overhead":
				if n, ok := v.(float64); ok && int(n) >= 0 {
					cfg.MoveOverhead = int(n)
				}
			case "multipv", "multi_pv":
				if n, ok := v.(float64); ok && int(n) >= 1 {
					cfg.MultiPV = int(n)
				}
			case "ownbook", "usebook", "openingbook":
				if b, ok := v.(bool); ok {
					cfg.OwnBook = b
				} else if s, ok := v.(string); ok {
					sLower := strings.ToLower(strings.TrimSpace(s))
					if sLower == "false" || sLower == "0" || sLower == "off" || sLower == "none" || sLower == "<empty>" {
						cfg.OwnBook = false
					} else if sLower == "true" || sLower == "1" || sLower == "on" {
						cfg.OwnBook = true
					}
				}
			case "book":
				if b, ok := v.(bool); ok {
					cfg.OwnBook = b
				} else if s, ok := v.(string); ok {
					sTrim := strings.TrimSpace(s)
					sLower := strings.ToLower(sTrim)
					if sLower == "false" || sLower == "0" || sLower == "off" || sLower == "none" || sLower == "<empty>" {
						cfg.OwnBook = false
					} else if sLower == "true" || sLower == "1" || sLower == "on" {
						cfg.OwnBook = true
					} else if sTrim != "" {
						cfg.BookFile = sTrim
					}
				}
			case "bookfile", "bookpath":
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					sTrim := strings.TrimSpace(s)
					sLower := strings.ToLower(sTrim)
					if sLower == "none" || sLower == "<empty>" || sLower == "off" || sLower == "false" {
						cfg.OwnBook = false
					} else {
						cfg.BookFile = sTrim
					}
				}
			}
		}
	}

	if raw.OwnBook != nil {
		cfg.OwnBook = *raw.OwnBook
	} else if raw.AltOwnBook != nil {
		cfg.OwnBook = *raw.AltOwnBook
	} else if raw.UseBook != nil {
		cfg.OwnBook = *raw.UseBook
	} else if raw.AltUseBook != nil {
		cfg.OwnBook = *raw.AltUseBook
	} else if raw.OpeningBook != nil {
		cfg.OwnBook = *raw.OpeningBook
	} else if raw.AltOpeningBook != nil {
		cfg.OwnBook = *raw.AltOpeningBook
	}

	if raw.BookVal != nil {
		switch v := raw.BookVal.(type) {
		case bool:
			cfg.OwnBook = v
		case string:
			sTrim := strings.TrimSpace(v)
			sLower := strings.ToLower(sTrim)
			if sLower == "false" || sLower == "0" || sLower == "off" || sLower == "none" || sLower == "<empty>" {
				cfg.OwnBook = false
			} else if sLower == "true" || sLower == "1" || sLower == "on" {
				cfg.OwnBook = true
			} else if sTrim != "" {
				cfg.BookFile = sTrim
			}
		}
	}

	if raw.BookFile != nil && *raw.BookFile != "" {
		sTrim := strings.TrimSpace(*raw.BookFile)
		sLower := strings.ToLower(sTrim)
		if sLower == "none" || sLower == "<empty>" || sLower == "off" || sLower == "false" {
			cfg.OwnBook = false
		} else {
			cfg.BookFile = sTrim
		}
	} else if raw.AltBookFile != nil && *raw.AltBookFile != "" {
		sTrim := strings.TrimSpace(*raw.AltBookFile)
		sLower := strings.ToLower(sTrim)
		if sLower == "none" || sLower == "<empty>" || sLower == "off" || sLower == "false" {
			cfg.OwnBook = false
		} else {
			cfg.BookFile = sTrim
		}
	} else if raw.BookPath != nil && *raw.BookPath != "" {
		sTrim := strings.TrimSpace(*raw.BookPath)
		sLower := strings.ToLower(sTrim)
		if sLower == "none" || sLower == "<empty>" || sLower == "off" || sLower == "false" {
			cfg.OwnBook = false
		} else {
			cfg.BookFile = sTrim
		}
	}

	if raw.LogFile != nil && *raw.LogFile != "" {
		cfg.LogFile = strings.TrimSpace(*raw.LogFile)
	} else if raw.AltLogFile != nil && *raw.AltLogFile != "" {
		cfg.LogFile = strings.TrimSpace(*raw.AltLogFile)
	} else if raw.LogPath != nil && *raw.LogPath != "" {
		cfg.LogFile = strings.TrimSpace(*raw.LogPath)
	} else if raw.AltLogPath != nil && *raw.AltLogPath != "" {
		cfg.LogFile = strings.TrimSpace(*raw.AltLogPath)
	}

	if raw.MoveOverhead != nil && *raw.MoveOverhead >= 0 {
		cfg.MoveOverhead = *raw.MoveOverhead
	} else if raw.AltMoveOverhead != nil && *raw.AltMoveOverhead >= 0 {
		cfg.MoveOverhead = *raw.AltMoveOverhead
	}

	if raw.MultiPV != nil && *raw.MultiPV >= 1 {
		cfg.MultiPV = *raw.MultiPV
	} else if raw.AltMultiPV != nil && *raw.AltMultiPV >= 1 {
		cfg.MultiPV = *raw.AltMultiPV
	}

	if raw.SyzygyProbeDepth != nil && *raw.SyzygyProbeDepth >= 1 {
		cfg.SyzygyProbeDepth = *raw.SyzygyProbeDepth
	}
	if raw.Syzygy50MoveRule != nil {
		cfg.Syzygy50MoveRule = *raw.Syzygy50MoveRule
	}
	if raw.SyzygyProbeLimit != nil && *raw.SyzygyProbeLimit >= 0 {
		cfg.SyzygyProbeLimit = *raw.SyzygyProbeLimit
	}

	return cfg, nil
}

// Save writes the config to the specified path (or ~/.devilfish/config.json if path is empty).
func Save(path string, cfg Config) error {
	if path == "" {
		path = GetDefaultConfigPath()
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return err
	}

	return os.WriteFile(path, buf.Bytes(), 0644)
}
