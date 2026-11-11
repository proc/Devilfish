package uci

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"devilfish/internal/config"
	"devilfish/internal/syzygy"
)

func TestAutoPondering(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := &bytes.Buffer{}

	cfg := config.DefaultConfig()
	cfg.Threads = 2
	cfg.Ponder = true
	u := New(in, out, cfg)

	// Send "go depth 3"
	u.cmdGo([]string{"depth", "3"})

	// Give the initial search time to complete and begin auto-pondering
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if u.isAutoPonder.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !u.isAutoPonder.Load() {
		t.Fatalf("expected engine to be auto-pondering after making its move, but it was not. Output:\n%s", out.String())
	}

	// Output should contain "bestmove" and "ponder"
	output := out.String()
	if !strings.Contains(output, "bestmove") || !strings.Contains(output, "ponder") {
		t.Errorf("expected bestmove with ponder in output, got:\n%s", output)
	}

	// Verify cmdStop stops the auto-ponder search
	u.cmdStop()

	if u.searching.Load() || u.isAutoPonder.Load() {
		t.Errorf("expected searching and isAutoPonder to be false after cmdStop")
	}
}

func TestAutoPonderPositionMatch(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := &bytes.Buffer{}

	cfg := config.DefaultConfig()
	cfg.Threads = 2
	cfg.Ponder = true
	u := New(in, out, cfg)

	// Search startpos
	u.cmdGo([]string{"depth", "3"})

	// Wait for auto-ponder to kick in
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if u.isAutoPonder.Load() && u.autoPonderHash.Load() != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	autoHash := u.autoPonderHash.Load()
	if autoHash == 0 {
		t.Fatalf("expected non-zero autoPonderHash")
	}

	// Simulate receiving a different position
	u.cmdPosition([]string{"startpos", "moves", "e2e4", "c7c5"})

	// Should have stopped because c7c5 doesn't match autoPonderHash
	if u.isAutoPonder.Load() {
		t.Errorf("expected auto-ponder to stop on non-matching position")
	}
}

func TestAutoPonderPonderHit(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := &bytes.Buffer{}

	cfg := config.DefaultConfig()
	cfg.Threads = 2
	cfg.Ponder = true
	u := New(in, out, cfg)

	// Search startpos
	u.cmdGo([]string{"depth", "3"})

	// Wait for auto-ponder to kick in
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if u.isAutoPonder.Load() && u.autoPonderHash.Load() != 0 {
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
		OwnBook:  true,
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
	if !strings.Contains(output, "option name SyzygyPath type string") {
		t.Errorf("expected SyzygyPath option in output:\n%s", output)
	}
	if !strings.Contains(output, "option name SyzygyProbeDepth type spin default 1") {
		t.Errorf("expected SyzygyProbeDepth option in output:\n%s", output)
	}
	if !strings.Contains(output, "option name Syzygy50MoveRule type check default true") {
		t.Errorf("expected Syzygy50MoveRule option in output:\n%s", output)
	}
	if !strings.Contains(output, "option name SyzygyProbeLimit type spin default 6") {
		t.Errorf("expected SyzygyProbeLimit option in output:\n%s", output)
	}
	if !strings.Contains(output, "option name Move Overhead type spin default 20") {
		t.Errorf("expected Move Overhead option in output:\n%s", output)
	}
	if !strings.Contains(output, "option name MultiPV type spin default 1 min 1 max 256") {
		t.Errorf("expected MultiPV option in output:\n%s", output)
	}
	if !strings.Contains(output, "option name OwnBook type check default true") {
		t.Errorf("expected OwnBook option in output:\n%s", output)
	}
	if !strings.Contains(output, "option name BookFile type string default <embedded>") {
		t.Errorf("expected BookFile option in output:\n%s", output)
	}
}

func TestSyzygySetOptions(t *testing.T) {
	out := &bytes.Buffer{}
	cfg := config.DefaultConfig()
	u := New(bytes.NewBuffer(nil), out, cfg)

	u.cmdSetOption([]string{"name", "SyzygyProbeDepth", "value", "3"})
	u.cmdSetOption([]string{"name", "Syzygy50MoveRule", "value", "false"})
	u.cmdSetOption([]string{"name", "SyzygyProbeLimit", "value", "5"})
	u.cmdSetOption([]string{"name", "SyzygyPath", "value", "/tmp/nonexistent/tb"})
	u.cmdSetOption([]string{"name", "Move", "Overhead", "value", "35"})
	if u.moveOverhead != 35 {
		t.Errorf("expected moveOverhead 35, got %d", u.moveOverhead)
	}
	u.cmdSetOption([]string{"name", "MultiPV", "value", "4"})
	if u.engine.MultiPV != 4 {
		t.Errorf("expected MultiPV 4, got %d", u.engine.MultiPV)
	}
	u.cmdSetOption([]string{"name", "OwnBook", "value", "false"})
	if u.engine.UseBook {
		t.Errorf("expected UseBook to be false")
	}
	u.cmdSetOption([]string{"name", "OwnBook", "value", "true"})
	if !u.engine.UseBook {
		t.Errorf("expected UseBook to be true")
	}
	u.cmdSetOption([]string{"name", "BookFile", "value", "<embedded>"})
	if u.bookFile != "<embedded>" {
		t.Errorf("expected BookFile to be <embedded>, got %s", u.bookFile)
	}
}

func TestAutoPonderDisabledByDefault(t *testing.T) {
	in := bytes.NewBuffer(nil)
	out := &bytes.Buffer{}

	cfg := config.DefaultConfig()
	cfg.Threads = 2
	u := New(in, out, cfg)

	if u.ponderEnabled {
		t.Errorf("expected ponderEnabled to be false by default")
	}

	// Search startpos
	u.cmdGo([]string{"depth", "3"})

	// Wait briefly to see if auto-ponder starts (it shouldn't)
	time.Sleep(200 * time.Millisecond)

	if u.isAutoPonder.Load() {
		t.Errorf("expected auto-ponder NOT to run when disabled by default")
	}
}

func TestDisableBookUCI(t *testing.T) {
	testCmds := [][]string{
		{"name", "Book", "value", "false"},
		{"name", "Book", "value", "off"},
		{"name", "Book", "value", "none"},
		{"name", "Opening", "Book", "value", "false"},
		{"name", "OpeningBook", "value", "false"},
		{"name", "UseBook", "value", "false"},
		{"name", "OwnBook", "value", "false"},
		{"name", "BookFile", "value", "none"},
		{"name", "BookFile", "value", "<empty>"},
	}

	for _, cmd := range testCmds {
		out := &bytes.Buffer{}
		cfg := config.DefaultConfig()
		cfg.OwnBook = true
		u := New(bytes.NewBuffer(nil), out, cfg)

		if !u.engine.UseBook {
			t.Fatal("expected engine to start with book enabled")
		}

		u.cmdSetOption(cmd)
		if u.engine.UseBook {
			t.Errorf("cmd %v failed to disable opening book", cmd)
		}
	}
}

func TestOpeningInfoMessage(t *testing.T) {
	out := &bytes.Buffer{}
	cfg := config.DefaultConfig()
	cfg.Threads = 2
	u := New(bytes.NewBuffer(nil), out, cfg)

	u.cmdPosition([]string{"startpos", "moves", "e2e4", "c7c5"})
	u.cmdGo([]string{"depth", "1"})

	time.Sleep(100 * time.Millisecond)
	u.cmdStop()

	output := out.String()
	if !strings.Contains(output, "info string Opening: B20: Sicilian Defense") {
		t.Errorf("expected opening info message in output, got:\n%s", output)
	}
}

func TestSyzygyTablebaseLogAndInfoMessage(t *testing.T) {
	out := &bytes.Buffer{}
	cfg := config.DefaultConfig()
	cfg.Threads = 1
	cfg.SyzygyPath = "../../syzygy"
	u := New(bytes.NewBuffer(nil), out, cfg)

	if !syzygy.IsAvailable() {
		t.Skip("Syzygy tablebases not available, skipping test")
	}

	u.cmdPosition([]string{"fen", "8/8/8/8/8/5k2/8/4K2R", "w", "-", "-", "0", "1"})
	u.cmdGo([]string{"depth", "5"})

	time.Sleep(100 * time.Millisecond)
	u.cmdStop()

	output := out.String()
	if !strings.Contains(output, "info string Syzygy tablebase determined best position:") {
		t.Errorf("expected Syzygy info message in output, got:\n%s", output)
	}
}
