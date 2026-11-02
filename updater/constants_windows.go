package updater

import (
	"fmt"

	"github.com/fosrl/windows/version"
)

const (
	// latestVersionPath is the path to the latest version signature file
	latestVersionPath = "/windows-client/latest.sig"
	// msiArchPrefix is the prefix for MSI filenames (use %s for architecture)
	msiArchPrefix = "pangolin-%s-"
	// msiSuffix is the suffix for MSI filenames
	msiSuffix = ".msi"
)

// candidatePattern returns the prefix and suffix around the version in the
// names of update files for this machine: pangolin-<arch>-<version>.msi.
func candidatePattern() (prefix, suffix string) {
	return fmt.Sprintf(msiArchPrefix, version.Arch()), msiSuffix
}

// ManualUpdate is empty on Windows: the MSI installs updates in the app.
func ManualUpdate() (instructions, command string) { return "", "" }
