//go:build windows

package ui

import (
	"fmt"
	"strings"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/tunnel"
	"github.com/pkg/browser"
)

const (
	urlHowItWorks = "https://docs.pangolin.net/about/how-pangolin-works"
	urlDocs       = "https://docs.pangolin.net/"
	urlTerms      = "https://pangolin.net/tos"
	urlPrivacy    = "https://pangolin.net/privacy"
)

func openURL(url string) {
	if err := browser.OpenURL(url); err != nil {
		logger.Error("Failed to open %s: %v", url, err)
	}
}

// invokeMenuItem runs the action for a tray popup item. It is called on a
// service goroutine, so it may block.
func invokeMenuItem(id string) {
	// Like the macOS menu, the popup stays open for the connect switch, for
	// account and organization switches so their progress shows, and for exit
	// node selection so the checkmark just moves.
	keepOpen := id == menuIDConnect || strings.HasPrefix(id, menuPrefixAccount) || strings.HasPrefix(id, menuPrefixOrg) ||
		id == menuIDExitNodeNone || strings.HasPrefix(id, menuPrefixExitNode)
	if !keepOpen {
		hideTrayPopup()
	}

	switch {
	case id == menuIDUpdate:
		showUpdateWindow()
	case id == menuIDReAuth:
		// Log in again to the active account, from Preferences > Accounts.
		var hostname string
		if accountManager != nil {
			if active, _ := accountManager.ActiveAccount(); active != nil {
				hostname = active.Hostname
			}
		}
		showPreferencesWindow(prefsTabAccounts, &LoginRequest{Hostname: hostname})
	case id == menuIDConnect:
		toggleConnection()
	case id == menuIDLogin, id == menuIDAddAccount:
		showPreferencesWindow(prefsTabAccounts, &LoginRequest{})
	case id == menuIDOpenSetup:
		showOnboardingWindow()
	case id == menuIDManageAccounts:
		showPreferencesWindow(prefsTabAccounts, nil)
	case id == menuIDLogout:
		logout()
	case id == menuIDPreferences:
		showPreferencesWindow(prefsTabPreferences, nil)
	case id == menuIDQuit:
		quit()
	case id == menuIDOpenStatus:
		showPreferencesWindow(prefsTabStatus, nil)
	case id == menuIDHowItWorks:
		openURL(urlHowItWorks)
	case id == menuIDDocs:
		openURL(urlDocs)
	case id == menuIDTerms:
		openURL(urlTerms)
	case id == menuIDPrivacy:
		openURL(urlPrivacy)
	case id == menuIDCheckUpdates:
		checkForUpdates()
	case id == menuIDInstallCLI:
		showCLIWindow()
	case strings.HasPrefix(id, menuPrefixAccount):
		switchAccount(strings.TrimPrefix(id, menuPrefixAccount))
	case strings.HasPrefix(id, menuPrefixOrg):
		selectOrganization(strings.TrimPrefix(id, menuPrefixOrg))
	case id == menuIDExitNodeNone:
		disableExitNode()
	case strings.HasPrefix(id, menuPrefixExitNode):
		selectExitNode(strings.TrimPrefix(id, menuPrefixExitNode))
	default:
		logger.Error("Unknown tray menu item %q", id)
	}
}

func toggleConnection() {
	if tunnelManager == nil {
		logger.Error("Tunnel manager not initialized")
		showConnectionErrorNotification("Connection Error", "Tunnel manager is not initialized. Please restart the application.")
		return
	}

	// Allow disconnect for any state other than Stopped or Stopping so users
	// can cancel the connection process at any time.
	switch state := tunnelManager.State(); state {
	case tunnel.StateStopped:
		// Connecting needs setup to be finished; open it instead, like macOS.
		if onboardingNeeded() {
			showOnboardingWindow()
			return
		}
		on := true
		setPendingTunnel(&on)
		setConnectionError("")
		publish()
		openStatusTabOnConnect()
		setAlwaysOn(true)
		if err := tunnelManager.Connect(); err != nil {
			logger.Error("Failed to start tunnel: %v", err)
			setAlwaysOn(false)
			setPendingTunnel(nil)
			notifyConnectionError(err, "Connection Failed")
		}
	case tunnel.StateStopping:
		// The connect switch is disabled while stopping.
		return
	default:
		logger.Info("Disconnecting...")
		off := false
		setPendingTunnel(&off)
		publish()
		setAlwaysOn(false)
		if err := tunnelManager.Disconnect(); err != nil {
			logger.Error("Failed to stop tunnel: %v", err)
			setPendingTunnel(nil)
			notifyConnectionError(err, "Disconnect Failed")
		}
	}
	// The state callback may have fired before the pending value was set.
	onTunnelStateForMenu(tunnelManager.State())
	publish()
}

func openStatusTabOnConnect() {
	if configManager == nil || !configManager.GetOpenStatusTabOnConnect() {
		return
	}
	showPreferencesWindow(prefsTabStatus, nil)
}

func switchAccount(userID string) {
	if accountManager == nil || authManager == nil {
		return
	}
	if _, ok := accountManager.Accounts[userID]; !ok {
		logger.Error("Account %s no longer exists", userID)
		publish()
		return
	}
	if active, _ := accountManager.ActiveAccount(); active != nil && active.UserID == userID {
		return
	}
	stateMu.RLock()
	busy := switchingAccountID != "" || switchingOrgID != "" || loggingOut || removingAccountID != ""
	stateMu.RUnlock()
	if busy {
		return
	}
	// A connection error belongs to the account it happened on.
	setConnectionError("")
	setSwitchingAccount(userID)
	defer setSwitchingAccount("")

	// Switching users requires the tunnel to go down.
	logger.Info("Stopping tunnel before switching accounts")
	if err := managers.IPCClientStopTunnel(); err != nil {
		logger.Error("Failed to shut down tunnel before switch: %v", err)
		showError(nil, "Tunnel Shutdown Failed", fmt.Sprintf("Failed to shut down tunnel before switching accounts: %v", err))
		publish()
		return
	}

	if err := authManager.SwitchAccount(userID); err != nil {
		logger.Error("Failed to switch account: %v", err)
		showError(nil, "Switching Account Failed", fmt.Sprintf("Failed to switch account: %v", err))
		publish()
		return
	}
	publish()
}

func selectOrganization(orgID string) {
	if authManager == nil {
		return
	}
	if org := authManager.CurrentOrg(); org != nil && org.Id == orgID {
		return
	}
	stateMu.RLock()
	busy := switchingAccountID != "" || switchingOrgID != "" || loggingOut || removingAccountID != ""
	stateMu.RUnlock()
	if busy {
		return
	}
	setConnectionError("")
	setSwitchingOrg(orgID)
	defer setSwitchingOrg("")
	var found bool
	for _, org := range authManager.Organizations() {
		if org.Id != orgID {
			continue
		}
		found = true
		org := org
		if err := authManager.SelectOrganization(&org); err != nil {
			logger.Error("Failed to select organization: %v", err)
			showError(nil, "Organization Selection Failed", fmt.Sprintf("Failed to select organization: %v", err))
			break
		}
		publish()
		refreshExitNodes()
		if tunnelManager != nil && tunnelManager.IsConnected() {
			if err := tunnelManager.SwitchOLMOrg(org.Id); err != nil {
				logger.Error("Failed to switch tunnel organization: %v", err)
				showError(nil, "Tunnel Organization Switch Failed", fmt.Sprintf("Failed to switch tunnel organization: %v", err))
			}
		}
		break
	}
	if !found {
		logger.Error("Organization %s no longer exists", orgID)
	}
	publish()
}

// logout signs out of the active account and removes it, like the macOS
// client's Log Out.
func logout() {
	if accountManager == nil || accountManager.ActiveUserID == "" {
		return
	}
	deleteAccount(accountManager.ActiveUserID)
}

// quit stops any active tunnels and exits the UI process. The manager service keeps running.
func quit() {
	setAlwaysOn(false)
	_ = managers.IPCClientStopAllTunnels() // ignore errors (e.g. no manager connection)
	app.Quit()
}
