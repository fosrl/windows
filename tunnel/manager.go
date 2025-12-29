//go:build windows

package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
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
	RegisterStateChangeCallback(cb func(State)) func() // Returns unregister function
}

// Manager manages tunnel connection state and operations
// It provides a simplified API for the UI layer, abstracting away IPC details
type Manager struct {
	mu             sync.RWMutex
	currentState   State
	isConnected    bool
	stateCallback  func(State)
	unregisterCb   func()
	ipcClient      IPCClient
	authManager    *auth.AuthManager
	configManager  *config.ConfigManager
	accountManager *config.AccountManager
	secretManager  *secrets.SecretManager
	// Status polling fields
	pollCtx       context.Context
	pollCancel    context.CancelFunc
	pollingActive bool
}

// NewManager creates a new Manager instance
func NewManager(
	authManager *auth.AuthManager,
	configManager *config.ConfigManager,
	accountManager *config.AccountManager,
	secretManager *secrets.SecretManager,
	ipcClient IPCClient,
) *Manager {
	tm := &Manager{
		currentState:   StateStopped,
		isConnected:    false,
		authManager:    authManager,
		configManager:  configManager,
		accountManager: accountManager,
		secretManager:  secretManager,
		ipcClient:      ipcClient,
	}

	// Register for tunnel state change notifications
	if ipcClient != nil {
		tm.unregisterCb = ipcClient.RegisterStateChangeCallback(func(state State) {
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

	// Stop status polling if active
	if tm.pollingActive && tm.pollCancel != nil {
		tm.pollCancel()
		tm.pollingActive = false
		tm.pollCancel = nil
		tm.pollCtx = nil
	}

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
	activeAccount, err := tm.accountManager.ActiveAccount()
	if err != nil {
		return Config{}, err
	}

	// Get session token from secret manager
	userToken, found := tm.secretManager.GetSessionToken(activeAccount.UserID)
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

	// Get DNS settings from config manager
	primaryDNS := tm.configManager.GetPrimaryDNS()
	secondaryDNS := tm.configManager.GetSecondaryDNS()
	dnsOverride := tm.configManager.GetDNSOverride()
	dnsTunnel := tm.configManager.GetDNSTunnel()

	// Build UpstreamDNS array with :53 appended to each
	upstreamDNS := []string{primaryDNS + ":53"}
	if secondaryDNS != "" {
		upstreamDNS = append(upstreamDNS, secondaryDNS+":53")
	}

	config := Config{
		Name:                "olm",
		ID:                  olmId,
		Secret:              olmSecret,
		UserToken:           userToken,
		MTU:                 1280,
		Holepunch:           true,
		PingIntervalSeconds: 5,
		PingTimeoutSeconds:  5,
		Endpoint:            activeAccount.Hostname,
		DNS:                 primaryDNS, // Use primary DNS without :53
		OrgID:               currentOrg.Id,
		InterfaceName:       "Pangolin",
		UpstreamDNS:         upstreamDNS, // Each value has :53 appended
		OverrideDNS:         dnsOverride,
		TunnelDNS:           dnsTunnel,
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
		activeAccount, err := tm.accountManager.ActiveAccount()
		if err != nil {
			logger.Error("Failed to get active account: %v", err)
			return formatConnectionError(
				"Authentication Error",
				"No user ID available. Please log in again.",
				err,
			)
		}

		if err := tm.authManager.EnsureOlmCredentials(activeAccount.UserID); err != nil {
			logger.Error("Failed to ensure OLM credentials: %v", err)
			return formatConnectionError(
				"OLM Credentials Error",
				fmt.Sprintf("Failed to set up device credentials: %v", err),
				err,
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

	logger.Info("Starting status polling")
	tm.StartStatusPolling()

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

	tm.StopStatusPolling()
	logger.Info("Disconnected tunnel")

	return nil
}

// OLMStatusResponse represents the status response from OLM API
type OLMStatusResponse struct {
	Connected       bool                   `json:"connected"`
	Registered      bool                   `json:"registered"`
	Terminated      bool                   `json:"terminated"`
	Version         string                 `json:"version,omitempty"`
	OrgID           string                 `json:"orgId,omitempty"`
	PeerStatuses    map[int]*OLMPeerStatus `json:"peers,omitempty"`
	NetworkSettings map[string]interface{} `json:"networkSettings,omitempty"`
}

// OLMPeerStatus represents the status of a peer connection
type OLMPeerStatus struct {
	SiteID    int           `json:"siteId"`
	SiteName  string        `json:"name"`
	Connected bool          `json:"connected"`
	RTT       time.Duration `json:"rtt"`
	LastSeen  time.Time     `json:"lastSeen"`
	Endpoint  string        `json:"endpoint,omitempty"`
	IsRelay   bool          `json:"isRelay"`
	PeerIP    string        `json:"peerAddress,omitempty"`
}

// SwitchOrgRequest represents the request body for switching organizations
type SwitchOrgRequest struct {
	OrgID string `json:"orgId"`
}

// getOLMPipePath returns the Windows named pipe path for OLM
func getOLMPipePath() string {
	return OLMNamedPipePath
}

// createOLMHTTPClient creates an HTTP client that can connect to OLM via named pipe
func createOLMHTTPClient() (*http.Client, error) {
	pipePath := getOLMPipePath()

	// Create a custom transport that dials the named pipe
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Ignore network and addr, we're connecting to a named pipe
			return winio.DialPipe(pipePath, nil)
		},
		DisableKeepAlives: false,
		MaxIdleConns:      1,
		IdleConnTimeout:   30 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	return client, nil
}

// GetOLMStatus retrieves the status from OLM via the named pipe API
func (tm *Manager) GetOLMStatus() (*OLMStatusResponse, error) {
	client, err := createOLMHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create OLM HTTP client: %w", err)
	}

	// Make GET request to /status endpoint
	// Use a dummy URL since we're connecting via named pipe
	req, err := http.NewRequest("GET", "http://localhost/status", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to OLM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OLM API returned status %d: %s", resp.StatusCode, string(body))
	}

	var statusResp OLMStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode OLM status response: %w", err)
	}

	return &statusResp, nil
}

// SwitchOLMOrg switches the organization in OLM via the named pipe API
func (tm *Manager) SwitchOLMOrg(orgID string) error {
	tm.mu.RLock()
	currentState := tm.currentState
	tm.mu.RUnlock()

	// Only allow switching org if tunnel is running
	if currentState != StateRunning {
		logger.Info("Tunnel is not running, cannot switch organization")
		return fmt.Errorf("tunnel is not running")
	}

	if orgID == "" {
		return fmt.Errorf("orgID cannot be empty")
	}

	logger.Info("Switching tunnel organization to: %s", orgID)

	client, err := createOLMHTTPClient()
	if err != nil {
		return fmt.Errorf("failed to create OLM HTTP client: %w", err)
	}

	// Create request body
	reqBody := SwitchOrgRequest{
		OrgID: orgID,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make POST request to /switch-org endpoint
	req, err := http.NewRequest("POST", "http://localhost/switch-org", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to OLM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OLM API returned status %d: %s", resp.StatusCode, string(body))
	}

	logger.Info("Successfully switched OLM organization to: %s", orgID)
	return nil
}

// StartStatusPolling starts polling the OLM status endpoint every 1 second
func (tm *Manager) StartStatusPolling() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// If already polling, stop the previous polling first
	if tm.pollingActive && tm.pollCancel != nil {
		tm.pollCancel()
	}

	// Create new context for polling
	tm.pollCtx, tm.pollCancel = context.WithCancel(context.Background())
	tm.pollingActive = true

	// Start polling goroutine
	// Capture context to avoid race conditions
	pollCtx := tm.pollCtx
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-pollCtx.Done():
				logger.Info("Status polling stopped")
				tm.mu.Lock()
				tm.pollingActive = false
				tm.mu.Unlock()
				return
			case <-ticker.C:
				// Poll the status
				status, err := tm.GetOLMStatus()
				if err != nil {
					logger.Error("Failed to poll OLM status: %v", err)
					continue
				}

				// If terminated, disconnect the tunnel
				if status.Terminated {
					logger.Info("OLM status indicates terminated, disconnecting tunnel")
					if err := tm.Disconnect(); err != nil {
						logger.Error("Failed to disconnect tunnel after termination: %v", err)
					}
					continue
				}

				// Update tunnel state based on OLM status
				// Connected takes precedence over Registered
				var newState State
				if status.Connected {
					newState = StateRunning
				} else if status.Registered {
					newState = StateRegistered
				} else {
					// If neither connected nor registered, don't update state
					// (keep current state)
					continue
				}

				// Update the global tunnel state (for consistency with GetState())
				SetState(newState)

				// Update Manager's internal state and trigger callback (this notifies the UI)
				tm.mu.Lock()
				oldState := tm.currentState
				tm.currentState = newState
				tm.isConnected = (newState == StateRunning)
				callback := tm.stateCallback
				tm.mu.Unlock()

				// Only trigger callback if state actually changed
				if oldState != newState && callback != nil {
					callback(newState)
				}
			}
		}
	}()

	logger.Info("Started OLM status polling (every 1 second)")
}

// StopStatusPolling stops the status polling
func (tm *Manager) StopStatusPolling() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.pollingActive {
		logger.Info("Status polling is not active")
		return
	}

	if tm.pollCancel != nil {
		tm.pollCancel()
		tm.pollCancel = nil
		tm.pollCtx = nil
		tm.pollingActive = false
		logger.Info("Stopped OLM status polling")
	}
}
