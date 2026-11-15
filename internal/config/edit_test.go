package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetAndRemoveKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")

	// A missing file is created with just the key.
	if prev, err := SetKey(path, "PruneKing", 1); err != nil || prev != "" {
		t.Fatalf("SetKey on a new file: %q, %v", prev, err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "{\n  \"PruneKing\": 1\n}\n" {
		t.Fatalf("new file:\n%s", got)
	}

	// Other keys keep their order, spelling and values; a key present under
	// another spelling is replaced where it stands.
	start := `{"threads": 4, "prune_king": 1, "eval_file": "<stockfish>",
	  "search_params": {"LMRBase": 90, "RFPDepth": 8}, "hash": 256}`
	if err := os.WriteFile(path, []byte(start), 0644); err != nil {
		t.Fatal(err)
	}
	if prev, err := SetKey(path, "PruneKing", 2); err != nil || prev != "1" {
		t.Fatalf("SetKey replace: %q, %v", prev, err)
	}
	if prev, err := SetKey(path, "RFPDepth", 9); err != nil || prev != "8" {
		t.Fatalf("SetKey inside search_params: %q, %v", prev, err)
	}
	if _, err := SetKey(path, "book_file", "<pohl>"); err != nil {
		t.Fatal(err)
	}
	want := `{
  "threads": 4,
  "PruneKing": 2,
  "eval_file": "<stockfish>",
  "search_params": {
    "LMRBase": 90,
    "RFPDepth": 9
  },
  "hash": 256,
  "book_file": "<pohl>"
}
`
	if got, _ := os.ReadFile(path); string(got) != want {
		t.Fatalf("after SetKey:\n%s\nwant:\n%s", got, want)
	}
	// The engine reads the result the same way.
	cfg, err := Load(path)
	if err != nil || cfg.Threads != 4 || cfg.Hash != 256 || cfg.BookFile != "<pohl>" ||
		cfg.SearchParams["PruneKing"] != 2 || cfg.SearchParams["RFPDepth"] != 9 || cfg.SearchParams["LMRBase"] != 90 {
		t.Fatalf("Load after SetKey: %+v, %v", cfg, err)
	}

	if v, ok, err := GetKey(path, "prune-king"); err != nil || !ok || v != "2" {
		t.Errorf("GetKey: %q %v %v", v, ok, err)
	}
	if v, ok, _ := GetKey(path, "rfp_depth"); !ok || v != "9" {
		t.Errorf("GetKey inside search_params: %q %v", v, ok)
	}

	for _, key := range []string{"pruneking", "RFPDepth"} {
		if removed, err := RemoveKey(path, key); err != nil || !removed {
			t.Fatalf("RemoveKey(%s): %v, %v", key, removed, err)
		}
	}
	before, _ := os.ReadFile(path)
	if removed, err := RemoveKey(path, "PruneKing"); err != nil || removed {
		t.Errorf("RemoveKey of an absent key: %v, %v", removed, err)
	}
	after, _ := os.ReadFile(path)
	want = `{
  "threads": 4,
  "eval_file": "<stockfish>",
  "search_params": {
    "LMRBase": 90
  },
  "hash": 256,
  "book_file": "<pohl>"
}
`
	if string(after) != want || string(before) != want {
		t.Fatalf("after RemoveKey:\n%s\nwant:\n%s", after, want)
	}
}

// A file that is not valid JSON is reported and left alone.
func TestSetKeyKeepsBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	broken := `{"threads": 4,`
	if err := os.WriteFile(path, []byte(broken), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetKey(path, "hash", 64); err == nil {
		t.Error("SetKey on a broken file: no error")
	}
	if _, err := RemoveKey(path, "threads"); err == nil {
		t.Error("RemoveKey on a broken file: no error")
	}
	if got, _ := os.ReadFile(path); string(got) != broken {
		t.Errorf("broken file was rewritten: %s", got)
	}
}
