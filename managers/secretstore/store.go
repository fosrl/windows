//go:build windows

package secretstore

import (
	"encoding/json"
	"errors"
	"os"
)

// Store persists Pangolin user secrets under ProgramData, encrypted with DPAPI as SYSTEM.
type Store struct{}

func NewStore() *Store {
	return &Store{}
}

// Load returns secrets for the given Windows user SID and Pangolin user id.
// Missing files yield a zero-valued UserSecrets and no error.
func (s *Store) Load(windowsSID, userID string) (UserSecrets, error) {
	path, err := userSecretsPath(windowsSID, userID)
	if err != nil {
		return UserSecrets{}, err
	}
	ciphertext, err := readSecretsFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return UserSecrets{}, nil
		}
		return UserSecrets{}, err
	}
	plaintext, err := decrypt(ciphertext, windowsSID, userID)
	if err != nil {
		return UserSecrets{}, err
	}
	var secrets UserSecrets
	if err := json.Unmarshal(plaintext, &secrets); err != nil {
		return UserSecrets{}, err
	}
	return secrets, nil
}

// Save merges updates into stored secrets and writes atomically.
func (s *Store) Save(windowsSID, userID string, update SecretsUpdate) error {
	current, err := s.Load(windowsSID, userID)
	if err != nil {
		return err
	}
	if update.SetSessionToken {
		current.SessionToken = update.Secrets.SessionToken
	}
	if update.SetOlmId {
		current.OlmId = update.Secrets.OlmId
	}
	if update.SetOlmSecret {
		current.OlmSecret = update.Secrets.OlmSecret
	}
	return s.write(windowsSID, userID, current)
}

// Delete clears selected fields; removes the file if no secrets remain.
func (s *Store) Delete(windowsSID, userID string, flags DeleteSecretsFlags) error {
	current, err := s.Load(windowsSID, userID)
	if err != nil {
		return err
	}
	if flags.SessionToken {
		current.SessionToken = ""
	}
	if flags.OlmCredentials {
		current.OlmId = ""
		current.OlmSecret = ""
	}
	if current.SessionToken == "" && current.OlmId == "" && current.OlmSecret == "" {
		path, err := userSecretsPath(windowsSID, userID)
		if err != nil {
			return err
		}
		return removeSecretsFile(path)
	}
	return s.write(windowsSID, userID, current)
}

func (s *Store) write(windowsSID, userID string, secrets UserSecrets) error {
	path, err := userSecretsPath(windowsSID, userID)
	if err != nil {
		return err
	}
	plaintext, err := json.Marshal(secrets)
	if err != nil {
		return err
	}
	if len(plaintext) == 0 {
		return errors.New("refusing to write empty secrets payload")
	}
	ciphertext, err := encrypt(plaintext, windowsSID, userID)
	if err != nil {
		return err
	}
	if err := writeLockedDownFile(path, ciphertext); err != nil {
		return err
	}
	return nil
}
