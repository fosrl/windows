//go:build windows

package ui

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/updater"
	"github.com/fosrl/windows/version"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Phases of the update window.
const (
	updatePhaseChecking    = "checking"
	updatePhaseUpToDate    = "upToDate"
	updatePhaseDisabled    = "disabled"
	updatePhaseCheckFailed = "checkFailed"
	updatePhaseAvailable   = "available"
	updatePhaseDownloading = "downloading"
	updatePhaseError       = "error"
	updatePhaseComplete    = "complete"
)

// UpdateInfo is what the update window shows.
type UpdateInfo struct {
	Phase           string `json:"phase"`
	Version         string `json:"version"`
	CurrentVersion  string `json:"currentVersion"`
	Activity        string `json:"activity"`
	Error           string `json:"error"`
	BytesDownloaded uint64 `json:"bytesDownloaded"`
	BytesTotal      uint64 `json:"bytesTotal"`
}

var (
	updateMu     sync.Mutex
	updateWindow *application.WebviewWindow
	updateInfo   = UpdateInfo{Phase: updatePhaseAvailable, CurrentVersion: version.Number}

	// updateToastShown makes sure the update toast is shown at most once per session.
	updateToastShown atomic.Bool
	// manualUpdateCheck is set while "Check for Updates…" runs, which opens the window itself.
	manualUpdateCheck atomic.Bool
)

// UpdateService backs the update window.
type UpdateService struct{}

func (UpdateService) State() UpdateInfo {
	updateMu.Lock()
	defer updateMu.Unlock()
	return updateInfo
}

func (UpdateService) Check()   { checkForUpdates() }
func (UpdateService) Install() { startUpdate() }
func (UpdateService) Close()   { hideUpdateWindow() }

// notifyUpdateAvailableOnce shows the update toast the first time an update is found.
func notifyUpdateAvailableOnce() {
	if manualUpdateCheck.Load() || updateToastShown.Swap(true) || updateWindowVisible() {
		return
	}
	v, err := managers.IPCClientUpdateVersion()
	if err != nil {
		logger.Error("Failed to get update version: %v", err)
	}
	notifyUpdateAvailable(v)
}

// showUpdateWindow opens the update window from the menu or the toast, or
// focuses it if it is already open. An earlier error is cleared so the update
// can be offered again.
func showUpdateWindow() {
	// Opening the window counts as having told the user about the update.
	updateToastShown.Store(true)

	if !updateInProgress() {
		offerUpdate()
	}
	presentUpdateWindow()
}

// checkForUpdates runs "Check for Updates…", showing the check and its result
// in the update window.
func checkForUpdates() {
	if updateInProgress() {
		presentUpdateWindow()
		return
	}
	setUpdateInfo(func(info *UpdateInfo) {
		*info = UpdateInfo{Phase: updatePhaseChecking, CurrentVersion: version.Number}
	})
	presentUpdateWindow()

	// The manager also reports a found update as an event; this window
	// replaces the toast that event would show.
	manualUpdateCheck.Store(true)
	defer manualUpdateCheck.Store(false)
	updateState, err := managers.IPCClientCheckForUpdates()
	switch {
	case err != nil:
		logger.Error("Update check failed: %v", err)
		setUpdateInfo(func(info *UpdateInfo) {
			info.Phase = updatePhaseCheckFailed
			info.Error = err.Error()
		})
	case updateState == managers.UpdateStateFoundUpdate:
		logger.Info("Update available")
		updateToastShown.Store(true)
		offerUpdate()
	case updateState == managers.UpdateStateUpdatesDisabledUnofficialBuild:
		setUpdateInfo(func(info *UpdateInfo) { info.Phase = updatePhaseDisabled })
	default:
		logger.Info("No update available")
		setUpdateInfo(func(info *UpdateInfo) { info.Phase = updatePhaseUpToDate })
	}
}

// updateInProgress reports whether an update is downloading or installing.
func updateInProgress() bool {
	updateMu.Lock()
	defer updateMu.Unlock()
	return updateInfo.Phase == updatePhaseDownloading || updateInfo.Phase == updatePhaseComplete
}

// offerUpdate puts the window in the "available" phase with the found version.
func offerUpdate() {
	v, err := managers.IPCClientUpdateVersion()
	if err != nil {
		logger.Error("Failed to get update version: %v", err)
	}
	setUpdateInfo(func(info *UpdateInfo) {
		*info = UpdateInfo{Phase: updatePhaseAvailable, Version: v, CurrentVersion: version.Number}
	})
}

// presentUpdateWindow shows the update window with the current state.
func presentUpdateWindow() {
	updateMu.Lock()
	defer updateMu.Unlock()
	if updateWindow == nil {
		// Closing only hides the window, so a running download can be shown again.
		updateWindow = newDialogWindow("update", "Software Update", "update", 200, hideUpdateWindow)
	}
	updateWindow.EmitEvent(eventUpdateState, updateInfo)
	updateWindow.Show()
	updateWindow.Focus()
}

func hideUpdateWindow() {
	updateMu.Lock()
	defer updateMu.Unlock()
	if updateWindow != nil {
		updateWindow.Hide()
	}
}

func updateWindowVisible() bool {
	updateMu.Lock()
	defer updateMu.Unlock()
	return updateWindow != nil && updateWindow.IsVisible()
}

// setUpdateInfo changes the update state and sends it to the window.
func setUpdateInfo(change func(*UpdateInfo)) {
	updateMu.Lock()
	change(&updateInfo)
	info := updateInfo
	w := updateWindow
	updateMu.Unlock()
	if w != nil {
		w.EmitEvent(eventUpdateState, info)
	}
}

// startUpdate asks the manager to download and install the update. Progress
// arrives through handleUpdateProgress.
func startUpdate() {
	setUpdateInfo(func(info *UpdateInfo) {
		info.Phase = updatePhaseDownloading
		info.Activity = "Preparing to download the update…"
		info.Error = ""
		info.BytesDownloaded, info.BytesTotal = 0, 0
	})

	logger.Info("Starting update download via manager...")
	if err := managers.IPCClientUpdate(); err != nil {
		logger.Error("Failed to trigger update: %v", err)
		setUpdateInfo(func(info *UpdateInfo) {
			info.Phase = updatePhaseError
			info.Error = fmt.Sprintf("Failed to start update: %v", err)
		})
	}
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
		setUpdateInfo(func(info *UpdateInfo) {
			info.Phase = updatePhaseError
			info.Error = dp.Error.Error()
		})
		presentUpdateWindow()
	case dp.Complete:
		logger.Info("Update complete! The application will restart.")
		setHasUpdate(false)
		setUpdateInfo(func(info *UpdateInfo) { info.Phase = updatePhaseComplete })
		presentUpdateWindow()
	default:
		setUpdateInfo(func(info *UpdateInfo) {
			info.Phase = updatePhaseDownloading
			if dp.Activity != "" {
				info.Activity = dp.Activity
			}
			info.BytesDownloaded, info.BytesTotal = dp.BytesDownloaded, dp.BytesTotal
		})
	}
}
