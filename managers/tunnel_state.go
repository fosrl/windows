package managers

import "github.com/fosrl/windows-linux/tunnel"

// TunnelState is an alias for tunnel.State to make it accessible from the managers package
type TunnelState = tunnel.State

// Tunnel state constants
const (
	TunnelStateStopped      = tunnel.StateStopped
	TunnelStateStarting     = tunnel.StateStarting
	TunnelStateRegistering  = tunnel.StateRegistering
	TunnelStateRegistered   = tunnel.StateRegistered
	TunnelStateRunning      = tunnel.StateRunning
	TunnelStateReconnecting = tunnel.StateReconnecting
	TunnelStateStopping     = tunnel.StateStopping
	TunnelStateInvalid      = tunnel.StateInvalid
	TunnelStateError        = tunnel.StateError
)
