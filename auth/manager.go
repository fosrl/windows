//go:build windows

package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/secrets"

	"github.com/fosrl/newt/logger"
)

// AuthError represents authentication-specific errors
type AuthError struct {
	Type AuthErrorType
}

type AuthErrorType int

const (
	AuthErrorTwoFactorRequired AuthErrorType = iota
	AuthErrorEmailVerificationRequired
	AuthErrorDeviceCodeExpired
	AuthErrorInvalidToken
)

func (e *AuthError) Error() string {
	switch e.Type {
	case AuthErrorTwoFactorRequired:
		return "Two-factor authentication code required"
	case AuthErrorEmailVerificationRequired:
		return "Email verification required"
	case AuthErrorDeviceCodeExpired:
		return "Device code expired. Please try again."
	case AuthErrorInvalidToken:
		return "Invalid session token"
	default:
		return "Authentication error"
	}
}

// AuthManager manages authentication state and operations
type AuthManager struct {
	apiClient      *api.APIClient
	configManager  *config.ConfigManager
	accountManager *config.AccountManager
	secretManager  *secrets.SecretManager

	// State
	mu                 sync.RWMutex
	isAuthenticated    bool
	currentUser        *api.User
	currentOrg         *api.Org
	organizations      []api.Org
	isInitializing     bool
	errorMessage       *string
	deviceAuthCode     *string
	deviceAuthLoginURL *string
}

// NewAuthManager creates a new AuthManager instance
func NewAuthManager(
	apiClient *api.APIClient,
	configManager *config.ConfigManager,
	accountManager *config.AccountManager,
	secretManager *secrets.SecretManager,
) *AuthManager {
	return &AuthManager{
		apiClient:      apiClient,
		configManager:  configManager,
		accountManager: accountManager,
		secretManager:  secretManager,
		isInitializing: true,
	}
}

// Initialize loads session token from secrets and verifies authentication
func (am *AuthManager) Initialize() error {
	am.mu.Lock()
	am.isInitializing = true
	am.mu.Unlock()

	defer func() {
		am.mu.Lock()
		am.isInitializing = false
		am.mu.Unlock()
	}()

	activeAccount, _ := am.accountManager.ActiveAccount()
	if activeAccount != nil {
		// Load session token from Keychain
		token, found := am.secretManager.GetSessionToken(activeAccount.UserID)
		if found && token != "" {
			am.apiClient.UpdateSessionToken(token)

			// Always fetch the latest user info to verify the user exists and update stored info
			user, err := am.apiClient.GetUser()
			if err != nil {
				// Token is invalid or user doesn't exist, clear it
				am.mu.Lock()
				am.isAuthenticated = false
				am.mu.Unlock()
				return nil // Not an error, just not authenticated
			}

			// Update stored config with latest user info
			return am.handleSuccessfulAuth(user, activeAccount.Hostname, token)
		}
	}

	am.mu.Lock()
	am.isAuthenticated = false
	am.mu.Unlock()
	return nil
}

// LoginWithDeviceAuth authenticates using device authentication flow
// The context can be used to cancel the polling operation
func (am *AuthManager) LoginWithDeviceAuth(ctx context.Context, hostnameOverride *string) error {
	// Use temporary API client if hostname override is provided
	var loginClient *api.APIClient
	if hostnameOverride != nil && *hostnameOverride != "" {
		// Create temporary client with override hostname
		loginClient = api.NewAPIClient(*hostnameOverride, "")
	} else {
		// Use main API client
		loginClient = am.apiClient
	}

	// Get friendly device name (e.g., "Windows Laptop" or "Windows Desktop")
	deviceName := config.GetFriendlyDeviceName()

	// Start device auth
	startResponse, err := loginClient.StartDeviceAuth("Pangolin Windows Client", &deviceName)
	if err != nil {
		am.mu.Lock()
		if apiErr, ok := err.(*api.APIError); ok {
			msg := apiErr.Error()
			am.errorMessage = &msg
		} else {
			msg := err.Error()
			am.errorMessage = &msg
		}
		am.mu.Unlock()
		return err
	}

	// Store code and URL for UI display
	code := startResponse.Code
	loginURL := fmt.Sprintf("%s/auth/login/device", loginClient.CurrentBaseURL())

	am.mu.Lock()
	am.deviceAuthCode = &code
	am.deviceAuthLoginURL = &loginURL
	am.mu.Unlock()

	// Poll for verification
	expiresAt := time.Now().Add(time.Duration(startResponse.ExpiresInSeconds) * time.Second)
	verified := false
	var sessionToken *string

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for !verified && time.Now().Before(expiresAt) {
		select {
		case <-ctx.Done():
			// Context canceled, clear state and return
			am.mu.Lock()
			am.deviceAuthCode = nil
			am.deviceAuthLoginURL = nil
			am.mu.Unlock()
			return ctx.Err()
		case <-ticker.C:
			pollResponse, token, err := loginClient.PollDeviceAuth(code)
			if err != nil {
				// Continue polling on error
				continue
			}

			if pollResponse.Verified {
				verified = true
				if token != nil {
					sessionToken = token
				}
			} else if pollResponse.Message != nil {
				message := *pollResponse.Message
				if contains(message, "expired") || contains(message, "not found") {
					am.mu.Lock()
					am.deviceAuthCode = nil
					am.deviceAuthLoginURL = nil
					am.mu.Unlock()
					return &AuthError{Type: AuthErrorDeviceCodeExpired}
				}
			}
		}
	}

	if !verified {
		am.mu.Lock()
		am.deviceAuthCode = nil
		am.deviceAuthLoginURL = nil
		am.mu.Unlock()
		return &AuthError{Type: AuthErrorDeviceCodeExpired}
	}

	if sessionToken == nil {
		return &AuthError{Type: AuthErrorInvalidToken}
	}

	// If hostname override was provided, update main API client's base URL
	if hostnameOverride != nil && *hostnameOverride != "" {
		am.apiClient.UpdateBaseURL(*hostnameOverride)
	}

	// Save token
	am.apiClient.UpdateSessionToken(*sessionToken)

	// Get user info using main API client (now with updated base URL if override was provided)
	user, err := am.apiClient.GetUser()
	if err != nil {
		am.mu.Lock()
		msg := err.Error()
		am.errorMessage = &msg
		am.mu.Unlock()
		return err
	}

	// Clear device auth UI state after successful auth
	am.mu.Lock()
	am.deviceAuthCode = nil
	am.deviceAuthLoginURL = nil
	am.mu.Unlock()

	return am.handleSuccessfulAuth(user, loginClient.CurrentBaseURL(), *sessionToken)
}

// Select an organization if there isn't one already. This happens
// only for account login and when switching accounts.
// Returns the selected organization's ID.
// This does NOT get persisted to the account store; callers
// persist it to the account store themselves.
func (am *AuthManager) ensureOrgIsSelected() string {
	var selectedOrgID string

	am.mu.RLock()
	userID := am.currentUser.UserId
	am.mu.RUnlock()

	orgsResponse, err := am.apiClient.ListUserOrgs(userID)
	if err != nil {
		// Non-fatal error, continue without org
		logger.Error("Failed to load organizations: %v", err)
		am.mu.Lock()
		am.organizations = []api.Org{}
		am.mu.Unlock()
	} else {
		am.mu.Lock()
		am.organizations = orgsResponse.Orgs
		am.mu.Unlock()

		// Restore last selected org from config,
		// or auto-select a random one.
		if activeAccount, _ := am.accountManager.ActiveAccount(); activeAccount != nil {
			for _, org := range orgsResponse.Orgs {
				if org.Id == activeAccount.OrgID {
					am.mu.Lock()
					am.currentOrg = &org
					selectedOrgID = am.currentOrg.Id
					am.mu.Unlock()
					break
				}
			}
		} else if len(orgsResponse.Orgs) > 0 {
			am.mu.Lock()
			am.currentOrg = &orgsResponse.Orgs[0]
			selectedOrgID = am.currentOrg.Id
			am.mu.Unlock()
		}
	}

	return selectedOrgID
}

// handleSuccessfulAuth handles successful authentication
func (am *AuthManager) handleSuccessfulAuth(user *api.User, hostname string, token string) error {
	am.apiClient.UpdateBaseURL(hostname)
	am.apiClient.UpdateSessionToken(token)

	// Ensure userId is set (map from Id if needed)
	if user.UserId == "" {
		user.UserId = user.Id
	}

	am.UpdateCurrentUser(user)

	selectedOrgID := am.ensureOrgIsSelected()

	_ = am.secretManager.SaveSessionToken(user.UserId, token)

	// Ensure OLM credentials exist for this device-account combo
	if err := am.EnsureOlmCredentials(user.UserId); err != nil {
		logger.Error("Failed to ensure OLM credentials: %v", err)
		// Non-fatal, continue
	}

	var username string
	if user.Username != nil {
		username = *user.Username
	}

	var name string
	if user.Name != nil {
		name = *user.Name
	}

	newAccount := config.Account{
		UserID:   user.UserId,
		Email:    user.Email,
		OrgID:    selectedOrgID,
		Username: username,
		Name:     name,
		Hostname: am.apiClient.CurrentBaseURL(),
	}

	_ = am.accountManager.AddAccount(newAccount)
	_ = am.accountManager.SetActiveUser(user.UserId)

	am.mu.Lock()
	am.isAuthenticated = true
	am.mu.Unlock()
	return nil
}

// RefreshOrganizations refreshes the list of organizations
func (am *AuthManager) RefreshOrganizations() error {
	am.mu.RLock()
	authenticated := am.isAuthenticated
	userId := ""
	if am.currentUser != nil {
		userId = am.currentUser.UserId
	}
	am.mu.RUnlock()

	// Only refresh if authenticated and user ID is available
	if !authenticated || userId == "" {
		return nil
	}

	orgsResponse, err := am.apiClient.ListUserOrgs(userId)
	if err != nil {
		logger.Error("Failed to refresh organizations in background: %v", err)
		return err
	}

	am.mu.Lock()
	newOrgs := orgsResponse.Orgs
	currentOrgId := ""
	if am.currentOrg != nil {
		currentOrgId = am.currentOrg.Id
	}

	// Preserve current org selection if it still exists in the new list
	if currentOrgId != "" {
		found := false
		for _, org := range newOrgs {
			if org.Id == currentOrgId {
				am.currentOrg = &org
				found = true
				break
			}
		}

		if !found {
			// Current org no longer exists, clear selection
			am.currentOrg = nil
			if activeAccount, _ := am.accountManager.ActiveAccount(); activeAccount != nil {
				_ = am.accountManager.SetUserOrganization(activeAccount.UserID, "")
			}
		}
	}

	// Update organizations list
	am.organizations = newOrgs
	am.mu.Unlock()

	logger.Info("Organizations refreshed successfully: %d orgs", len(newOrgs))
	return nil
}

// RefreshFromMyDevice refreshes user info, organizations, and authentication status from MyDevice API
func (am *AuthManager) RefreshFromMyDevice(olmId string) error {
	am.mu.RLock()
	authenticated := am.isAuthenticated
	userId := ""
	if am.currentUser != nil {
		userId = am.currentUser.UserId
	}
	am.mu.RUnlock()

	// Only refresh if authenticated and user ID is available
	if !authenticated || userId == "" {
		return nil
	}

	// Get MyDevice data
	myDevice, err := am.apiClient.GetMyDevice(olmId)
	if err != nil {
		logger.Error("Failed to refresh from MyDevice: %v", err)
		// If we get an unauthorized error, user might be logged out
		if apiErr, ok := err.(*api.APIError); ok && apiErr.Status == 401 {
			logger.Info("Session expired, clearing authentication")
			am.mu.Lock()
			am.isAuthenticated = false
			am.mu.Unlock()
		}
		return err
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	// Update user info
	if myDevice.User.UserId != "" {
		// Update current user if it matches
		if am.currentUser != nil && am.currentUser.UserId == myDevice.User.UserId {
			am.currentUser.Email = myDevice.User.Email
			am.currentUser.Username = myDevice.User.Username
			am.currentUser.Name = myDevice.User.Name
		}
	}

	// Convert ResponseOrg to Org and update organizations
	newOrgs := make([]api.Org, 0, len(myDevice.Orgs))
	currentOrgId := ""
	if am.currentOrg != nil {
		currentOrgId = am.currentOrg.Id
	}

	for _, responseOrg := range myDevice.Orgs {
		org := api.Org{
			Id:   responseOrg.OrgId,
			Name: responseOrg.OrgName,
		}
		newOrgs = append(newOrgs, org)

		// Preserve current org selection if it still exists
		if currentOrgId != "" && org.Id == currentOrgId {
			am.currentOrg = &org
		}
	}

	// If current org no longer exists, clear selection
	if currentOrgId != "" && am.currentOrg != nil && am.currentOrg.Id != currentOrgId {
		am.currentOrg = nil
		_ = am.accountManager.SetUserOrganization(am.currentUser.UserId, "")
	}

	// Update organizations list
	am.organizations = newOrgs

	// Ensure authentication is still set (should be true if we got here)
	am.isAuthenticated = true

	logger.Info("Refreshed from MyDevice")
	return nil
}

// GetOlmId gets the OLM ID for the current user
func (am *AuthManager) GetOlmId() (string, bool) {
	am.mu.RLock()
	userId := ""
	if am.currentUser != nil {
		userId = am.currentUser.UserId
	}
	am.mu.RUnlock()

	if userId == "" {
		return "", false
	}

	return am.secretManager.GetOlmId(userId)
}

// CheckOrgAccess checks if the user has access to an organization
func (am *AuthManager) CheckOrgAccess(orgId string) (bool, error) {
	// First, try to fetch the org to check access
	_, err := am.apiClient.GetOrg(orgId)
	if err == nil {
		return true, nil
	}

	// Check if it's an unauthorized error
	apiErr, ok := err.(*api.APIError)
	if !ok {
		return false, err
	}

	if apiErr.Type == api.ErrorTypeHTTPError && (apiErr.Status == 401 || apiErr.Status == 403) {
		// Try to get org policy to understand why access was denied
		am.mu.RLock()
		userId := ""
		if am.currentUser != nil {
			userId = am.currentUser.UserId
		}
		am.mu.RUnlock()

		if userId != "" {
			policyResponse, err := am.apiClient.CheckOrgUserAccess(orgId, userId)
			if err == nil {
				// Check if access is denied and show error message
				if !policyResponse.Allowed {
					// Get hostname for the resolution URL
					var hostname string
					if activeAccount, _ := am.accountManager.ActiveAccount(); activeAccount != nil {
						hostname = activeAccount.Hostname
					} else {
						// Ideally this should never happen, but use a safe fallback
						// just in case.
						hostname = config.DefaultHostname
					}

					resolutionURL := fmt.Sprintf("%s/%s", hostname, orgId)

					// Always use fallback message format
					fallbackMsg := "Access denied due to organization policy violations."
					if policyResponse.Error != nil && *policyResponse.Error != "" {
						fallbackMsg = fmt.Sprintf("Access denied: %s", *policyResponse.Error)
					}
					fallbackMsg += fmt.Sprintf("\n\nSee more and resolve the issues by visiting: %s", resolutionURL)
					return false, errors.New(fallbackMsg)
				}

				// Return false with a descriptive error (shouldn't reach here if Allowed is true)
				return false, fmt.Errorf("org policy preventing access to this org")
			}
		}

		// Return false with generic unauthorized message
		return false, errors.New("unauthorized access to this org. Contact your admin")
	}

	// Some other error occurred
	return false, err
}

// SelectOrganization selects an organization
func (am *AuthManager) SelectOrganization(org *api.Org) error {
	// First check org access
	hasAccess, err := am.CheckOrgAccess(org.Id)
	if err != nil || !hasAccess {
		return err
	}

	// If access is granted, proceed with selecting the org
	am.mu.Lock()
	am.currentOrg = org
	am.mu.Unlock()

	// Save selected org to accounts store
	am.mu.RLock()
	userID := am.currentUser.UserId
	am.mu.RUnlock()

	if err := am.accountManager.SetUserOrganization(userID, org.Id); err != nil {
		logger.Warn("failed to persist selected account to store: %v", err)
	}

	return nil
}

// EnsureOlmCredentials ensures OLM credentials exist for the user
func (am *AuthManager) EnsureOlmCredentials(userId string) error {
	// Check if OLM credentials already exist locally
	if am.secretManager.HasOlmCredentials(userId) {
		// Verify OLM exists on server by getting the OLM directly
		olmIdString, found := am.secretManager.GetOlmId(userId)
		if found {
			olm, err := am.apiClient.GetUserOlm(userId, olmIdString)
			if err == nil && olm != nil {
				// Verify the olmId matches
				if olm.OlmId == olmIdString {
					logger.Info("OLM credentials verified successfully")
					return nil
				} else {
					logger.Error("OLM ID mismatch - olm olmId: %s, stored olmId: %s", olm.OlmId, olmIdString)
					// Clear invalid credentials
					am.secretManager.DeleteOlmCredentials(userId)
				}
			} else {
				// If getting OLM fails, the OLM might not exist
				logger.Error("Failed to verify OLM credentials: %v", err)
				// Clear invalid credentials so we can try to create new ones
				am.secretManager.DeleteOlmCredentials(userId)
			}
		}
	}

	// If credentials don't exist or were cleared, create new ones
	if !am.secretManager.HasOlmCredentials(userId) {
		// Get friendly device name (e.g., "Windows Laptop" or "Windows Desktop")
		deviceName := config.GetFriendlyDeviceName()

		olmResponse, err := am.apiClient.CreateOlm(userId, deviceName)
		if err != nil {
			return fmt.Errorf("failed to create OLM: %w", err)
		}

		// Save OLM credentials
		saved := am.secretManager.SaveOlmCredentials(userId, olmResponse.OlmId, olmResponse.Secret)
		if !saved {
			return errors.New("failed to save OLM credentials")
		}
	}

	return nil
}

func (am *AuthManager) SwitchAccount(userID string) error {
	am.mu.Lock()
	am.isAuthenticated = false
	am.mu.Unlock()

	defer func() {
		am.mu.Lock()
		am.isAuthenticated = true
		am.mu.Unlock()
	}()

	accountToSwitchTo, exists := am.accountManager.Accounts[userID]
	if !exists {
		return errors.New("account does not exist")
	}

	token, found := am.secretManager.GetSessionToken(accountToSwitchTo.UserID)
	if found && token != "" {
		am.apiClient.UpdateBaseURL(accountToSwitchTo.Hostname)
		am.apiClient.UpdateSessionToken(token)

		// Always fetch the latest user info to verify the user exists and update stored info
		var err error
		user, err := am.apiClient.GetUser()
		if err != nil {
			// This should never happen, but if it does, silently
			// fail and switch to an unauthenticated state to prevent
			// any more unreachable situations from happening.
			am.mu.Lock()
			am.isAuthenticated = false
			am.mu.Unlock()
			return nil
		}

		am.UpdateCurrentUser(user)
	} else {
		return errors.New("session token does not exist for this user")
	}

	selectedOrgID := am.ensureOrgIsSelected()

	err := am.accountManager.SetUserOrganization(userID, selectedOrgID)
	if err != nil {
		logger.Warn("failed to set user's org ID in accounts store: %v", err)
	}

	err = am.accountManager.SetActiveUser(userID)
	if err != nil {
		logger.Warn("failed to set active user in accounts store: %v", err)
	}

	return nil
}

// Logout logs out the current user
func (am *AuthManager) Logout() error {
	// Try to call logout endpoint (ignore errors)
	_ = am.apiClient.Logout()

	userID := am.accountManager.ActiveUserID

	// Clear local data
	am.apiClient.UpdateSessionToken("")

	am.mu.Lock()
	am.isAuthenticated = false
	am.currentOrg = nil
	am.organizations = []api.Org{}
	am.errorMessage = nil
	am.deviceAuthCode = nil
	am.deviceAuthLoginURL = nil
	am.mu.Unlock()

	_ = am.secretManager.DeleteSessionToken(userID)
	_ = am.secretManager.DeleteOlmCredentials(userID)

	_ = am.accountManager.RemoveAccount(userID)

	return nil
}

// Getters for state (thread-safe)

func (am *AuthManager) IsAuthenticated() bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.isAuthenticated
}

func (am *AuthManager) CurrentUser() *api.User {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.currentUser
}

func (am *AuthManager) CurrentOrg() *api.Org {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.currentOrg
}

func (am *AuthManager) Organizations() []api.Org {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.organizations
}

func (am *AuthManager) IsInitializing() bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.isInitializing
}

func (am *AuthManager) ErrorMessage() *string {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.errorMessage
}

func (am *AuthManager) DeviceAuthCode() *string {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.deviceAuthCode
}

func (am *AuthManager) DeviceAuthLoginURL() *string {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.deviceAuthLoginURL
}

// ClearDeviceAuth clears the device authentication code and URL
func (am *AuthManager) ClearDeviceAuth() {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.deviceAuthCode = nil
	am.deviceAuthLoginURL = nil
}

// UpdateCurrentUser updates the current user (used for session verification)
func (am *AuthManager) UpdateCurrentUser(user *api.User) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.currentUser = user
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			strings.Contains(s, substr))))
}
