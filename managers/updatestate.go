//go:build windows

package managers

import (
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
)

var updateState = UpdateStateUnknown

func jitterSleep(min, max time.Duration) {
	time.Sleep(min + time.Millisecond*time.Duration(fastrandn(uint32((max-min+1)/time.Millisecond))))
}

func checkForUpdates() {
	if !config.AutoUpdateChecksEnabled() {
		logger.Info("Automatic update checks are disabled by config")
		return
	}

	// Initial jitter if started at boot - prevents all machines from checking at once after boot
	if services.StartedAtBoot() {
		jitterSleep(time.Minute*2, time.Minute*5)
	}

	noError, didNotify := true, false
	for {
		update, err := updater.CheckForUpdate()
		if err == nil && update != nil && !didNotify {
			logger.Info("An update is available")
			updateState = UpdateStateFoundUpdate
			IPCServerNotifyUpdateFound(updateState)
			didNotify = true
		} else if err != nil && !didNotify {
			logger.Error("Update checker: %v", err)
			if noError {
				jitterSleep(time.Minute*4, time.Minute*6)
				noError = false
			} else {
				jitterSleep(time.Minute*25, time.Minute*30)
			}
			continue
		}

		interval := config.UpdateCheckInterval()
		jitter := interval / 20 // ±5%
		if jitter < time.Minute {
			jitter = time.Minute
		}
		jitterSleep(interval-jitter, interval+jitter)
	}
}
