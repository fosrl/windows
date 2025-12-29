//go:build windows

package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/elevate"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/secrets"
	"github.com/fosrl/windows/ui"
	"github.com/fosrl/windows/version"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func execElevatedManagerServiceInstaller() error {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	err = elevate.ShellExecute(path, "/installmanagerservice", "", windows.SW_SHOW)
	if err != nil && err != windows.ERROR_CANCELLED {
		return err
	}
	os.Exit(0)
	return windows.ERROR_UNHANDLED_EXCEPTION // Not reached
}

func main() {
	// Setup logging first
	setupLogging()

	// Log version on startup
	logger.Info("Pangolin version %s starting", version.Number)

	// Check if we're being run as the manager service
	if len(os.Args) >= 2 && os.Args[1] == "/managerservice" {
		// Run as Windows service
		logger.Info("Starting as manager service")
		if err := managers.Run(); err != nil {
			logger.Fatal("Manager service failed: %v", err)
		}
		return
	}

	// Check if we're being run as a tunnel service
	if len(os.Args) >= 3 && os.Args[1] == "/tunnelservice" {
		// Run as tunnel service
		configPath := os.Args[2]
		logger.Info("Starting as tunnel service with config: %s", configPath)

		// Read config from file
		configJSON, err := os.ReadFile(configPath)
		if err != nil {
			logger.Fatal("Failed to read tunnel config: %v", err)
		}

		// Run the tunnel service
		if err := managers.RunTunnelService(string(configJSON)); err != nil {
			logger.Fatal("Tunnel service failed: %v", err)
		}
		return
	}

	// Handle /installmanagerservice flag (called after elevation)
	if len(os.Args) >= 2 && os.Args[1] == "/installmanagerservice" {
		err := managers.InstallManager()
		if err != nil {
			if err == managers.ErrManagerAlreadyRunning {
				logger.Info("Manager service is already running")
				// Wait a bit for UI to appear
				time.Sleep(5 * time.Second)
				return
			}
			logger.Fatal("Failed to install manager service: %v", err)
		}
		logger.Info("Manager service installed successfully")
		// Wait a bit for service to start and UI to appear
		time.Sleep(5 * time.Second)
		return
	}

	// Check if we're being launched by the manager service with /ui flag
	if len(os.Args) >= 5 && os.Args[1] == "/ui" {
		// We're being launched by the manager service
		// Args: [exe, "/ui", readerFd, writerFd, eventsFd]
		readerFd, err1 := strconv.ParseUint(os.Args[2], 10, 64)
		writerFd, err2 := strconv.ParseUint(os.Args[3], 10, 64)
		eventsFd, err3 := strconv.ParseUint(os.Args[4], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			logger.Fatal("Invalid file descriptors from manager service")
		}

		// Open the file descriptors as files
		reader := os.NewFile(uintptr(readerFd), "reader")
		writer := os.NewFile(uintptr(writerFd), "writer")
		events := os.NewFile(uintptr(eventsFd), "events")

		// Initialize IPC client to connect to manager service
		managers.InitializeIPCClient(reader, writer, events)

		logger.Info("Connected to manager service via IPC")
		// Fall through to run UI
	} else {
		// No arguments - check if manager service is running, install/start if needed
		// This is the normal entry point when user double-clicks the .exe
		serviceName := config.AppName + "Manager"

		// Try to connect to service manager
		m, err := mgr.Connect()
		if err != nil {
			// If we can't connect to service manager, we can't check service status
			// Try to use sc query as an alternative, or just try to elevate to install/start
			if err == windows.ERROR_ACCESS_DENIED {
				logger.Info("Cannot access service manager without admin privileges")
				logger.Info("Attempting to install/start manager service (will show UAC prompt)...")
				// Try to elevate to install/start the service
				err = execElevatedManagerServiceInstaller()
				if err != nil {
					logger.Fatal("Failed to install/start manager service: %v\nPlease run as administrator to install the service.", err)
				}
				return
			}
			logger.Fatal("Failed to connect to service manager: %v", err)
		}
		defer m.Disconnect()

		service, err := m.OpenService(serviceName)
		if err != nil {
			// Service doesn't exist, need to install it (requires elevation)
			logger.Info("Manager service not found, installing...")
			err = execElevatedManagerServiceInstaller()
			if err != nil {
				logger.Fatal("Failed to install manager service: %v", err)
			}
			return
		}
		defer service.Close()

		status, err := service.Query()
		if err != nil {
			logger.Fatal("Failed to query service status: %v", err)
		}

		if status.State == svc.Stopped {
			// Service exists but is stopped, try to start it
			logger.Info("Manager service is stopped, starting...")
			err = service.Start()
			if err != nil {
				// If we don't have permission to start the service, try to elevate
				if err == windows.ERROR_ACCESS_DENIED {
					logger.Info("Need admin privileges to start service, requesting elevation...")
					// Use cmd.exe to run net start, which can be elevated to start the service
					// This will show a UAC prompt if needed
					err = elevate.ShellExecute("cmd.exe", fmt.Sprintf("/c net start \"%s\"", serviceName), "", windows.SW_HIDE)
					if err != nil && err != windows.ERROR_CANCELLED {
						logger.Fatal("Failed to start manager service (access denied): %v\nPlease start the service manually or run as administrator.", err)
					}
					if err == windows.ERROR_CANCELLED {
						logger.Info("User cancelled elevation, cannot start service")
						return
					}
					// Wait a moment for service to start
					time.Sleep(2 * time.Second)
					// Verify it started
					status, err = service.Query()
					if err != nil {
						logger.Fatal("Failed to query service status after start: %v", err)
					}
					if status.State == svc.Stopped {
						logger.Fatal("Service failed to start. Please start it manually or run as administrator.")
					}
					logger.Info("Manager service started via elevation, UI should appear shortly")
				} else {
					logger.Fatal("Failed to start manager service: %v", err)
				}
			} else {
				// Wait a moment for service to start and launch UI
				logger.Info("Manager service started, UI should appear shortly")
				time.Sleep(2 * time.Second)
			}
		} else {
			// Service is running, manager will launch UI automatically if needed
			logger.Info("Manager service is already running")
		}

		// Exit - the manager service will handle launching the UI
		// The manager service automatically launches UI processes for logged-in users
		return
	}

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

	// Initialize managers
	accountManager := config.NewAccountManager()
	configManager := config.NewConfigManager()
	secretManager := secrets.NewSecretManager()

	var hostname string
	if activeAccount, _ := accountManager.ActiveAccount(); activeAccount != nil {
		hostname = activeAccount.Hostname
	} else {
		hostname = config.DefaultHostname
	}

	apiClient := api.NewAPIClient(hostname, "")
	authManager := auth.NewAuthManager(apiClient, configManager, accountManager, secretManager)

	// Initialize auth manager (loads saved session token if available)
	if err := authManager.Initialize(); err != nil {
		logger.Error("Failed to initialize auth manager: %v", err)
	}

	// Setup tray icon and menu
	if err := ui.SetupTray(mw, authManager, configManager, accountManager, apiClient, secretManager); err != nil {
		logger.Fatal("Failed to setup tray: %v", err)
	}

	// Manager service handles all update checking
	// If we're launched with /ui flag, we're connected to manager via IPC
	if len(os.Args) >= 5 && os.Args[1] == "/ui" {
		logger.Info("Connected to manager service - update checking handled by manager")
	} else {
		logger.Info("Running standalone - manager service should be running separately")
		// Note: In production, the UI should always be launched by the manager service
		// This standalone mode is mainly for development/testing
	}

	// Run the application
	app.Run()
}
