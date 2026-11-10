package syzygy

import (
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/types"
)

func TestSyzygyLifecycle(t *testing.T) {
	// Initial state
	if IsAvailable() {
		t.Errorf("expected Syzygy to not be available initially")
	}

	// Init with empty string
	Init("")
	if MaxPieces() != 0 {
		t.Errorf("expected 0 max pieces for empty path")
	}

	// Init with non-existent directory
	Init("/non/existent/path")
	if MaxPieces() != 0 {
		t.Errorf("expected 0 max pieces for non-existent path")
	}

	// Probe on uninitialized tablebases should return false, not crash
	b, err := board.ParseFEN("8/8/8/8/8/4k3/8/4K2R w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}

	wdl, ok := ProbeWDL(b)
	if ok {
		t.Errorf("expected ProbeWDL to return false, got ok=true wdl=%d", wdl)
	}

	m, wdlRoot, dtz, okRoot := ProbeRoot(b)
	if okRoot || m != types.MoveNone {
		t.Errorf("expected ProbeRoot to return false, got ok=%v move=%s wdl=%d dtz=%d", okRoot, m, wdlRoot, dtz)
	}

	Free()
	if IsAvailable() {
		t.Errorf("expected Syzygy to not be available after Free()")
	}
}

func TestNormalizePath(t *testing.T) {
	norm := NormalizePath("syzygy")
	if norm == "" {
		t.Errorf("expected non-empty normalized path for syzygy")
	}

	if NormalizePath("") != "" {
		t.Errorf("expected empty string for empty path")
	}
}

func TestProbeRootKRK(t *testing.T) {
	Init("../../syzygy")
	defer Free()
	if !IsAvailable() {
		t.Skip("Syzygy tables not available locally")
	}

	b, err := board.ParseFEN("k7/8/1K6/8/8/8/8/1R6 w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}

	m, wdl, dtz, ok := ProbeRoot(b)
	t.Logf("ProbeRoot result: m=%s, wdl=%d, dtz=%d, ok=%t", m, wdl, dtz, ok)
	if !ok || m == types.MoveNone || wdl != Win {
		t.Errorf("expected winning probe root for KRK, got m=%s wdl=%d ok=%t", m, wdl, ok)
	}
}

func TestProbeRootEliminateThreatByCapture(t *testing.T) {
	Init("../../syzygy")
	defer Free()
	if !IsAvailable() {
		t.Skip("Syzygy tables not available locally")
	}

	// King in check by undefended Bishop on e2: must capture e1e2 instead of fleeing
	b, err := board.ParseFEN("8/8/8/8/8/8/4b3/4K2k w - - 0 1")
	if err != nil {
		t.Fatal(err)
	}

	m, wdl, _, ok := ProbeRoot(b)
	if !ok || m.String() != "e1e2" {
		t.Errorf("expected e1e2 capturing checker, got %s (ok=%t, wdl=%d)", m, ok, wdl)
	}
}
