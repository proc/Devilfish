package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Threads int `json:"threads"`
	Hash    int `json:"hash,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Threads: 1,
		Hash:    64,
	}
}

func GetDefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".devilfish", "config.json")
}

// Load reads the config file from path (or ~/.devilfish/config.json if path is empty).
// If the file does not exist, it returns the default config without error.
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
			return cfg, nil
		}
		return cfg, err
	}

	var fileCfg Config
	if err := json.Unmarshal(data, &fileCfg); err != nil {
		return cfg, err
	}

	if fileCfg.Threads > 0 {
		cfg.Threads = fileCfg.Threads
	}
	if fileCfg.Hash > 0 {
		cfg.Hash = fileCfg.Hash
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

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
