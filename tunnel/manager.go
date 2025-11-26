//go:build windows

package tunnel

import (
	"fmt"
	"sync"

	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/secrets"

	"github.com/fosrl/newt/logger"
)

// IPCClient provides an interface for IPC operations needed by the tunnel manager
// This avoids circular dependencies between tunnel and managers packages
type IPCClient interface {
	StartTunnel(config Config) error
	StopTunnel() error
	SwitchOrg(orgID string) error
	RegisterStateChangeCallback(cb func(State)) func() // Returns unregister function
}

// Manager manages tunnel connection state and operations
// It provides a simplified API for the UI layer, abstracting away IPC details
type Manager struct {
	mu            sync.RWMutex
	currentState  State
	isConnected   bool
	stateCallback func(State)
	unregisterCb  func()
	ipcClient     IPCClient
	authManager   *auth.AuthManager
	configManager *config.ConfigManager
	secretManager *secrets.SecretManager
}

// NewManager creates a new Manager instance
func NewManager(am *auth.AuthManager, cm *config.ConfigManager, sm *secrets.SecretManager, ipc IPCClient) *Manager {
	tm := &Manager{
		currentState:  StateStopped,
		isConnected:   false,
		authManager:   am,
		configManager: cm,
		secretManager: sm,
		ipcClient:     ipc,
	}

	// Register for tunnel state change notifications
	if ipc != nil {
		tm.unregisterCb = ipc.RegisterStateChangeCallback(func(state State) {
			tm.mu.Lock()
			tm.currentState = state
			tm.isConnected = (state == StateRunning)
			tm.mu.Unlock()

			// Call user-provided callback if set
			if tm.stateCallback != nil {
				tm.stateCallback(state)
			}
		})
	}

	// Get initial state
	go func() {
		// Initial state will be updated when the first state change notification arrives
		// For now, we assume stopped
	}()

	return tm
}

// Close cleans up resources used by the Manager
func (tm *Manager) Close() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.unregisterCb != nil {
		tm.unregisterCb()
		tm.unregisterCb = nil
	}
}

// State returns the current tunnel state
func (tm *Manager) State() State {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.currentState
}

// IsConnected returns whether the tunnel is currently connected
func (tm *Manager) IsConnected() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.isConnected
}

// RegisterStateChangeCallback registers a callback that will be called when tunnel state changes
func (tm *Manager) RegisterStateChangeCallback(cb func(State)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.stateCallback = cb
}

// buildConfig builds the tunnel configuration from auth manager, config manager, and secret manager
func (tm *Manager) buildConfig() (Config, error) {
	// Get session token from secret manager
	userToken, found := tm.secretManager.GetSecret("session-token")
	if !found || userToken == "" {
		return Config{}, fmt.Errorf("session token not found")
	}

	// Get current organization
	currentOrg := tm.authManager.CurrentOrg()
	if currentOrg == nil {
		return Config{}, fmt.Errorf("no organization selected")
	}

	userId := tm.authManager.CurrentUser().UserId
	olmId, found := tm.secretManager.GetOlmId(userId)
	if !found || olmId == "" {
		return Config{}, fmt.Errorf("OLM ID not found")
	}
	olmSecret, found := tm.secretManager.GetOlmSecret(userId)
	if !found || olmSecret == "" {
		return Config{}, fmt.Errorf("OLM secret not found")
	}

	config := Config{
		Name:                "pangolin-tunnel",
		ID:                  olmId,
		Secret:              olmSecret,
		UserToken:           userToken,
		MTU:                 1280,
		Holepunch:           false,
		PingIntervalSeconds: 5,
		PingTimeoutSeconds:  5,
		Endpoint:            tm.configManager.GetHostname(),
		DNS:                 "8.8.8.8",
		OrgID:               currentOrg.Id,
		InterfaceName:       "olm",
		UpstreamDNS:         []string{"8.8.8.8:53"},
	}

	return config, nil
}

// ConnectionError represents a connection error with a user-friendly message
type ConnectionError struct {
	Title   string
	Message string
	Err     error
}

func (e *ConnectionError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

// formatConnectionError creates a user-friendly error message
func formatConnectionError(title, message string, err error) *ConnectionError {
	return &ConnectionError{
		Title:   title,
		Message: message,
		Err:     err,
	}
}

// Connect starts the tunnel, building the configuration internally
func (tm *Manager) Connect() error {
	tm.mu.RLock()
	currentState := tm.currentState
	tm.mu.RUnlock()

	// Check if already connected or connecting
	if currentState == StateRunning {
		logger.Info("Tunnel is already running")
		return formatConnectionError(
			"Tunnel Already Running",
			"The tunnel is already running. Please disconnect it before connecting again.",
			nil,
		)
	}
	if currentState == StateStarting || currentState == StateRegistering || currentState == StateRegistered {
		logger.Info("Tunnel is already starting/connecting")
		return formatConnectionError(
			"Tunnel Already Starting",
			"The tunnel is already starting. Please wait for it to complete.",
			nil,
		)
	}

	// Require an organization to be selected before connecting
	currentOrg := tm.authManager.CurrentOrg()
	if currentOrg == nil {
		logger.Error("No organization selected, aborting connection")
		return formatConnectionError(
			"No Organization Selected",
			"Please select an organization before connecting.",
			nil,
		)
	}

	// Check org access before connecting
	hasAccess, err := tm.authManager.CheckOrgAccess(currentOrg.Id)
	if err != nil {
		logger.Error("Failed to check org access: %v", err)
		return formatConnectionError(
			"Access Check Failed",
			fmt.Sprintf("Failed to verify access to the organization: %v", err),
			err,
		)
	}
	if !hasAccess {
		logger.Error("Access denied for org %s, aborting connection", currentOrg.Id)
		return formatConnectionError(
			"Access Denied",
			"You do not have access to the selected organization.",
			nil,
		)
	}

	// Ensure OLM credentials exist before connecting
	currentUser := tm.authManager.CurrentUser()
	if currentUser != nil && currentUser.UserId != "" {
		if err := tm.authManager.EnsureOlmCredentials(currentUser.UserId); err != nil {
			logger.Error("Failed to ensure OLM credentials: %v", err)
			return formatConnectionError(
				"OLM Credentials Error",
				fmt.Sprintf("Failed to set up device credentials: %v", err),
				err,
			)
		}
	} else {
		// Try to get userId from config as fallback
		cfg := tm.configManager.GetConfig()
		if cfg != nil && cfg.UserId != nil && *cfg.UserId != "" {
			if err := tm.authManager.EnsureOlmCredentials(*cfg.UserId); err != nil {
				logger.Error("Failed to ensure OLM credentials: %v", err)
				return formatConnectionError(
					"OLM Credentials Error",
					fmt.Sprintf("Failed to set up device credentials: %v", err),
					err,
				)
			}
		} else {
			logger.Error("No user ID available for OLM credentials")
			return formatConnectionError(
				"Authentication Error",
				"No user ID available. Please log in again.",
				nil,
			)
		}
	}

	// Build config from dependencies
	config, err := tm.buildConfig()
	if err != nil {
		logger.Error("Failed to build tunnel config: %v", err)
		// Format config build errors
		if err.Error() == "session token not found" {
			return formatConnectionError(
				"Authentication Error",
				"Session token not found. Please log in again.",
				err,
			)
		}
		return formatConnectionError(
			"Configuration Error",
			fmt.Sprintf("Failed to build tunnel configuration: %v", err),
			err,
		)
	}

	logger.Info("Connecting tunnel with config: Name=%s, Endpoint=%s", config.Name, config.Endpoint)
	if tm.ipcClient == nil {
		return formatConnectionError(
			"Connection Error",
			"IPC client not initialized. Please restart the application.",
			nil,
		)
	}
	err = tm.ipcClient.StartTunnel(config)
	if err != nil {
		logger.Error("Failed to start tunnel: %v", err)
		return formatConnectionError(
			"Connection Failed",
			fmt.Sprintf("Failed to start the tunnel: %v", err),
			err,
		)
	}

	return nil
}

// Disconnect stops the tunnel
func (tm *Manager) Disconnect() error {
	tm.mu.RLock()
	currentState := tm.currentState
	tm.mu.RUnlock()

	// Check if already disconnected or disconnecting
	if currentState == StateStopped {
		logger.Info("Tunnel is already stopped")
		return nil
	}
	if currentState == StateStopping {
		logger.Info("Tunnel is already stopping")
		return nil
	}

	logger.Info("Disconnecting tunnel")
	if tm.ipcClient == nil {
		return fmt.Errorf("IPC client not initialized")
	}
	err := tm.ipcClient.StopTunnel()
	if err != nil {
		logger.Error("Failed to stop tunnel: %v", err)
		return err
	}

	return nil
}

// SwitchOrg switches the organization for the running tunnel
func (tm *Manager) SwitchOrg(orgID string) error {
	tm.mu.RLock()
	currentState := tm.currentState
	tm.mu.RUnlock()

	// Only allow switching org if tunnel is running
	if currentState != StateRunning {
		logger.Info("Tunnel is not running, cannot switch organization")
		return fmt.Errorf("tunnel is not running")
	}

	logger.Info("Switching tunnel organization to: %s", orgID)
	if tm.ipcClient == nil {
		return fmt.Errorf("IPC client not initialized")
	}
	err := tm.ipcClient.SwitchOrg(orgID)
	if err != nil {
		logger.Error("Failed to switch organization: %v", err)
		return err
	}

	return nil
}

// GetStatusDisplayText returns a human-readable status text for the current state
func (tm *Manager) GetStatusDisplayText() string {
	tm.mu.RLock()
	state := tm.currentState
	tm.mu.RUnlock()

	switch state {
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
