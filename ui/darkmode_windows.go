package ui

import "github.com/wailsapp/wails/v3/pkg/w32"

// systemDarkMode reports whether apps are set to use dark mode.
func systemDarkMode() bool { return w32.IsCurrentlyDarkMode() }
