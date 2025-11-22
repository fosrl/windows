//go:build windows

package main

import (
	"time"

	"github.com/fosrl/windows/ui"
	"github.com/fosrl/windows/version"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
)

func main() {
	// Setup logging first
	setupLogging()

	// Log version on startup
	logger.Info("Pangolin version %s starting", version.Number)

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

	// Start background update checker after app starts running
	// We need to wait for app.Run() to start the message loop before
	// the background checker can safely use walk.App().Synchronize()
	go func() {
		// Wait a moment for app.Run() to start processing messages
		time.Sleep(10 * time.Second)
		ui.StartBackgroundUpdateChecker(mw, 6*time.Hour)
	}()

	// Run the application
	app.Run()
}
