//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fosrl/windows/config"

	"github.com/fosrl/newt/logger"
)

// stringToLogLevel converts a string log level to logger.LogLevel.
// Returns INFO as default if the string doesn't match any known level.
func stringToLogLevel(levelStr string) logger.LogLevel {
	switch strings.ToLower(levelStr) {
	case "debug":
		return logger.DEBUG
	case "info":
		return logger.INFO
	case "warn":
		return logger.WARN
	case "error":
		return logger.ERROR
	case "fatal":
		return logger.FATAL
	default:
		return logger.INFO
	}
}

// setupLogging initializes the logger and sets up log file output with rotation
func setupLogging() {
	// Initialize the logger and set log level FIRST, before any logging calls
	logInstance := logger.GetLogger()

	// Resolve log level from system config file (with built-in default fallback)
	logLevelStr := config.GetSystemLogLevel()
	logLevel := stringToLogLevel(logLevelStr)
	logInstance.SetLevel(logLevel)

	// Create log directory if it doesn't exist
	logDir := config.GetLogDir()
	err := os.MkdirAll(logDir, 0755)
	if err != nil {
		logger.Error("Failed to create log directory: %v", err)
		return
	}

	logFile := filepath.Join(logDir, "pangolin.log")

	// Rotate log file if needed
	err = rotateLogFile(logDir, logFile)
	if err != nil {
		logger.Error("Failed to rotate log file: %v", err)
		// Continue anyway to create new log file
	}

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		logger.Error("Failed to open log file: %v", err)
		return
	}

	// Set the custom logger output
	logInstance.SetOutput(file)

	logger.Info("Pangolin logging initialized - log file: %s, log level: %s", logFile, logLevelStr)
}

// rotateLogFile handles daily log rotation
func rotateLogFile(logDir string, logFile string) error {
	// Get current log file info
	info, err := os.Stat(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No current log file to rotate
		}
		return fmt.Errorf("failed to stat log file: %v", err)
	}

	// Check if log file is from today
	now := time.Now()
	fileTime := info.ModTime()

	// If the log file is from today, no rotation needed
	if now.Year() == fileTime.Year() && now.YearDay() == fileTime.YearDay() {
		return nil
	}

	// Create rotated filename with date
	rotatedName := fmt.Sprintf("pangolin-%s.log", fileTime.Format("2006-01-02"))
	rotatedPath := filepath.Join(logDir, rotatedName)

	// Rename current log file to dated filename
	err = os.Rename(logFile, rotatedPath)
	if err != nil {
		return fmt.Errorf("failed to rotate log file: %v", err)
	}

	cleanupOldLogFiles(logDir, 3)
	return nil
}

// cleanupOldLogFiles removes log files older than specified days
func cleanupOldLogFiles(logDir string, daysToKeep int) {
	cutoff := time.Now().AddDate(0, 0, -daysToKeep)
	files, err := os.ReadDir(logDir)
	if err != nil {
		return
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasPrefix(file.Name(), "pangolin-") && strings.HasSuffix(file.Name(), ".log") {
			filePath := filepath.Join(logDir, file.Name())
			info, err := file.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				os.Remove(filePath)
			}
		}
	}
}
