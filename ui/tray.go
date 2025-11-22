//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/updater"
	"github.com/fosrl/windows/version"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

var (
	trayIcon        *walk.NotifyIcon
	contextMenu     *walk.Menu
	mainWindow      *walk.MainWindow
	availableUpdate *updater.UpdateFound
	updateMutex     sync.RWMutex
	updateAction    *walk.Action // Action for "Update Available" menu item
	labelAction     *walk.Action
	loginAction     *walk.Action
	connectAction   *walk.Action
	moreAction      *walk.Action
	quitAction      *walk.Action
)

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
	// Store references for update menu management
	mainWindow = mw

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
	labelAction = walk.NewAction()
	labelAction.SetText("milo@pangolin.net")
	labelAction.SetEnabled(false) // Gray out the text

	// Create Login action
	loginAction = walk.NewAction()
	loginAction.SetText("Login")
	loginAction.Triggered().Attach(func() {
		ShowLoginDialog(mw)
	})

	// Create Connect action (toggle button with checkmark)
	connectAction = walk.NewAction()
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

	// Create Check for Updates action
	updateAction := walk.NewAction()
	updateAction.SetText("Check for Updates")
	updateAction.Triggered().Attach(func() {
		go func() {
			logger.Info("Checking for updates...")
			logger.Info("Current version: %s", version.Number)
			update, err := updater.CheckForUpdate()
			if err != nil {
				logger.Error("Update check failed: %v", err)
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mw,
						Title:         "Update Check Failed",
						Content:       fmt.Sprintf("Failed to check for updates: %v", err),
						IconSystem:    walk.TaskDialogSystemIconError,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
				return
			}
			if update == nil {
				logger.Info("No update available")
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mw,
						Title:         "No Update Available",
						Content:       "You are running the latest version.",
						IconSystem:    walk.TaskDialogSystemIconInformation,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
				return
			}

			logger.Info("Update found: %s", update.Name())
			userAcceptedChan := make(chan bool, 1)
			walk.App().Synchronize(func() {
				td := walk.NewTaskDialog()
				opts := walk.TaskDialogOpts{
					Owner:         mw,
					Title:         "Update Available",
					Content:       fmt.Sprintf("A new version is available: %s\n\nWould you like to download and install it now?", update.Name()),
					IconSystem:    walk.TaskDialogSystemIconInformation,
					CommonButtons: win.TDCBF_YES_BUTTON | win.TDCBF_NO_BUTTON,
					DefaultButton: walk.TaskDialogDefaultButtonYes,
				}
				opts.CommonButtonClicked(win.TDCBF_YES_BUTTON).Attach(func() bool {
					userAcceptedChan <- true
					return true
				})
				opts.CommonButtonClicked(win.TDCBF_NO_BUTTON).Attach(func() bool {
					userAcceptedChan <- false
					return true
				})
				_, _ = td.Show(opts)
			})

			userAccepted := <-userAcceptedChan
			if !userAccepted {
				logger.Info("User declined update")
				return
			}

			// Start download and installation
			logger.Info("Starting update download...")
			progress := updater.DownloadVerifyAndExecute(0) // 0 = use SYSTEM token

			for dp := range progress {
				if dp.Error != nil {
					logger.Error("Update error: %v", dp.Error)
					walk.App().Synchronize(func() {
						td := walk.NewTaskDialog()
						_, _ = td.Show(walk.TaskDialogOpts{
							Owner:         mw,
							Title:         "Update Failed",
							Content:       fmt.Sprintf("Update failed: %v", dp.Error),
							IconSystem:    walk.TaskDialogSystemIconError,
							CommonButtons: win.TDCBF_OK_BUTTON,
						})
					})
					return
				}

				if len(dp.Activity) > 0 {
					logger.Info("Update: %s", dp.Activity)
				}

				if dp.BytesTotal > 0 {
					percent := float64(dp.BytesDownloaded) / float64(dp.BytesTotal) * 100
					logger.Info("Download progress: %.1f%% (%d/%d bytes)", percent, dp.BytesDownloaded, dp.BytesTotal)
				}

				if dp.Complete {
					logger.Info("Update complete! The application will restart.")
					walk.App().Synchronize(func() {
						td := walk.NewTaskDialog()
						_, _ = td.Show(walk.TaskDialogOpts{
							Owner:         mw,
							Title:         "Update Complete",
							Content:       "The update has been installed successfully. The application will now restart.",
							IconSystem:    walk.TaskDialogSystemIconInformation,
							CommonButtons: win.TDCBF_OK_BUTTON,
						})
					})
					// The MSI installer will handle the restart
					return
				}
			}
		}()
	})
	moreMenu.Actions().Add(updateAction)

	// Add version info at the bottom, grayed out
	versionAction := walk.NewAction()
	versionAction.SetText(fmt.Sprintf("Version %s", version.Number))
	versionAction.SetEnabled(false) // Gray out the text
	moreMenu.Actions().Add(versionAction)

	moreAction = walk.NewMenuAction(moreMenu)
	moreAction.SetText("More")

	// Create Quit action
	quitAction = walk.NewAction()
	quitAction.SetText("Quit")
	quitAction.Triggered().Attach(func() {
		walk.App().Exit(0)
	})

	// Initialize context menu and add all initial actions
	contextMenu = ni.ContextMenu()
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

// updateMenuWithAvailableUpdate adds or removes the "Update Available" menu item
// based on whether an update is available. Uses Insert/Remove like WireGuard does.
func updateMenuWithAvailableUpdate() {
	if contextMenu == nil {
		return
	}

	// Safely get the app instance - it might not be ready yet
	app := walk.App()
	if app == nil {
		logger.Error("Cannot update menu: walk.App() is nil (app not initialized)")
		return
	}

	updateMutex.RLock()
	hasUpdate := availableUpdate != nil
	updateMutex.RUnlock()

	// Use defer/recover to catch any panics from Synchronize
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in updateMenuWithAvailableUpdate: %v", r)
		}
	}()

	app.Synchronize(func() {
		// Recover from any panics that occur on the UI thread
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Panic in Synchronize callback (UI thread): %v", r)
			}
		}()

		actions := contextMenu.Actions()

		// Check if update action is already in the menu
		updateActionInMenu := false
		if updateAction != nil {
			for i := 0; i < actions.Len(); i++ {
				if actions.At(i) == updateAction {
					updateActionInMenu = true
					break
				}
			}
		}

		if hasUpdate {
			// Create update menu item if it doesn't exist
			if updateAction == nil {
				updateAction = walk.NewAction()
				updateAction.SetText("Update available")
				updateAction.Triggered().Attach(func() {
					// Run in goroutine to avoid blocking the UI thread
					go func() {
						updateMutex.RLock()
						update := availableUpdate
						updateMutex.RUnlock()
						if update == nil {
							return
						}

						// Show confirmation dialog
						userAcceptedChan := make(chan bool, 1)
						walk.App().Synchronize(func() {
							td := walk.NewTaskDialog()
							opts := walk.TaskDialogOpts{
								Owner:         mainWindow,
								Title:         "Update Available",
								Content:       fmt.Sprintf("A new version is available: %s\n\nWould you like to download and install it now?", update.Name()),
								IconSystem:    walk.TaskDialogSystemIconInformation,
								CommonButtons: win.TDCBF_YES_BUTTON | win.TDCBF_NO_BUTTON,
								DefaultButton: walk.TaskDialogDefaultButtonYes,
							}
							opts.CommonButtonClicked(win.TDCBF_YES_BUTTON).Attach(func() bool {
								userAcceptedChan <- true
								return true
							})
							opts.CommonButtonClicked(win.TDCBF_NO_BUTTON).Attach(func() bool {
								userAcceptedChan <- false
								return true
							})
							_, _ = td.Show(opts)
						})

						userAccepted := <-userAcceptedChan
						if !userAccepted {
							logger.Info("User declined update")
							return
						}

						// Start download and installation
						logger.Info("Starting update download...")
						progress := updater.DownloadVerifyAndExecute(0) // 0 = use SYSTEM token

						for dp := range progress {
							if dp.Error != nil {
								logger.Error("Update error: %v", dp.Error)
								walk.App().Synchronize(func() {
									td := walk.NewTaskDialog()
									_, _ = td.Show(walk.TaskDialogOpts{
										Owner:         mainWindow,
										Title:         "Update Failed",
										Content:       fmt.Sprintf("Update failed: %v", dp.Error),
										IconSystem:    walk.TaskDialogSystemIconError,
										CommonButtons: win.TDCBF_OK_BUTTON,
									})
								})
								return
							}

							if len(dp.Activity) > 0 {
								logger.Info("Update: %s", dp.Activity)
							}

							if dp.BytesTotal > 0 {
								percent := float64(dp.BytesDownloaded) / float64(dp.BytesTotal) * 100
								logger.Info("Download progress: %.1f%% (%d/%d bytes)", percent, dp.BytesDownloaded, dp.BytesTotal)
							}

							if dp.Complete {
								logger.Info("Update complete! The application will restart.")
								walk.App().Synchronize(func() {
									td := walk.NewTaskDialog()
									_, _ = td.Show(walk.TaskDialogOpts{
										Owner:         mainWindow,
										Title:         "Update Complete",
										Content:       "The update has been installed successfully. The application will now restart.",
										IconSystem:    walk.TaskDialogSystemIconInformation,
										CommonButtons: win.TDCBF_OK_BUTTON,
									})
								})
								// Clear the update after installation starts
								updateMutex.Lock()
								availableUpdate = nil
								updateMutex.Unlock()
								updateMenuWithAvailableUpdate()
								// The MSI installer will handle the restart
								return
							}
						}
					}()
				})
			} else {
				// Update the text if action already exists (keep it simple)
				updateAction.SetText("Update available")
			}

			// Insert update action if it's not already in the menu
			// Insert after connectAction (before moreAction)
			if !updateActionInMenu {
				// Find the index of moreAction to insert before it
				moreActionIndex := -1
				for i := 0; i < actions.Len(); i++ {
					if actions.At(i) == moreAction {
						moreActionIndex = i
						break
					}
				}
				if moreActionIndex >= 0 {
					actions.Insert(moreActionIndex, updateAction)
				} else {
					// Fallback: just add it
					actions.Add(updateAction)
				}
			}
		} else {
			// Remove update action if it exists in the menu
			if updateActionInMenu && updateAction != nil {
				actions.Remove(updateAction)
			}
			// Note: We don't set updateAction to nil here because we want to keep
			// the action object for potential reuse, just remove it from the menu
		}
	})
}

// StartBackgroundUpdateChecker starts a background update checker that periodically
// checks for updates and updates the menu when an update is found.
func StartBackgroundUpdateChecker(mw *walk.MainWindow, interval time.Duration) {
	updater.StartBackgroundUpdateChecker(interval, func(update *updater.UpdateFound) {
		// Use defer/recover to catch any panics
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Panic in update callback: %v", r)
			}
		}()

		// Store the update
		updateMutex.Lock()
		availableUpdate = update
		updateMutex.Unlock()

		// Update the menu to show the update item
		updateMenuWithAvailableUpdate()
	})
}
