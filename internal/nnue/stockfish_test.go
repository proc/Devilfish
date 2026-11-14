package nnue

import (
	"bufio"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"devilfish/internal/board"
	"devilfish/internal/movegen"
)

// loadSFTestNet returns the Stockfish network built into the binary.
func loadSFTestNet(t testing.TB) *EightNetwork {
	net, err := EmbeddedSF()
	if err != nil {
		t.Fatalf("loading the embedded Stockfish network: %v", err)
	}
	return net
}

// The network's outputs equal those Stockfish itself reports for the same
// file ("eval": NNUE evaluation, side to move, internal units).
func TestSFMatchesStockfish(t *testing.T) {
	net := loadSFTestNet(t)
	f, err := os.Open("testdata/sf_evals.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	checked := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want, fen, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("bad line %q", line)
		}
		wantV, err := strconv.Atoi(want)
		if err != nil {
			t.Fatal(err)
		}
		b, err := board.ParseFEN(fen)
		if err != nil {
			t.Fatalf("%s: %v", fen, err)
		}
		out, ok := SFEvaluateRaw(net, b)
		if !ok {
			t.Fatalf("%s: not evaluated", fen)
		}
		if got := int(out); got != wantV {
			t.Errorf("%s: got %d, Stockfish %d", fen, got, wantV)
		}
		checked++
	}
	if checked < 50 {
		t.Fatalf("only %d positions checked", checked)
	}
}

// Along random games with takebacks, as a search makes them, the incremental
// evaluation equals an evaluation from nothing.
func TestSFIncrementalMatchesScratch(t *testing.T) {
	net := loadSFTestNet(t)
	rng := rand.New(rand.NewSource(5))
	st := NewEightState(64)
	checked := 0
	for game := 0; game < 40; game++ {
		b, _ := board.ParseFEN(board.StartFEN)
		st.Reset()
		ply := 0
		for step := 0; step < 200 && ply < 60; step++ {
			legal := movegen.GenerateLegalMoves(b)
			if len(legal) == 0 || b.IsFiftyMoveDraw() {
				break
			}
			// Sometimes step back a ply or two and take another branch.
			if ply > 2 && rng.Intn(5) == 0 {
				for back := 1 + rng.Intn(2); back > 0; back-- {
					b.UnmakeMove(b.UndoStack[len(b.UndoStack)-1].Move)
					ply--
				}
				continue
			}
			parent := b.Hash
			if !b.MakeMove(legal[rng.Intn(len(legal))]) {
				continue
			}
			ply++
			// Evaluate only some positions, so that updates span several moves.
			if rng.Intn(3) != 0 {
				got := st.Evaluate(net, b, ply, parent, true)
				if want := EvaluateEight(net, b); got != want {
					t.Fatalf("game %d step %d (%s): incremental %d, from scratch %d", game, step, b.ToFEN(), got, want)
				}
				checked++
			}
		}
	}
	if checked < 1000 {
		t.Fatalf("only %d positions checked", checked)
	}
	t.Logf("checked %d positions; %.1f rows per accumulator set, %d king-bucket refreshes, %d threat rebuilds",
		checked, float64(st.RowOps)/float64(st.Evals), st.FinnyRefreshes, st.ThreatRebuilds)
}

func BenchmarkSFIncremental(b *testing.B) {
	net := loadSFTestNet(b)
	pos, _ := board.ParseFEN(eightTestFENs[5])
	moves := movegen.GenerateLegalMoves(pos)
	st := NewEightState(8)
	st.Evaluate(net, pos, 0, 0, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := moves[i%len(moves)]
		parent := pos.Hash
		pos.MakeMove(m)
		st.Evaluate(net, pos, 1, parent, true)
		pos.UnmakeMove(m)
	}
}

// sparse32 (this architecture's kernel) agrees with its definition.
func TestSparse32MatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	const inputs = 1024
	wt := make([]int8, inputs*32)
	for i := range wt {
		wt[i] = int8(rng.Intn(256) - 128)
	}
	for trial := 0; trial < 200; trial++ {
		var in [inputs]uint8
		var idx [inputs]uint16
		n := 0
		density := []float64{0, 0.01, 0.1, 0.5, 1}[trial%5]
		for i := range in {
			if rng.Float64() < density {
				in[i] = uint8(1 + rng.Intn(255))
				idx[n] = uint16(i)
				n++
			}
		}
		var got, want [32]int32
		got[0], want[0] = 77, -77 // both must overwrite
		sparse32(&idx[0], n, &in[0], &wt[0], &got)
		refSparse32(&idx[0], n, &in[0], &wt[0], &want)
		if got != want {
			t.Fatalf("trial %d (%d inputs): got %v want %v", trial, n, got, want)
		}
	}
}

// A file that is not a supported Stockfish network is refused with a reason.
func TestSFLoadRejectsBadFiles(t *testing.T) {
	if _, err := LoadSFBytes([]byte{1, 2, 3}); err == nil {
		t.Error("truncated file accepted")
	}
	old := []byte{0x20, 0x2f, 0xf3, 0x7a, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := LoadSFBytes(old); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("older format: %v", err)
	}
	data := embeddedSFNNUE
	if _, err := LoadSFBytes(data[:len(data)-5]); err == nil {
		t.Error("file cut short accepted")
	}
	if _, err := LoadSFBytes(append(data[:len(data):len(data)], 0)); err == nil {
		t.Error("file with trailing bytes accepted")
	}
}

// The small dense-layer kernels agree with their definitions over the whole
// range of sums, including values that saturate.
func TestSFDenseKernelsMatchReference(t *testing.T) {
	rng := rand.New(rand.NewSource(21))
	edge := []int32{0, 1, -1, 63, 64, 127, 128, 16255, 16256, 16383, 16384, 32767, 32768, -32768, -32769, 1 << 20, -(1 << 20), 1<<31 - 1, -1 << 31}
	for trial := 0; trial < 2000; trial++ {
		var x, bias [32]int32
		for i := range x {
			switch rng.Intn(4) {
			case 0:
				x[i] = edge[rng.Intn(len(edge))]
			case 1:
				x[i] = int32(rng.Intn(40000) - 20000)
			case 2:
				x[i] = int32(rng.Intn(2000) - 1000)
			default:
				x[i] = int32(rng.Uint32()) >> uint(rng.Intn(16))
			}
			if rng.Intn(3) == 0 {
				bias[i] = int32(rng.Intn(20000) - 10000)
			}
			// The network's sums stay far inside int32; keep x+bias from wrapping.
			if s := int64(x[i]) + int64(bias[i]); s > 1<<31-1 || s < -1<<31 {
				bias[i] = 0
			}
		}
		for _, bits := range []uint{7, 6} {
			var got, want [64]uint8
			sfActivate(&x, &bias, &got, bits)
			refSFActivate(&x, &bias, &want, bits)
			if got != want {
				t.Fatalf("trial %d bits %d:\n x    %v\n bias %v\n got  %v\n want %v", trial, bits, x, bias, got, want)
			}
		}

		var a [128]uint8
		var w [128]int8
		for i := range a {
			a[i] = uint8(rng.Intn(128))
			w[i] = int8(rng.Intn(256) - 128)
		}
		if got, want := sfDot128(&a, &w), refDot128(&a, &w); got != want {
			t.Fatalf("trial %d: dot %d, want %d", trial, got, want)
		}

		var in [64]uint8
		for i := range in {
			if rng.Intn(3) == 0 {
				in[i] = uint8(1 + rng.Intn(255))
			}
		}
		var idx [72]uint16
		n, k := nonzero64(&in, &idx), 0
		for i, v := range in {
			if v != 0 {
				if k >= n || int(idx[k]) != i {
					t.Fatalf("trial %d: nonzero64 entry %d", trial, k)
				}
				k++
			}
		}
		if k != n {
			t.Fatalf("trial %d: nonzero64 count %d, want %d", trial, n, k)
		}
	}
}

// EvalFile "<stockfish>" selects the embedded network, and "<embedded>"
// (ResetToDefault) goes back to the built-in one.
func TestLoadFileStockfishName(t *testing.T) {
	savedEight, savedNet, savedPath, savedEnabled := ActiveEightNetwork, ActiveNetwork, CurrentFilePath, Enabled
	defer func() {
		ActiveEightNetwork, ActiveNetwork, CurrentFilePath, Enabled = savedEight, savedNet, savedPath, savedEnabled
	}()
	for _, name := range []string{"<stockfish>", " <Stockfish> "} {
		if err := LoadFile(name); err != nil {
			t.Fatalf("LoadFile(%q): %v", name, err)
		}
		if !ActiveEightNetwork.IsSF() || !strings.Contains(CurrentFilePath, EmbeddedSFName) {
			t.Fatalf("LoadFile(%q): active network %q is not the Stockfish one", name, CurrentFilePath)
		}
	}
	first := ActiveEightNetwork
	if err := LoadFile(StockfishName); err != nil || ActiveEightNetwork != first {
		t.Error("selecting the embedded network again should reuse it")
	}
	b, _ := board.ParseFEN(board.StartFEN)
	if v := EvaluateEight(ActiveEightNetwork, b); v < 1 || v > 60 {
		t.Errorf("start position evaluates to %d cp", v)
	}
	ResetToDefault()
	if ActiveEightNetwork.IsSF() {
		t.Error("ResetToDefault left the Stockfish network active")
	}
}
