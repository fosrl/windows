//go:build windows

package main

import (
	"windows/ui"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
)

func main() {
	// Setup logging first
	setupLogging()

	app, err := walk.InitApp()
	if err != nil {
		logger.Fatal("Failed to initialize app: %v", err)
	}

	// Create a hidden main window (required for NotifyIcon)
	mw, err := walk.NewMainWindow()
	if err != nil {
		logger.Fatal("Failed to create main window: %v", err)
	}
	mw.SetVisible(false)

	// Setup tray icon and menu
	if err := ui.SetupTray(mw); err != nil {
		logger.Fatal("Failed to setup tray: %v", err)
	}

	// Run the application
	app.Run()
}
