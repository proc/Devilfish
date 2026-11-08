package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Threads != 1 {
		t.Errorf("expected default threads 1, got %d", cfg.Threads)
	}
	if cfg.Hash != 64 {
		t.Errorf("expected default hash 64, got %d", cfg.Hash)
	}
}

func TestLoadNonExistent(t *testing.T) {
	cfg, err := Load("/path/to/non/existent/config.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.Threads != 1 || cfg.Hash != 64 {
		t.Errorf("expected default config, got %+v", cfg)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devilfish_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")
	origCfg := Config{
		Threads: 8,
		Hash:    256,
	}

	if err := Save(configPath, origCfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loadedCfg.Threads != origCfg.Threads {
		t.Errorf("expected threads %d, got %d", origCfg.Threads, loadedCfg.Threads)
	}
	if loadedCfg.Hash != origCfg.Hash {
		t.Errorf("expected hash %d, got %d", origCfg.Hash, loadedCfg.Hash)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path := GetDefaultConfigPath()
	if path == "" {
		t.Error("expected non-empty default config path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %s", path)
	}
	if filepath.Base(path) != "config.json" {
		t.Errorf("expected filename config.json, got %s", filepath.Base(path))
	}
}
