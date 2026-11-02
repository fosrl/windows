package fingerprint

import (
	"os"
	"os/user"
	"runtime"

	"golang.org/x/sys/unix"
)

// gatherDevicePosture returns the basic device identity on Linux. Posture
// checks and the hardware fingerprint come with the Linux daemon, ported from
// fosrl/cli's fingerprint_linux.go; until then they are left empty.
func gatherDevicePosture() (*Fingerprint, *PostureChecks) {
	fp := &Fingerprint{
		Platform:     "linux",
		Architecture: runtime.GOARCH,
	}
	if u, err := user.Current(); err == nil {
		fp.Username = u.Username
	}
	fp.Hostname, _ = os.Hostname()
	var uts unix.Utsname
	if unix.Uname(&uts) == nil {
		fp.KernelVersion = unix.ByteSliceToString(uts.Release[:])
	}
	return fp, &PostureChecks{}
}

func cpuFingerprint() string { return "" }

func dmiFingerprint() string { return "" }
