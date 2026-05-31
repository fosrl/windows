//go:build windows

package auth

// DevicePostureIPC fetches cached device fingerprint/posture from the manager service.
type DevicePostureIPC interface {
	PlatformFingerprint() (string, error)
}

var devicePostureIPC DevicePostureIPC

// SetDevicePostureIPC registers the manager IPC implementation (call after InitializeIPCClient).
func SetDevicePostureIPC(client DevicePostureIPC) {
	devicePostureIPC = client
}
