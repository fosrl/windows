//go:build windows

package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/managers/secretstore"
	"github.com/zalando/go-keyring"
)

const (
	legacyKeyringService = "Pangolin: pangolin-windows"
	migrationFlagName    = "secrets-migrated.flag"
)

func migrationFlagPath() (string, error) {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	if appData == "" {
		return "", fmt.Errorf("LOCALAPPDATA is not set")
	}
	return filepath.Join(appData, config.AppName, migrationFlagName), nil
}

func migrationCompleted() bool {
	path, err := migrationFlagPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func markMigrationCompleted() error {
	path, err := migrationFlagPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("1"), 0o600)
}

func accountsFilePath() string {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	return filepath.Join(appData, config.AppName, config.AccountsFileName)
}

func migrateFromCredentialManager() error {
	if migrationCompleted() {
		logger.Debug("Secrets migration: already completed (flag present)")
		return nil
	}
	if ipc == nil || !ipc.Ready() {
		return fmt.Errorf("manager IPC is not connected")
	}

	logger.Info("Secrets migration: reading accounts file")
	data, err := os.ReadFile(accountsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return markMigrationCompleted()
		}
		return err
	}

	var store struct {
		Accounts map[string]config.Account `json:"accounts"`
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return err
	}

	logger.Info("Secrets migration: migrating %d account(s) from Credential Manager", len(store.Accounts))
	for userID := range store.Accounts {
		logger.Info("Secrets migration: user %s starting", userID)
		if err := migrateUserFromKeyring(userID); err != nil {
			logger.Warn("Failed to migrate secrets for user %s from Credential Manager: %v", userID, err)
		} else {
			logger.Info("Secrets migration: user %s finished", userID)
		}
	}

	logger.Info("Secrets migration: writing completion flag")
	return markMigrationCompleted()
}

func migrateUserFromKeyring(userID string) error {
	sessionKey := fmt.Sprintf("session-token-%s", userID)
	olmIDKey := fmt.Sprintf("olm-id-%s", userID)
	olmSecretKey := fmt.Sprintf("olm-secret-%s", userID)

	logger.Debug("Secrets migration: keyring.Get session token (userId=%s)", userID)
	sessionToken, sessionErr := keyring.Get(legacyKeyringService, sessionKey)
	logger.Debug("Secrets migration: keyring.Get olm id (userId=%s)", userID)
	olmID, olmIDErr := keyring.Get(legacyKeyringService, olmIDKey)
	logger.Debug("Secrets migration: keyring.Get olm secret (userId=%s)", userID)
	olmSecret, olmSecretErr := keyring.Get(legacyKeyringService, olmSecretKey)

	if sessionErr != nil && sessionErr != keyring.ErrNotFound {
		return sessionErr
	}
	if olmIDErr != nil && olmIDErr != keyring.ErrNotFound {
		return olmIDErr
	}
	if olmSecretErr != nil && olmSecretErr != keyring.ErrNotFound {
		return olmSecretErr
	}

	hasSession := sessionErr == nil && sessionToken != ""
	hasOLM := olmIDErr == nil && olmSecretErr == nil && olmID != "" && olmSecret != ""
	if !hasSession && !hasOLM {
		return nil
	}

	update := secretstore.SecretsUpdate{}
	if hasSession {
		update.Secrets.SessionToken = sessionToken
		update.SetSessionToken = true
	}
	if hasOLM {
		update.Secrets.OlmId = olmID
		update.Secrets.OlmSecret = olmSecret
		update.SetOlmId = true
		update.SetOlmSecret = true
	}
	logger.Debug("Secrets migration: saving to DPAPI via IPC (userId=%s)", userID)
	if ipc == nil || !ipc.Ready() {
		return fmt.Errorf("manager IPC is not connected")
	}
	if err := ipc.SaveUserSecrets(userID, update); err != nil {
		return fmt.Errorf("failed to save migrated secrets for user %s: %w", userID, err)
	}

	if hasSession {
		_ = keyring.Delete(legacyKeyringService, sessionKey)
	}
	if hasOLM {
		_ = keyring.Delete(legacyKeyringService, olmIDKey)
		_ = keyring.Delete(legacyKeyringService, olmSecretKey)
	}
	return nil
}
