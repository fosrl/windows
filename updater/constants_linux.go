package updater

import (
	"os"
	"runtime"
	"strings"
)

const (
	// latestVersionPath is the signed Linux manifest on the update server.
	latestVersionPath = "/linux-client/latest.sig"
	// packageFormatFile is written by each package (deb, rpm, Arch) to say how
	// Pangolin was installed; see packaging/linux/nfpm.yaml.
	packageFormatFile = "/usr/share/pangolin-client/package-format"
)

// Package formats, as written to packageFormatFile.
const (
	formatDeb  = "deb"
	formatRPM  = "rpm"
	formatArch = "archlinux"
	formatNone = ""
)

// packageFormat reports how this copy was installed, or formatNone for a
// build installed by hand.
func packageFormat() string {
	b, err := os.ReadFile(packageFormatFile)
	if err != nil {
		return formatNone
	}
	return strings.TrimSpace(string(b))
}

// candidatePattern returns the prefix and suffix around the version in the
// names of update files for this machine: pangolin-client-<arch>-<version>
// followed by .deb or .rpm. Arch and hand-installed builds look at the .deb
// entries only to learn the latest version.
func candidatePattern() (prefix, suffix string) {
	prefix = "pangolin-client-" + runtime.GOARCH + "-"
	if packageFormat() == formatRPM {
		return prefix, ".rpm"
	}
	return prefix, ".deb"
}
