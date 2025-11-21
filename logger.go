//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"windows/config"

	"github.com/fosrl/newt/logger"
)

// setupLogging initializes the logger and sets up log file output with rotation
func setupLogging() {
	// Initialize the logger (GetLogger will auto-initialize if needed)
	_ = logger.GetLogger()

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
	logger.GetLogger().SetOutput(file)
	logger.Info("Pangolin logging initialized - log file: %s", logFile)
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

	// Clean up old log files (keep last 30 days)
	cleanupOldLogFiles(logDir, 30)
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
