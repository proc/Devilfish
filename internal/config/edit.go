package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Editing a config file in place: one key is set or removed, and every other
// key keeps its value, its spelling and its position in the file.

// field is one key of a JSON object with its value as written.
type field struct {
	key string
	val json.RawMessage
}

// parseObject reads a JSON object, keeping the order of its keys. Empty
// input is an empty object.
func parseObject(data []byte) ([]field, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if t, err := dec.Token(); err != nil {
		return nil, err
	} else if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("not a JSON object")
	}
	var fields []field
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := t.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected token %v", t)
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		fields = append(fields, field{key, val})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("text after the JSON object")
	}
	return fields, nil
}

// formatObject writes the object with two-space indentation.
func formatObject(fields []field) []byte {
	var buf bytes.Buffer
	buf.WriteString("{")
	for i, f := range fields {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString("\n  ")
		k, _ := json.Marshal(f.key)
		buf.Write(k)
		buf.WriteString(": ")
		var v bytes.Buffer
		if err := json.Indent(&v, f.val, "  ", "  "); err != nil {
			v.Reset()
			v.Write(f.val)
		}
		buf.Write(v.Bytes())
	}
	if len(fields) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes()
}

func marshalValue(v interface{}) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep "<stockfish>" readable
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

func readFields(path string) ([]field, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fields, err := parseObject(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return fields, nil
}

// writeFields replaces the file in one step, so that an interrupted write
// cannot leave half a config behind.
func writeFields(path string, fields []field) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(formatObject(fields)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// find returns the index of the field whose key is name, compared as
// NormalizeKey compares them, or -1.
func find(fields []field, name string) int {
	want := NormalizeKey(name)
	for i, f := range fields {
		if NormalizeKey(f.key) == want {
			return i
		}
	}
	return -1
}

// nested returns the index of the "search_params" object and its fields.
func nested(fields []field) (int, []field) {
	i := find(fields, "search_params")
	if i < 0 {
		return -1, nil
	}
	sub, err := parseObject(fields[i].val)
	if err != nil {
		return -1, nil
	}
	return i, sub
}

// SetKey sets key to value in the config file at path, creating the file if
// it does not exist. A key already present under another spelling (see
// NormalizeKey), at the top level or inside "search_params", is replaced
// where it stands; otherwise the key is added at the end. It returns the
// value the key had before, as written in the file ("" if it was not set).
func SetKey(path, key string, value interface{}) (previous string, err error) {
	fields, err := readFields(path)
	if err != nil {
		return "", err
	}
	raw, err := marshalValue(value)
	if err != nil {
		return "", err
	}
	if i := find(fields, key); i >= 0 {
		previous = string(fields[i].val)
		fields[i] = field{key, raw}
	} else if si, sub := nested(fields); si >= 0 && find(sub, key) >= 0 {
		j := find(sub, key)
		previous = string(sub[j].val)
		sub[j] = field{key, raw}
		fields[si].val = bytes.TrimSpace(formatObject(sub))
	} else {
		fields = append(fields, field{key, raw})
	}
	return previous, writeFields(path, fields)
}

// RemoveKey removes key from the config file at path, at the top level and
// inside "search_params", under any spelling. It reports whether the key
// was there; the file is left untouched if it was not.
func RemoveKey(path, key string) (removed bool, err error) {
	fields, err := readFields(path)
	if err != nil {
		return false, err
	}
	for i := find(fields, key); i >= 0; i = find(fields, key) {
		fields = append(fields[:i], fields[i+1:]...)
		removed = true
	}
	if si, sub := nested(fields); si >= 0 {
		changed := false
		for j := find(sub, key); j >= 0; j = find(sub, key) {
			sub = append(sub[:j], sub[j+1:]...)
			changed = true
		}
		if changed {
			removed = true
			fields[si].val = bytes.TrimSpace(formatObject(sub))
		}
	}
	if !removed {
		return false, nil
	}
	return true, writeFields(path, fields)
}

// GetKey returns the value of key as written in the config file at path.
func GetKey(path, key string) (value string, ok bool, err error) {
	fields, err := readFields(path)
	if err != nil {
		return "", false, err
	}
	if i := find(fields, key); i >= 0 {
		return string(fields[i].val), true, nil
	}
	if _, sub := nested(fields); sub != nil {
		if j := find(sub, key); j >= 0 {
			return string(sub[j].val), true, nil
		}
	}
	return "", false, nil
}
