//go:build windows

package secrets

import (
	"sync"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/managers/secretstore"
)

var credentialMigrationOnce sync.Once

// SecretManager stores and retrieves secrets via the manager service (DPAPI files as SYSTEM).
type SecretManager struct{}

// NewSecretManager creates a new SecretManager instance.
func NewSecretManager() *SecretManager {
	return &SecretManager{}
}

func (sm *SecretManager) ensureReady() bool {
	if ipc == nil || !ipc.Ready() {
		logger.Error("Secret manager requires an active manager IPC connection")
		return false
	}
	credentialMigrationOnce.Do(func() {
		logger.Info("Secrets: Credential Manager migration starting")
		if err := migrateFromCredentialManager(); err != nil {
			logger.Warn("Secrets: Credential Manager migration finished with error: %v", err)
		} else {
			logger.Info("Secrets: Credential Manager migration finished")
		}
	})
	return true
}

func (sm *SecretManager) load(userID string) (secretstore.UserSecrets, bool) {
	if !sm.ensureReady() {
		return secretstore.UserSecrets{}, false
	}
	logger.Debug("Secrets: IPC GetUserSecrets() starting (userId=%s)", userID)
	secrets, err := ipc.GetUserSecrets(userID)
	if err != nil {
		logger.Error("Failed to load secrets for user %s: %v", userID, err)
		return secretstore.UserSecrets{}, false
	}
	return secrets, true
}

func (sm *SecretManager) saveUpdate(userID string, update secretstore.SecretsUpdate) bool {
	if !sm.ensureReady() {
		return false
	}
	logger.Debug("Secrets: IPC SaveUserSecrets() starting (userId=%s)", userID)
	if err := ipc.SaveUserSecrets(userID, update); err != nil {
		logger.Error("Failed to save secrets for user %s: %v", userID, err)
		return false
	}
	return true
}

func (sm *SecretManager) deleteFlags(userID string, flags secretstore.DeleteSecretsFlags) bool {
	if !sm.ensureReady() {
		return false
	}
	if err := ipc.DeleteUserSecrets(userID, flags); err != nil {
		logger.Error("Failed to delete secrets for user %s: %v", userID, err)
		return false
	}
	return true
}

// GetSessionToken retrieves the session token for the given user ID.
func (sm *SecretManager) GetSessionToken(userId string) (string, bool) {
	secrets, ok := sm.load(userId)
	if !ok || secrets.SessionToken == "" {
		return "", false
	}
	return secrets.SessionToken, true
}

// GetOlmId retrieves the OLM ID for the given user ID.
func (sm *SecretManager) GetOlmId(userId string) (string, bool) {
	secrets, ok := sm.load(userId)
	if !ok || secrets.OlmId == "" {
		return "", false
	}
	return secrets.OlmId, true
}

// GetOlmSecret retrieves the OLM secret for the given user ID.
func (sm *SecretManager) GetOlmSecret(userId string) (string, bool) {
	secrets, ok := sm.load(userId)
	if !ok || secrets.OlmSecret == "" {
		return "", false
	}
	return secrets.OlmSecret, true
}

// SaveSessionToken saves a session token for the given user ID.
func (sm *SecretManager) SaveSessionToken(userId string, token string) bool {
	return sm.saveUpdate(userId, secretstore.SecretsUpdate{
		Secrets:         secretstore.UserSecrets{SessionToken: token},
		SetSessionToken: true,
	})
}

// SaveOlmCredentials saves both OLM ID and secret for the given user ID.
func (sm *SecretManager) SaveOlmCredentials(userId, olmId, secret string) bool {
	return sm.saveUpdate(userId, secretstore.SecretsUpdate{
		Secrets: secretstore.UserSecrets{
			OlmId:     olmId,
			OlmSecret: secret,
		},
		SetOlmId:     true,
		SetOlmSecret: true,
	})
}

// HasOlmCredentials checks if OLM credentials exist for the given user ID.
func (sm *SecretManager) HasOlmCredentials(userId string) bool {
	secrets, ok := sm.load(userId)
	if !ok {
		return false
	}
	return secrets.OlmId != "" && secrets.OlmSecret != ""
}

// DeleteSessionToken removes a session token for a given user.
func (sm *SecretManager) DeleteSessionToken(userId string) bool {
	return sm.deleteFlags(userId, secretstore.DeleteSecretsFlags{SessionToken: true})
}

// DeleteOlmCredentials deletes both OLM ID and secret for the given user ID.
func (sm *SecretManager) DeleteOlmCredentials(userId string) bool {
	return sm.deleteFlags(userId, secretstore.DeleteSecretsFlags{OlmCredentials: true})
}
