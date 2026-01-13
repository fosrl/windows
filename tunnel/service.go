//go:build windows

package tunnel

import (
	"encoding/json"
	"sync"

	"github.com/fosrl/newt/logger"
)

var (
	tunnelState       State = StateStopped
	tunnelStateLock   sync.RWMutex
	stateChangeCb     func(State)
	stateChangeLock   sync.RWMutex
	currentTunnelName string
	tunnelNameLock    sync.RWMutex
)

// SetStateChangeCallback sets a callback that will be called when tunnel state changes
func SetStateChangeCallback(cb func(State)) {
	stateChangeLock.Lock()
	defer stateChangeLock.Unlock()
	stateChangeCb = cb
}

func notifyStateChange(state State) {
	stateChangeLock.RLock()
	cb := stateChangeCb
	stateChangeLock.RUnlock()
	if cb != nil {
		cb(state)
	}
}

// OLMNamedPipePath is the Windows named pipe path for OLM API communication
const OLMNamedPipePath = `\\.\pipe\pangolin-olm`

// State represents the state of a tunnel
type State int

const (
	StateStopped State = iota
	StateStarting
	StateRegistering
	StateRegistered
	StateRunning
	StateReconnecting
	StateStopping
	StateInvalid
	StateError
)

// String returns the string representation of the tunnel state
func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRegistering:
		return "registering"
	case StateRegistered:
		return "registered"
	case StateRunning:
		return "running"
	case StateReconnecting:
		return "reconnecting"
	case StateStopping:
		return "stopping"
	case StateInvalid:
		return "invalid"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// DisplayText returns a human-readable display text for the tunnel state
func (s State) DisplayText() string {
	switch s {
	case StateStopped:
		return "Disconnected"
	case StateStarting:
		return "Connecting..."
	case StateRegistering:
		return "Registering..."
	case StateRegistered:
		return "Connecting..."
	case StateRunning:
		return "Connected"
	case StateReconnecting:
		return "Reconnecting..."
	case StateStopping:
		return "Disconnecting..."
	case StateInvalid:
		return "Invalid"
	case StateError:
		return "Error"
	default:
		return "Unknown"
	}
}

// Config represents the tunnel configuration
type Config struct {
	Name string `json:"name"` // for Windows service name

	Endpoint            string   `json:"endpoint"`
	ID                  string   `json:"id"`
	Secret              string   `json:"secret"`
	MTU                 int      `json:"mtu"`
	DNS                 string   `json:"dns"`
	Holepunch           bool     `json:"holepunch"`
	PingIntervalSeconds int      `json:"pingIntervalSeconds"`
	PingTimeoutSeconds  int      `json:"pingTimeoutSeconds"`
	UserToken           string   `json:"userToken"`
	OrgID               string   `json:"orgId"`
	InterfaceName       string   `json:"interfaceName"`
	UpstreamDNS         []string `json:"upstreamDns"`
	OverrideDNS         bool     `json:"overrideDns"`
	TunnelDNS           bool     `json:"tunnelDns"`
}

func StartTunnel(config Config) error {
	logger.Info("Tunnel: StartTunnel called")

	// Log the config
	logger.Info("Tunnel: Starting tunnel with config")

	// Store tunnel name for later use
	tunnelNameLock.Lock()
	currentTunnelName = config.Name
	tunnelNameLock.Unlock()

	// Update state to registering when user clicks start
	tunnelStateLock.Lock()
	tunnelState = StateRegistering
	tunnelStateLock.Unlock()
	notifyStateChange(StateRegistering)

	// Install and start the Windows service
	// This will spawn a new process
	// Note: InstallTunnel is called from managers package to avoid import cycle
	// This function should be called from managers/ipc_server.go
	// Convert config to JSON for storage
	configJSON, err := json.Marshal(config)
	if err != nil {
		logger.Error("Tunnel: Failed to marshal config: %v", err)
		tunnelStateLock.Lock()
		tunnelState = StateStopped
		tunnelStateLock.Unlock()
		notifyStateChange(StateStopped)
		return err
	}
	err = installTunnelFromManager(string(configJSON))
	if err != nil {
		logger.Error("Tunnel: Failed to install tunnel service: %v", err)
		tunnelStateLock.Lock()
		tunnelState = StateStopped
		tunnelStateLock.Unlock()
		notifyStateChange(StateStopped)
		return err
	}

	return nil
}

func StopTunnel() error {
	logger.Info("Tunnel: StopTunnel called")

	// Update state
	tunnelStateLock.Lock()
	tunnelState = StateStopping
	tunnelStateLock.Unlock()
	notifyStateChange(StateStopping)

	// Get tunnel name that was stored when starting
	tunnelNameLock.RLock()
	name := currentTunnelName
	tunnelNameLock.RUnlock()

	if name == "" {
		// Fallback if name wasn't set (shouldn't happen)
		name = "pangolin-tunnel"
	}

	// Uninstall the Windows service (this stops and removes it)
	// Note: UninstallTunnel is called from managers package to avoid import cycle
	// This function should be called from managers/ipc_server.go
	err := uninstallTunnelFromManager(name)
	if err != nil {
		logger.Error("Tunnel: Failed to uninstall tunnel service: %v", err)
		// Still transition to stopped on error
	}

	tunnelStateLock.Lock()
	tunnelState = StateStopped
	tunnelStateLock.Unlock()
	logger.Info("Tunnel: State transitioned to stopped")
	notifyStateChange(StateStopped)

	// Clear tunnel name
	tunnelNameLock.Lock()
	currentTunnelName = ""
	tunnelNameLock.Unlock()

	return err
}

func GetState() State {
	tunnelStateLock.RLock()
	defer tunnelStateLock.RUnlock()
	return tunnelState
}

func SetState(state State) {
	tunnelStateLock.Lock()
	defer tunnelStateLock.Unlock()
	tunnelState = state
}

// GetTunnelName returns the name of the currently active tunnel
func GetTunnelName() string {
	tunnelNameLock.RLock()
	defer tunnelNameLock.RUnlock()
	return currentTunnelName
}

// InstallTunnelCallback and UninstallTunnelCallback allow managers package to register
// functions to avoid import cycles
var (
	installTunnelFunc   func(string) error
	uninstallTunnelFunc func(string) error
)

func SetInstallTunnelCallback(fn func(string) error) {
	installTunnelFunc = fn
}

func SetUninstallTunnelCallback(fn func(string) error) {
	uninstallTunnelFunc = fn
}

func installTunnelFromManager(configJSON string) error {
	if installTunnelFunc == nil {
		return nil // Stub - will be set by managers package
	}
	return installTunnelFunc(configJSON)
}

// ConfigFromJSON parses a JSON string into a Config struct
func ConfigFromJSON(jsonStr string) (Config, error) {
	var config Config
	err := json.Unmarshal([]byte(jsonStr), &config)
	return config, err
}

// ToJSON converts a Config struct to JSON string
func (c Config) ToJSON() (string, error) {
	jsonBytes, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

func uninstallTunnelFromManager(name string) error {
	if uninstallTunnelFunc == nil {
		return nil // Stub - will be set by managers package
	}
	return uninstallTunnelFunc(name)
}
