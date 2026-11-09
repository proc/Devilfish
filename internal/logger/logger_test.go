package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "devilfish_logger_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "sub", "engine.log")

	if IsEnabled() {
		t.Errorf("expected logger not to be enabled before init")
	}

	// Logging before init should not crash
	Infof("Test info before init")

	if err := Init(logFile); err != nil {
		t.Fatalf("failed to init logger: %v", err)
	}

	if !IsEnabled() {
		t.Errorf("expected logger to be enabled after init")
	}

	if Path() != logFile {
		t.Errorf("expected path %s, got %s", logFile, Path())
	}

	Infof("Engine started: %s", "Devilfish")
	Profilef("Search profiling: nodes=%d nps=%d", 12345, 67890)
	LogUCIIn("position startpos")
	LogUCIOut("bestmove e2e4")

	if err := Close(); err != nil {
		t.Fatalf("failed to close logger: %v", err)
	}

	if IsEnabled() {
		t.Errorf("expected logger not to be enabled after close")
	}

	// Verify file content
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	content := string(data)
	expectedStrings := []string{
		"SESSION START",
		"[INFO] Engine started: Devilfish",
		"[PROFILE] Search profiling: nodes=12345 nps=67890",
		"[UCI-IN] <- position startpos",
		"[UCI-OUT] -> bestmove e2e4",
		"SESSION END",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(content, s) {
			t.Errorf("expected log to contain %q, but got:\n%s", s, content)
		}
	}
}
