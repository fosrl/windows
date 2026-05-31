//go:build windows

package managers

import (
	"errors"

	"github.com/fosrl/windows/auth"
)

var errDevicePostureUnavailable = errors.New("platform fingerprint not available from manager")

type devicePostureIPCBridge struct{}

func (devicePostureIPCBridge) PlatformFingerprint() (string, error) {
	snapshot, err := IPCClientGetDevicePosture()
	if err != nil {
		return "", err
	}
	fp, ok := snapshot.PlatformFingerprint()
	if !ok {
		return "", errDevicePostureUnavailable
	}
	return fp, nil
}

func registerDevicePostureIPC() {
	auth.SetDevicePostureIPC(devicePostureIPCBridge{})
}
