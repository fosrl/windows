//go:build windows

package managers

import (
	"sync"
	"time"
	_ "unsafe"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/services"
	"github.com/fosrl/windows/updater"
)

//go:linkname fastrandn runtime.fastrandn
func fastrandn(n uint32) uint32

type UpdateState uint32

const (
	UpdateStateUnknown UpdateState = iota
	UpdateStateFoundUpdate
	UpdateStateUpdatesDisabledUnofficialBuild
	// UpdateStateUpdateDownloaded means the update was downloaded silently and will
	// be installed the next time Pangolin quits or launches, for users with
	// autoDownloadUpdatesEnabled.
	UpdateStateUpdateDownloaded
	// UpdateStateInstallReminder means a downloaded update has waited long enough
	// that the user should be prompted to install it.
	UpdateStateInstallReminder
)

// HasUpdate reports whether an update is available to install.
func (s UpdateState) HasUpdate() bool {
	return s == UpdateStateFoundUpdate || s == UpdateStateUpdateDownloaded || s == UpdateStateInstallReminder
}

// stagedUpdateReminderAge is how long a downloaded update waits for the user to
// quit before they are prompted to install it, like Sparkle.
const stagedUpdateReminderAge = 7 * 24 * time.Hour

var (
	updateLock   sync.Mutex
	updateState  = UpdateStateUnknown
	foundVersion string                // version of the found update, when updateState.HasUpdate()
	stagedUpdate *updater.StagedUpdate // silently downloaded update waiting to be installed

	installingStagedUpdate bool     // a background install started by installStagedUpdate is running
	pendingLaunchSessions  []uint32 // UI launches held back by that install
)

func jitterSleep(min, max time.Duration) {
	time.Sleep(min + time.Millisecond*time.Duration(fastrandn(uint32((max-min+1)/time.Millisecond))))
}

func currentUpdateState() (UpdateState, string) {
	updateLock.Lock()
	defer updateLock.Unlock()
	return updateState, foundVersion
}

// setFoundUpdate records that version is available and reports whether the state changed.
func setFoundUpdate(version string) (state UpdateState, changed bool) {
	updateLock.Lock()
	defer updateLock.Unlock()
	state = UpdateStateFoundUpdate
	if stagedUpdate != nil && stagedUpdate.Version() == version {
		state = UpdateStateUpdateDownloaded
		if updateState == UpdateStateInstallReminder && foundVersion == version {
			state = UpdateStateInstallReminder
		}
	}
	changed = state != updateState || version != foundVersion
	updateState, foundVersion = state, version
	return state, changed
}

// loadStagedUpdate picks up an update downloaded before the manager last stopped.
func loadStagedUpdate() {
	staged := updater.LoadStagedUpdate()
	if staged == nil {
		return
	}
	updateLock.Lock()
	stagedUpdate = staged
	updateState, foundVersion = UpdateStateUpdateDownloaded, staged.Version()
	updateLock.Unlock()
}

// stageUpdate downloads version in the background unless it is already staged.
func stageUpdate(version string) error {
	updateLock.Lock()
	alreadyStaged := installingStagedUpdate || stagedUpdate != nil && stagedUpdate.Version() == version
	updateLock.Unlock()
	if alreadyStaged {
		return nil
	}

	// A new download replaces the staged files on disk.
	staged, err := updater.DownloadInBackground()
	if err != nil {
		return err
	}
	updateLock.Lock()
	stagedUpdate = staged
	updateLock.Unlock()
	logger.Info("Update %s downloaded; it will be installed the next time Pangolin quits or launches", staged.Version())
	return nil
}

// autoDownloadUpdatesForSession reports whether the user logged into session has
// automatic update downloads on in their merged config. Session 0 means no
// particular user: the machine config or any signed-in user turning it on counts.
func autoDownloadUpdatesForSession(session uint32) bool {
	if session == 0 {
		return autoDownloadUpdatesWanted()
	}
	localAppData, err := localAppDataForSession(session)
	if err != nil {
		logger.Error("Automatic update downloads: %v", err)
	}
	return config.AutoDownloadUpdatesEnabled(localAppData)
}

// autoDownloadUpdatesWanted reports whether updates should be downloaded in the
// background: the machine config or a signed-in user's config turns it on.
func autoDownloadUpdatesWanted() bool {
	if config.AutoDownloadUpdatesEnabled("") {
		return true
	}
	for _, session := range signedInSessions() {
		if localAppData, err := localAppDataForSession(session); err == nil && config.AutoDownloadUpdatesEnabled(localAppData) {
			return true
		}
	}
	return false
}

// autoDownloadUpdatesEnabled applies a user turning on automatic downloads in
// Preferences right away, downloading an available update now instead of at the
// next scheduled check.
func autoDownloadUpdatesEnabled() {
	if !config.AutoUpdateChecksEnabled() {
		return
	}
	go func() {
		update, err := updater.CheckForUpdate()
		if err != nil || update == nil {
			return
		}
		if err := stageUpdate(update.Version()); err != nil {
			logger.Error("Unable to download update: %v", err)
			return
		}
		if state, changed := setFoundUpdate(update.Version()); changed {
			IPCServerNotifyUpdateFound(state)
		}
	}()
}

// takeStagedUpdate reports whether an update is staged, handing it to the caller to install.
func takeStagedUpdate() bool {
	updateLock.Lock()
	defer updateLock.Unlock()
	staged := stagedUpdate != nil
	stagedUpdate = nil
	return staged
}

// installStagedUpdate installs the staged update, if one is waiting and the user in
// session (0 at service start) has automatic downloads on. With launch set, it runs
// instead of launching the UI in session, and that user sees msiexec's progress bar;
// otherwise (at quit or service start) the install is silent. It reports whether an
// install is running, in which case the caller must not launch the UI: the manager
// that starts after the upgrade launches it. Launches requested while installing are
// queued the same way. If the install fails, onFailure gets the queued sessions so
// the caller can launch them on the current version.
func installStagedUpdate(session uint32, launch bool, onFailure func(sessions []uint32)) bool {
	updateLock.Lock()
	installing, hasStaged := installingStagedUpdate, stagedUpdate != nil
	updateLock.Unlock()
	if !installing && (!hasStaged || !autoDownloadUpdatesForSession(session)) {
		return false
	}

	launchSession := session
	if !launch {
		launchSession = 0
	}
	updateLock.Lock()
	if installingStagedUpdate {
		if launchSession != 0 {
			pendingLaunchSessions = append(pendingLaunchSessions, launchSession)
		}
		updateLock.Unlock()
		queueUIRelaunchAfterUpdate(launchSession)
		return true
	}
	staged := stagedUpdate
	if staged == nil {
		updateLock.Unlock()
		return false
	}
	stagedUpdate = nil
	installingStagedUpdate = true
	pendingLaunchSessions = nil
	if launchSession != 0 {
		pendingLaunchSessions = append(pendingLaunchSessions, launchSession)
	}
	updateLock.Unlock()
	queueUIRelaunchAfterUpdate(launchSession)

	logger.Info("Installing update %s in the background", staged.Version())
	go func() {
		stopActiveTunnels()
		err := updater.InstallStagedInBackground(launchSession)
		if err == nil {
			// msiexec usually stops this service before getting here.
			return
		}
		logger.Error("Unable to install update %s: %v", staged.Version(), err)
		updater.CancelUIRelaunchAfterUpdate()
		updateLock.Lock()
		installingStagedUpdate = false
		sessions := pendingLaunchSessions
		pendingLaunchSessions = nil
		if updateState.HasUpdate() {
			updateState = UpdateStateFoundUpdate
		}
		updateLock.Unlock()
		if onFailure != nil {
			onFailure(sessions)
		}
	}()
	return true
}

func queueUIRelaunchAfterUpdate(session uint32) {
	if session == 0 {
		return
	}
	if err := updater.RelaunchUIAfterUpdate(session); err != nil {
		logger.Error("Unable to queue UI relaunch for session %d after update: %v", session, err)
	}
}

// remindAboutStagedUpdate prompts the user to install a downloaded update once it
// has waited stagedUpdateReminderAge without the user quitting.
func remindAboutStagedUpdate() {
	for range time.Tick(time.Hour) {
		updateLock.Lock()
		remind := stagedUpdate != nil && updateState == UpdateStateUpdateDownloaded &&
			time.Since(stagedUpdate.StagedAt) >= stagedUpdateReminderAge
		if remind {
			updateState = UpdateStateInstallReminder
		}
		updateLock.Unlock()
		if remind {
			logger.Info("Downloaded update has not been installed for a week, prompting to install")
			IPCServerNotifyUpdateFound(UpdateStateInstallReminder)
		}
	}
}

func checkForUpdates() {
	if !config.AutoUpdateChecksEnabled() {
		logger.Info("Automatic update checks are disabled by config")
		return
	}

	go remindAboutStagedUpdate()

	// Initial jitter if started at boot - prevents all machines from checking at once after boot
	if services.StartedAtBoot() {
		jitterSleep(time.Minute*2, time.Minute*5)
	}

	noError, didNotify := true, false
	for {
		update, err := updater.CheckForUpdate()
		if err == nil && update != nil && autoDownloadUpdatesWanted() {
			err = stageUpdate(update.Version())
		}
		if err == nil && update != nil {
			if state, changed := setFoundUpdate(update.Version()); changed {
				logger.Info("An update is available")
				IPCServerNotifyUpdateFound(state)
			}
			didNotify = true
		} else if err != nil {
			logger.Error("Update checker: %v", err)
			if !didNotify {
				if noError {
					jitterSleep(time.Minute*4, time.Minute*6)
					noError = false
				} else {
					jitterSleep(time.Minute*25, time.Minute*30)
				}
				continue
			}
		}

		interval := config.UpdateCheckInterval()
		jitter := interval / 20 // ±5%
		if jitter < time.Minute {
			jitter = time.Minute
		}
		jitterSleep(interval-jitter, interval+jitter)
	}
}
