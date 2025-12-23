//go:build windows

package ui

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/secrets"
	"github.com/fosrl/windows/tunnel"
	"github.com/fosrl/windows/ui/preferences"
	"github.com/fosrl/windows/updater"
	"github.com/fosrl/windows/version"

	"github.com/fosrl/newt/logger"
	browser "github.com/pkg/browser"
	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

var (
	trayIcon           *walk.NotifyIcon
	contextMenu        *walk.Menu
	mainWindow         *walk.MainWindow
	hasUpdate          bool
	updateMutex        sync.RWMutex
	startupDialogShown bool
	startupDialogMutex sync.Mutex
	updateAction       *walk.Action
	loadingAction      *walk.Action
	statusAction       *walk.Action
	connectAction      *walk.Action
	orgsMenuAction     *walk.Action
	accountMenuAction  *walk.Action
	loginAction        *walk.Action
	logoutAction       *walk.Action
	addAccountAction   *walk.Action
	moreAction         *walk.Action
	quitAction         *walk.Action
	updateFoundCb      *managers.UpdateFoundCallback
	updateProgressCb   *managers.UpdateProgressCallback
	managerStoppingCb  *managers.ManagerStoppingCallback
	isConnected        bool
	connectMutex       sync.RWMutex
	isLoggedOut        bool
	loggedOutMutex     sync.RWMutex
	currentTunnelState managers.TunnelState
	tunnelStateMutex   sync.RWMutex
	authManager        *auth.AuthManager
	configManager      *config.ConfigManager
	accountManager     *config.AccountManager
	apiClient          *api.APIClient
	tunnelManager      *tunnel.Manager
	orgMenu            *walk.Menu
	accountMenu        *walk.Menu
	moreMenu           *walk.Menu
	orgActions         map[string]*walk.Action
	accountActions     map[string]*walk.Action
	noOrgsAction       *walk.Action
	noAccountsAction   *walk.Action
	menuUpdateMutex    sync.Mutex
)

// updateTrayTooltip updates the tray icon tooltip to show the current tunnel state
func updateTrayTooltip(state tunnel.State) {
	if trayIcon == nil {
		return
	}

	stateText := state.DisplayText()
	tooltipText := fmt.Sprintf("%s: %s", config.AppName, stateText)
	if err := trayIcon.SetToolTip(tooltipText); err != nil {
		logger.Error("Failed to set tray tooltip: %v", err)
	}
}

// setTrayIconForState sets the tray icon based on tunnel state, with overlay for transitional states
func setTrayIconForState(state tunnel.State) {
	if trayIcon == nil {
		return
	}

	// For simple states (stopped/running), use icon directly to avoid conversion artifacts
	if state == tunnel.StateStopped || state == tunnel.StateRunning {
		var iconName string
		if state == tunnel.StateRunning {
			iconName = "icon-orange.ico"
		} else {
			iconName = "icon-gray.ico"
		}
		iconPath := filepath.Join(config.GetIconsPath(), iconName)
		icon, err := walk.NewIconFromFile(iconPath)
		if err != nil {
			logger.Error("Failed to load icon from %s: %v", iconPath, err)
			return
		}
		if err := trayIcon.SetIcon(icon); err != nil {
			logger.Error("Failed to set tray icon: %v", err)
		}
		return
	}

	// For transitional states, use icon provider with overlay
	icon, err := iconWithOverlayForState(state, 16)
	if err != nil {
		logger.Error("Failed to create icon for state %s: %v", state.String(), err)
		// Fallback to gray icon
		iconPath := filepath.Join(config.GetIconsPath(), "icon-gray.ico")
		fallbackIcon, err := walk.NewIconFromFile(iconPath)
		if err != nil {
			logger.Error("Failed to load fallback icon from %s: %v", iconPath, err)
			return
		}
		if err := trayIcon.SetIcon(fallbackIcon); err != nil {
			logger.Error("Failed to set tray icon: %v", err)
		}
		return
	}

	// SetIcon accepts walk.Image for composite icons with overlay
	if err := trayIcon.SetIcon(icon); err != nil {
		logger.Error("Failed to set tray icon: %v", err)
	}
}

// openURL opens a URL in the default browser
func openURL(url string) {
	browser.OpenURL(url)
}

// handleMenuOpen verifies session and refreshes organizations when menu opens
func handleMenuOpen() {
	if authManager == nil || apiClient == nil {
		return
	}

	// Only handle if authenticated
	if !authManager.IsAuthenticated() {
		return
	}

	// Run in background goroutine to avoid blocking menu
	go func() {
		// First, try to get the user to verify session is still valid
		user, err := apiClient.GetUser()
		if err != nil {
			// If getting user fails, mark as logged out
			loggedOutMutex.Lock()
			isLoggedOut = true
			loggedOutMutex.Unlock()
			// Update menu to reflect logged out state
			updateMenu()
			return
		}

		// If successful, update user and clear logged out state
		authManager.UpdateCurrentUser(user)
		loggedOutMutex.Lock()
		isLoggedOut = false
		loggedOutMutex.Unlock()

		// Update menu to reflect updated state
		updateMenu()

		// Refresh organizations in background
		if authManager.IsAuthenticated() {
			if err := authManager.RefreshOrganizations(); err != nil {
				logger.Error("Failed to refresh organizations: %v", err)
			} else {
				// Update menu again after orgs refresh
				updateMenu()
			}
		}
	}()
}

// setupMenu creates the menu structure once
func setupMenu() error {
	if contextMenu == nil {
		return fmt.Errorf("context menu not initialized")
	}

	actions := contextMenu.Actions()

	// Create update action (initially hidden)
	updateAction = walk.NewAction()
	updateAction.SetText("Update Available")
	updateAction.SetVisible(false) // Hidden initially
	updateAction.Triggered().Attach(func() {
		go triggerUpdate(mainWindow)
	})
	actions.Add(updateAction)

	// Create loading action
	loadingAction = walk.NewAction()
	loadingAction.SetText("Loading...")
	loadingAction.SetEnabled(false)
	actions.Add(loadingAction)

	// Create status action
	statusAction = walk.NewAction()
	statusAction.SetText("Disconnected")
	statusAction.SetEnabled(false)
	statusAction.SetVisible(false) // Hidden initially
	actions.Add(statusAction)

	// Create connect action
	connectAction = walk.NewAction()
	connectAction.SetText("Connect")
	connectAction.SetVisible(false) // Hidden initially
	connectAction.Triggered().Attach(func() {
		go func() {
			if tunnelManager == nil {
				logger.Error("Tunnel manager not initialized")
				// Show error dialog to user
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mainWindow,
						Title:         "Connection Error",
						Content:       "Tunnel manager is not initialized. Please restart the application.",
						IconSystem:    walk.TaskDialogSystemIconError,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
				return
			}

			// Get current state to determine action
			currentState := tunnelManager.State()

			// Allow disconnect for any state other than Stopped or Stopping
			// This allows users to cancel the connection process at any time
			if currentState != tunnel.StateStopped && currentState != tunnel.StateStopping {
				// Disconnect (or cancel connection)
				logger.Info("Disconnecting...")
				err := tunnelManager.Disconnect()
				if err != nil {
					logger.Error("Failed to stop tunnel: %v", err)
					// Show error dialog to user
					walk.App().Synchronize(func() {
						var title, message string

						// Check if it's a ConnectionError with formatted title/message
						if connErr, ok := err.(*tunnel.ConnectionError); ok {
							title = connErr.Title
							message = connErr.Message
						} else {
							// Fallback to generic error
							title = "Disconnect Failed"
							message = err.Error()
						}

						td := walk.NewTaskDialog()
						_, _ = td.Show(walk.TaskDialogOpts{
							Owner:         mainWindow,
							Title:         title,
							Content:       message,
							IconSystem:    walk.TaskDialogSystemIconError,
							CommonButtons: win.TDCBF_OK_BUTTON,
						})
					})
				}
			} else if currentState == tunnel.StateStopped {
				// Connect
				err := tunnelManager.Connect()
				if err != nil {
					logger.Error("Failed to start tunnel: %v", err)
					// Show error dialog to user
					walk.App().Synchronize(func() {
						var title, message string

						// Check if it's a ConnectionError with formatted title/message
						if connErr, ok := err.(*tunnel.ConnectionError); ok {
							title = connErr.Title
							message = connErr.Message
						} else {
							// Fallback to generic error
							title = "Connection Failed"
							message = err.Error()
						}

						td := walk.NewTaskDialog()
						_, _ = td.Show(walk.TaskDialogOpts{
							Owner:         mainWindow,
							Title:         title,
							Content:       message,
							IconSystem:    walk.TaskDialogSystemIconError,
							CommonButtons: win.TDCBF_OK_BUTTON,
						})
					})
				}
			}
			// If state is Stopping, do nothing (button should be disabled)
		}()
	})
	actions.Add(connectAction)

	actions.Add(walk.NewSeparatorAction())

	// Create account selector menu
	var err error
	accountMenu, err = walk.NewMenu()
	if err != nil {
		logger.Error("Failed to create org menu: %v", err)
		return err
	}
	accountMenuAction = walk.NewMenuAction(accountMenu)
	accountMenuAction.SetText("Accounts")
	accountMenuAction.SetVisible(false) // Hidden initially
	actions.Add(accountMenuAction)

	// Create organizations menu
	orgMenu, err = walk.NewMenu()
	if err != nil {
		logger.Error("Failed to create org menu: %v", err)
		return err
	}
	orgsMenuAction = walk.NewMenuAction(orgMenu)
	orgsMenuAction.SetText("Organizations")
	orgsMenuAction.SetVisible(false) // Hidden initially
	actions.Add(orgsMenuAction)

	// Separator before login
	actions.Add(walk.NewSeparatorAction())

	// Create login action (only when no accounts are available)
	loginAction = walk.NewAction()
	loginAction.SetText("Login to account")
	loginAction.Triggered().Attach(func() {
		ShowLoginDialog(mainWindow, authManager, configManager, accountManager, apiClient, tunnelManager)
		// Update menu after dialog closes (login may have succeeded)
		time.Sleep(100 * time.Millisecond) // Small delay to let auth state update
		updateMenu()
	})
	actions.Add(loginAction)

	// Separator before More
	actions.Add(walk.NewSeparatorAction())

	// Create More submenu
	moreMenu, err = walk.NewMenu()
	if err != nil {
		logger.Error("Failed to create more menu: %v", err)
		return err
	}

	// Support section
	supportLabel := walk.NewAction()
	supportLabel.SetText("Support")
	supportLabel.SetEnabled(false)
	moreMenu.Actions().Add(supportLabel)

	howItWorksAction := walk.NewAction()
	howItWorksAction.SetText("How Pangolin Works")
	howItWorksAction.Triggered().Attach(func() {
		openURL("https://docs.pangolin.net/about/how-pangolin-works")
	})
	moreMenu.Actions().Add(howItWorksAction)

	docAction := walk.NewAction()
	docAction.SetText("Documentation")
	docAction.Triggered().Attach(func() {
		openURL("https://docs.pangolin.net/")
	})
	moreMenu.Actions().Add(docAction)

	moreMenu.Actions().Add(walk.NewSeparatorAction())

	// Copyright
	copyrightText := fmt.Sprintf("© %d Fossorial, Inc.", time.Now().Year())
	copyrightAction := walk.NewAction()
	copyrightAction.SetText(copyrightText)
	copyrightAction.SetEnabled(false)
	moreMenu.Actions().Add(copyrightAction)

	termsAction := walk.NewAction()
	termsAction.SetText("Terms of Service")
	termsAction.Triggered().Attach(func() {
		openURL("https://pangolin.net/terms-of-service.html")
	})
	moreMenu.Actions().Add(termsAction)

	privacyAction := walk.NewAction()
	privacyAction.SetText("Privacy Policy")
	privacyAction.Triggered().Attach(func() {
		openURL("https://pangolin.net/privacy-policy.html")
	})
	moreMenu.Actions().Add(privacyAction)

	moreMenu.Actions().Add(walk.NewSeparatorAction())

	// Version information
	versionAction := walk.NewAction()
	versionAction.SetText(fmt.Sprintf("Version: %s", version.Number))
	versionAction.SetEnabled(false)
	moreMenu.Actions().Add(versionAction)

	// Check for Updates action
	checkUpdateAction := walk.NewAction()
	checkUpdateAction.SetText("Check for Updates")
	checkUpdateAction.Triggered().Attach(func() {
		go func() {
			logger.Info("Checking for updates via manager...")
			logger.Info("Current version: %s", version.Number)

			// Check update state via manager IPC
			updateState, err := managers.IPCClientUpdateState()
			if err != nil {
				logger.Error("Update check failed: %v", err)
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mainWindow,
						Title:         "Update Check Failed",
						Content:       fmt.Sprintf("Failed to check for updates: %v", err),
						IconSystem:    walk.TaskDialogSystemIconError,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
				return
			}

			switch updateState {
			case managers.UpdateStateFoundUpdate:
				logger.Info("Update available")
				// Trigger the update
				triggerUpdate(mainWindow)
			case managers.UpdateStateUpdatesDisabledUnofficialBuild:
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mainWindow,
						Title:         "Updates Disabled",
						Content:       "Updates are disabled for unofficial builds.",
						IconSystem:    walk.TaskDialogSystemIconInformation,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
			default:
				logger.Info("No update available")
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mainWindow,
						Title:         "No Update Available",
						Content:       "You are running the latest version.",
						IconSystem:    walk.TaskDialogSystemIconInformation,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
			}
		}()
	})
	moreMenu.Actions().Add(checkUpdateAction)

	// Preferences action
	preferencesAction := walk.NewAction()
	preferencesAction.SetText("Preferences")
	preferencesAction.Triggered().Attach(func() {
		go func() {
			walk.App().Synchronize(func() {
				if err := preferences.ShowPreferencesWindow(mainWindow, tunnelManager, configManager, trayIcon); err != nil {
					logger.Error("Failed to show preferences window: %v", err)
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mainWindow,
						Title:         "Error",
						Content:       fmt.Sprintf("Failed to open preferences window: %v", err),
						IconSystem:    walk.TaskDialogSystemIconError,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				}
			})
		}()
	})
	moreMenu.Actions().Add(preferencesAction)

	moreAction = walk.NewMenuAction(moreMenu)
	moreAction.SetText("More")
	actions.Add(moreAction)

	// Separator before Quit
	actions.Add(walk.NewSeparatorAction())

	// Create quit action
	quitAction = walk.NewAction()
	quitAction.SetText("Quit")
	quitAction.Triggered().Attach(func() {
		// Try to quit the manager service (stops tunnels and quits manager)
		go func() {
			alreadyQuit, err := managers.IPCClientQuit(true) // true = stop tunnels on quit
			if err != nil {
				logger.Error("Failed to quit manager service: %v", err)
				// Show error dialog to user
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mainWindow,
						Title:         "Quit Failed",
						Content:       fmt.Sprintf("Failed to quit manager service: %v", err),
						IconSystem:    walk.TaskDialogSystemIconError,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
					// Still try to exit even if quit failed
					walk.App().Exit(0)
				})
				return
			} else if alreadyQuit {
				logger.Info("Manager service already quitting")
			} else {
				logger.Info("Manager service quit requested")
			}
			// Exit the UI after the quit request has been sent and acknowledged
			walk.App().Synchronize(func() {
				walk.App().Exit(0)
			})
		}()
	})
	actions.Add(quitAction)

	// Initialize org actions map
	orgActions = make(map[string]*walk.Action)
	accountActions = make(map[string]*walk.Action)

	// Initial update to set correct visibility and text
	updateMenu()

	return nil
}

// updateMenu updates all menu items based on current state
func updateMenu() {
	if contextMenu == nil {
		return
	}

	app := walk.App()
	if app == nil {
		return
	}

	menuUpdateMutex.Lock()
	defer menuUpdateMutex.Unlock()

	app.Synchronize(func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Panic in updateMenu: %v", r)
			}
		}()

		// Check auth state
		isInitializing := authManager != nil && authManager.IsInitializing()
		isAuthenticated := authManager != nil && authManager.IsAuthenticated()
		loggedOutMutex.RLock()
		isLoggedOutLocal := isLoggedOut
		loggedOutMutex.RUnlock()

		// Check if user info exists locally (from current user or config)
		hasLocalUserInfo := false
		if authManager != nil {
			user := authManager.CurrentUser()
			if user != nil && user.Email != "" {
				hasLocalUserInfo = true
			}
		}

		if !hasLocalUserInfo && accountManager != nil {
			activeAccount, _ := accountManager.ActiveAccount()
			if activeAccount != nil {
				hasLocalUserInfo = true
			}
		}

		// Update loading state
		if loadingAction != nil {
			loadingAction.SetVisible(isInitializing)
		}

		// Update authenticated section visibility
		// Show full auth section if: authenticated and not logged out
		showAuthSection := isAuthenticated && !isLoggedOutLocal && !isInitializing

		if statusAction != nil {
			statusAction.SetVisible(showAuthSection)
		}
		if connectAction != nil {
			connectAction.SetVisible(showAuthSection)
		}
		if orgsMenuAction != nil {
			orgsMenuAction.SetVisible(showAuthSection)
		}

		// Update tunnel state and organizations only when fully authenticated
		if showAuthSection {
			updateTunnelState()
			updateOrganizations()
		}

		updateAccountMenu()
		updateLoginAction()

		// Update update action visibility
		updateMutex.RLock()
		hasUpdateLocal := hasUpdate
		updateMutex.RUnlock()
		if updateAction != nil {
			updateAction.SetVisible(hasUpdateLocal)
		}
	})
}

// updateTunnelState updates the tunnel status and connect button
func updateTunnelState() {
	if statusAction == nil || connectAction == nil {
		return
	}

	var state tunnel.State
	if tunnelManager != nil {
		state = tunnelManager.State()
	} else {
		tunnelStateMutex.RLock()
		state = tunnel.State(currentTunnelState)
		tunnelStateMutex.RUnlock()
	}

	statusAction.SetText(state.DisplayText())

	var connected bool
	if tunnelManager != nil {
		connected = tunnelManager.IsConnected()
	} else {
		connectMutex.RLock()
		connected = isConnected
		connectMutex.RUnlock()
	}

	// Show "Disconnect" for any state other than Stopped or Stopping
	// This allows users to cancel the connection process at any time
	connectText := "Connect"
	if state == tunnel.StateStopping {
		connectText = "Disconnecting..."
		connectAction.SetEnabled(false) // Disable during disconnection
	} else if state != tunnel.StateStopped {
		connectText = "Disconnect"
		connectAction.SetEnabled(true) // Enable to allow cancellation
	} else {
		connectText = "Connect"
		connectAction.SetEnabled(true)
	}
	connectAction.SetText(connectText)
	// Set checked state based on whether we're connected or in a connecting state
	connectAction.SetChecked(state == tunnel.StateRunning || connected)
}

func updateAccountMenu() {
	if accountMenu == nil || accountMenuAction == nil || accountManager == nil {
		return
	}

	accounts := accountManager.Accounts
	currentAccount, _ := accountManager.ActiveAccount()

	var state tunnel.State
	if tunnelManager != nil {
		state = tunnelManager.State()
	} else {
		tunnelStateMutex.RLock()
		state = tunnel.State(currentTunnelState)
		tunnelStateMutex.RUnlock()
	}
	shouldDisable := state == tunnel.StateStarting || state == tunnel.StateRegistering || state == tunnel.StateRegistered || state == tunnel.StateStopping

	actions := accountMenu.Actions()
	hasMenuTitle := false
	hasSeparator := false
	if actions.Len() > 0 {
		firstAction := actions.At(0)
		if firstAction.Text() != "" && !firstAction.Enabled() {
			hasMenuTitle = true
		}
	}
	if actions.Len() > 1 {
		secondAction := actions.At(1)
		if secondAction.Text() == "" {
			hasSeparator = true
		}
	}

	var accountSubmenuTitleAction *walk.Action
	if !hasMenuTitle {
		accountSubmenuTitleAction = walk.NewAction()
		accountSubmenuTitleAction.SetEnabled(false)
		accountSubmenuTitleAction.SetText("Available Accounts")
		accountSubmenuTitleAction.SetVisible(true)
		actions.Insert(0, accountSubmenuTitleAction)
	} else {
		accountSubmenuTitleAction = actions.At(0)
	}

	if !hasSeparator {
		separator := walk.NewSeparatorAction()
		actions.Insert(1, separator)
	}

	for accountID, action := range accountActions {
		if _, ok := accountManager.Accounts[accountID]; !ok {
			actions.Remove(action)
			delete(accountActions, accountID)
		}
	}

	// Handle "No accounts" message when there are no accounts.
	// This should not be reachable, but in case this does happen,
	// it's handled here.
	if len(accounts) == 0 {
		// Add "No organizations" action if it doesn't exist
		if noAccountsAction == nil {
			noAccountsAction = walk.NewAction()
			noAccountsAction.SetText("No accounts")
			noAccountsAction.SetEnabled(false)
			// Insert after separator (index 2: count label at 0, separator at 1)
			actions.Insert(2, noAccountsAction)
		}
		noAccountsAction.SetVisible(true)
	} else {
		// Remove "No accounts" action if it exists
		if noAccountsAction != nil {
			actions.Remove(noAccountsAction)
			noAccountsAction = nil
		}
	}

	// Figure out whether to display the hostname
	// for a particular account email by if there
	// are more than 1 accounts with the same email.
	emailCounts := map[string]int{}
	for _, account := range accounts {
		emailCounts[account.Email]++
	}

	// Update or add orgs
	for _, account := range accounts {
		action, exists := accountActions[account.UserID]
		if !exists {
			// Create new action
			action = walk.NewAction()

			var accountText string
			if emailCounts[account.Email] > 1 {
				accountText = fmt.Sprintf("%s (%s)", account.Email, account.Hostname)
			} else {
				accountText = account.Email
			}

			action.SetText(accountText)
			action.SetCheckable(true)

			action.Triggered().Attach(func() {
				go func() {
					account := account

					// Shut down tunnel here. Switching users requires the tunnel must go
					// down.
					logger.Info("Stopping tunnel before switching accounts")
					if err := managers.IPCClientStopTunnel(); err != nil {
						logger.Error("Failed to shut down tunnel before switch: %v", err)
						walk.App().Synchronize(func() {
							td := walk.NewTaskDialog()
							_, _ = td.Show(walk.TaskDialogOpts{
								Owner:         mainWindow,
								Title:         "Tunnel Shutdown Failed",
								Content:       fmt.Sprintf("Failed to shut down tunnel before switching accounts: %v", err),
								IconSystem:    walk.TaskDialogSystemIconError,
								CommonButtons: win.TDCBF_OK_BUTTON,
							})
						})
						updateMenu()
						return
					}

					// After shutting down the tunnel, switch accounts in the auth manager.
					if err := authManager.SwitchAccount(account.UserID); err != nil {
						logger.Error("Failed to select organization: %v", err)
						// Show error dialog to user
						walk.App().Synchronize(func() {
							td := walk.NewTaskDialog()
							_, _ = td.Show(walk.TaskDialogOpts{
								Owner:         mainWindow,
								Title:         "Switching Account Failed",
								Content:       fmt.Sprintf("Failed to switch account: %v", err),
								IconSystem:    walk.TaskDialogSystemIconError,
								CommonButtons: win.TDCBF_OK_BUTTON,
							})
						})
						updateMenu()
						return
					}

					updateMenu()
				}()
			})
			accountActions[account.UserID] = action

			// Insert after separator (index 2: count label at 0, separator at 1)
			actions.Insert(2, action)
		} else {
			// Update existing action
			var accountText string
			if emailCounts[account.Email] > 1 {
				accountText = fmt.Sprintf("%s (%s)", account.Email, account.Hostname)
			} else {
				accountText = account.Email
			}
			action.SetText(accountText)
		}

		// Update checked state
		action.SetChecked(currentAccount != nil && account.UserID == currentAccount.UserID)
		action.SetEnabled(!shouldDisable)
	}

	if addAccountAction == nil {
		actions.Add(walk.NewSeparatorAction())
		addAccountAction = walk.NewAction()
		addAccountAction.SetText("Add Account")
		addAccountAction.Triggered().Attach(func() {
			go func() {
				walk.App().Synchronize(func() {
					// Show login dialog
					ShowLoginDialog(mainWindow, authManager, configManager, accountManager, apiClient, tunnelManager)
					// Small delay to allow state to update
					time.Sleep(100 * time.Millisecond)
					updateMenu()
				})
			}()
		})
		actions.Add(addAccountAction)
	}
	addAccountAction.SetVisible(true)

	// Create logout action
	if logoutAction == nil {
		logoutAction = walk.NewAction()
		logoutAction.SetText("Logout")
		logoutAction.SetVisible(false) // Initially hidden
		logoutAction.Triggered().Attach(func() {
			go func() {
				// Always stop any running tunnel before logout
				logger.Info("Stopping tunnel before logout")
				if err := managers.IPCClientStopTunnel(); err != nil {
					logger.Error("Failed to stop tunnel before logout: %v", err)
					// Continue with logout even if stopping tunnel fails
				}

				if err := authManager.Logout(); err != nil {
					logger.Error("Failed to logout: %v", err)
					// Show error dialog to user
					walk.App().Synchronize(func() {
						td := walk.NewTaskDialog()
						_, _ = td.Show(walk.TaskDialogOpts{
							Owner:         mainWindow,
							Title:         "Logout Failed",
							Content:       fmt.Sprintf("Failed to logout: %v", err),
							IconSystem:    walk.TaskDialogSystemIconError,
							CommonButtons: win.TDCBF_OK_BUTTON,
						})
					})
				}
				updateMenu()
			}()
		})
		actions.Add(logoutAction)
	}
	logoutAction.SetVisible(currentAccount != nil)

	// Update accounts menu action text
	accountMenuActionText := "Select Account"
	if currentAccount != nil {
		if emailCounts[currentAccount.Email] > 1 {
			accountMenuActionText = fmt.Sprintf("%s (%s)", currentAccount.Email, currentAccount.Hostname)
		} else {
			accountMenuActionText = currentAccount.Email
		}
	}
	accountMenuAction.SetText(accountMenuActionText)
	accountMenuAction.SetVisible(len(accounts) > 0)
}

// updateOrganizations updates the organizations menu
func updateOrganizations() {
	if orgMenu == nil || orgsMenuAction == nil || authManager == nil {
		return
	}

	orgs := authManager.Organizations()
	currentOrg := authManager.CurrentOrg()
	currentOrgId := ""
	if currentOrg != nil {
		currentOrgId = currentOrg.Id
	}

	// Get tunnel state to determine if org buttons should be disabled
	var state tunnel.State
	if tunnelManager != nil {
		state = tunnelManager.State()
	} else {
		tunnelStateMutex.RLock()
		state = tunnel.State(currentTunnelState)
		tunnelStateMutex.RUnlock()
	}
	shouldDisable := state == tunnel.StateStarting || state == tunnel.StateRegistering || state == tunnel.StateRegistered || state == tunnel.StateStopping

	// Ensure org count label and separator exist
	actions := orgMenu.Actions()
	hasCountLabel := false
	hasSeparator := false
	if actions.Len() > 0 {
		firstAction := actions.At(0)
		if firstAction.Text() != "" && !firstAction.Enabled() {
			hasCountLabel = true
		}
	}
	if actions.Len() > 1 {
		secondAction := actions.At(1)
		if secondAction.Text() == "" {
			hasSeparator = true
		}
	}

	var orgCountAction *walk.Action
	if !hasCountLabel {
		orgCountAction = walk.NewAction()
		orgCountAction.SetEnabled(false)
		actions.Insert(0, orgCountAction)
	} else {
		orgCountAction = actions.At(0)
	}

	if !hasSeparator {
		separator := walk.NewSeparatorAction()
		actions.Insert(1, separator)
	}

	// Update org count label
	orgCountText := fmt.Sprintf("%d Organization", len(orgs))
	if len(orgs) != 1 {
		orgCountText += "s"
	}
	orgCountAction.SetText(orgCountText)
	orgCountAction.SetVisible(true) // Always show count, even when 0

	// Create set of current org IDs
	orgSet := make(map[string]bool)
	for _, org := range orgs {
		orgSet[org.Id] = true
	}

	// Remove orgs that no longer exist (skip count label at 0 and separator at 1)
	for orgId, action := range orgActions {
		if !orgSet[orgId] {
			actions.Remove(action)
			delete(orgActions, orgId)
		}
	}

	// Handle "No organizations" message when there are no orgs
	if len(orgs) == 0 {
		// Add "No organizations" action if it doesn't exist
		if noOrgsAction == nil {
			noOrgsAction = walk.NewAction()
			noOrgsAction.SetText("No organizations")
			noOrgsAction.SetEnabled(false)
			// Insert after separator (index 2: count label at 0, separator at 1)
			actions.Insert(2, noOrgsAction)
		}
		noOrgsAction.SetVisible(true)
	} else {
		// Remove "No organizations" action if it exists
		if noOrgsAction != nil {
			actions.Remove(noOrgsAction)
			noOrgsAction = nil
		}
	}

	// Update or add orgs
	for _, org := range orgs {
		action, exists := orgActions[org.Id]
		if !exists {
			// Create new action
			action = walk.NewAction()
			action.SetText(org.Name)
			action.SetCheckable(true)
			action.Triggered().Attach(func() {
				org := org

				go func() {
					if err := authManager.SelectOrganization(&org); err != nil {
						logger.Error("Failed to select organization: %v", err)
						// Show error dialog to user
						walk.App().Synchronize(func() {
							td := walk.NewTaskDialog()
							_, _ = td.Show(walk.TaskDialogOpts{
								Owner:         mainWindow,
								Title:         "Organization Selection Failed",
								Content:       fmt.Sprintf("Failed to select organization: %v", err),
								IconSystem:    walk.TaskDialogSystemIconError,
								CommonButtons: win.TDCBF_OK_BUTTON,
							})
						})
					} else {
						updateMenu()

						if tunnelManager.IsConnected() {
							if err := tunnelManager.SwitchOLMOrg(org.Id); err != nil {
								logger.Error("Failed to switch tunnel organization: %v", err)
								// Show error dialog to user
								walk.App().Synchronize(func() {
									td := walk.NewTaskDialog()
									_, _ = td.Show(walk.TaskDialogOpts{
										Owner:         mainWindow,
										Title:         "Tunnel Organization Switch Failed",
										Content:       fmt.Sprintf("Failed to switch tunnel organization: %v", err),
										IconSystem:    walk.TaskDialogSystemIconError,
										CommonButtons: win.TDCBF_OK_BUTTON,
									})
								})
							}
						}
					}
				}()
			})
			orgActions[org.Id] = action

			// Insert after separator (index 2: count label at 0, separator at 1)
			actions.Insert(2, action)
		} else {
			// Update existing action
			action.SetText(org.Name)
		}

		// Update checked state
		action.SetChecked(currentOrgId != "" && org.Id == currentOrgId)
		action.SetEnabled(!shouldDisable)
	}

	// Update orgs menu action text
	currentOrgName := "Organizations"
	if currentOrg != nil {
		currentOrgName = currentOrg.Name
	}
	orgsMenuAction.SetText(currentOrgName)
	// Always show menu when authenticated (visibility controlled by updateMenu based on auth state)
}

// updateLoginAction updates the login button text and enabled state
func updateLoginAction() {
	if loginAction == nil || authManager == nil || accountManager == nil {
		return
	}

	isAuthenticated := authManager.IsAuthenticated()

	if isAuthenticated {
		activeAccount, _ := accountManager.ActiveAccount()
		if activeAccount != nil {
			loginAction.SetText(activeAccount.Email)
		} else {
			loginAction.SetText("Select Account")
		}
	} else {
		loginAction.SetText("Login to Account")
	}

	loginAction.SetVisible(len(accountManager.Accounts) == 0)
}

func SetupTray(
	mw *walk.MainWindow,
	am *auth.AuthManager,
	cm *config.ConfigManager,
	accm *config.AccountManager,
	ac *api.APIClient,
	sm *secrets.SecretManager,
) error {
	// Store references for update menu management
	mainWindow = mw
	authManager = am
	configManager = cm
	apiClient = ac
	accountManager = accm

	// Initialize tunnel manager with IPC adapter
	ipcAdapter := managers.NewIPCAdapter()
	tunnelManager = tunnel.NewManager(am, cm, accm, sm, ipcAdapter)

	// Create NotifyIcon
	ni, err := walk.NewNotifyIcon()
	if err != nil {
		return err
	}
	trayIcon = ni // Store reference for icon updates

	// Load default gray icon (disconnected state)
	setTrayIconForState(tunnel.StateStopped)

	// Set initial tooltip
	updateTrayTooltip(tunnel.StateStopped)

	// Initialize context menu
	contextMenu = ni.ContextMenu()

	// Setup menu structure once
	if err := setupMenu(); err != nil {
		logger.Error("Failed to setup menu: %v", err)
		return err
	}

	// Handle left-click to show popup menu using Windows API
	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			// Handle menu open - verify session and refresh orgs
			handleMenuOpen()

			// Get cursor position
			var pt win.POINT
			win.GetCursorPos(&pt)

			// Get the menu handle from the context menu using unsafe
			// The Menu struct should have an hMenu field as the first field
			menuPtr := (*struct {
				hMenu win.HMENU
			})(unsafe.Pointer(contextMenu))

			if menuPtr.hMenu != 0 {
				// Update menu before showing (in case state changed)
				updateMenu()

				// Show the menu using TrackPopupMenu
				// TrackPopupMenu automatically closes when clicking away
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

	// Register for update notifications from manager (if connected via IPC)
	// These callbacks will be called when the manager finds updates or makes progress
	updateFoundCb = managers.IPCClientRegisterUpdateFound(func(updateState managers.UpdateState) {
		if updateState == managers.UpdateStateFoundUpdate {
			updateMutex.Lock()
			hasUpdate = true
			updateMutex.Unlock()
			updateMenu()
		} else {
			updateMutex.Lock()
			hasUpdate = false
			updateMutex.Unlock()
			updateMenu()
		}
	})

	// Register for manager stopping notification
	managerStoppingCb = managers.IPCClientRegisterManagerStopping(func() {
		logger.Info("Manager service is stopping, exiting UI")
		walk.App().Synchronize(func() {
			walk.App().Exit(0)
		})
	})

	updateProgressCb = managers.IPCClientRegisterUpdateProgress(func(dp updater.DownloadProgress) {
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
			// Clear the update after installation starts
			updateMutex.Lock()
			hasUpdate = false
			updateMutex.Unlock()
			updateMenu()
			// The MSI installer will handle the restart
		}
	})

	// Check initial update state on startup
	// If an update is found on startup, show a dialog prompting the user to update
	// This only happens on startup - later checks via callback will only update the menu
	go func() {
		// Check immediately first (in case update was already found)
		updateState, err := managers.IPCClientUpdateState()
		if err == nil && updateState == managers.UpdateStateFoundUpdate {
			updateMutex.Lock()
			hasUpdate = true
			updateMutex.Unlock()
			updateMenu()
			// Show dialog on startup if update is available (only once)
			startupDialogMutex.Lock()
			if !startupDialogShown {
				startupDialogShown = true
				startupDialogMutex.Unlock()
				triggerUpdate(mainWindow)
			} else {
				startupDialogMutex.Unlock()
			}
			return
		}
		// If no update found yet, wait a bit for the initial check to complete
		// (the manager service's checkForUpdates might still be running)
		time.Sleep(3 * time.Second)
		updateState, err = managers.IPCClientUpdateState()
		if err == nil && updateState == managers.UpdateStateFoundUpdate {
			updateMutex.Lock()
			hasUpdate = true
			updateMutex.Unlock()
			updateMenu()
			// Show dialog on startup if update is available (only once)
			startupDialogMutex.Lock()
			if !startupDialogShown {
				startupDialogShown = true
				startupDialogMutex.Unlock()
				triggerUpdate(mainWindow)
			} else {
				startupDialogMutex.Unlock()
			}
		}
	}()

	// Register for tunnel state change notifications via tunnel manager
	tunnelManager.RegisterStateChangeCallback(func(state tunnel.State) {
		logger.Info("Tunnel state changed: %s", state.String())
		tunnelStateMutex.Lock()
		currentTunnelState = managers.TunnelState(state)
		tunnelStateMutex.Unlock()

		walk.App().Synchronize(func() {
			// Update connection state
			switch state {
			case tunnel.StateRunning:
				connectMutex.Lock()
				isConnected = true
				connectMutex.Unlock()
			case tunnel.StateStopped:
				connectMutex.Lock()
				isConnected = false
				connectMutex.Unlock()
			}

			// Update tray icon for all states (including transitional)
			setTrayIconForState(state)

			// Update tooltip with current state
			updateTrayTooltip(state)

			// Update menu to update status text and connect button
			updateMenu()
		})
	})

	// Monitor auth state changes to rebuild menu
	go func() {
		// Initial state
		lastAuthState := authManager != nil && authManager.IsAuthenticated()
		lastInitializing := authManager != nil && authManager.IsInitializing()

		for {
			time.Sleep(500 * time.Millisecond)
			if authManager == nil {
				continue
			}

			currentAuthState := authManager.IsAuthenticated()
			currentInitializing := authManager.IsInitializing()

			if currentAuthState != lastAuthState || currentInitializing != lastInitializing {
				lastAuthState = currentAuthState
				lastInitializing = currentInitializing
				updateMenu()
			}
		}
	}()

	// Background refresh loop - DISABLED for now
	/*
		go func() {
			// Initial delay before first refresh (with jitter)
			initialJitter := time.Duration(rand.Intn(7000)) * time.Millisecond
			time.Sleep(initialJitter)

			for {
				if authManager == nil {
					time.Sleep(180 * time.Second)
					continue
				}

				// Only refresh if authenticated
				if authManager.IsAuthenticated() {
					// Get OLM ID
					olmId, found := authManager.GetOlmId()
					if found && olmId != "" {
						// Refresh from MyDevice
						err := authManager.RefreshFromMyDevice(olmId)
						if err != nil {
							logger.Error("Failed to refresh from MyDevice: %v", err)
						} else {
							// Update menu to reflect updated orgs
							updateMenu()
						}
					}
				}

				// Check if unauthenticated after refresh (refresh might result in logout)
				if !authManager.IsAuthenticated() {
					// If unauthenticated, stop tunnel
					if tunnelManager != nil && tunnelManager.IsConnected() {
						logger.Info("User is unauthenticated, stopping tunnel")
						if err := tunnelManager.Disconnect(); err != nil {
							logger.Error("Failed to stop tunnel after authentication loss: %v", err)
						}
					}
				}

				baseInterval := 180 * time.Second
				jitterRange := 15 * time.Second
				jitter := time.Duration(rand.Intn(int(2*jitterRange))) - jitterRange
				time.Sleep(baseInterval + jitter)
			}
		}()
	*/

	return nil
}

// triggerUpdate asks the user for confirmation and then triggers the update via manager
func triggerUpdate(mw *walk.MainWindow) {
	userAcceptedChan := make(chan bool, 1)

	// Show dialog on UI thread - Show() blocks until dialog is closed
	walk.App().Synchronize(func() {
		td := walk.NewTaskDialog()
		opts := walk.TaskDialogOpts{
			Owner:         mw,
			Title:         "Update Available",
			Content:       "A new version is available.\n\nWould you like to download and install it now?",
			IconSystem:    walk.TaskDialogSystemIconInformation,
			CommonButtons: win.TDCBF_YES_BUTTON | win.TDCBF_NO_BUTTON,
			DefaultButton: walk.TaskDialogDefaultButtonYes,
		}
		opts.CommonButtonClicked(win.TDCBF_YES_BUTTON).Attach(func() bool {
			select {
			case userAcceptedChan <- true:
			default:
			}
			return false // Return false to allow dialog to close normally
		})
		opts.CommonButtonClicked(win.TDCBF_NO_BUTTON).Attach(func() bool {
			select {
			case userAcceptedChan <- false:
			default:
			}
			return false // Return false to allow dialog to close normally
		})
		td.Show(opts)
	})

	// Wait for user response
	userAccepted := <-userAcceptedChan
	if !userAccepted {
		logger.Info("User declined update")
		return
	}

	// Trigger update via manager IPC
	logger.Info("Starting update download via manager...")
	err := managers.IPCClientUpdate()
	if err != nil {
		logger.Error("Failed to trigger update: %v", err)
		walk.App().Synchronize(func() {
			td := walk.NewTaskDialog()
			td.Show(walk.TaskDialogOpts{
				Owner:         mw,
				Title:         "Update Failed",
				Content:       fmt.Sprintf("Failed to start update: %v", err),
				IconSystem:    walk.TaskDialogSystemIconError,
				CommonButtons: win.TDCBF_OK_BUTTON,
			})
		})
	}
}
