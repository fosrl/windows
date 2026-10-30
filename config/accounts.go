//go:build windows

package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/fosrl/newt/logger"
)

const (
	AccountsFileName = "accounts.json"
)

type AccountManager struct {
	mu sync.RWMutex

	path string

	ActiveUserID string             `json:"activeUserId"`
	Accounts     map[string]Account `json:"accounts"`
}

type Account struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	OrgID    string `json:"orgId"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`

	OrgSettings map[string]OrgSettings `json:"orgSettings,omitempty"`
}

type OrgSettings struct {
	ExitNodeResourceID int `json:"exitNodeResourceId,omitempty"`

	// The settings the org's server manages for this user, as last fetched
	// from it. Refreshed on every connect and whenever Preferences opens; only
	// kept here so they still apply if the server can't be reached.
	ServerSettings *ClientSettings `json:"serverSettings,omitempty"`
}

func (s OrgSettings) IsEmpty() bool {
	return s.ExitNodeResourceID == 0 && s.ServerSettings == nil
}

func NewAccountManager() *AccountManager {
	// Get Local AppData directory (equivalent to Application Support on macOS)
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		// Fallback to APPDATA if LOCALAPPDATA is not set
		appData = os.Getenv("APPDATA")
	}

	pangolinDir := filepath.Join(appData, AppName)
	accountsPath := filepath.Join(pangolinDir, AccountsFileName)

	mgr := &AccountManager{
		path:     accountsPath,
		Accounts: make(map[string]Account),
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(pangolinDir, 0o755); err != nil {
		logger.Error("Failed to create config directory: %v", err)
	}

	data, err := os.ReadFile(accountsPath)
	if err != nil {
		mgr.Save()

		logger.Error("failed to read accounts file: %v", err)
		return mgr
	}

	if err := json.Unmarshal(data, mgr); err != nil {
		logger.Error("failed to parse accounts file: %v", err)
		return mgr
	}

	if mgr.Accounts == nil {
		mgr.Accounts = make(map[string]Account)
	}

	return mgr
}

func (m *AccountManager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveLocked()
}

func (m *AccountManager) saveLocked() error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(m.path, data, 0o600); err != nil {
		return err
	}

	return nil
}

func (m *AccountManager) AddAccount(account Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.Accounts[account.UserID]; ok && len(existing.OrgSettings) > 0 {
		account.OrgSettings = existing.OrgSettings
	}
	m.Accounts[account.UserID] = account
	return m.saveLocked()
}

func (m *AccountManager) RemoveAccount(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.Accounts, userID)

	if m.ActiveUserID == userID {
		m.ActiveUserID = ""
	}

	return m.saveLocked()
}

func (m *AccountManager) ActiveAccount() (*Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.ActiveUserID == "" {
		return nil, errors.New("no active account")
	}

	account, ok := m.Accounts[m.ActiveUserID]
	if !ok {
		return nil, errors.New("active account not present in list")
	}

	return &account, nil
}

func (m *AccountManager) SetActiveUser(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.Accounts[userID]; !ok {
		return errors.New("account does not exist")
	}

	m.ActiveUserID = userID
	return m.saveLocked()
}

func (m *AccountManager) SetUserOrganization(userID string, orgID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if account, ok := m.Accounts[userID]; ok {
		account.OrgID = orgID
		m.Accounts[userID] = account // Put the modified account back in the map
	} else {
		return errors.New("account does not exist")
	}

	return m.saveLocked()
}

// GetOrgSettings returns the settings for orgID under userID, or empty defaults
// if none are stored or the account doesn't exist.
func (m *AccountManager) GetOrgSettings(userID, orgID string) OrgSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.Accounts[userID].OrgSettings[orgID]
}

// UpdateOrgSettings updates the settings for orgID under userID. Empty settings
// remove that org's entry.
func (m *AccountManager) UpdateOrgSettings(userID, orgID string, update func(*OrgSettings)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	account, ok := m.Accounts[userID]
	if !ok {
		return errors.New("account does not exist")
	}

	settings := make(map[string]OrgSettings, len(account.OrgSettings)+1)
	for id, s := range account.OrgSettings {
		settings[id] = s
	}
	org := settings[orgID]
	update(&org)
	if org.IsEmpty() {
		delete(settings, orgID)
	} else {
		settings[orgID] = org
	}
	if len(settings) == 0 {
		settings = nil
	}
	account.OrgSettings = settings
	m.Accounts[userID] = account
	return m.saveLocked()
}

// GetServerSettings returns the settings last fetched from the server for orgID
// under userID, or nil if it manages none.
func (m *AccountManager) GetServerSettings(userID, orgID string) *ClientSettings {
	return m.GetOrgSettings(userID, orgID).ServerSettings
}

// SetServerSettings records the settings fetched from the server for orgID
// under userID.
func (m *AccountManager) SetServerSettings(userID, orgID string, settings *ClientSettings) error {
	return m.UpdateOrgSettings(userID, orgID, func(s *OrgSettings) {
		s.ServerSettings = settings
	})
}

// GetExitNode returns the resource ID of the exit node selected for orgID under
// userID, or 0 if none is selected or the account doesn't exist.
func (m *AccountManager) GetExitNode(userID, orgID string) int {
	return m.GetOrgSettings(userID, orgID).ExitNodeResourceID
}

// SetExitNode records the selected exit node (a gateway resource) for orgID
// under userID; resourceID 0 clears it.
func (m *AccountManager) SetExitNode(userID, orgID string, resourceID int) error {
	return m.UpdateOrgSettings(userID, orgID, func(s *OrgSettings) {
		s.ExitNodeResourceID = resourceID
	})
}

func (m *AccountManager) UpdateAccountUserInfo(userID, username, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if account, ok := m.Accounts[userID]; ok {
		account.Username = username
		account.Name = name
		m.Accounts[userID] = account // Put the modified account back in the map
	} else {
		return errors.New("account does not exist")
	}

	return m.saveLocked()
}
