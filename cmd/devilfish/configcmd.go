package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"devilfish/internal/config"
	"devilfish/internal/search"
)

// The "config" subcommand edits the config file from the command line:
//
//	devilfish config set PruneKing 1
//	devilfish config rm PruneKing
//	devilfish config get PruneKing
//	devilfish config list
//	devilfish config path
//
// -c/--config <path> selects the file; otherwise it is the one the engine
// itself would load (see config.FindConfigFile).

var configUsage = []string{
	"config set <name> <value>   Set an option in the config file (e.g. config set PruneKing 1)",
	"config rm <name>...         Remove options from the config file, returning them to their defaults",
	"config get <name>           Show an option's value in the config file",
	"config list                 Show the config file",
	"config path                 Show which config file is used",
}

type settingKind int

const (
	kindInt settingKind = iota
	kindBool
	kindString
)

// setting describes an option the config file understands.
type setting struct {
	key      string // as written to the file
	kind     settingKind
	min, max int // kindInt
}

// standardSettings are the settings with a field of their own in the config
// file, by key.
var standardSettings = []setting{
	{"threads", kindInt, 1, 512},
	{"hash", kindInt, 1, 65536},
	{"ponder", kindBool, 0, 0},
	{"use_nnue", kindBool, 0, 0},
	{"eval_file", kindString, 0, 0},
	{"syzygy_path", kindString, 0, 0},
	{"syzygy_probe_depth", kindInt, 1, 64},
	{"syzygy_50_move_rule", kindBool, 0, 0},
	{"syzygy_probe_limit", kindInt, 0, 7},
	{"log_file", kindString, 0, 0},
	{"move_overhead", kindInt, 0, 5000},
	{"multipv", kindInt, 1, 256},
	{"show_wdl", kindBool, 0, 0},
	{"share_cores", kindBool, 0, 0},
	{"own_book", kindBool, 0, 0},
	{"book_file", kindString, 0, 0},
}

// lookupSetting finds an option by name. Names are compared as the config
// file compares them, so "EvalFile", "eval_file" and "eval-file" are one
// option, and the search tunables go by their UCI names.
func lookupSetting(name string) (setting, bool) {
	want := config.NormalizeKey(name)
	for _, s := range standardSettings {
		if config.NormalizeKey(s.key) == want {
			return s, true
		}
	}
	for _, ps := range search.TunableParams() {
		if config.NormalizeKey(ps.Name) == want {
			return setting{ps.Name, kindInt, int(ps.Min), int(ps.Max)}, true
		}
	}
	return setting{}, false
}

// parseSettingValue converts the text given on the command line to the
// value written to the file.
func parseSettingValue(s setting, text string) (interface{}, error) {
	text = strings.TrimSpace(text)
	switch s.kind {
	case kindBool:
		switch strings.ToLower(text) {
		case "true", "1", "on", "yes":
			return true, nil
		case "false", "0", "off", "no":
			return false, nil
		}
		return nil, fmt.Errorf("%s takes true or false, not %q", s.key, text)
	case kindInt:
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, fmt.Errorf("%s takes a whole number, not %q", s.key, text)
		}
		if n < s.min || n > s.max {
			return nil, fmt.Errorf("%s must be between %d and %d, not %d", s.key, s.min, s.max, n)
		}
		return n, nil
	}
	return text, nil
}

func unknownSetting(name string) error {
	var names []string
	for _, s := range standardSettings {
		names = append(names, s.key)
	}
	want := config.NormalizeKey(name)
	var close []string
	for _, ps := range search.TunableParams() {
		if n := config.NormalizeKey(ps.Name); strings.Contains(n, want) || strings.Contains(want, n) {
			close = append(close, ps.Name)
		}
	}
	sort.Strings(close)
	msg := fmt.Sprintf("unknown option %q", name)
	if len(close) > 0 {
		msg += "; did you mean " + strings.Join(close, ", ") + "?"
	}
	return fmt.Errorf("%s\nSettings: %s, and the search options listed by the \"uci\" command", msg, strings.Join(names, ", "))
}

// runConfigCommand carries out "devilfish config <args>" on the config file
// at path and writes what it did to out.
func runConfigCommand(args []string, path string, out io.Writer) error {
	usage := fmt.Errorf("usage:\n  devilfish %s", strings.Join(configUsage, "\n  devilfish "))
	if len(args) == 0 {
		return usage
	}
	if path == "" {
		return fmt.Errorf("no config file location (the home directory is unknown); pass --config <path>")
	}
	switch args[0] {
	case "path":
		fmt.Fprintln(out, path)
		return nil

	case "list", "show":
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			fmt.Fprintf(out, "%s does not exist; every option has its default\n", path)
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s:\n%s", path, data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			fmt.Fprintln(out)
		}
		return nil

	case "get":
		if len(args) != 2 {
			return usage
		}
		s, ok := lookupSetting(args[1])
		if !ok {
			return unknownSetting(args[1])
		}
		v, found, err := config.GetKey(path, s.key)
		if err != nil {
			return err
		}
		if !found {
			fmt.Fprintf(out, "%s is not set in %s (default applies)\n", s.key, path)
			return nil
		}
		fmt.Fprintf(out, "%s = %s\n", s.key, v)
		return nil

	case "set":
		if len(args) < 3 {
			return usage
		}
		s, ok := lookupSetting(args[1])
		if !ok {
			return unknownSetting(args[1])
		}
		// A path or file name may contain spaces.
		value, err := parseSettingValue(s, strings.Join(args[2:], " "))
		if err != nil {
			return err
		}
		prev, err := config.SetKey(path, s.key, value)
		if err != nil {
			return err
		}
		now, _, _ := config.GetKey(path, s.key)
		if prev == "" {
			fmt.Fprintf(out, "%s = %s in %s\n", s.key, now, path)
		} else {
			fmt.Fprintf(out, "%s = %s (was %s) in %s\n", s.key, now, prev, path)
		}
		return nil

	case "rm", "remove", "unset", "delete":
		if len(args) < 2 {
			return usage
		}
		// Check every name before changing anything.
		var keys []string
		for _, name := range args[1:] {
			s, ok := lookupSetting(name)
			if !ok {
				return unknownSetting(name)
			}
			keys = append(keys, s.key)
		}
		for _, key := range keys {
			removed, err := config.RemoveKey(path, key)
			if err != nil {
				return err
			}
			if removed {
				fmt.Fprintf(out, "%s removed from %s\n", key, path)
			} else {
				fmt.Fprintf(out, "%s was not set in %s\n", key, path)
			}
		}
		return nil
	}
	return usage
}

// configCommandArgs picks the "config" subcommand out of the command line:
// its operands, and the file named with -c/--config if any. ok is false if
// the command line is not a config command.
func configCommandArgs(args []string) (sub []string, path string, ok bool) {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-c" || a == "-config" || a == "--config":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--config=") || strings.HasPrefix(a, "-config=") || strings.HasPrefix(a, "-c="):
			path = strings.SplitN(a, "=", 2)[1]
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || rest[0] != "config" {
		return nil, "", false
	}
	return rest[1:], path, true
}
