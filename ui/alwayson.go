//go:build windows

package ui

import (
	"time"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/tunnel"
)

const (
	startupRetryInitialDelay = 2 * time.Second
	startupRetryMaxDelay     = 5 * time.Minute
)

func alwaysOnEnabled() bool {
	alwaysOnMutex.Lock()
	defer alwaysOnMutex.Unlock()
	return alwaysOn
}

func setAlwaysOn(enabled bool) {
	alwaysOnMutex.Lock()
	changed := alwaysOn != enabled
	alwaysOn = enabled
	alwaysOnMutex.Unlock()
	if !changed {
		return
	}
	if err := managers.IPCClientSetAlwaysOn(enabled); err != nil {
		logger.Error("Failed to tell the manager Always-On is %v: %v", enabled, err)
	}
}

// handleAlwaysOnStopped runs when the tunnel reaches Stopped. A user or
// error shutdown clears Always-On. A lost tunnel reconnects while Always-On is set.
func handleAlwaysOnStopped() {
	if tunnelManager == nil {
		return
	}
	switch tunnelManager.TakeStopReason() {
	case tunnel.StopReasonError, tunnel.StopReasonUser:
		setAlwaysOn(false)
	case tunnel.StopReasonLost:
		if !alwaysOnEnabled() {
			return
		}
		go reconnectAlwaysOn()
	}
}

func reconnectAlwaysOn() {
	if tunnelManager == nil {
		return
	}
	logger.Info("Always-On: reconnecting after the tunnel dropped")
	if err := tunnelManager.Connect(); err != nil {
		logger.Error("Always-On reconnect failed: %v", err)
		setAlwaysOn(false)
		notifyConnectionError(err, "Connection Failed")
		publish()
	}
}

func tunnelIsStopped() bool {
	if tunnelManager == nil {
		return false
	}
	state := tunnelManager.State()
	return state == tunnel.StateStopped || state == tunnel.StateStopping
}

// waitForServer retries the server health check with exponential backoff until
// it passes or keepWaiting returns false. On success it reloads auth, because
// Initialize skips loading the user and organizations while the server is down.
func waitForServer(am *auth.AuthManager, label string, keepWaiting func() bool) bool {
	delay := startupRetryInitialDelay
	for {
		logger.Info("%s: server health check failed, retrying in %v", label, delay)
		time.Sleep(delay)
		if !am.IsAuthenticated() || !keepWaiting() {
			logger.Info("%s: stopped waiting for the server", label)
			return false
		}
		_ = am.CheckHealthAndSetState()
		if !am.IsServerDown() {
			logger.Info("%s: server is reachable, reloading account", label)
			if err := am.Initialize(); err != nil {
				logger.Error("%s: failed to reload account: %v", label, err)
			}
			publish()
			return true
		}
		delay *= 2
		if delay > startupRetryMaxDelay {
			delay = startupRetryMaxDelay
		}
	}
}

// resumeAlwaysOn reconnects after the UI restarts while Always-On is set.
func resumeAlwaysOn(am *auth.AuthManager) {
	setAlwaysOn(true)
	if am == nil || !am.IsAuthenticated() {
		logger.Info("Always-On resume skipped: not signed in")
		publish()
		return
	}
	if am.IsServerDown() {
		keepWaiting := func() bool { return alwaysOnEnabled() && tunnelIsStopped() }
		if !waitForServer(am, "Always-On resume", keepWaiting) {
			return
		}
	}
	if !am.IsAuthenticated() || am.SessionExpired() || am.CurrentOrg() == nil {
		logger.Info("Always-On resume skipped: not signed in or no organization selected")
		publish()
		return
	}
	if tunnelManager == nil {
		logger.Error("Always-On resume skipped: tunnel manager is not initialized")
		return
	}
	state := tunnelManager.State()
	if state != tunnel.StateStopped && state != tunnel.StateStopping {
		logger.Info("Always-On resume: tunnel already active")
		publish()
		return
	}
	if err := tunnelManager.Connect(); err != nil {
		logger.Error("Always-On resume failed: %v", err)
		setAlwaysOn(false)
		notifyConnectionError(err, "Connection Failed")
		publish()
	}
}

// autoConnect starts the tunnel when the app starts and connect-at-start is enabled.
// It does nothing when the user is signed out, the session needs
// re-authentication, or no organization is selected. A failed connect shows
// the same error notification as the tray Connect action.
func autoConnect(am *auth.AuthManager) {
	if am == nil || !am.IsAuthenticated() {
		logger.Info("Auto-connect skipped: not signed in")
		return
	}
	if am.IsServerDown() {
		if !waitForServer(am, "Auto-connect", tunnelIsStopped) {
			return
		}
	}
	if !am.IsAuthenticated() || am.SessionExpired() || am.CurrentOrg() == nil {
		logger.Info("Auto-connect skipped: not signed in or no organization selected")
		return
	}
	if tunnelManager == nil {
		logger.Error("Auto-connect skipped: tunnel manager is not initialized")
		return
	}

	setAlwaysOn(true)

	state := tunnelManager.State()
	if state != tunnel.StateStopped && state != tunnel.StateStopping {
		logger.Info("Auto-connect: tunnel already active")
		publish()
		return
	}

	if err := tunnelManager.Connect(); err != nil {
		logger.Error("Auto-connect failed: %v", err)
		if stopped := tunnelManager.State(); stopped == tunnel.StateStopped || stopped == tunnel.StateStopping {
			setAlwaysOn(false)
		}
		notifyConnectionError(err, "Connection Failed")
		publish()
	}
}
