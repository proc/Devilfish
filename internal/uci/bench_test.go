package uci

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"devilfish/internal/config"
)

func TestBenchInts(t *testing.T) {
	if got := benchInts([]string{"250", "x"}, 1000, 8); !reflect.DeepEqual(got, []int{250, 8}) {
		t.Errorf("got %v", got)
	}
	if got := benchInts(nil, 5); !reflect.DeepEqual(got, []int{5}) {
		t.Errorf("got %v", got)
	}
	if got := benchInts([]string{"-3", "0"}, 7, 9); !reflect.DeepEqual(got, []int{7, 9}) {
		t.Errorf("non-positive values should fall back: %v", got)
	}
}

func TestBenchThreadCounts(t *testing.T) {
	if got := benchThreadCounts(4); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Errorf("got %v", got)
	}
	big := benchThreadCounts(96)
	if big[0] != 1 || big[len(big)-1] != 96 || len(big) > 16 {
		t.Errorf("got %v", big)
	}
	for i := 1; i < len(big); i++ {
		if big[i] <= big[i-1] {
			t.Errorf("not increasing: %v", big)
		}
	}
}

// Each mode runs, prints its result, and the thread benchmark restores the
// configured thread count.
func TestBenchModes(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Threads = 1
	cfg.Hash = 16
	cfg.OwnBook = false
	out := &bytes.Buffer{}
	u := New(bytes.NewBuffer(nil), out, cfg)

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"3"}, "Nodes/sec"},
		{[]string{"depth", "3"}, "Running benchmark at depth 3"},
		{[]string{"time", "20"}, "Average depth"},
		{[]string{"threads", "20", "2"}, "Recommended:"},
		{[]string{"perft", "2"}, "Total nodes"},
		{[]string{"eval", "20"}, "evals/sec"},
		{[]string{"help"}, "bench threads"},
	} {
		out.Reset()
		u.RunBench(c.args)
		if !strings.Contains(out.String(), c.want) {
			t.Errorf("bench %v: output lacks %q:\n%s", c.args, c.want, out.String())
		}
	}
	if u.engine.NumThreads != 1 {
		t.Errorf("thread benchmark left %d threads set", u.engine.NumThreads)
	}
}
