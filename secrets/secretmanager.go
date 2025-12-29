//go:build windows

package secrets

import (
	"fmt"

	"github.com/fosrl/newt/logger"
	"github.com/zalando/go-keyring"
)

// SecretManager is responsible for storing and retrieving secrets using the Windows Credential Manager
type SecretManager struct {
	service string
}

// NewSecretManager creates a new SecretManager instance
func NewSecretManager() *SecretManager {
	return &SecretManager{
		service: "Pangolin: pangolin-windows",
	}
}

// SaveSecret saves a secret value with the given key
// Returns true if successful, false otherwise
func (sm *SecretManager) saveSecret(key, value string) bool {
	// Delete existing item if it exists (go-keyring doesn't have an update method)
	_ = sm.deleteSecret(key)

	err := keyring.Set(sm.service, key, value)
	if err != nil {
		logger.Error("Failed to save secret for key %s: %v", key, err)
	}
	return err == nil
}

// GetSecret retrieves a secret value for the given key
// Returns the value if found, or an empty string and false if not found
func (sm *SecretManager) getSecret(key string) (string, bool) {
	value, err := keyring.Get(sm.service, key)
	if err != nil {
		return "", false
	}
	return value, true
}

// DeleteSecret deletes a secret with the given key
// Returns true if successful or if the item was not found, false on error
func (sm *SecretManager) deleteSecret(key string) bool {
	err := keyring.Delete(sm.service, key)
	// Consider both success and "not found" as success
	return err == nil || err == keyring.ErrNotFound
}

// GetSessionToken retrieves the session token for the given user ID
func (sm *SecretManager) GetSessionToken(userId string) (string, bool) {
	return sm.getSecret(sm.sessionTokenKey(userId))
}

// GetOlmId retrieves the OLM ID for the given user ID
func (sm *SecretManager) GetOlmId(userId string) (string, bool) {
	return sm.getSecret(sm.olmIdKey(userId))
}

// GetOlmSecret retrieves the OLM secret for the given user ID
func (sm *SecretManager) GetOlmSecret(userId string) (string, bool) {
	return sm.getSecret(sm.olmSecretKey(userId))
}

// SaveSessionToken saves a session token for the given user ID
func (sm *SecretManager) SaveSessionToken(userId string, token string) bool {
	return sm.saveSecret(sm.sessionTokenKey(userId), token)
}

// SaveOlmCredentials saves both OLM ID and secret for the given user ID
// Returns true if both were saved successfully
func (sm *SecretManager) SaveOlmCredentials(userId, olmId, secret string) bool {
	idSaved := sm.saveSecret(sm.olmIdKey(userId), olmId)
	secretSaved := sm.saveSecret(sm.olmSecretKey(userId), secret)
	return idSaved && secretSaved
}

// HasOlmCredentials checks if OLM credentials exist for the given user ID
func (sm *SecretManager) HasOlmCredentials(userId string) bool {
	_, hasId := sm.GetOlmId(userId)
	_, hasSecret := sm.GetOlmSecret(userId)
	return hasId && hasSecret
}

// DeleteSessionToken removes a session token for a given user.
func (sm *SecretManager) DeleteSessionToken(userId string) bool {
	return sm.deleteSecret(sm.sessionTokenKey(userId))
}

// DeleteOlmCredentials deletes both OLM ID and secret for the given user ID
// Returns true if both were deleted successfully (or didn't exist)
func (sm *SecretManager) DeleteOlmCredentials(userId string) bool {
	idDeleted := sm.deleteSecret(sm.olmIdKey(userId))
	secretDeleted := sm.deleteSecret(sm.olmSecretKey(userId))
	return idDeleted && secretDeleted
}

// sessionTokenKey returns the key for storing session tokens
func (sm *SecretManager) sessionTokenKey(userId string) string {
	return fmt.Sprintf("session-token-%s", userId)
}

// olmIdKey returns the key for storing OLM ID
func (sm *SecretManager) olmIdKey(userId string) string {
	return fmt.Sprintf("olm-id-%s", userId)
}

// olmSecretKey returns the key for storing OLM secret
func (sm *SecretManager) olmSecretKey(userId string) string {
	return fmt.Sprintf("olm-secret-%s", userId)
}
