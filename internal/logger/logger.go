package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu      sync.Mutex
	file    *os.File
	enabled bool
	logPath string
)

// Init opens or creates the log file at path for writing.
func Init(path string) error {
	mu.Lock()
	defer mu.Unlock()

	if path == "" {
		if file != nil {
			_ = file.Close()
			file = nil
		}
		enabled = false
		logPath = ""
		return nil
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	if file != nil {
		_ = file.Close()
	}

	file = f
	enabled = true
	logPath = path

	writeRaw(fmt.Sprintf("\n========== DEVILFISH SESSION START [%s] ==========\n", time.Now().Format("2006-01-02 15:04:05.000")))
	return nil
}

// Close flushes and closes the active log file.
func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if file != nil {
		writeRaw(fmt.Sprintf("========== DEVILFISH SESSION END [%s] ==========\n\n", time.Now().Format("2006-01-02 15:04:05.000")))
		err := file.Close()
		file = nil
		enabled = false
		logPath = ""
		return err
	}
	return nil
}

// IsEnabled returns true if logging is currently active.
func IsEnabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}

// Path returns the path of the active log file.
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return logPath
}

// writeRaw issues a single unbuffered write; no fsync, since an fsync per
// log line stalls the search for milliseconds at a time.
func writeRaw(str string) {
	if file != nil {
		_, _ = file.WriteString(str)
	}
}

func logMessage(tag, msg string) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled || file == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("%s [%s] %s\n", timestamp, tag, msg)
	writeRaw(line)
}

func Infof(format string, args ...any) {
	logMessage("INFO", fmt.Sprintf(format, args...))
}

func Debugf(format string, args ...any) {
	logMessage("DEBUG", fmt.Sprintf(format, args...))
}

func Profilef(format string, args ...any) {
	logMessage("PROFILE", fmt.Sprintf(format, args...))
}

func Warnf(format string, args ...any) {
	logMessage("WARN", fmt.Sprintf(format, args...))
}

func Errorf(format string, args ...any) {
	logMessage("ERROR", fmt.Sprintf(format, args...))
}

func LogUCIIn(line string) {
	logMessage("UCI-IN", "<- "+line)
}

func LogUCIOut(line string) {
	logMessage("UCI-OUT", "-> "+line)
}
