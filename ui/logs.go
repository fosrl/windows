//go:build windows

package ui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
)

const (
	maxLogLinesDisplayed = 10000
	maxInitialLogLines   = 1000
)

// LogEntry is a log line as sent to the Logs tab. Seq increases with every
// line read, so the frontend can drop appends it already has.
type LogEntry struct {
	Seq   uint64 `json:"seq"`
	Stamp string `json:"stamp"`
	Level string `json:"level"`
	Line  string `json:"line"`
}

// LogsSnapshot replaces everything shown in the Logs tab.
type LogsSnapshot struct {
	Entries []LogEntry `json:"entries"`
}

type logEntryItem struct {
	seq  uint64
	line LogLine
}

var (
	logsMu      sync.Mutex
	logsQuit    chan struct{}
	logsItems   []logEntryItem
	logsSeq     uint64
	logsFilePos int64
	logsSize    int64
)

func logFilePath() string {
	return filepath.Join(config.GetLogDir(), "pangolin.log")
}

func toLogEntry(it logEntryItem) LogEntry {
	return LogEntry{Seq: it.seq, Stamp: it.line.Stamp.Format(logStampFormat), Level: it.line.Level, Line: it.line.Line}
}

// logsSnapshotLocked must be called with logsMu held.
func logsSnapshotLocked() LogsSnapshot {
	entries := make([]LogEntry, len(logsItems))
	for i, it := range logsItems {
		entries[i] = toLogEntry(it)
	}
	return LogsSnapshot{Entries: entries}
}

func currentLogsSnapshot() LogsSnapshot {
	logsMu.Lock()
	defer logsMu.Unlock()
	return logsSnapshotLocked()
}

// appendLogLinesLocked must be called with logsMu held.
func appendLogLinesLocked(lines []LogLine) []LogEntry {
	added := make([]LogEntry, 0, len(lines))
	for _, l := range lines {
		logsSeq++
		it := logEntryItem{seq: logsSeq, line: l}
		logsItems = append(logsItems, it)
		added = append(added, toLogEntry(it))
	}
	if len(logsItems) > maxLogLinesDisplayed {
		logsItems = logsItems[len(logsItems)-maxLogLinesDisplayed:]
	}
	return added
}

// startLogTail loads the last lines of the log file and tails it once a second.
func startLogTail() {
	logsMu.Lock()
	if logsQuit != nil {
		logsMu.Unlock()
		return
	}
	logsQuit = make(chan struct{})
	quit := logsQuit
	logsItems = nil
	logsFilePos, logsSize = 0, 0
	loadInitialLogsLocked()
	snapshot := logsSnapshotLocked()
	logsMu.Unlock()

	app.Event.Emit(eventLogsReset, snapshot)

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ticker.C:
				readNewLogLines(quit)
			}
		}
	}()
}

func stopLogTail() {
	logsMu.Lock()
	defer logsMu.Unlock()
	if logsQuit != nil {
		close(logsQuit)
		logsQuit = nil
	}
}

func loadInitialLogsLocked() {
	file, err := os.Open(logFilePath())
	if err != nil {
		// The file may not exist yet.
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return
	}
	logsSize = info.Size()

	lines := make([]string, 0, maxInitialLogLines)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > maxInitialLogLines {
			lines = lines[1:]
		}
	}

	parsed := make([]LogLine, 0, len(lines))
	for _, line := range lines {
		if p := parseLogLine(line); p != nil {
			parsed = append(parsed, *p)
		}
	}
	appendLogLinesLocked(parsed)
	logsFilePos = logsSize
}

func readNewLogLines(quit chan struct{}) {
	file, err := os.Open(logFilePath())
	if err != nil {
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return
	}
	currentSize := info.Size()

	logsMu.Lock()
	defer logsMu.Unlock()
	if logsQuit != quit {
		return
	}

	rotated := false
	if currentSize < logsSize {
		// The file was rotated; start over.
		logsFilePos = 0
		logsItems = nil
		rotated = true
	}
	if currentSize <= logsFilePos {
		logsSize = currentSize
		if rotated {
			app.Event.Emit(eventLogsReset, logsSnapshotLocked())
		}
		return
	}

	if _, err := file.Seek(logsFilePos, 0); err != nil {
		return
	}
	var newLines []LogLine
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if p := parseLogLine(scanner.Text()); p != nil {
			newLines = append(newLines, *p)
		}
	}
	logsFilePos = currentSize
	logsSize = currentSize

	added := appendLogLinesLocked(newLines)
	if rotated {
		app.Event.Emit(eventLogsReset, logsSnapshotLocked())
	} else if len(added) > 0 {
		app.Event.Emit(eventLogsAppend, LogsSnapshot{Entries: added})
	}
}

func clearLogs() {
	logsMu.Lock()
	logsItems = nil
	snapshot := logsSnapshotLocked()
	logsMu.Unlock()
	app.Event.Emit(eventLogsReset, snapshot)
}

// formatLogLines formats the lines with the given sequence numbers for the clipboard or a file.
func formatLogLines(seqs []uint64) string {
	want := make(map[uint64]bool, len(seqs))
	for _, s := range seqs {
		want[s] = true
	}
	var b strings.Builder
	logsMu.Lock()
	defer logsMu.Unlock()
	for _, it := range logsItems {
		if seqs == nil || want[it.seq] {
			b.WriteString(it.line.String())
		}
	}
	return b.String()
}

func copyLogLines(seqs []uint64) {
	if len(seqs) == 0 {
		return
	}
	app.Clipboard.SetText(formatLogLines(seqs))
}

// exportLogs asks for a file and writes all displayed log lines to it.
func exportLogs() {
	owner := preferencesWindowOrNil()
	d := app.Dialog.SaveFile().
		SetFilename(fmt.Sprintf("pangolin-log-%s.txt", time.Now().Format("2006-01-02T150405"))).
		SetMessage("Export log to file").
		AddFilter("Text Files (*.txt)", "*.txt").
		AddFilter("All Files (*.*)", "*.*")
	if owner != nil {
		d.AttachToWindow(owner)
	}
	path, err := d.PromptForSingleSelection()
	if err != nil || path == "" {
		return
	}
	if filepath.Ext(path) == "" {
		path += ".txt"
	}

	if _, err := os.Stat(path); err == nil {
		if !confirm(owner, "File Exists",
			fmt.Sprintf("The file %s already exists. Do you want to overwrite it?", filepath.Base(path)), false) {
			return
		}
	}

	if err := os.WriteFile(path, []byte(formatLogLines(nil)), 0o644); err != nil {
		logger.Error("Failed to write log file: %v", err)
		showError(owner, "Save Failed", fmt.Sprintf("Failed to write file: %v", err))
		return
	}
	showInfo(owner, "Save Successful", fmt.Sprintf("Log saved to %s", path))
}
