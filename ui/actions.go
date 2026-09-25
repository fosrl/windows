//go:build windows

package ui

import (
	"fmt"
	"strings"
	"sync"

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
	hideTrayPopup()

	switch {
	case id == menuIDUpdate:
		triggerUpdate()
	case id == menuIDReAuth:
		if authManager != nil {
			authManager.SetStartDeviceAuthImmediately(true)
		}
		showLoginWindow()
	case id == menuIDConnect:
		toggleConnection()
	case id == menuIDLogin, id == menuIDAddAccount:
		showLoginWindow()
	case id == menuIDLogout:
		logout()
	case id == menuIDPreferences:
		showPreferencesWindow(0)
	case id == menuIDQuit:
		quit()
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
		triggerCLIInstall()
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
		openStatusTabOnConnect()
		setAlwaysOn(true)
		if err := tunnelManager.Connect(); err != nil {
			logger.Error("Failed to start tunnel: %v", err)
			setAlwaysOn(false)
			notifyConnectionError(err, "Connection Failed")
		}
	case tunnel.StateStopping:
		// The Connect item is disabled while stopping.
	default:
		logger.Info("Disconnecting...")
		setAlwaysOn(false)
		if err := tunnelManager.Disconnect(); err != nil {
			logger.Error("Failed to stop tunnel: %v", err)
			notifyConnectionError(err, "Disconnect Failed")
		}
	}
}

func openStatusTabOnConnect() {
	if configManager == nil || !configManager.GetOpenStatusTabOnConnect() {
		return
	}
	showPreferencesWindow(1)
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

func logout() {
	if authManager == nil {
		return
	}
	// Always stop any running tunnel before logout.
	logger.Info("Stopping tunnel before logout")
	if err := managers.IPCClientStopTunnel(); err != nil {
		logger.Error("Failed to stop tunnel before logout: %v", err)
	}
	if err := authManager.Logout(); err != nil {
		logger.Error("Failed to logout: %v", err)
		showError(nil, "Logout Failed", fmt.Sprintf("Failed to logout: %v", err))
	}
	publish()
}

// quit stops any active tunnels and exits the UI process. The manager service keeps running.
func quit() {
	setAlwaysOn(false)
	_ = managers.IPCClientStopAllTunnels() // ignore errors (e.g. no manager connection)
	app.Quit()
}

func checkForUpdates() {
	updateState, err := managers.IPCClientCheckForUpdates()
	if err != nil {
		logger.Error("Update check failed: %v", err)
		showError(nil, "Update Check Failed", fmt.Sprintf("Failed to check for updates: %v", err))
		return
	}
	switch updateState {
	case managers.UpdateStateFoundUpdate:
		logger.Info("Update available")
		triggerUpdate()
	case managers.UpdateStateUpdatesDisabledUnofficialBuild:
		showInfo(nil, "Updates Disabled", "Updates are disabled for unofficial builds.")
	default:
		logger.Info("No update available")
		showInfo(nil, "No Update Available", "You are running the latest version.")
	}
}

const (
	progressKindUpdate = "update"
	progressKindCLI    = "cli"
)

var (
	appUpdateProgressMu    sync.Mutex
	appUpdateProgressClose func()
)

// triggerUpdate asks the user for confirmation and then starts the update via the manager.
func triggerUpdate() {
	if !confirm(nil, "Pangolin Update Available",
		"A new Pangolin version is available.\n\nWould you like to download and install it now?", true) {
		logger.Info("User declined update")
		return
	}

	// Show progress before IPC so early updater events are reflected in the same window.
	closeAppUpdateProgressUI()
	closeFn := openProgressWindow(progressKindUpdate, "Updating Pangolin", "Preparing to download the update…")
	appUpdateProgressMu.Lock()
	appUpdateProgressClose = closeFn
	appUpdateProgressMu.Unlock()

	logger.Info("Starting update download via manager...")
	if err := managers.IPCClientUpdate(); err != nil {
		logger.Error("Failed to trigger update: %v", err)
		closeAppUpdateProgressUI()
		showError(nil, "Update Failed", fmt.Sprintf("Failed to start update: %v", err))
	}
}

func closeAppUpdateProgressUI() {
	appUpdateProgressMu.Lock()
	closeFn := appUpdateProgressClose
	appUpdateProgressClose = nil
	appUpdateProgressMu.Unlock()
	if closeFn != nil {
		closeFn()
	}
}

func triggerCLIInstall() {
	if !confirm(nil, "Install Pangolin CLI",
		"This will download and run the Pangolin CLI installer.\n\nWould you like to continue?", true) {
		logger.Info("User declined CLI installation")
		return
	}

	logger.Info("Starting Pangolin CLI installer via manager...")
	setCLIInstallInProgress(true)
	closeProgress := openProgressWindow(progressKindCLI, "Installing Pangolin CLI", "Downloading the installer, then running setup.")

	err := managers.IPCClientInstallCLI()
	closeProgress()
	setCLIInstallInProgress(false)
	if err != nil {
		logger.Error("Failed to install Pangolin CLI: %v", err)
		showError(nil, "CLI Install Failed", fmt.Sprintf("Failed to install Pangolin CLI: %v", err))
		return
	}
	showInfo(nil, "CLI Installed", "Pangolin CLI was installed successfully and added to your PATH. You can now use the 'pangolin' command in your terminal.")
	refreshCLIInstallState()
}
