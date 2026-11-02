package services

import (
	"errors"
	"sync"
	"time"

	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

var (
	startedAtBoot     bool
	startedAtBootOnce sync.Once
)

// StartedAtBoot returns true if the service was started at boot time (automatically),
// false if it was started manually by a user.
func StartedAtBoot() bool {
	startedAtBootOnce.Do(func() {
		// If not running as a service, it wasn't started at boot
		if isService, err := svc.IsWindowsService(); err == nil && !isService {
			return
		}
		
		// Try to get the dynamic start reason (Windows 8+)
		if reason, err := svc.DynamicStartReason(); err == nil {
			startedAtBoot = (reason&svc.StartReasonAuto) != 0 || (reason&svc.StartReasonDelayedAuto) != 0
		} else if errors.Is(err, windows.ERROR_PROC_NOT_FOUND) {
			// Windows 7 compatibility: if service started within 10 minutes of boot, assume it started at boot
			startedAtBoot = windows.DurationSinceBoot() < time.Minute*10
		} else {
			logger.Error("Unable to determine service start reason: %v", err)
		}
	})
	return startedAtBoot
}

