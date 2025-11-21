//go:build windows

package config

import (
	"os"
	"path/filepath"
)

const (
	AppName = "Pangolin"
)

// GetProgramDataDir returns the base ProgramData directory for the application
// The installer should create this directory and place application files here
func GetProgramDataDir() string {
	return filepath.Join(os.Getenv("PROGRAMDATA"), AppName)
}

// GetLogDir returns the directory path for log files
func GetLogDir() string {
	return filepath.Join(GetProgramDataDir(), "logs")
}

// GetIconsPath returns the directory path for icon files
func GetIconsPath() string {
	return filepath.Join(GetProgramDataDir(), "icons")
}
