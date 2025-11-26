//go:build windows

package managers

import "github.com/fosrl/windows/tunnel"

// IPCAdapter implements tunnel.IPCClient interface to avoid circular dependencies
type IPCAdapter struct{}

// NewIPCAdapter creates a new IPCAdapter instance
func NewIPCAdapter() *IPCAdapter {
	return &IPCAdapter{}
}

// StartTunnel starts a tunnel with the given configuration
func (a *IPCAdapter) StartTunnel(config tunnel.Config) error {
	return IPCClientStartTunnel(TunnelConfig(config))
}

// StopTunnel stops the tunnel
func (a *IPCAdapter) StopTunnel() error {
	return IPCClientStopTunnel()
}

// RegisterStateChangeCallback registers a callback for tunnel state changes
// Returns an unregister function
func (a *IPCAdapter) RegisterStateChangeCallback(cb func(tunnel.State)) func() {
	callback := IPCClientRegisterTunnelStateChange(func(state TunnelState) {
		cb(tunnel.State(state))
	})
	return func() {
		callback.Unregister()
	}
}

// SwitchOrg switches the organization for the running tunnel
func (a *IPCAdapter) SwitchOrg(orgID string) error {
	return IPCClientSwitchOrg(orgID)
}
