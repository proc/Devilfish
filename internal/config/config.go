package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Threads  int    `json:"threads"`
	Hash     int    `json:"hash"`
	Ponder   bool   `json:"ponder"`
	UseNNUE  bool   `json:"use_nnue"`
	EvalFile string `json:"eval_file"`
}

func DefaultConfig() Config {
	return Config{
		Threads:  10,
		Hash:     2000,
		Ponder:   true,
		UseNNUE:  true,
		EvalFile: "<embedded>",
	}
}

func GetDefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".devilfish", "config.json")
}

type rawConfig struct {
	Threads     *int    `json:"threads"`
	Hash        *int    `json:"hash"`
	Ponder      *bool   `json:"ponder"`
	UseNNUE     *bool   `json:"use_nnue"`
	AltUseNNUE  *bool   `json:"usennue"`
	EvalFile    *string `json:"eval_file"`
	AltEvalFile *string `json:"evalfile"`
}

// Load reads the config file from path (or ~/.devilfish/config.json if path is empty).
// If the file does not exist, it creates the file with default settings and returns it.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		path = GetDefaultConfigPath()
	}

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = Save(path, cfg)
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

	return cfg, nil
}

// Save writes the config to the specified path (or ~/.devilfish/config.json if path is empty).
func Save(path string, cfg Config) error {
	if path == "" {
		path = GetDefaultConfigPath()
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
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
