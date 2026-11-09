package uci

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"devilfish/internal/config"
)

func TestAutoPondering(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := &bytes.Buffer{}

	cfg := config.DefaultConfig()
	cfg.Threads = 2
	u := New(in, out, cfg)

	// Send "go depth 3"
	u.cmdGo([]string{"depth", "3"})

	// Give the initial search time to complete and begin auto-pondering
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		u.searchMu.Lock()
		isAuto := u.isAutoPonder
		u.searchMu.Unlock()
		if isAuto {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	u.searchMu.Lock()
	if !u.isAutoPonder {
		u.searchMu.Unlock()
		t.Fatalf("expected engine to be auto-pondering after making its move, but it was not. Output:\n%s", out.String())
	}
	u.searchMu.Unlock()

	// Output should contain "bestmove" and "ponder"
	output := out.String()
	if !strings.Contains(output, "bestmove") || !strings.Contains(output, "ponder") {
		t.Errorf("expected bestmove with ponder in output, got:\n%s", output)
	}

	// Verify cmdStop stops the auto-ponder search
	u.cmdStop()

	u.searchMu.Lock()
	if u.searching || u.isAutoPonder {
		u.searchMu.Unlock()
		t.Errorf("expected searching and isAutoPonder to be false after cmdStop")
	} else {
		u.searchMu.Unlock()
	}
}

func TestAutoPonderPositionMatch(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := &bytes.Buffer{}

	cfg := config.DefaultConfig()
	cfg.Threads = 2
	u := New(in, out, cfg)

	// Search startpos
	u.cmdGo([]string{"depth", "3"})

	// Wait for auto-ponder to kick in
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		u.searchMu.Lock()
		isAuto := u.isAutoPonder
		u.searchMu.Unlock()
		if isAuto {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	u.searchMu.Lock()
	autoHash := u.autoPonderHash
	u.searchMu.Unlock()

	if autoHash == 0 {
		t.Fatalf("expected non-zero autoPonderHash")
	}

	// Simulate receiving a different position
	u.cmdPosition([]string{"startpos", "moves", "e2e4", "c7c5"})

	// Should have stopped because c7c5 doesn't match autoPonderHash
	u.searchMu.Lock()
	if u.isAutoPonder {
		u.searchMu.Unlock()
		t.Errorf("expected auto-ponder to stop on non-matching position")
	} else {
		u.searchMu.Unlock()
	}
}

func TestAutoPonderPonderHit(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := &bytes.Buffer{}

	cfg := config.DefaultConfig()
	cfg.Threads = 2
	u := New(in, out, cfg)

	// Search startpos
	u.cmdGo([]string{"depth", "3"})

	// Wait for auto-ponder to kick in
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		u.searchMu.Lock()
		isAuto := u.isAutoPonder
		u.searchMu.Unlock()
		if isAuto {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Send ponderhit
	u.cmdPonderHit()

	// Wait a moment and then stop
	time.Sleep(50 * time.Millisecond)
	u.cmdStop()

	// Output should have had at least one bestmove
	output := out.String()
	if !strings.Contains(output, "bestmove") {
		t.Errorf("expected bestmove output, got:\n%s", output)
	}
}

func TestConfigUCIOptions(t *testing.T) {
	out := &bytes.Buffer{}
	cfg := config.Config{
		Threads:  4,
		Hash:     128,
		Ponder:   false,
		UseNNUE:  false,
		EvalFile: "my_custom.nnue",
	}
	u := New(bytes.NewBuffer(nil), out, cfg)
	u.cmdUCI()

	output := out.String()
	if !strings.Contains(output, "option name Hash type spin default 128") {
		t.Errorf("expected Hash default 128 in output:\n%s", output)
	}
	if !strings.Contains(output, "option name Threads type spin default 4") {
		t.Errorf("expected Threads default 4 in output:\n%s", output)
	}
	if !strings.Contains(output, "option name Ponder type check default false") {
		t.Errorf("expected Ponder default false in output:\n%s", output)
	}
	if !strings.Contains(output, "option name EvalFile type string default my_custom.nnue") {
		t.Errorf("expected EvalFile default my_custom.nnue in output:\n%s", output)
	}
}
