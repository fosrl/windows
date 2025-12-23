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
	} else {
		return errors.New("account does not exist")
	}

	return m.saveLocked()
}
