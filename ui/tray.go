//go:build windows

package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"

	"windows/config"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

var trayIcon *walk.NotifyIcon

// setTrayIcon updates the tray icon based on connection status
// connected: true for orange icon, false for gray icon
func setTrayIcon(connected bool) {
	if trayIcon == nil {
		return
	}

	var iconName string
	if connected {
		iconName = "icon-orange.ico"
	} else {
		iconName = "icon-gray.ico"
	}

	iconPath := filepath.Join(config.GetIconsPath(), iconName)
	icon, err := walk.NewIconFromFile(iconPath)
	if err != nil {
		logger.Error("Failed to load icon from %s: %v", iconPath, err)
		// Fallback to system icon
		icon, err = walk.NewIconFromResourceId(32517) // IDI_INFORMATION
		if err != nil {
			logger.Error("Failed to load fallback icon: %v", err)
			return
		}
	}

	if icon != nil {
		if err := trayIcon.SetIcon(icon); err != nil {
			logger.Error("Failed to set tray icon: %v", err)
		}
	}
}

func SetupTray(mw *walk.MainWindow) error {
	// Create NotifyIcon
	ni, err := walk.NewNotifyIcon()
	if err != nil {
		return err
	}
	trayIcon = ni // Store reference for icon updates

	// Load default gray icon (disconnected state)
	setTrayIcon(false)

	// Set tooltip
	ni.SetToolTip(config.AppName)

	// Create grayed out label action
	labelAction := walk.NewAction()
	labelAction.SetText("milo@pangolin.net")
	labelAction.SetEnabled(false) // Gray out the text

	// Create Login action
	loginAction := walk.NewAction()
	loginAction.SetText("Login")
	loginAction.Triggered().Attach(func() {
		ShowLoginDialog(mw)
	})

	// Create Connect action (toggle button with checkmark)
	connectAction := walk.NewAction()
	var isConnected bool
	connectAction.SetText("Connect")
	connectAction.SetChecked(false) // Initially unchecked
	connectAction.Triggered().Attach(func() {
		isConnected = !isConnected
		connectAction.SetChecked(isConnected)

		// Update icon based on connection status
		setTrayIcon(isConnected)

		if isConnected {
			logger.Info("connecting...")
		} else {
			logger.Info("disconnecting...")
		}
	})

	// Create More submenu with Documentation and Open Logs
	moreMenu, err := walk.NewMenu()
	if err != nil {
		return err
	}
	docAction := walk.NewAction()
	docAction.SetText("Documentation")
	docAction.Triggered().Attach(func() {
		url := "https://github.com/tailscale/walk"
		cmd := exec.Command("cmd", "/c", "start", url)
		if err := cmd.Run(); err != nil {
			logger.Error("Failed to open documentation: %v", err)
		}
	})
	moreMenu.Actions().Add(docAction)

	openLogsAction := walk.NewAction()
	openLogsAction.SetText("Open Logs Location")
	openLogsAction.Triggered().Attach(func() {
		logDir := config.GetLogDir()
		// Ensure the directory exists
		if err := os.MkdirAll(logDir, 0755); err != nil {
			logger.Error("Failed to create log directory: %v", err)
		}
		// Open the directory in Windows Explorer
		cmd := exec.Command("explorer", logDir)
		if err := cmd.Run(); err != nil {
			logger.Error("Failed to open log directory: %v", err)
		}
	})
	moreMenu.Actions().Add(openLogsAction)

	moreAction := walk.NewMenuAction(moreMenu)
	moreAction.SetText("More")

	// Create Quit action
	quitAction := walk.NewAction()
	quitAction.SetText("Quit")
	quitAction.Triggered().Attach(func() {
		walk.App().Exit(0)
	})

	// Add actions to context menu (works for right-click)
	contextMenu := ni.ContextMenu()
	contextMenu.Actions().Add(labelAction) // Add label first (grayed out)
	contextMenu.Actions().Add(loginAction) // Add Login button
	contextMenu.Actions().Add(connectAction)
	contextMenu.Actions().Add(moreAction)
	contextMenu.Actions().Add(quitAction)

	// Handle left-click to show popup menu using Windows API
	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			// Get cursor position
			var pt win.POINT
			win.GetCursorPos(&pt)

			// Get the menu handle from the context menu using unsafe
			// The Menu struct should have an hMenu field as the first field
			menuPtr := (*struct {
				hMenu win.HMENU
			})(unsafe.Pointer(contextMenu))

			if menuPtr.hMenu != 0 {
				// Show the menu using TrackPopupMenu
				// TrackPopupMenu automatically closes when clicking away
				// We need to set the window as foreground to ensure proper message handling
				win.SetForegroundWindow(mw.Handle())
				win.TrackPopupMenu(
					menuPtr.hMenu,
					win.TPM_LEFTALIGN|win.TPM_LEFTBUTTON|win.TPM_RIGHTBUTTON,
					pt.X,
					pt.Y,
					0,
					mw.Handle(),
					nil,
				)
				// Post a null message to ensure the menu closes properly
				win.PostMessage(mw.Handle(), win.WM_NULL, 0, 0)
			}
		}
	})

	ni.SetVisible(true)

	return nil
}
