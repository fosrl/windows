//go:build windows

package ui

import (
	"fmt"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/tunnel"
	"github.com/fosrl/windows/updater"
)

// registerIPCCallbacks subscribes to manager and tunnel notifications.
func registerIPCCallbacks() {
	managers.IPCClientRegisterUpdateFound(func(updateState managers.UpdateState) {
		setHasUpdate(updateState == managers.UpdateStateFoundUpdate)
	})

	managers.IPCClientRegisterManagerStopping(func() {
		logger.Info("Manager service is stopping, exiting UI")
		app.Quit()
	})

	managers.IPCClientRegisterUpdateProgress(handleUpdateProgress)

	tunnelManager.RegisterStateChangeCallback(func(state tunnel.State) {
		logger.Info("Tunnel state changed: %s", state.String())
		if state == tunnel.StateStopped {
			handleAlwaysOnStopped()
		}
		onTunnelStateForExitNodes(state)
		onTunnelStateForMenu(state)
		updateTrayForState(state)
		publish()
	})

	tunnelManager.RegisterErrorCallback(func(err *tunnel.OLMStatusError) {
		logger.Error("Tunnel error detected: code=%s, message=%s", err.Code, err.Message)
		message := err.Message
		if message == "" {
			message = fmt.Sprintf("Error code: %s", err.Code)
		}
		setConnectionError(message)
		showConnectionErrorNotification("Connection Error", message)
		publish()
	})
}

func handleUpdateProgress(dp updater.DownloadProgress) {
	if len(dp.Activity) > 0 {
		logger.Info("Update: %s", dp.Activity)
	}
	if dp.BytesTotal > 0 {
		percent := float64(dp.BytesDownloaded) / float64(dp.BytesTotal) * 100
		const mb = 1024 * 1024
		logger.Info("Download progress: %.2f%% (%.2f / %.2f MB)", percent,
			float64(dp.BytesDownloaded)/mb, float64(dp.BytesTotal)/mb)
	}

	switch {
	case dp.Error != nil:
		logger.Error("Update error: %v", dp.Error)
		closeAppUpdateProgressUI()
		go showError(nil, "Update Failed", fmt.Sprintf("Update failed: %v", dp.Error))
	case dp.Complete:
		logger.Info("Update complete! The application will restart.")
		closeAppUpdateProgressUI()
		setHasUpdate(false)
		go showInfo(nil, "Update Complete", "The update has been installed successfully. The application will now restart.")
	default:
		// The bar stays in marquee mode; only the status text changes.
		text := dp.Activity
		if text == "" {
			text = "Working…"
		}
		setProgressText(progressKindUpdate, text)
	}
}

// checkStartupUpdate prompts once at startup when the manager already found an update.
// Later checks only update the menu.
func checkStartupUpdate() {
	promptIfFound := func() bool {
		updateState, err := managers.IPCClientUpdateState()
		if err != nil || updateState != managers.UpdateStateFoundUpdate {
			return false
		}
		setHasUpdate(true)
		startupDialogOnce.Do(triggerUpdate)
		return true
	}
	if promptIfFound() {
		return
	}
	// The manager's initial update check may still be running.
	time.Sleep(3 * time.Second)
	promptIfFound()
}
