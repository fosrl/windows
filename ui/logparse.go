package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// LogLine is a single parsed log line shown in the Logs tab.
type LogLine struct {
	Stamp time.Time `json:"stamp"`
	Level string    `json:"level"`
	Line  string    `json:"line"`
}

const logStampFormat = "2006-01-02 15:04:05.000"

// String formats the line the way Copy and Save write it.
func (l LogLine) String() string {
	return fmt.Sprintf("%s [%s] %s\r\n", l.Stamp.Format(logStampFormat), l.Level, l.Line)
}

var (
	// LEVEL: YYYY/MM/DD HH:MM:SS message (pangolin format)
	logPangolinRe = regexp.MustCompile(`^(\w+):\s+(\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2})\s+(.+)$`)
	// [2006-01-02 15:04:05.000] [LEVEL] message
	logBracketRe = regexp.MustCompile(`^\[([^\]]+)\]\s+\[([^\]]+)\]\s+(.+)$`)
	// 2006-01-02T15:04:05.000Z level message
	logISORe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})?)\s+(\w+)\s+(.+)$`)
	// 2006-01-02 15:04:05.000 level message
	logSpaceRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(\w+)\s+(.+)$`)
	// 2006-01-02 15:04:05.000 message (no level)
	logNoLevelRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(.+)$`)
)

// parseLogLine parses a log line in one of the supported formats. Lines in no
// known format get the current time and level UNKNOWN.
func parseLogLine(line string) *LogLine {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	if m := logPangolinRe.FindStringSubmatch(line); len(m) == 4 {
		if t, err := parseTimestamp(m[2]); err == nil {
			return &LogLine{Stamp: t, Level: m[1], Line: m[3]}
		}
	}
	if m := logBracketRe.FindStringSubmatch(line); len(m) == 4 {
		if t, err := parseTimestamp(m[1]); err == nil {
			return &LogLine{Stamp: t, Level: m[2], Line: m[3]}
		}
	}
	if m := logISORe.FindStringSubmatch(line); len(m) == 4 {
		if t, err := time.Parse(time.RFC3339Nano, m[1]); err == nil {
			return &LogLine{Stamp: t, Level: m[2], Line: m[3]}
		}
	}
	if m := logSpaceRe.FindStringSubmatch(line); len(m) == 4 {
		if t, err := parseTimestamp(m[1]); err == nil {
			return &LogLine{Stamp: t, Level: m[2], Line: m[3]}
		}
	}
	if m := logNoLevelRe.FindStringSubmatch(line); len(m) == 3 {
		if t, err := parseTimestamp(m[1]); err == nil {
			return &LogLine{Stamp: t, Level: "INFO", Line: m[2]}
		}
	}

	return &LogLine{Stamp: time.Now(), Level: "UNKNOWN", Line: line}
}

func parseTimestamp(ts string) (time.Time, error) {
	formats := []string{
		"2006/01/02 15:04:05",
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
	}
	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse timestamp: %s", ts)
}
