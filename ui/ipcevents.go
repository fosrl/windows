//go:build windows

package ui

import (
	"fmt"
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/tunnel"
)

// registerIPCCallbacks subscribes to manager and tunnel notifications.
func registerIPCCallbacks() {
	managers.IPCClientRegisterUpdateFound(func(updateState managers.UpdateState) {
		setHasUpdate(updateState.HasUpdate())
		switch userUpdateState(updateState) {
		case managers.UpdateStateFoundUpdate:
			go notifyUpdateAvailableOnce()
		case managers.UpdateStateInstallReminder:
			go remindToInstallUpdate()
		}
		// UpdateStateUpdateDownloaded is silent: it installs when the user quits.
	})

	managers.IPCClientRegisterManagerStopping(func() {
		logger.Info("Manager service is stopping, exiting UI")
		app.Quit()
	})

	managers.IPCClientRegisterUpdateProgress(handleUpdateProgress)

	managers.IPCClientRegisterUIAction(func(action managers.UIAction) {
		switch action {
		case managers.UIActionOpenUpdate:
			go showUpdateWindow()
		case managers.UIActionShow:
			go showPreferencesWindow(prefsTabStatus, nil)
		}
	})

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

// userUpdateState is updateState as it applies to this user. The manager downloads
// updates in the background when any user has automatic downloads on, but for a
// user without it an update is only found: it installs when they choose to.
func userUpdateState(updateState managers.UpdateState) managers.UpdateState {
	if updateState.HasUpdate() && (configManager == nil || !configManager.GetAutoDownloadUpdates()) {
		return managers.UpdateStateFoundUpdate
	}
	return updateState
}

// checkStartupUpdate shows the update toast at startup when the manager already found an update,
// or the update window when a silently downloaded update is overdue.
func checkStartupUpdate() {
	promptIfFound := func() bool {
		updateState, err := managers.IPCClientUpdateState()
		if err != nil || !updateState.HasUpdate() {
			return false
		}
		setHasUpdate(true)
		switch userUpdateState(updateState) {
		case managers.UpdateStateFoundUpdate:
			notifyUpdateAvailableOnce()
		case managers.UpdateStateInstallReminder:
			remindToInstallUpdate()
		}
		return true
	}
	if promptIfFound() {
		return
	}
	// The manager's initial update check may still be running.
	time.Sleep(3 * time.Second)
	promptIfFound()
}
