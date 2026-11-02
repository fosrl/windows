package fingerprint

import "testing"

// TestGatherDevicePosture runs the real checks on this machine; it only
// verifies they complete and fill in the basics.
func TestGatherDevicePosture(t *testing.T) {
	fp, postures := gatherDevicePosture()
	if fp.Platform != "linux" || fp.Hostname == "" || fp.KernelVersion == "" || fp.PlatformFingerprint == "" {
		t.Fatalf("incomplete fingerprint: %+v", fp)
	}
	t.Logf("fingerprint: %+v", *fp)
	t.Logf("postures: %+v", *postures)
}
