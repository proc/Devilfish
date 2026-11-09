package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Threads != 10 {
		t.Errorf("expected default threads 10, got %d", cfg.Threads)
	}
	if cfg.Hash != 64 {
		t.Errorf("expected default hash 64, got %d", cfg.Hash)
	}
	if !cfg.Ponder {
		t.Errorf("expected default ponder true, got %t", cfg.Ponder)
	}
	if !cfg.UseNNUE {
		t.Errorf("expected default use_nnue true, got %t", cfg.UseNNUE)
	}
	if cfg.EvalFile != "<embedded>" {
		t.Errorf("expected default eval_file <embedded>, got %s", cfg.EvalFile)
	}
}

func TestLoadNonExistentCreatesFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devilfish_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "sub", "config.json")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.Threads != 10 || cfg.Hash != 64 || !cfg.Ponder || !cfg.UseNNUE || cfg.EvalFile != "<embedded>" {
		t.Errorf("expected default config, got %+v", cfg)
	}

	// Verify file was created on disk with correct defaults
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("expected config file to be created at %s, but it was not", configPath)
	}

	// Read and verify saved file content
	reloaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to reload created config: %v", err)
	}
	if reloaded.Threads != 10 || reloaded.Hash != 64 || !reloaded.Ponder || !reloaded.UseNNUE || reloaded.EvalFile != "<embedded>" {
		t.Errorf("expected reloaded config matching default, got %+v", reloaded)
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
		Threads:  8,
		Hash:     256,
		Ponder:   false,
		UseNNUE:  false,
		EvalFile: "/path/to/custom.nnue",
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
	if loadedCfg.Ponder != origCfg.Ponder {
		t.Errorf("expected ponder %t, got %t", origCfg.Ponder, loadedCfg.Ponder)
	}
	if loadedCfg.UseNNUE != origCfg.UseNNUE {
		t.Errorf("expected use_nnue %t, got %t", origCfg.UseNNUE, loadedCfg.UseNNUE)
	}
	if loadedCfg.EvalFile != origCfg.EvalFile {
		t.Errorf("expected eval_file %s, got %s", origCfg.EvalFile, loadedCfg.EvalFile)
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
