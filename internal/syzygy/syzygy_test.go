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
