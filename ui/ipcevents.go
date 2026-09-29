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
		setHasUpdate(updateState == managers.UpdateStateFoundUpdate)
		if updateState == managers.UpdateStateFoundUpdate {
			go notifyUpdateAvailableOnce()
		}
	})

	managers.IPCClientRegisterManagerStopping(func() {
		logger.Info("Manager service is stopping, exiting UI")
		app.Quit()
	})

	managers.IPCClientRegisterUpdateProgress(handleUpdateProgress)

	managers.IPCClientRegisterUIAction(func(action managers.UIAction) {
		if action == managers.UIActionOpenUpdate {
			go showUpdateWindow()
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

// checkStartupUpdate shows the update toast at startup when the manager already found an update.
func checkStartupUpdate() {
	promptIfFound := func() bool {
		updateState, err := managers.IPCClientUpdateState()
		if err != nil || updateState != managers.UpdateStateFoundUpdate {
			return false
		}
		setHasUpdate(true)
		notifyUpdateAvailableOnce()
		return true
	}
	if promptIfFound() {
		return
	}
	// The manager's initial update check may still be running.
	time.Sleep(3 * time.Second)
	promptIfFound()
}
