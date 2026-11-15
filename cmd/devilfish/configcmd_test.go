package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := runConfigCommand(args, path, &out)
		return out.String(), err
	}
	file := func() string {
		data, _ := os.ReadFile(path)
		return string(data)
	}

	if out, err := run("set", "pruneking", "1"); err != nil || !strings.Contains(out, "PruneKing = 1") {
		t.Fatalf("set: %q, %v", out, err)
	}
	if file() != "{\n  \"PruneKing\": 1\n}\n" {
		t.Fatalf("file after set:\n%s", file())
	}
	// Standard settings go by any spelling and are written under their key.
	for _, c := range [][]string{
		{"set", "EvalFile", "<8chess>"},
		{"set", "Threads", "4"},
		{"set", "own-book", "off"},
		{"set", "Move Overhead", "50"},
		{"set", "log_file", "my", "log.txt"},
	} {
		if _, err := run(c...); err != nil {
			t.Fatalf("%v: %v", c, err)
		}
	}
	want := `{
  "PruneKing": 1,
  "eval_file": "<8chess>",
  "threads": 4,
  "own_book": false,
  "move_overhead": 50,
  "log_file": "my log.txt"
}
`
	if file() != want {
		t.Fatalf("file:\n%s\nwant:\n%s", file(), want)
	}
	if out, err := run("set", "PruneKing", "2"); err != nil || !strings.Contains(out, "(was 1)") {
		t.Errorf("set again: %q, %v", out, err)
	}
	if out, err := run("get", "prune_king"); err != nil || !strings.HasPrefix(out, "PruneKing = 2") {
		t.Errorf("get: %q, %v", out, err)
	}

	// Rejected, with the file untouched.
	before := file()
	for _, c := range [][]string{
		{"set", "PruneKing", "9"},   // out of range
		{"set", "PruneKing", "yes"}, // not a number
		{"set", "ponder", "maybe"},  // not a boolean
		{"set", "NoSuchOption", "1"},
		{"rm", "threads", "NoSuchOption"}, // nothing is removed if one name is wrong
		{"set", "PruneKing"},
		{"frobnicate"},
		{},
	} {
		if _, err := run(c...); err == nil {
			t.Errorf("%v: no error", c)
		}
	}
	if file() != before {
		t.Fatalf("a rejected command changed the file:\n%s", file())
	}

	if out, err := run("rm", "PruneKing", "evalfile"); err != nil || !strings.Contains(out, "PruneKing removed") || !strings.Contains(out, "eval_file removed") {
		t.Fatalf("rm: %q, %v", out, err)
	}
	if out, err := run("rm", "PruneKing"); err != nil || !strings.Contains(out, "was not set") {
		t.Errorf("rm of an absent option: %q, %v", out, err)
	}
	if strings.Contains(file(), "PruneKing") || strings.Contains(file(), "eval_file") || !strings.Contains(file(), `"threads": 4`) {
		t.Fatalf("file after rm:\n%s", file())
	}
	if out, _ := run("get", "PruneKing"); !strings.Contains(out, "not set") {
		t.Errorf("get after rm: %q", out)
	}
}

func TestConfigCommandArgs(t *testing.T) {
	sub, path, ok := configCommandArgs([]string{"-c", "x.json", "config", "set", "Hash", "64"})
	if !ok || path != "x.json" || strings.Join(sub, " ") != "set Hash 64" {
		t.Errorf("got %v %q %v", sub, path, ok)
	}
	sub, path, ok = configCommandArgs([]string{"config", "rm", "Hash", "--config=y.json"})
	if !ok || path != "y.json" || strings.Join(sub, " ") != "rm Hash" {
		t.Errorf("got %v %q %v", sub, path, ok)
	}
	if _, _, ok := configCommandArgs([]string{"--config", "config"}); ok {
		t.Error("a config file named \"config\" was taken for the subcommand")
	}
	if _, _, ok := configCommandArgs([]string{"bench", "12"}); ok {
		t.Error("bench was taken for the config subcommand")
	}
}
