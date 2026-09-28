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

	// The exit node (a gateway-mode site resource) selected from the tray
	// menu, re-applied on the next connect. It can differ per account, so
	// it's stored here rather than on the root config, and it belongs to the
	// account's currently selected org (OrgID above). Only the resource ID
	// is stored (not the niceId, which can be renamed); its sites are looked
	// up from the server on every connect so they can't go stale.
	ExitNodeResourceID int `json:"exitNodeResourceId,omitempty"`
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

// GetExitNode returns the resource ID of userID's selected exit node, or 0 if
// none is selected or the account doesn't exist.
func (m *AccountManager) GetExitNode(userID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	account, ok := m.Accounts[userID]
	if !ok {
		return 0
	}
	return account.ExitNodeResourceID
}

// SetExitNode records the selected exit node (a gateway resource) for userID;
// resourceID 0 clears it.
func (m *AccountManager) SetExitNode(userID string, resourceID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	account, ok := m.Accounts[userID]
	if !ok {
		return errors.New("account does not exist")
	}

	account.ExitNodeResourceID = resourceID
	m.Accounts[userID] = account
	return m.saveLocked()
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
