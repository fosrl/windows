package managers

import "errors"

// errNotImplemented marks daemon features that arrive with the Linux daemon,
// which runs each tunnel as a child process instead of a Windows service.
var errNotImplemented = errors.New("not implemented on Linux yet")

// InstallTunnel starts a tunnel for configJSON.
func InstallTunnel(configJSON string) error { return errNotImplemented }

// UninstallTunnel stops the tunnel called name.
func UninstallTunnel(name string) error { return errNotImplemented }
