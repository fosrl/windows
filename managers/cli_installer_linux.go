package managers

import "os/exec"

// IsCLIInstalled reports whether the pangolin CLI is on PATH.
func IsCLIInstalled() bool {
	_, err := exec.LookPath("pangolin")
	return err == nil
}

// InstallCLI is not offered on Linux yet; the CLI comes from its own package.
func InstallCLI(userToken uintptr) error { return errNotImplemented }
