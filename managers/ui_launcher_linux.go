package managers

import (
	"os"
	"time"
)

// currentSessionID identifies this UI to the daemon. Linux runs one UI per
// user, so the UID plays the part of the Windows session ID.
func currentSessionID() uint32 {
	return uint32(os.Getuid())
}

// RequestUIAction asks the running UI to perform action. The Linux daemon
// socket arrives in a later step; until then it always reports false.
func RequestUIAction(action UIAction) bool { return false }

// RequestUILaunch asks the daemon to show the UI; see RequestUIAction.
func RequestUILaunch() bool { return false }

// RequestUILaunchWithRetry is RequestUILaunch retried until timeout.
func RequestUILaunchWithRetry(timeout time.Duration) bool { return false }
