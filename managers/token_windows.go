package managers

import (
	"github.com/fosrl/windows-linux/fingerprint"
	"golang.org/x/sys/windows"
)

// userToken is the connected user's elevated token, or 0 if they are not an admin.
type userToken = windows.Token

// postureForClient returns the cached posture as reported for a client. On
// Windows it is reported as gathered.
func postureForClient(snapshot fingerprint.DevicePostureSnapshot, clientUserKey string) fingerprint.DevicePostureSnapshot {
	return snapshot
}

// prepareUpdateRestart is a no-op on Windows: the updater writes the
// restart-ui flag itself before running the MSI.
func prepareUpdateRestart() {}
