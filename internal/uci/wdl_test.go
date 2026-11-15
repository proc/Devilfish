package uci

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"devilfish/internal/config"
)

func searchOutput(t *testing.T, u *UCI, out *bytes.Buffer) string {
	t.Helper()
	u.cmdGo([]string{"depth", "4"})
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "bestmove") {
		if time.Now().After(deadline) {
			t.Fatalf("no bestmove. Output:\n%s", out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	return out.String()
}

func TestShowWDL(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Threads = 1
	cfg.OwnBook = false

	out := &bytes.Buffer{}
	u := New(bytes.NewBuffer(nil), out, cfg)
	u.cmdUCI()
	if !strings.Contains(out.String(), "option name UCI_ShowWDL type check default false") {
		t.Errorf("UCI_ShowWDL is not announced as off by default:\n%s", out.String())
	}
	if s := searchOutput(t, u, out); strings.Contains(s, " wdl ") {
		t.Errorf("wdl printed although the option is off:\n%s", s)
	}

	out = &bytes.Buffer{}
	u = New(bytes.NewBuffer(nil), out, cfg)
	u.cmdSetOption([]string{"name", "UCI_ShowWDL", "value", "true"})
	s := searchOutput(t, u, out)
	re := regexp.MustCompile(`score cp -?\d+ wdl (\d+) (\d+) (\d+) nodes`)
	scored := 0
	for _, line := range strings.Split(s, "\n") {
		if !strings.Contains(line, "score cp") {
			continue
		}
		scored++
		if !re.MatchString(line) {
			t.Errorf("score without wdl: %s", line)
		}
	}
	if scored == 0 {
		t.Fatalf("no score lines:\n%s", s)
	}

	// The config file can turn it on too.
	cfg.ShowWDL = true
	out = &bytes.Buffer{}
	u = New(bytes.NewBuffer(nil), out, cfg)
	if s := searchOutput(t, u, out); !re.MatchString(s) {
		t.Errorf("show_wdl in the config did not turn wdl on:\n%s", s)
	}
}
