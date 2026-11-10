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
	if cfg.Hash != 128 {
		t.Errorf("expected default hash 128, got %d", cfg.Hash)
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
	if cfg.Threads != 10 || cfg.Hash != 128 || !cfg.Ponder || !cfg.UseNNUE || cfg.EvalFile != "<embedded>" {
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
	if reloaded.Threads != 10 || reloaded.Hash != 128 || !reloaded.Ponder || !reloaded.UseNNUE || reloaded.EvalFile != "<embedded>" {
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
		Threads:          8,
		Hash:             256,
		Ponder:           false,
		UseNNUE:          false,
		EvalFile:         "/path/to/custom.nnue",
		SyzygyPath:       "/path/to/syzygy",
		SyzygyProbeDepth: 3,
		Syzygy50MoveRule: false,
		SyzygyProbeLimit: 5,
		LogFile:          "/path/to/engine.log",
		MoveOverhead:     30,
		MultiPV:          4,
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
	if loadedCfg.SyzygyPath != origCfg.SyzygyPath {
		t.Errorf("expected syzygy_path %s, got %s", origCfg.SyzygyPath, loadedCfg.SyzygyPath)
	}
	if loadedCfg.SyzygyProbeDepth != origCfg.SyzygyProbeDepth {
		t.Errorf("expected syzygy_probe_depth %d, got %d", origCfg.SyzygyProbeDepth, loadedCfg.SyzygyProbeDepth)
	}
	if loadedCfg.Syzygy50MoveRule != origCfg.Syzygy50MoveRule {
		t.Errorf("expected syzygy_50_move_rule %t, got %t", origCfg.Syzygy50MoveRule, loadedCfg.Syzygy50MoveRule)
	}
	if loadedCfg.SyzygyProbeLimit != origCfg.SyzygyProbeLimit {
		t.Errorf("expected syzygy_probe_limit %d, got %d", origCfg.SyzygyProbeLimit, loadedCfg.SyzygyProbeLimit)
	}
	if loadedCfg.LogFile != origCfg.LogFile {
		t.Errorf("expected log_file %s, got %s", origCfg.LogFile, loadedCfg.LogFile)
	}
	if loadedCfg.MoveOverhead != origCfg.MoveOverhead {
		t.Errorf("expected move_overhead %d, got %d", origCfg.MoveOverhead, loadedCfg.MoveOverhead)
	}
	if loadedCfg.MultiPV != origCfg.MultiPV {
		t.Errorf("expected multipv %d, got %d", origCfg.MultiPV, loadedCfg.MultiPV)
	}
}

func TestSyzygyPathFlexibleFormats(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devilfish_syzygy_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test 1: string with "syzygy_path"
	p1 := filepath.Join(tmpDir, "cfg1.json")
	if err := os.WriteFile(p1, []byte(`{"syzygy_path": "/single/path"}`), 0644); err != nil {
		t.Fatal(err)
	}
	c1, err := Load(p1)
	if err != nil || c1.SyzygyPath != "/single/path" {
		t.Errorf("failed string syzygy_path: %+v, err: %v", c1, err)
	}

	// Test 2: array of strings with "SyzygyPath"
	p2 := filepath.Join(tmpDir, "cfg2.json")
	if err := os.WriteFile(p2, []byte(`{"SyzygyPath": ["/path1", "/path2"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p2)
	if err != nil || c2.SyzygyPath != "/path1:/path2" {
		t.Errorf("failed array SyzygyPath: %+v, err: %v", c2, err)
	}

	// Test 3: "syzygy" key
	p3 := filepath.Join(tmpDir, "cfg3.json")
	if err := os.WriteFile(p3, []byte(`{"syzygy": "syzygy/tb"}`), 0644); err != nil {
		t.Fatal(err)
	}
	c3, err := Load(p3)
	if err != nil || c3.SyzygyPath != "syzygy/tb" {
		t.Errorf("failed syzygy key: %+v, err: %v", c3, err)
	}
}

func TestLogFileInConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devilfish_log_config_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test 1: log_file
	p1 := filepath.Join(tmpDir, "cfg1.json")
	if err := os.WriteFile(p1, []byte(`{"log_file": "/var/log/devilfish.log"}`), 0644); err != nil {
		t.Fatal(err)
	}
	c1, err := Load(p1)
	if err != nil || c1.LogFile != "/var/log/devilfish.log" {
		t.Errorf("failed log_file: %+v, err: %v", c1, err)
	}

	// Test 2: log_path
	p2 := filepath.Join(tmpDir, "cfg2.json")
	if err := os.WriteFile(p2, []byte(`{"log_path": "custom/path.log"}`), 0644); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p2)
	if err != nil || c2.LogFile != "custom/path.log" {
		t.Errorf("failed log_path: %+v, err: %v", c2, err)
	}

	// Test 3: logfile
	p3 := filepath.Join(tmpDir, "cfg3.json")
	if err := os.WriteFile(p3, []byte(`{"logfile": "third.log"}`), 0644); err != nil {
		t.Fatal(err)
	}
	c3, err := Load(p3)
	if err != nil || c3.LogFile != "third.log" {
		t.Errorf("failed logfile: %+v, err: %v", c3, err)
	}
}

func TestMultiPVInConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devilfish_multipv_config_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Test 1: standard "multipv"
	p1 := filepath.Join(tmpDir, "cfg1.json")
	if err := os.WriteFile(p1, []byte(`{"multipv": 3}`), 0644); err != nil {
		t.Fatal(err)
	}
	c1, err := Load(p1)
	if err != nil || c1.MultiPV != 3 {
		t.Errorf("failed multipv key: %+v, err: %v", c1, err)
	}

	// Test 2: casing "MultiPV"
	p2 := filepath.Join(tmpDir, "cfg2.json")
	if err := os.WriteFile(p2, []byte(`{"MultiPV": 4}`), 0644); err != nil {
		t.Fatal(err)
	}
	c2, err := Load(p2)
	if err != nil || c2.MultiPV != 4 {
		t.Errorf("failed MultiPV key: %+v, err: %v", c2, err)
	}

	// Test 3: snake_case "multi_pv"
	p3 := filepath.Join(tmpDir, "cfg3.json")
	if err := os.WriteFile(p3, []byte(`{"multi_pv": 5}`), 0644); err != nil {
		t.Fatal(err)
	}
	c3, err := Load(p3)
	if err != nil || c3.MultiPV != 5 {
		t.Errorf("failed multi_pv key: %+v, err: %v", c3, err)
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
