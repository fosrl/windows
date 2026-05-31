//go:build windows

package fingerprint

// DevicePostureSnapshot is fingerprint and posture data from the manager cache.
type DevicePostureSnapshot struct {
	Fingerprint map[string]any
	Postures    map[string]any
}

// PlatformFingerprint returns the hashed platform fingerprint from the snapshot.
func (s DevicePostureSnapshot) PlatformFingerprint() (string, bool) {
	if s.Fingerprint == nil {
		return "", false
	}
	v, ok := s.Fingerprint["platformFingerprint"].(string)
	return v, ok && v != ""
}

// CachedDevicePosture returns the current cached snapshot, if available.
func CachedDevicePosture() (DevicePostureSnapshot, bool) {
	fp, postures, ok := SnapshotPostureMemory()
	if !ok {
		return DevicePostureSnapshot{}, false
	}
	return DevicePostureSnapshot{
		Fingerprint: fp,
		Postures:    postures,
	}, true
}
