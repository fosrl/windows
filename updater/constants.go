//go:build windows

package updater

const (
	// releasePublicKeyBase64 is the base64-encoded Ed25519 public key used to verify update signatures.
	// This should be replaced with your own public key.
	releasePublicKeyBase64 = "RWQWK7GF/RR35J1NETi57nk9cbngz7sBDsCrC3yce2CcKfACMpIcpvKV"
	// updateServerHost is the hostname of the update server
	updateServerHost = "static.pangolin.net"
	// updateServerPort is the port number for the update server
	updateServerPort = 443
	// updateServerUseHttps indicates whether to use HTTPS
	updateServerUseHttps = true
	// latestVersionPath is the path to the latest version signature file
	latestVersionPath = "/windows-client/latest.sig"
	// msiArchPrefix is the prefix for MSI filenames (use %s for architecture)
	msiArchPrefix = "pangolin-%s-"
	// msiSuffix is the suffix for MSI filenames
	msiSuffix = ".msi"
)
